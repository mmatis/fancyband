// Package ui holds everything in fancyband-tui that touches Limoni.
//
// Keeping every Limoni import in this one package means a Limoni upgrade
// changes only this package. See the risks section of
// docs/scenario-2-plan.md.
package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
	"fancyband/internal/session"
	"fancyband/internal/settings"
	"fancyband/internal/tiles"
)

// flashTime is how long the screen flashes for the bell.
const flashTime = 120 * time.Millisecond

// quitTimeout is how long Ctrl+C waits for the engine to save before the
// client gives up and closes the connection, which also makes it save.
const quitTimeout = 10 * time.Second

// noticeTime is how long a notice such as "Game saved." shows.
const noticeTime = 3 * time.Second

// menuGuard is how long after the game stops acting by itself, at the
// end of a run or a rest, Escape still goes to the game rather than
// opening the game menu. An Escape meant to stop the run could otherwise
// open the menu by surprise.
const menuGuard = 500 * time.Millisecond

// busyTime is how long the game must go without a key before a WAIT
// counts as the end of something the game did by itself.
const busyTime = 300 * time.Millisecond

// StartFunc starts an engine with the given engine and net module
// options, which opens the given terms and is told the client's
// capabilities.
type StartFunc func(args, module []string, terms []proto.TermSize, caps []string) (*session.Session, error)

// caps are the capabilities the client claims in HELLO. "events" feeds
// the gauges and the menus; "waits" says when Escape may open the game
// menu; "screen:death" hands over the death screen; "tiles:N" asks for a
// tile set.
func (m *Model) caps() []string {
	caps := []string{"events", "waits", "screen:death"}
	if m.cfg.Tiles > 0 {
		caps = append(caps, fmt.Sprintf("tiles:%d", m.cfg.Tiles))
	}
	return caps
}

// Config sets up a Model.
type Config struct {
	// Start starts the engine.
	Start StartFunc
	// Width and Height are the screen size when the client starts.
	Width, Height int
	// Subwindows shows the message log and monster list beside or below
	// the main term when there is room.
	Subwindows bool
	// Log receives diagnostics. Nil discards them.
	Log *log.Logger
	// Tiles is the tile set to draw the map with, by its number in the
	// engine's lib/tiles/list.txt, or 0 for text.
	Tiles int
	// Animate shifts lava between red and yellow.
	Animate bool
	// EngineArgs are the engine options the player gave. A character
	// switch replaces -n and -u in them.
	EngineArgs []string
	// SettingsPath is fancyband-tui.json. Empty keeps settings in memory.
	SettingsPath string
}

// Model is the client's Limoni model. It owns the session with the
// engine and a copy of every term.
type Model struct {
	cfg    Config
	log    *log.Logger
	sess   *session.Session
	stream chan session.Batch
	terms  grid.Set
	layout Layout
	// sizes holds the size last asked for each open term.
	sizes map[int][2]int

	gauges gauges
	// sheet holds the engine's tile set once TILESET arrives.
	sheet *tiles.Sheet

	// tick counts animation steps; it drives the lava wave.
	tick int

	flashing  bool
	flashSeq  int
	quitting  bool
	quitTimed bool
	byeReason string
	err       error

	// gen counts engines; messages from an older one are dropped.
	gen int
	// launch is how the running engine was started, and next, when set,
	// the engine to start once this one exits.
	launch Launch
	next   *Launch

	settings settings.Settings
	// stack holds the open native screens, the top one last.
	stack []screen

	// Commands sent with command(), by id, and the next id.
	pending map[int]func(proto.EventData)
	nextID  int

	// saves is the last SAVEFILES answer, and saveName the running
	// character's savefile.
	saves    []proto.SaveData
	saveName string

	// waiting is set from a WAIT until the next key: the game waits for
	// a command with nothing open. waitAt is when the WAIT came, and
	// late is set when it ended a stretch with no keys, such as a run.
	waiting bool
	waitAt  time.Time
	late    bool
	keyAt   time.Time
	// played is set after the first WAIT in the game, per engine.
	played bool

	borgOn bool
	// borgStopping is set from asking the borg to stop until the engine
	// says it has.
	borgStopping bool
	borgMissing  bool
	// deathID is the death screen's request.
	deathID int

	notice      string
	noticeSeq   int
	noticeTimed int
}

// New returns a model that starts the engine when it starts.
func New(cfg Config) *Model {
	m := &Model{cfg: cfg, log: cfg.Log, sizes: map[int][2]int{}, pending: map[int]func(proto.EventData){}}
	if m.log == nil {
		m.log = log.New(io.Discard, "", 0)
	}
	if cfg.SettingsPath != "" {
		s, err := settings.Load(cfg.SettingsPath)
		if err != nil {
			m.log.Printf("cannot read %s: %v", cfg.SettingsPath, err)
		}
		m.settings = s
	}
	m.layout = Compute(cfg.Width, cfg.Height, cfg.Subwindows)
	return m
}

// Err returns why the session ended, or nil if the engine said BYE.
func (m *Model) Err() error { return m.err }

// ByeReason returns the reason in the engine's BYE, if one came.
func (m *Model) ByeReason() string { return m.byeReason }

// Session returns the engine session, or nil before it starts.
func (m *Model) Session() *session.Session { return m.sess }

// Messages from commands to Update.
type (
	startedMsg struct {
		sess *session.Session
		err  error
		gen  int
	}
	batchMsg struct {
		session.Batch
		gen int
	}
	flashOff   struct{ seq int }
	noticeOff  struct{ seq int }
	quitExpiry struct{ gen int }
	animTick   struct{}
)

// Init starts the engine.
func (m *Model) Init() []engine.Cmd {
	cmds := []engine.Cmd{m.startEngine(Launch{})}
	if m.cfg.Animate {
		cmds = append(cmds, nextTick())
	}
	return cmds
}

// startEngine returns a command that starts an engine on l. Any engine
// still running is waited for first, so it can finish saving.
func (m *Model) startEngine(l Launch) engine.Cmd {
	old := m.sess
	m.gen++
	m.launch, m.next = l, nil
	m.sess, m.stream = nil, nil
	m.terms = grid.Set{}
	m.sizes = map[int][2]int{}
	m.sheet = nil
	m.gauges = gauges{}
	m.stack = nil
	m.pending = map[int]func(proto.EventData){}
	m.saves, m.saveName = nil, ""
	m.waiting, m.played, m.borgOn, m.borgStopping = false, false, false, false
	m.quitting, m.quitTimed = false, false

	terms := m.initialTerms()
	for _, t := range terms {
		m.sizes[int(t.Index)] = [2]int{int(t.Width), int(t.Height)}
	}
	args, module := l.Args(m.cfg.EngineArgs)
	start, caps, gen, logger := m.cfg.Start, m.caps(), m.gen, m.log
	if !l.first() {
		logger.Printf("starting a new engine with %q %q", args, module)
	}
	return func(context.Context) engine.Msg {
		if old != nil {
			old.Close()
			waitOld(old, logger)
		}
		s, err := start(args, module, terms, caps)
		return startedMsg{s, err, gen}
	}
}

// waitOld waits for an engine that is exiting, and stops it if it hangs.
func waitOld(s *session.Session, logger *log.Logger) {
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		logger.Printf("old engine exited: %v", err)
	case <-time.After(quitTimeout):
		logger.Print("old engine did not exit; stopping it")
		s.Kill()
		<-done
	}
}

// initialTerms lists the terms to ask for in HELLO. Subwindows are opened
// even when the screen has no room for them yet, so a later resize can
// show them.
func (m *Model) initialTerms() []proto.TermSize {
	main := Box{W: MinWidth, H: MinHeight}
	if !m.layout.TooSmall {
		main = m.layout.Main
	}
	terms := []proto.TermSize{{Index: 0, Width: uint16(main.W), Height: uint16(main.H)}}
	if !m.cfg.Subwindows {
		return terms
	}
	for _, t := range []int{TermMessages, TermMonsters} {
		w, h := 40, 10
		if p, ok := m.layout.Pane(t); ok {
			b := p.TermBox()
			w, h = max(b.W, 1), max(b.H, 1)
		}
		terms = append(terms, proto.TermSize{Index: uint8(t), Width: uint16(w), Height: uint16(h)})
	}
	return terms
}

// listen waits for the next batch from the engine.
func (m *Model) listen() engine.Cmd {
	stream, gen := m.stream, m.gen
	return func(ctx context.Context) engine.Msg {
		select {
		case b, ok := <-stream:
			if !ok {
				return batchMsg{session.Batch{Err: io.EOF}, gen}
			}
			return batchMsg{b, gen}
		case <-ctx.Done():
			return nil
		}
	}
}

// Update handles one message.
func (m *Model) Update(msg engine.Msg) engine.UpdateResult {
	r := m.update(msg)
	// The quit timer starts here, whatever asked to quit. A click runs
	// outside Update, so a quit it asks for gets its timer on the next
	// message.
	if m.quitting && !m.quitTimed {
		m.quitTimed = true
		r.Commands = append(r.Commands, m.quitTimer())
	}
	// A notice set while handling the message gets its timer here.
	if m.noticeSeq != m.noticeTimed {
		m.noticeTimed = m.noticeSeq
		seq := m.noticeSeq
		r.Redraw = true
		r.Commands = append(r.Commands, func(ctx context.Context) engine.Msg {
			select {
			case <-time.After(noticeTime):
				return noticeOff{seq}
			case <-ctx.Done():
				return nil
			}
		})
	}
	return r
}

func (m *Model) update(msg engine.Msg) engine.UpdateResult {
	switch msg := msg.(type) {
	case startedMsg:
		if msg.gen != m.gen {
			if msg.sess != nil {
				msg.sess.Close()
			}
			return engine.UpdateResult{}
		}
		if msg.err != nil {
			m.err = msg.err
			return engine.UpdateResult{Quit: true}
		}
		m.sess = msg.sess
		m.log.Printf("engine started: %s, protocol %d", m.sess.Server.Name, m.sess.Server.Version)
		m.stream = make(chan session.Batch, 4)
		go m.sess.Stream(m.stream)
		m.sendResizes()
		return engine.UpdateResult{Commands: []engine.Cmd{m.listen()}, Redraw: true}

	case animTick:
		m.tick++
		g := m.terms.Terms[0]
		return engine.UpdateResult{
			Commands: []engine.Cmd{nextTick()},
			Redraw:   g != nil && m.hasLava(g),
		}

	case batchMsg:
		if msg.gen != m.gen {
			return engine.UpdateResult{}
		}
		return m.handleBatch(msg.Batch)

	case engine.KeyPressMsg:
		return m.handleKey(msg.Key)

	case engine.MousePressMsg:
		if n := len(m.stack); n > 0 {
			if msg.Button == driver.MouseLeft {
				m.stack[n-1].click(int(msg.Position.X), int(msg.Position.Y))
			}
			return m.afterAction()
		}
		m.handleMouse(msg)
		return engine.UpdateResult{}

	case engine.ResizeMsg:
		m.layout = Compute(int(msg.Width), int(msg.Height), m.cfg.Subwindows)
		m.sendResizes()
		return engine.UpdateResult{Redraw: true}

	case flashOff:
		if msg.seq == m.flashSeq {
			m.flashing = false
			return engine.UpdateResult{Redraw: true}
		}

	case noticeOff:
		if msg.seq == m.noticeSeq {
			m.notice = ""
			return engine.UpdateResult{Redraw: true}
		}

	case quitExpiry:
		if msg.gen == m.gen && m.sess != nil {
			m.log.Print("no BYE after QUIT; closing the connection")
			m.sess.Close()
		}
	}
	return engine.UpdateResult{}
}

func (m *Model) handleBatch(b session.Batch) engine.UpdateResult {
	redraw := false
	var cmds []engine.Cmd
	for _, msg := range b.Msgs {
		switch msg := msg.(type) {
		case proto.Bye:
			m.byeReason = msg.Reason
			m.log.Printf("engine said BYE %q", msg.Reason)
			if m.next != nil {
				return engine.UpdateResult{Commands: []engine.Cmd{m.startEngine(*m.next)}, Redraw: true}
			}
			return engine.UpdateResult{Quit: true}
		case proto.Bell:
			m.flashing = true
			m.flashSeq++
			seq := m.flashSeq
			cmds = append(cmds, func(ctx context.Context) engine.Msg {
				select {
				case <-time.After(flashTime):
					return flashOff{seq}
				case <-ctx.Done():
					return nil
				}
			})
			redraw = true
		case proto.TermOpen:
			m.log.Printf("term %d is %dx%d", msg.Term, msg.Width, msg.Height)
		case proto.Event:
			if m.handleEvent(msg) {
				redraw = true
			}
		case proto.Request:
			m.handleRequest(msg)
			redraw = true
		case proto.TileSet:
			sheet, err := tiles.Load(msg)
			if err != nil {
				m.log.Printf("cannot load the tile set: %v", err)
			} else {
				m.sheet = sheet
				m.log.Printf("drawing the map with %s (%dx%d cells a tile)", msg.Name, msg.CellsW, msg.CellsH)
			}
		}
		if m.terms.Apply(msg) >= 0 {
			redraw = true
		}
	}
	if b.Err != nil {
		if m.next != nil {
			m.log.Printf("engine closed the connection (%v); starting the next one", b.Err)
			return engine.UpdateResult{Commands: []engine.Cmd{m.startEngine(*m.next)}, Redraw: true}
		}
		if !errors.Is(b.Err, io.EOF) {
			m.err = fmt.Errorf("reading from the engine: %w", b.Err)
		} else {
			m.err = errors.New("the engine closed the connection without saying goodbye")
		}
		m.log.Print(m.err)
		return engine.UpdateResult{Quit: true}
	}
	cmds = append(cmds, m.listen())
	return engine.UpdateResult{Commands: cmds, Redraw: redraw}
}

// handleEvent updates the gauges from an EVENT. It returns whether the
// screen needs redrawing.
func (m *Model) handleEvent(ev proto.Event) bool {
	d, err := ev.Parse()
	if err != nil {
		m.log.Printf("bad EVENT: %v", err)
		return false
	}
	switch d.Event {
	case "COMMAND":
		if done, ok := m.pending[d.ID]; ok {
			delete(m.pending, d.ID)
			done(d)
			return true
		}
	case "SAVEFILES":
		m.saves = d.Saves
		m.saveName = d.Current
	case "WAIT":
		return m.handleWait(d.Context)
	case "BORG":
		m.borgOn = d.On
		m.borgStopping = false
		return true
	case "HP":
		m.gauges.hp, m.gauges.mhp = d.HP, d.MHP
		m.gauges.known = true
		return true
	case "MANA":
		m.gauges.sp, m.gauges.msp = d.SP, d.MSP
		m.gauges.hasMagic = d.MSP > 0
		return true
	}
	return false
}

func (m *Model) handleKey(ev driver.KeyEvent) engine.UpdateResult {
	if m.sess == nil {
		return engine.UpdateResult{}
	}
	if isQuitKey(ev) {
		m.next = nil
		m.closeAll()
		return m.requestQuit()
	}
	if n := len(m.stack); n > 0 {
		m.stack[n-1].key(ev)
		return m.afterAction()
	}
	menuKey := ev.Type == driver.KeyEsc || ev.Type == driver.KeyF10
	if menuKey && m.borgOn {
		// The borg ignores keys in a store, so stop it by command. It
		// stops when the engine's BORG event says so.
		if !m.borgStopping {
			m.borgStopping = true
			m.borgCommand(false)
		}
		m.openGameMenu()
		return m.afterAction()
	}
	if menuKey && m.waiting && m.played && !m.guarded(ev) {
		m.openGameMenu()
		return m.afterAction()
	}
	if k, ok := TranslateKey(ev); ok {
		m.sendKey(k)
	}
	return engine.UpdateResult{}
}

// guarded reports whether Escape should still go to the game, just after
// a run or a rest ended. F10 is never guarded.
func (m *Model) guarded(ev driver.KeyEvent) bool {
	return ev.Type == driver.KeyEsc && m.late && time.Since(m.waitAt) < menuGuard
}

// afterAction returns the result of a key a native screen handled. If
// the key asked the engine to quit, Update starts the quit timer.
func (m *Model) afterAction() engine.UpdateResult {
	return engine.UpdateResult{Redraw: true}
}

// requestQuit asks the engine to save and exit. On its BYE the client
// exits, or starts the next engine if one is set. Update starts the timer
// that drops the connection if no BYE comes.
func (m *Model) requestQuit() engine.UpdateResult {
	if m.quitting {
		return engine.UpdateResult{}
	}
	m.quitting = true
	m.log.Print("asking the engine to save and quit")
	m.send(proto.Quit{})
	return engine.UpdateResult{Redraw: true}
}

func (m *Model) quitTimer() engine.Cmd {
	gen := m.gen
	return func(ctx context.Context) engine.Msg {
		select {
		case <-time.After(quitTimeout):
			return quitExpiry{gen}
		case <-ctx.Done():
			return nil
		}
	}
}

// sendKey sends a key press to the engine.
func (m *Model) sendKey(k proto.Key) {
	m.waiting = false
	m.keyAt = time.Now()
	m.send(k)
}

// command sends a structured command; done runs with its result.
func (m *Model) command(name string, args map[string]any, done func(proto.EventData)) {
	m.nextID++
	id := m.nextID
	if done != nil {
		m.pending[id] = done
	}
	m.send(proto.NewCommand(id, name, args))
}

// handleWait notes a WAIT: the game waits for a command, or for a key
// on the splash screen. It returns whether to redraw.
func (m *Model) handleWait(ctx string) bool {
	switch ctx {
	case "splash":
		switch {
		case !m.launch.first() && !m.launch.Menu:
			// This engine was started for a chosen character.
			m.pressOn()
		default:
			m.command("SAVEFILES", nil, func(proto.EventData) { m.openStartMenu() })
		}
		return false
	case "game":
		m.waiting = true
		m.waitAt = time.Now()
		m.late = m.waitAt.Sub(m.keyAt) > busyTime
		if !m.played {
			m.played = true
			// Learn the character's savefile, which Continue loads next
			// time.
			m.command("SAVEFILES", nil, func(proto.EventData) {
				if m.saveName != "" && m.saveName != m.settings.LastCharacter {
					m.settings.LastCharacter = m.saveName
					m.saveSettings()
				}
			})
		}
	}
	return false
}

// handleRequest answers a REQUEST. The client claims only screens.
func (m *Model) handleRequest(r proto.Request) {
	d, err := r.Parse()
	if err != nil {
		m.log.Printf("bad REQUEST: %v", err)
		return
	}
	if d.Prompt == "screen" && d.Name == "death" {
		m.waiting = false
		m.openDeathMenu(d.ID)
		return
	}
	m.log.Printf("unexpected REQUEST %s %s; cancelling it", d.Prompt, d.Name)
	m.send(proto.NewReply(d.ID, nil))
}

// saveSettings writes the settings file.
func (m *Model) saveSettings() {
	if m.cfg.SettingsPath == "" {
		return
	}
	if err := m.settings.Save(m.cfg.SettingsPath); err != nil {
		m.log.Printf("cannot write %s: %v", m.cfg.SettingsPath, err)
	}
}

func (m *Model) handleMouse(msg engine.MousePressMsg) {
	if m.sess == nil || m.layout.TooSmall {
		return
	}
	x, y := int(msg.Position.X), int(msg.Position.Y)
	main := m.layout.Main
	if x < main.X || y < main.Y || x >= main.X+main.W || y >= main.Y+main.H {
		return
	}
	var button uint8
	switch msg.Button {
	case driver.MouseLeft:
		button = 1
	case driver.MouseMiddle:
		button = 2
	case driver.MouseRight:
		button = 3
	default:
		return
	}
	m.send(proto.Mouse{Term: 0, X: uint16(x - main.X), Y: uint16(y - main.Y), Button: button})
}

// sendResizes asks the engine to fit each shown term to its box.
func (m *Model) sendResizes() {
	if m.sess == nil || m.layout.TooSmall {
		return
	}
	m.resize(0, m.layout.Main)
	for _, p := range m.layout.Panes {
		m.resize(p.Term, p.TermBox())
	}
}

func (m *Model) resize(term int, b Box) {
	want := [2]int{max(b.W, 1), max(b.H, 1)}
	if m.sizes[term] == want {
		return
	}
	m.sizes[term] = want
	m.send(proto.Resize{Term: uint8(term), Width: uint16(want[0]), Height: uint16(want[1])})
}

func (m *Model) send(msg proto.Message) {
	if m.sess == nil {
		return
	}
	if err := m.sess.Send(msg); err != nil {
		m.log.Printf("sending %T: %v", msg, err)
	}
}

// View draws every shown term.
func (m *Model) View(f *terminal.Frame) {
	area := f.Area()
	if m.layout.TooSmall {
		msg := fmt.Sprintf("Fancyband needs a window of at least %dx%d. This one is %dx%d.",
			MinWidth, MinHeight, area.Width, area.Height)
		f.RenderWidget(limoni.NewParagraph(msg), area)
		return
	}
	pal := newPalette(m.terms.Palette)
	if g := m.terms.Terms[0]; g != nil {
		pictures := m.sheet != nil && m.terms.TileSet != nil
		view := &termView{grid: g, pal: pal, cursor: true, flash: m.flashing, pictures: pictures}
		if m.cfg.Animate {
			view.lavaTick = m.tick
			view.lava = true
		}
		f.RenderWidget(view, rect(m.layout.Main))
		var covered []Box
		for _, s := range m.stack {
			covered = append(covered, s.box(Box{0, 0, m.layout.Width, m.layout.Height}))
		}
		m.placeTiles(f, g, covered)
	}
	divider := limoni.Fg(limoni.RGB(90, 90, 110))
	if m.layout.Divider >= 0 {
		if m.layout.Vertical {
			for y := 0; y < m.layout.Height; y++ {
				f.Text(m.layout.Divider, y, "│", divider)
			}
		} else {
			for x := 0; x < m.layout.Width; x++ {
				f.Text(x, m.layout.Divider, "─", divider)
			}
		}
	}
	drawGauges(f, m.layout.Gauges, m.gauges)
	for _, p := range m.layout.Panes {
		f.TextFit(p.Box.X, p.Box.Y, p.Box.W, " "+p.Title+" ", divider.Bold())
		if g := m.terms.Terms[p.Term]; g != nil {
			f.RenderWidget(&termView{grid: g, pal: pal}, rect(p.TermBox()))
		}
	}
	screenArea := Box{0, 0, m.layout.Width, m.layout.Height}
	env := &drawEnv{pal: pal}
	if m.sheet != nil && m.terms.TileSet != nil {
		env.sprite = m.sheet.Sprite
	}
	for _, s := range m.stack {
		s.draw(f, s.box(screenArea), env)
	}
	if m.notice != "" {
		text := noticeText(m.notice)
		w := min(limoni.StringWidth(text), m.layout.Width)
		f.TextFit((m.layout.Width-w)/2, m.layout.Height-1, w, text, screenSelected)
	}
}

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
	"image"
	"image/color"
	"io"
	"log"
	"slices"
	"time"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/cell"
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

// Caps are the capabilities the client claims in HELLO. "events" feeds
// the gauges and the menus; "waits" says when Escape may open the game
// menu; "status" feeds the stats bar and status line; "moves" and
// "scrolls" feed smooth movement and scrolling; the native screens'
// own hand them and their prompts over; "tiles:N" asks for a tile set.
func (m *Model) Caps() []string {
	caps := []string{"events", "waits", "status", "moves", "scrolls"}
	for _, ns := range nativeScreens {
		if !slices.Contains(m.settings.Classic, ns.name) {
			caps = append(caps, ns.caps...)
		}
	}
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
	// Animate runs the animated filters on tiles.
	Animate bool
	// EngineArgs are the engine options the player gave. A character
	// switch replaces -n and -u in them.
	EngineArgs []string
	// SettingsPath is fancyband-tui.json. Empty keeps settings in memory.
	SettingsPath string
	// Automation is a Unix socket to serve the screen on, for tests and
	// agents. Empty serves nothing. It needs a limoni_debug build.
	Automation string

	// TilesGiven, AnimateGiven and SubwindowsGiven say the flag was given,
	// so it wins over the settings file.
	TilesGiven, AnimateGiven, SubwindowsGiven bool
	// TilesBlocked says why the terminal cannot show tiles, if it cannot.
	TilesBlocked string
	// Protocol names the image protocol, such as "Kitty" or "Sixel".
	Protocol string

	// fullRedraw makes the terminal redraw every cell and picture.
	fullRedraw func()
	// setMouseMode changes how much of the mouse the terminal reports.
	setMouseMode func(driver.MouseMode) error
}

// applySettings fills in what no flag gave from the settings file.
func (cfg *Config) applySettings(s settings.Settings) {
	if !cfg.TilesGiven && s.TileSet != nil {
		cfg.Tiles = *s.TileSet
	}
	if !cfg.AnimateGiven && s.Animate != nil {
		cfg.Animate = *s.Animate
	}
	if !cfg.SubwindowsGiven && s.Subwindows != nil {
		cfg.Subwindows = *s.Subwindows
	}
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
	// status is the last status the engine sent, for the stats bar and
	// status line.
	status *proto.StatusData
	// slides are the creatures sliding from grid to grid, since slideAt,
	// and scroll the map scrolling, or pendingScroll one waiting for the
	// game to redraw the map. motionTicking is set while their frames are
	// due, and startMotion asks Update to start them.
	slides                     []slide
	slideAt                    time.Time
	scroll, pendingScroll      *mapScroll
	motionTicking, startMotion bool
	// now tells the time; tests set it.
	now func() time.Time
	// panelsSent is set once PANELS has gone to this engine, and
	// panelsNative says whether it hid the engine's own panels.
	panelsSent, panelsNative bool
	// sheet holds the engine's tile set once TILESET arrives.
	sheet *tiles.Sheet

	// tick counts animation steps; it drives the animated filters.
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
	// marker is the grid a look screen points at on the map, in screen
	// cells, or nil.
	marker *Box
	// sendHook, if set, sees every message sent, for tests.
	sendHook func(proto.Message)
	// frames caches tile pictures with the marker's frame.
	frames map[image.Image]image.Image

	// Commands sent with command(), by id, and the next id.
	pending map[int]func(proto.EventData)
	nextID  int
	// inGame is set from the game's start (ENTER_GAME) to the death
	// screen's request, while the stats bar and status line show.
	inGame bool
	// answers holds the answers to queries sent with query(), by id,
	// until their COMMAND result comes.
	answers map[int]proto.EventData

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

	// scale is the tile size: each tile covers scale times the cells it
	// would at size 1.
	scale int
	// mouseMode is the mouse mode the terminal is in (see hover.go).
	mouseMode driver.MouseMode
	// pointer is the cell under the mouse pointer, once pointerSet, while
	// hover is on; hovered is the control View lit up under it.
	pointer    cell.Point
	pointerSet bool
	hovered    Box
	// pieces caches the parts of tile pictures shown beside a screen.
	pieces map[pieceKey]image.Image
	// wantScale is the tile size the player chose; the map uses less
	// when the window has no room for it.
	wantScale int
	// ticking is set while an animation tick is due, and startTicking
	// asks Update to start one.
	ticking, startTicking bool
	// lastOptions is the last OPTIONS answer, and lastVisuals the last
	// VISUALS one.
	lastOptions, lastVisuals proto.EventData
	// visualsFile is the tile set's file of changes, from the last
	// VISUALS answer; the filters live beside it, in editsPath.
	visualsFile, editsPath string
	// classicNext answers the next options screen request with the
	// game's own screen.
	classicNext bool

	borgOn bool
	// borgStopping is set from asking the borg to stop until the engine
	// says it has.
	borgStopping bool
	borgMissing  bool
	// deathID is the death screen's request.
	deathID int
	// store is the open store screen, which shows the game's messages,
	// and birth the open birth screen.
	store *storeScreen
	birth *birthScreen

	notice      string
	noticeSeq   int
	noticeTimed int
}

// New returns a model that starts the engine when it starts.
func New(cfg Config) *Model {
	m := &Model{cfg: cfg, log: cfg.Log, sizes: map[int][2]int{}, pending: map[int]func(proto.EventData){}, answers: map[int]proto.EventData{}, now: time.Now}
	if m.log == nil {
		m.log = log.New(io.Discard, "", 0)
	}
	if cfg.SettingsPath != "" {
		s, err := settings.Load(cfg.SettingsPath)
		if err != nil {
			m.log.Printf("cannot read %s: %v", cfg.SettingsPath, err)
		}
		m.settings = s
		m.cfg.applySettings(s)
	}
	m.wantScale = m.settings.Scale()
	m.mouseMode = m.wantMouseMode()
	m.layout = m.computeLayout(m.cfg.Width, m.cfg.Height, false)
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
		m.ticking = true
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
	m.status, m.panelsSent = nil, false
	m.inGame = false
	m.layout = m.computeLayout(m.layout.Width, m.layout.Height, false)
	m.slides, m.scroll, m.pendingScroll = nil, nil, nil
	m.stack = nil
	m.pending = map[int]func(proto.EventData){}
	m.answers = map[int]proto.EventData{}
	m.saves, m.saveName = nil, ""
	m.waiting, m.played, m.borgOn, m.borgStopping = false, false, false, false
	m.quitting, m.quitTimed = false, false

	terms := m.initialTerms()
	for _, t := range terms {
		m.sizes[int(t.Index)] = [2]int{int(t.Width), int(t.Height)}
	}
	args, module := l.Args(m.cfg.EngineArgs)
	start, caps, gen, logger := m.cfg.Start, m.Caps(), m.gen, m.log
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
	// The side panels' terms open even when the panels are off, so
	// turning them on in the options needs no restart.
	terms := []proto.TermSize{{Index: 0, Width: uint16(main.W), Height: uint16(main.H)}}
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
func (m *Model) Update(msg engine.Msg) (r engine.UpdateResult) {
	// A key or a click ends every slide and scroll, so the game never
	// waits for one.
	switch msg.(type) {
	case engine.KeyPressMsg, engine.MousePressMsg:
		if m.endMotion() {
			defer func() { r.Redraw = true }()
		}
	}
	r = m.update(msg)
	if m.startMotion {
		m.startMotion = false
		m.motionTicking = true
		r.Commands = append(r.Commands, motionTickCmd())
	}
	if m.startTicking {
		m.startTicking = false
		m.ticking = true
		r.Commands = append(r.Commands, nextTick())
	}
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
		m.syncPanels()
		return engine.UpdateResult{Commands: []engine.Cmd{m.listen()}, Redraw: true}

	case motionTick:
		return m.handleMotionTick()

	case animTick:
		if !m.cfg.Animate {
			m.ticking = false
			return engine.UpdateResult{Redraw: true}
		}
		m.tick++
		g := m.terms.Terms[0]
		return engine.UpdateResult{
			Commands: []engine.Cmd{nextTick()},
			Redraw:   g != nil && m.hasAnimation(g) || m.screenAnimated(),
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

	case engine.MouseMotionMsg:
		return engine.UpdateResult{Redraw: m.pointerMoved(int(msg.Position.X), int(msg.Position.Y))}

	case engine.ResizeMsg:
		m.relayout(int(msg.Width), int(msg.Height))
		m.fitScale()
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
			m.useTileSet(msg)
			redraw = true
			continue
		case proto.Snapshot:
			if m.handleSnapshot(msg) {
				redraw = true
			}
			continue
		}
		if m.terms.Apply(msg) >= 0 {
			redraw = true
		}
		// The game has redrawn the map after a SCROLL: scroll to it.
		if f, ok := msg.(proto.Fresh); ok && f.Term == 0 && m.pendingScroll != nil {
			m.beginScroll()
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

// useTileSet switches to the tile set in a TILESET, or to text for set
// 0, which the engine sends at start and after GRAPHICS. The terminal
// redraws everything, so no picture of the old set stays on screen.
func (m *Model) useTileSet(ts proto.TileSet) {
	had := m.sheet != nil
	m.sheet = nil
	m.terms.Apply(ts)
	if ts.ID == 0 {
		m.terms.TileSet = nil
		m.cfg.Tiles = 0
		m.log.Print("drawing the map with text")
	} else if sheet, err := tiles.Load(ts); err != nil {
		m.log.Printf("cannot load the tile set: %v", err)
		m.terms.TileSet = nil
	} else {
		m.sheet = sheet
		m.editsPath = ""
		m.command("VISUALS", map[string]any{"brief": true}, func(proto.EventData) { m.loadEdits(sheet) })
		m.cfg.Tiles = int(ts.ID)
		m.scale = max(int(ts.CellsH), 1)
		m.log.Printf("drawing the map with %s (%dx%d cells a tile)", ts.Name, ts.CellsW, ts.CellsH)
	}
	if (had || m.sheet != nil) && m.cfg.fullRedraw != nil {
		m.cfg.fullRedraw()
	}
	m.fitScale()
}

// loadEdits reads the player's filters for sheet, from beside the file
// of changes the last VISUALS answer named. A sheet no longer in use is
// left alone.
func (m *Model) loadEdits(sheet *tiles.Sheet) {
	if m.sheet != sheet || m.visualsFile == "" {
		return
	}
	path := tiles.EditsPath(m.visualsFile)
	e, err := tiles.LoadEdits(path)
	if err != nil {
		m.log.Printf("cannot read the filters: %v", err)
	}
	sheet.SetEdits(e)
	m.editsPath = path
}

// Room the map needs at a tile size to be worth playing: grids across and
// down. The map leaves out the sidebar's 13 columns and the top and
// bottom rows.
const (
	minMapGridsW = 15
	minMapGridsH = 7
	maxScale     = 3
)

// baseCells returns how many columns a tile covers at size 1.
func (m *Model) baseCells() int {
	ts := m.terms.TileSet
	if ts == nil || ts.CellsH == 0 {
		return 2
	}
	return max(int(ts.CellsW)/int(ts.CellsH), 1)
}

// scaleFits reports whether the map shows enough grids at tile size s.
func (m *Model) scaleFits(s int) bool {
	if s <= 1 {
		return true
	}
	w, h := m.layout.Main.W-13, m.layout.Main.H-3
	if m.layout.Native() {
		// No sidebar, and the map takes the bottom row.
		w, h = m.layout.Main.W, m.layout.Main.H-1
	}
	return w/(m.baseCells()*s) >= minMapGridsW && h/s >= minMapGridsH
}

// fittingScale returns the largest tile size up to want that fits.
func (m *Model) fittingScale(want int) int {
	s := min(max(want, 1), maxScale)
	for s > 1 && !m.scaleFits(s) {
		s--
	}
	return s
}

// fitScale asks the engine for the tile size the player chose, or the
// largest that fits the window, when the map uses another.
func (m *Model) fitScale() {
	if m.sheet == nil || m.sess == nil || m.layout.TooSmall {
		return
	}
	if s := m.fittingScale(m.wantScale); s != m.scale {
		m.scale = s
		m.command("GRAPHICS", map[string]any{"set": m.cfg.Tiles, "scale": s}, nil)
	}
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
	case "OPTIONS":
		m.lastOptions = d
	case "VISUALS":
		m.visualsFile = d.File
		if !d.Brief {
			m.lastVisuals = d
		}
	case "SAVEFILES":
		m.saves = d.Saves
		m.saveName = d.Current
	case "RECALL", "INVENTORY", "SPELLS", "STORE", "BIRTH", "LOOK", "MONSTER_LIST",
		"KNOWLEDGE", "CHARACTER", "MESSAGES", "HELP", "MAP":
		m.answers[d.ID] = d
	case "MESSAGE":
		if m.store != nil && d.Text != "" {
			m.store.say(d.Text)
			return true
		}
	case "WAIT":
		return m.handleWait(d.Context)
	case "MOVES":
		return m.startSlides(d)
	case "SCROLL":
		return m.prepareScroll(d)
	case "ENTER_GAME":
		m.setInGame(true)
		return true
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
	if ev.Type == driver.KeyF2 && m.waiting && m.played {
		m.openTileEditor()
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

// query sends a query whose answer is an event of its own, such as
// RECALL; done runs with that answer, or is not run if the engine
// refused the query.
func (m *Model) query(name string, args map[string]any, done func(proto.EventData)) {
	m.command(name, args, func(r proto.EventData) {
		a, ok := m.answers[r.ID]
		delete(m.answers, r.ID)
		if !r.OK || !ok {
			m.log.Printf("%s refused: %s", name, r.Error)
			return
		}
		done(a)
	})
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
		m.syncPanels()
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

// handleRequest answers a REQUEST: a screen or a prompt the client
// claimed.
func (m *Model) handleRequest(r proto.Request) {
	d, err := r.Parse()
	if err != nil {
		m.log.Printf("bad REQUEST: %v", err)
		return
	}
	if d.Prompt == "screen" && d.Name == "death" {
		// The game is over: no stats bar behind the death menu. Not
		// earlier: a resize while the game waits at -more- before the
		// death screen would wipe the prompt.
		m.setInGame(false)
		m.waiting = false
		m.openDeathMenu(d.ID)
		return
	}
	if d.Prompt == "screen" && d.Name == "inventory" {
		m.waiting = false
		m.openInventory(d.ID, d.View, false)
		return
	}
	if d.Prompt == "screen" && (d.Name == "look" || d.Name == "target") {
		m.waiting = false
		m.openLook(d)
		return
	}
	if d.Prompt == "screen" && d.Name == "store" {
		m.waiting = false
		m.openStore(d.ID)
		return
	}
	if d.Prompt == "screen" && d.Name == "birth" {
		m.waiting = false
		m.openBirth(d.ID)
		return
	}
	if d.Prompt == "screen" {
		open := map[string]func(){
			"knowledge": func() { m.openKnowledge(d.ID, d.View) },
			"character": func() { m.openCharacter(d.ID) },
			"messages":  func() { m.openMessages(d.ID) },
			"help":      func() { m.openHelp(d.ID, "") },
			"map":       func() { m.openMap(d.ID) },
		}[d.Name]
		if open != nil {
			m.waiting = false
			open()
			return
		}
	}
	if d.Prompt != "screen" {
		m.waiting = false
		if m.openPrompt(d) {
			return
		}
	}
	if d.Prompt == "screen" && d.Name == "options" {
		m.waiting = false
		if m.classicNext {
			m.classicNext = false
			m.send(proto.NewReply(d.ID, map[string]any{"classic": true}))
			return
		}
		m.openOptions(d.ID)
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
	if p, row, ok := m.paneAt(x, y); ok {
		if p.Term == TermMonsters && msg.Button == driver.MouseLeft {
			m.openRecallAt(row)
		}
		return
	}
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
	if m.sendHook != nil {
		m.sendHook(msg)
	}
	if m.sess == nil {
		return
	}
	if err := m.sess.Send(msg); err != nil {
		m.log.Printf("sending %T: %v", msg, err)
	}
}

// View draws every shown term.
func (m *Model) View(f *terminal.Frame) {
	m.syncMouseMode()
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
		var covered []Box
		for _, s := range m.stack {
			covered = append(covered, s.box(Box{0, 0, m.layout.Width, m.layout.Height}))
		}
		regions, dropped := m.placeTiles(g, covered)
		view := &termView{grid: g, pal: pal, cursor: true, flash: m.flashing, pictures: pictures, dropped: dropped, scroll: m.textScrollNow()}
		f.RenderWidget(view, rect(m.layout.Main))
		for _, r := range regions {
			f.RegisterImageRegion(r)
		}
		if !pictures {
			m.drawMarker(f)
		}
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
	var sprite func(row, col uint8, bg color.RGBA) image.Image
	if m.sheet != nil && m.terms.TileSet != nil {
		sprite = m.sheet.Sprite
	}
	if m.layout.Native() {
		for y := 0; y < m.layout.Stats.H; y++ {
			f.Text(m.layout.StatsDivider, y, "│", divider)
		}
		drawStats(f, m.layout.Stats, m.status, pal, sprite)
		drawStatusLine(f, m.layout.StatusLine, m.status, pal)
	}
	for _, p := range m.layout.Panes {
		f.TextFit(p.Box.X, p.Box.Y, p.Box.W, " "+p.Title+" ", divider.Bold())
		if g := m.terms.Terms[p.Term]; g != nil {
			f.RenderWidget(&termView{grid: g, pal: pal}, rect(p.TermBox()))
		}
	}
	screenArea := Box{0, 0, m.layout.Width, m.layout.Height}
	env := &drawEnv{pal: pal, sprite: sprite, pointer: m.pointerAt}
	for _, s := range m.stack {
		s.draw(f, s.box(screenArea), env)
	}
	m.drawHover(f)
	if m.notice != "" {
		text := noticeText(m.notice)
		w := min(limoni.StringWidth(text), m.layout.Width)
		f.TextFit((m.layout.Width-w)/2, m.layout.Height-1, w, text, screenSelected)
	}
}

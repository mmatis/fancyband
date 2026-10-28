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
)

// flashTime is how long the screen flashes for the bell.
const flashTime = 120 * time.Millisecond

// quitTimeout is how long Ctrl+C waits for the engine to save before the
// client gives up and closes the connection, which also makes it save.
const quitTimeout = 10 * time.Second

// StartFunc starts an engine that opens the given terms.
type StartFunc func(terms []proto.TermSize) (*session.Session, error)

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

	flashing  bool
	flashSeq  int
	quitting  bool
	byeReason string
	err       error
}

// New returns a model that starts the engine when it starts.
func New(cfg Config) *Model {
	m := &Model{cfg: cfg, log: cfg.Log, sizes: map[int][2]int{}}
	if m.log == nil {
		m.log = log.New(io.Discard, "", 0)
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
	}
	batchMsg   session.Batch
	flashOff   struct{ seq int }
	quitExpiry struct{}
)

// Init starts the engine.
func (m *Model) Init() []engine.Cmd {
	terms := m.initialTerms()
	for _, t := range terms {
		m.sizes[int(t.Index)] = [2]int{int(t.Width), int(t.Height)}
	}
	start := m.cfg.Start
	return []engine.Cmd{func(context.Context) engine.Msg {
		s, err := start(terms)
		return startedMsg{s, err}
	}}
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
	stream := m.stream
	return func(ctx context.Context) engine.Msg {
		select {
		case b, ok := <-stream:
			if !ok {
				return batchMsg{Err: io.EOF}
			}
			return batchMsg(b)
		case <-ctx.Done():
			return nil
		}
	}
}

// Update handles one message.
func (m *Model) Update(msg engine.Msg) engine.UpdateResult {
	switch msg := msg.(type) {
	case startedMsg:
		if msg.err != nil {
			m.err = msg.err
			return engine.UpdateResult{Quit: true}
		}
		m.sess = msg.sess
		m.log.Printf("engine started: %s, protocol %d", m.sess.Server.Name, m.sess.Server.Version)
		m.stream = make(chan session.Batch, 4)
		go m.sess.Stream(m.stream)
		return engine.UpdateResult{Commands: []engine.Cmd{m.listen()}}

	case batchMsg:
		return m.handleBatch(session.Batch(msg))

	case engine.KeyPressMsg:
		return m.handleKey(msg.Key)

	case engine.MousePressMsg:
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

	case quitExpiry:
		if m.sess != nil {
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
		}
		if m.terms.Apply(msg) >= 0 {
			redraw = true
		}
	}
	if b.Err != nil {
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

func (m *Model) handleKey(ev driver.KeyEvent) engine.UpdateResult {
	if m.sess == nil {
		return engine.UpdateResult{}
	}
	if isQuitKey(ev) {
		if m.quitting {
			return engine.UpdateResult{}
		}
		m.quitting = true
		m.log.Print("Ctrl+C: asking the engine to save and quit")
		m.send(proto.Quit{})
		return engine.UpdateResult{Commands: []engine.Cmd{func(ctx context.Context) engine.Msg {
			select {
			case <-time.After(quitTimeout):
				return quitExpiry{}
			case <-ctx.Done():
				return nil
			}
		}}}
	}
	if k, ok := TranslateKey(ev); ok {
		m.send(k)
	}
	return engine.UpdateResult{}
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
		f.RenderWidget(&termView{grid: g, pal: pal, cursor: true, flash: m.flashing}, rect(m.layout.Main))
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
	for _, p := range m.layout.Panes {
		f.TextFit(p.Box.X, p.Box.Y, p.Box.W, " "+p.Title+" ", divider.Bold())
		if g := m.terms.Terms[p.Term]; g != nil {
			f.RenderWidget(&termView{grid: g, pal: pal}, rect(p.TermBox()))
		}
	}
}

package terminal

import (
	"bytes"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
)

// stripes is a scene whose colour is the time in tenths of a second, so a
// test can tell which moment a cell was drawn for.
type stripes struct{ interval time.Duration }

func (s stripes) Interval() time.Duration { return s.interval }

func (s stripes) Render(dst *buffer.Buffer, t time.Duration) {
	v := uint8(t / (100 * time.Millisecond))
	for i := range dst.Content {
		dst.Content[i] = cell.Cell{Content: '~', Style: cell.Style{
			Fg: cell.NewColorRGB(200, 0, 0),
			Bg: cell.NewColorRGB(v, uint8(i%7), 30),
		}}
	}
}

func sceneBg(t time.Duration, i int) cell.Color {
	return cell.NewColorRGB(uint8(t/(100*time.Millisecond)), uint8(i%7), 30)
}

func TestComposeBackdropRule(t *testing.T) {
	scene := cell.Cell{Content: '✦', Style: cell.Style{Fg: cell.NewColorRGB(1, 2, 3), Bg: cell.NewColorRGB(4, 5, 6)}}
	panel := cell.NewColorRGB(40, 40, 60)
	cases := []struct {
		name     string
		app, out cell.Cell
	}{
		{"a blank cell shows the scene whole",
			cell.Cell{Content: ' '}, scene},
		{"text keeps its glyph and colour over the scene's background",
			cell.Cell{Content: 'a', Style: cell.Style{Fg: cell.NewColorRGB(9, 9, 9), Modifier: cell.ModifierBold}},
			cell.Cell{Content: 'a', Style: cell.Style{Fg: cell.NewColorRGB(9, 9, 9), Bg: scene.Style.Bg, Modifier: cell.ModifierBold}}},
		{"a styled space is text, not a hole",
			cell.Cell{Content: ' ', Style: cell.Style{Modifier: cell.ModifierUnderline}},
			cell.Cell{Content: ' ', Style: cell.Style{Bg: scene.Style.Bg, Modifier: cell.ModifierUnderline}}},
		{"a background colour hides the scene",
			cell.Cell{Content: ' ', Style: cell.Style{Bg: panel}},
			cell.Cell{Content: ' ', Style: cell.Style{Bg: panel}}},
		{"an ANSI background hides it too",
			cell.Cell{Content: 'x', Style: cell.Style{Bg: cell.NewColorANSI(4)}},
			cell.Cell{Content: 'x', Style: cell.Style{Bg: cell.NewColorANSI(4)}}},
		{"a reversed cell draws its background from the default foreground",
			cell.Cell{Content: '>', Style: cell.Style{Modifier: cell.ModifierReverse}},
			cell.Cell{Content: '>', Style: cell.Style{Modifier: cell.ModifierReverse}}},
		{"an image cell is left to the image",
			cell.Cell{Content: cell.RuneImage},
			cell.Cell{Content: cell.RuneImage}},
	}
	for _, tc := range cases {
		dst := []cell.Cell{tc.app}
		ComposeBackdrop(dst, []cell.Cell{scene})
		if dst[0] != tc.out {
			t.Errorf("%s: got %+v, want %+v", tc.name, dst[0], tc.out)
		}
	}
}

// backdropTerminal is a 20×4 truecolor terminal writing to memory, whose
// backdrop clock the test moves by hand.
func backdropTerminal(t *testing.T) (*Terminal, *driver.MemoryTerminalIO, *time.Time) {
	t.Helper()
	io := driver.NewMemoryTerminalIO(nil, 20, 4)
	b := driver.NewPortableBackend(io)
	if err := b.Setup(); err != nil {
		t.Fatal(err)
	}
	term, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	caps := term.Capabilities()
	caps.TrueColor = true
	term.SetCapabilities(caps)
	now := time.Unix(1000, 0)
	term.bgClock = func() time.Time { return now }
	return term, io, &now
}

func TestBackdropIsDrawnUnderTheApplication(t *testing.T) {
	term, io, now := backdropTerminal(t)
	term.SetBackdrop(stripes{interval: 50 * time.Millisecond})
	*now = now.Add(300 * time.Millisecond)

	calls := 0
	app := func(f *Frame) {
		calls++
		f.Buffer.SetString(0, 0, "hi", cell.Style{})
		f.Buffer.SetString(0, 1, "box", cell.Style{Bg: cell.NewColorRGB(50, 50, 50)})
	}
	if err := term.Draw(app); err != nil {
		t.Fatal(err)
	}
	check := func(at time.Duration) {
		t.Helper()
		front := term.FrontBuffer()
		if c := front.CellAt(0, 0); c.Content != 'h' || c.Style.Bg != sceneBg(at, 0) {
			t.Errorf("text over the scene at %v: %+v", at, c)
		}
		if c := front.CellAt(0, 1); c.Content != 'b' || c.Style.Bg != cell.NewColorRGB(50, 50, 50) {
			t.Errorf("panel at %v: %+v", at, c)
		}
		if c := front.CellAt(10, 2); c.Content != '~' || c.Style.Bg != sceneBg(at, 2*20+10) {
			t.Errorf("open scene at %v: %+v", at, c)
		}
	}
	check(300 * time.Millisecond)

	// The scene moves on without the application being asked to draw.
	before := len(io.Output())
	*now = now.Add(200 * time.Millisecond)
	if err := term.DrawBackdrop(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("DrawBackdrop called the application: %d calls", calls)
	}
	if len(io.Output()) == before {
		t.Fatal("a new frame of the scene wrote nothing")
	}
	check(500 * time.Millisecond)

	// A moment the scene does not change at writes nothing.
	before = len(io.Output())
	*now = now.Add(20 * time.Millisecond)
	if err := term.DrawBackdrop(); err != nil {
		t.Fatal(err)
	}
	if extra := io.Output()[before:]; len(extra) != 0 {
		t.Fatalf("an unchanged scene wrote %q", extra)
	}

	// Removing the backdrop gives the application's own cells back.
	term.SetBackdrop(nil)
	if err := term.Draw(app); err != nil {
		t.Fatal(err)
	}
	if c := term.FrontBuffer().CellAt(10, 2); c != (cell.Cell{Content: ' '}) {
		t.Fatalf("scene left behind after SetBackdrop(nil): %+v", c)
	}
	if err := term.DrawBackdrop(); err != nil {
		t.Fatal(err)
	}
}

func TestBackdropWaitsForTheApplicationAfterAResize(t *testing.T) {
	term, io, now := backdropTerminal(t)
	term.SetBackdrop(stripes{interval: 50 * time.Millisecond})
	setup := len(io.Output())
	if err := term.DrawBackdrop(); err != nil {
		t.Fatal(err)
	}
	if len(io.Output()) != setup {
		t.Fatal("DrawBackdrop drew before the application's first frame")
	}
	if err := term.Draw(func(f *Frame) {}); err != nil {
		t.Fatal(err)
	}
	// Both, since the backends differ: Linux keeps the size SetSize gives,
	// Windows reads it from the IO on every call.
	io.Width, io.Height = 30, 6
	term.driver.SetSize(30, 6)
	before := len(io.Output())
	*now = now.Add(time.Second)
	if err := term.DrawBackdrop(); err != nil {
		t.Fatal(err)
	}
	if len(io.Output()) != before {
		t.Fatal("DrawBackdrop drew over a layout made for the old size")
	}
}

func TestBackdropIsLeftOutWhereItCannotLookRight(t *testing.T) {
	t.Run("16 colours", func(t *testing.T) {
		term, _, _ := backdropTerminal(t)
		caps := term.Capabilities()
		caps.TrueColor, caps.Colors256 = false, false
		term.SetCapabilities(caps)
		term.SetBackdrop(stripes{interval: time.Second})
		if err := term.Draw(func(f *Frame) {}); err != nil {
			t.Fatal(err)
		}
		if c := term.FrontBuffer().CellAt(0, 0); c != (cell.Cell{Content: ' '}) {
			t.Fatalf("scene drawn in 16 colours: %+v", c)
		}
		if term.BackdropInterval() != 0 {
			t.Fatal("a backdrop that is not drawn still asks for frames")
		}
	})
	t.Run("LIMONI_BACKDROP=off", func(t *testing.T) {
		t.Setenv("LIMONI_BACKDROP", "off")
		term, _, _ := backdropTerminal(t)
		term.SetBackdrop(stripes{interval: time.Second})
		if err := term.Draw(func(f *Frame) {}); err != nil {
			t.Fatal(err)
		}
		if c := term.FrontBuffer().CellAt(0, 0); c != (cell.Cell{Content: ' '}) {
			t.Fatalf("scene drawn although the user turned it off: %+v", c)
		}
	})
}

func TestDrawBackdropDoesNotAllocate(t *testing.T) {
	term, _, now := backdropTerminal(t)
	term.SetBackdrop(stripes{interval: 50 * time.Millisecond})
	if err := term.Draw(func(f *Frame) { f.Buffer.SetString(1, 1, "steady", cell.Style{}) }); err != nil {
		t.Fatal(err)
	}
	// Warm the style cache for every colour the scene will use.
	for i := 0; i < 20; i++ {
		*now = now.Add(100 * time.Millisecond)
		_ = term.DrawBackdrop()
	}
	*now = now.Add(-2 * time.Second)
	allocs := testing.AllocsPerRun(20, func() {
		*now = now.Add(100 * time.Millisecond)
		if err := term.DrawBackdrop(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("DrawBackdrop allocated %v times a frame", allocs)
	}
}

func TestBackdropTickerPacesOnlyWhatNeedsIt(t *testing.T) {
	term, _, _ := backdropTerminal(t)
	var bt BackdropTicker
	defer bt.Stop()
	if bt.C(term, 0) != nil {
		t.Fatal("ticking without a backdrop")
	}
	term.SetBackdrop(stripes{})
	if bt.C(term, 0) != nil {
		t.Fatal("ticking for a still backdrop")
	}
	term.SetBackdrop(stripes{interval: 50 * time.Millisecond})
	if bt.C(term, 0) == nil {
		t.Fatal("no ticks for a moving backdrop in an application that draws on events")
	}
	if bt.C(term, 16*time.Millisecond) != nil {
		t.Fatal("ticking for a backdrop the application already draws faster than")
	}
	if bt.C(term, 100*time.Millisecond) == nil {
		t.Fatal("no ticks for a backdrop faster than the application")
	}
}

// The escape stream of a frame with a backdrop is an ordinary diff: the
// application's text reaches the terminal with the scene's colour behind it.
func TestBackdropReachesTheTerminalAsCells(t *testing.T) {
	term, io, now := backdropTerminal(t)
	term.SetBackdrop(stripes{interval: 50 * time.Millisecond})
	*now = now.Add(700 * time.Millisecond)
	if err := term.Draw(func(f *Frame) { f.Buffer.SetString(0, 0, "hi", cell.Style{}) }); err != nil {
		t.Fatal(err)
	}
	out := io.Output()
	if !bytes.Contains(out, []byte("\x1b[48;2;7;0;30mh\x1b[48;2;7;1;30mi")) {
		t.Fatalf("application text not sent over the scene's colours: %q", out)
	}
}

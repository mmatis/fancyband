package ui

import (
	"math"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
	"fancyband/internal/session"
)

var (
	floorTile = proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}
	wallTile  = proto.PictCell{FgRow: 0, FgCol: 4, BgRow: 0, BgCol: 4}
)

// tileRow returns a PICT of one row of tiles from x, two cells each.
func tileRow(x, y, n int, c proto.PictCell) []proto.Message {
	var msgs []proto.Message
	for i := 0; i < n; i++ {
		msgs = append(msgs, proto.Pict{Term: 0, X: uint16(x + 2*i), Y: uint16(y), Cells: []proto.PictCell{c}})
	}
	return msgs
}

// scrollModel returns a model with Adam Bolt's tiles on Kitty, showing a
// row of walls, then scrolled: the game says the map moves 4 cells left,
// and redraws it with floors.
func scrollModel(t *testing.T, method string) (*Model, *time.Time) {
	t.Helper()
	m, clock := slideModel(t)
	m.cfg.Protocol = "Kitty"
	m.settings.Scroll = method
	m.handleBatch(session.Batch{Msgs: append(tileRow(0, 5, 20, wallTile), proto.Fresh{Term: 0})})
	scroll := proto.Event{JSON: mustJSONForTest(map[string]any{
		"event": "SCROLL", "shift": []int{-4, 0}, "map": map[string]int{"x": 0, "y": 1, "w": 40, "h": 20},
	})}
	msgs := []proto.Message{scroll}
	msgs = append(msgs, tileRow(0, 5, 20, floorTile)...)
	m.handleBatch(session.Batch{Msgs: append(msgs, proto.Fresh{Term: 0})})
	if m.scroll == nil {
		t.Fatal("the redrawn map did not start a scroll")
	}
	return m, clock
}

func TestScrollPixels(t *testing.T) {
	m, clock := scrollModel(t, scrollPixel)
	regions := view(m)
	// Before its first frame the new map sits where the old one was: 4
	// cells right. The two old walls that scroll out sit in their places.
	walls, floors := 0, 0
	for _, r := range regions {
		if r.Clip.Width != 40 {
			t.Errorf("picture %+v not cut to the map", r.Area)
		}
		switch r.Img {
		case m.sheet.Picture(floorTile):
			floors++
			if r.ShiftX != 4 {
				t.Errorf("new floor at %d shifted %v, want 4", r.Area.X, r.ShiftX)
			}
		case m.sheet.Picture(wallTile):
			walls++
			if r.ShiftX != 0 || r.Area.X > 2 {
				t.Errorf("old wall at %d shifted %v", r.Area.X, r.ShiftX)
			}
		}
	}
	if floors != 20 || walls != 2 {
		t.Errorf("%d floors and %d walls, want 20 and the 2 scrolling out", floors, walls)
	}

	// The clock starts at the first frame; halfway, everything has moved
	// two cells.
	m.Update(motionTick{})
	*clock = clock.Add(scrollTime / 2)
	for _, r := range view(m) {
		if r.Img == m.sheet.Picture(floorTile) && math.Abs(r.ShiftX-2) > 1e-9 {
			t.Fatalf("halfway, a floor is shifted %v, want 2", r.ShiftX)
		}
	}
	*clock = clock.Add(scrollTime)
	m.Update(motionTick{})
	if m.scroll != nil {
		t.Error("the scroll did not end")
	}
}

// TestScrollLeavesCellsToPictures checks that while tiles scroll, every
// cell of the map holds Limoni's picture marker. The pictures sit under
// the text, so a blank with a background would hide them.
func TestScrollLeavesCellsToPictures(t *testing.T) {
	m, _ := scrollModel(t, scrollPixel)
	buf := buffer.NewBuffer(cell.NewRect(0, 0, 80, 24))
	m.View(terminal.NewFrame(buf, terminal.NewFocusManager()))
	main := m.layout.Main
	// Row 5 holds tiles; row 10 holds none on either screen.
	for _, y := range []int{5, 10} {
		for x := 0; x < 40; x++ {
			if r := buf.CellAt(uint16(main.X+x), uint16(main.Y+y)).Content; r != cell.RuneImage {
				t.Fatalf("map cell %d, %d holds %q while the map scrolls, not cell.RuneImage", x, y, r)
			}
		}
	}
}

func TestScrollWholeCells(t *testing.T) {
	m, clock := scrollModel(t, scrollCell)
	m.Update(motionTick{})
	*clock = clock.Add(scrollTime / 4)
	pieces := 0
	for _, r := range view(m) {
		if r.Area.Width == 1 {
			pieces++
			if r.Clip.Width != 0 {
				t.Error("a piece is cut to the pixel")
			}
		}
	}
	if pieces == 0 {
		t.Error("no tile at the edge was cut into cells")
	}
}

func TestScrollWholeMap(t *testing.T) {
	m, _ := scrollModel(t, scrollMap)
	regions := view(m)
	if len(regions) != 1 {
		t.Fatalf("got %d pictures, want the whole map as one", len(regions))
	}
	r := regions[0]
	if r.Area.Width != 44 || r.Area.Height != 20 || r.ShiftX != 0 || r.Clip.Width != 40 || !r.Transparent {
		t.Errorf("whole map %+v, want 44x20 cells, at the old screen's place, cut to the map", r)
	}
	if again := view(m); again[0].Img != r.Img {
		t.Error("the whole-map picture was made again")
	}
}

func TestScrollText(t *testing.T) {
	line := func(text string) *grid.Grid {
		var set grid.Set
		set.Apply(proto.TermOpen{Term: 0, Width: 10, Height: 1})
		set.Apply(proto.Text{Term: 0, X: 0, Y: 0, Text: text})
		set.Apply(proto.Fresh{Term: 0})
		return set.Terms[0]
	}
	// The map moves 4 cells left: "efghij" stays in sight and "WXYZ"
	// scrolls in.
	old, now := line("abcdefghij"), line("efghijWXYZ")
	ts := &textScroll{old: old, box: Box{0, 0, 10, 1}, shift: [2]int{-4, 0}}
	shown := func(off int) string {
		ts.off = [2]int{off, 0}
		var b []rune
		for x := 0; x < 10; x++ {
			c, _ := ts.cell(now, x, 0)
			b = append(b, c.Rune)
		}
		return string(b)
	}
	for off, want := range map[int]string{4: "abcdefghij", 2: "cdefghijWX", 0: "efghijWXYZ"} {
		if got := shown(off); got != want {
			t.Errorf("drawn %d cells from its place: %q, want %q", off, got, want)
		}
	}
	if _, ok := ts.cell(now, 0, 5); ok {
		t.Error("a cell below the map counts as map")
	}
}

func TestScrollSkipped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		proto  string
		busy   bool
	}{
		{"off", scrollOff, "Kitty", false},
		{"busy", scrollPixel, "Kitty", true},
		{"Sixel", scrollPixel, "Sixel", false},
	} {
		m, _ := slideModel(t)
		m.cfg.Protocol = tc.proto
		m.settings.Scroll = tc.method
		m.handleBatch(session.Batch{Msgs: []proto.Message{
			proto.Event{JSON: mustJSONForTest(map[string]any{
				"event": "SCROLL", "busy": tc.busy, "shift": []int{-4, 0}, "map": map[string]int{"x": 0, "y": 1, "w": 40, "h": 20},
			})},
			proto.Fresh{Term: 0},
		}})
		if m.scroll != nil || m.pendingScroll != nil {
			t.Errorf("%s: the map scrolled", tc.name)
		}
	}
}

func TestScrollEndsOnKey(t *testing.T) {
	m, _ := scrollModel(t, scrollPixel)
	if r := m.Update(engine.KeyPressMsg{Key: driver.KeyEvent{Type: driver.KeyRune, Ch: '6'}}); m.scroll != nil || !r.Redraw {
		t.Error("a key did not end the scroll")
	}
	for _, r := range view(m) {
		if r.ShiftX != 0 {
			t.Errorf("a picture is still shifted after the scroll ended: %+v", r.Area)
		}
	}
}

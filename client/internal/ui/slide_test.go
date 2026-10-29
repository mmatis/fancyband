package ui

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
	"fancyband/internal/session"
)

// slideModel returns a model with Adam Bolt's tiles, a floor at 20,5
// and the @ (tile 3,4) standing on the floor at 22,5, as just after it
// stepped right, and a clock the test moves.
func slideModel(t *testing.T) (*Model, *time.Time) {
	t.Helper()
	sheet, err := filepath.Abs("../../../engine/lib/tiles/adam-bolt/16x16.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sheet); err != nil {
		t.Skip("no engine tiles in this checkout")
	}
	m := New(Config{Width: 80, Height: 24, Tiles: 2})
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	m.handleBatch(session.Batch{Msgs: []proto.Message{
		proto.TermOpen{Term: 0, Width: 80, Height: 24},
		proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1, Name: "Adam Bolt's tiles", Path: sheet},
		proto.Pict{Term: 0, X: 20, Y: 5, Cells: []proto.PictCell{{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}}},
		proto.Pict{Term: 0, X: 22, Y: 5, Cells: []proto.PictCell{{FgRow: 3, FgCol: 4, BgRow: 0, BgCol: 1}}},
		proto.Fresh{Term: 0},
	}})
	return m, &clock
}

func movesEvent(busy bool, path ...[2]int) proto.Event {
	d := map[string]any{"event": "MOVES", "busy": busy, "moves": []any{map[string]any{"path": path}}}
	return proto.Event{JSON: mustJSONForTest(d)}
}

func view(m *Model) []terminal.ImageRegion {
	buf := buffer.NewBuffer(cell.NewRect(0, 0, 80, 24))
	f := terminal.NewFrame(buf, terminal.NewFocusManager())
	m.View(f)
	return f.ImageRegionsSnapshot()
}

func TestSlide(t *testing.T) {
	m, clock := slideModel(t)
	if !slices.Contains(m.caps(), "moves") {
		t.Errorf("caps %q lack moves", m.caps())
	}
	if !m.handleEvent(movesEvent(false, [2]int{20, 5}, [2]int{22, 5})) || len(m.slides) != 1 {
		t.Fatalf("a MOVES event started %d slides", len(m.slides))
	}
	if s := m.slides[0].tile; s.FgRow != 3 || s.FgCol != 4 {
		t.Errorf("sliding tile %+v, want the @'s", s)
	}

	// Halfway: the @ is a see-through picture one cell along, and the
	// cell it ends on shows only the floor.
	*clock = clock.Add(slideTime / 2)
	regions := view(m)
	var sprite *terminal.ImageRegion
	for i, r := range regions {
		if r.Track != 0 {
			sprite = &regions[i]
		}
	}
	if sprite == nil {
		t.Fatal("no sliding picture")
	}
	if sprite.Area.X != 20 || sprite.Area.Y != 5 || math.Abs(sprite.ShiftX-1) > 0.01 || sprite.ShiftY != 0 || !sprite.Transparent || sprite.ZIndex <= 0 {
		t.Errorf("sliding picture %+v, want it from 20,5 shifted one cell, see-through, over the map", *sprite)
	}
	floor := m.sheet.Picture(proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1})
	for _, r := range regions {
		if r.Track == 0 && r.Area.X == 22 && r.Img != floor {
			t.Error("the cell the @ ends on still shows the @")
		}
	}

	// Its time up, the slide ends and the @ is drawn in its cell again.
	*clock = clock.Add(slideTime)
	if r := m.Update(motionTick{}); !r.Redraw || len(m.slides) != 0 {
		t.Errorf("the slide did not end: %d slides, redraw %v", len(m.slides), r.Redraw)
	}
	for _, r := range view(m) {
		if r.Track != 0 {
			t.Error("a sliding picture is left after the slide")
		}
	}
}

func TestSlideEndsOnKey(t *testing.T) {
	m, _ := slideModel(t)
	m.handleEvent(movesEvent(false, [2]int{20, 5}, [2]int{22, 5}))
	r := m.Update(engine.KeyPressMsg{Key: driver.KeyEvent{Type: driver.KeyRune, Ch: '6'}})
	if len(m.slides) != 0 || !r.Redraw {
		t.Errorf("a key left %d slides, redraw %v", len(m.slides), r.Redraw)
	}
}

func TestSlideSkipped(t *testing.T) {
	m, _ := slideModel(t)
	if m.handleEvent(movesEvent(true, [2]int{20, 5}, [2]int{22, 5})); len(m.slides) != 0 {
		t.Error("a busy MOVES event, as in a run, started a slide")
	}
	off := false
	m.settings.Slide = &off
	if m.handleEvent(movesEvent(false, [2]int{20, 5}, [2]int{22, 5})); len(m.slides) != 0 {
		t.Error("a slide started with smooth movement off")
	}
	m.settings.Slide = nil
	if m.handleEvent(movesEvent(false, [2]int{22, 5}, [2]int{20, 5})); len(m.slides) != 0 {
		t.Error("a slide started for a creature the map does not show at its end")
	}
}

func TestSlideAlongPath(t *testing.T) {
	s := slide{path: [][2]int{{0, 0}, {2, 0}, {2, 1}}}
	for _, tc := range []struct {
		t    float64
		x, y float64
	}{{0, 0, 0}, {0.5, 2, 0}, {1, 2, 1}} {
		if x, y := s.at(tc.t); math.Abs(x-tc.x) > 1e-9 || math.Abs(y-tc.y) > 1e-9 {
			t.Errorf("at %v: %v,%v, want %v,%v", tc.t, x, y, tc.x, tc.y)
		}
	}
}

func mustJSONForTest(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

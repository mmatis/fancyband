package ui

import (
	"testing"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

func TestLavaStep(t *testing.T) {
	seen := map[int]bool{}
	for tick := 0; tick < 2*tiles.ShiftSteps; tick++ {
		s := lavaStep(tick, 3, 4)
		if s < 0 || s >= tiles.ShiftSteps {
			t.Fatalf("step %d out of range", s)
		}
		seen[s] = true
		// Neighbors along a diagonal are one tick apart.
		if lavaStep(tick, 4, 4) != lavaStep(tick+1, 3, 4) {
			t.Errorf("tick %d: the wave does not ripple", tick)
		}
	}
	if len(seen) != tiles.ShiftSteps {
		t.Errorf("a full swing visits %d steps, want %d", len(seen), tiles.ShiftSteps)
	}
}

func TestLavaColor(t *testing.T) {
	p := newPalette([]proto.RGB{4: {R: 192}, 11: {R: 255, G: 255}})
	if r, g, b := p.lavaColor(0).RGB(); r != 192 || g != 0 || b != 0 {
		t.Errorf("step 0 = %d,%d,%d, want the palette's red", r, g, b)
	}
	if r, g, b := p.lavaColor(tiles.ShiftSteps - 1).RGB(); r != 255 || g != 255 || b != 0 {
		t.Errorf("last step = %d,%d,%d, want the palette's yellow", r, g, b)
	}
}

// TestTextLava checks that a red '#' changes color with the wave and a
// white '#' does not.
func TestTextLava(t *testing.T) {
	var set grid.Set
	set.Apply(proto.TermOpen{Term: 0, Width: 10, Height: 1})
	set.Apply(proto.Text{Term: 0, X: 0, Y: 0, Attr: colorRed, Text: "##"})
	set.Apply(proto.Text{Term: 0, X: 2, Y: 0, Attr: 1, Text: "#"})
	set.Apply(proto.Fresh{Term: 0})
	g := set.Terms[0]

	m := New(Config{Width: 80, Height: 24, Animate: true})
	if !m.hasLava(g) {
		t.Error("hasLava missed the red '#'")
	}
	pal := newPalette([]proto.RGB{4: {R: 192}, 11: {R: 255, G: 255}})
	draw := func(tick int) *buffer.Buffer {
		buf := buffer.NewBuffer(cell.NewRect(0, 0, 10, 1))
		v := &termView{grid: g, pal: pal, lava: true, lavaTick: tick}
		v.Draw(cell.Context{Area: buf.Area}, buf)
		return buf
	}
	a, b := draw(0), draw(3)
	if a.CellAt(0, 0).Style.Fg == b.CellAt(0, 0).Style.Fg {
		t.Error("lava kept its color across ticks")
	}
	if a.CellAt(2, 0).Style.Fg != b.CellAt(2, 0).Style.Fg {
		t.Error("a white '#' changed color")
	}
}

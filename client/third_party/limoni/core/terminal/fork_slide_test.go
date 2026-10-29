package terminal

import (
	"image"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/cell"
)

// Fancyband fork: tests for shifted and tracked pictures.

func TestShifted(t *testing.T) {
	area := cell.NewRect(10, 5, 2, 1)
	for _, tc := range []struct {
		sx, sy           float64
		x, y, offX, offY int
	}{
		{0, 0, 10, 5, 0, 0},
		{0.5, 0, 10, 5, 5, 0},
		{2.25, 0, 12, 5, 3, 0},
		{0, 1.5, 10, 6, 0, 10},
		{-0.25, -0.5, 9, 4, 7, 10},
	} {
		x, y, ox, oy := shifted(ImageRegion{Area: area, ShiftX: tc.sx, ShiftY: tc.sy}, 10, 20)
		if x != tc.x || y != tc.y || ox != tc.offX || oy != tc.offY {
			t.Errorf("shift %v,%v: cell %d,%d offset %d,%d; want %d,%d offset %d,%d",
				tc.sx, tc.sy, x, y, ox, oy, tc.x, tc.y, tc.offX, tc.offY)
		}
	}
}

func TestKittyTrackMoves(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var k kittyImages
	reg := ImageRegion{Area: cell.NewRect(10, 5, 2, 1), Img: img, Transparent: true, ZIndex: 1, Track: 9}
	k.place(nil, []ImageRegion{reg}, 10, 20, false)
	reg.ShiftX = 1.3
	out := string(k.place(nil, []ImageRegion{reg}, 10, 20, false))
	if !strings.Contains(out, "X=3") || strings.Contains(out, "a=d") {
		t.Errorf("moving a tracked picture sent %q; want one placement with X=3 and no deletion", out)
	}
	if !strings.Contains(out, "p=1") || len(k.placed) != 1 {
		t.Errorf("the tracked picture did not keep its placement: %q, %d placements", out, len(k.placed))
	}
	if again := k.place(nil, []ImageRegion{reg}, 10, 20, false); len(again) != 0 {
		t.Errorf("an unmoved picture sent %q", again)
	}
}

func TestWholeCellShifts(t *testing.T) {
	plain := ImageRegion{Area: cell.NewRect(1, 1, 2, 1)}
	moved := ImageRegion{Area: cell.NewRect(4, 4, 2, 1), ShiftX: 1.6, ShiftY: -0.4}
	regs := []ImageRegion{plain}
	if got := wholeCellShifts(regs); &got[0] != &regs[0] {
		t.Error("regions with no shift were copied")
	}
	got := wholeCellShifts([]ImageRegion{plain, moved})
	if len(got) != 2 || got[0] != plain || got[1].Area != cell.NewRect(6, 4, 2, 1) || got[1].ShiftX != 0 {
		t.Errorf("got %+v", got)
	}
}

// Fancyband fork: a shifted picture is cut at its Clip.
func TestKittyClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20)) // the size of its 4×1 cells
	clip := cell.NewRect(10, 0, 20, 5)
	reg := ImageRegion{Area: cell.NewRect(10, 2, 4, 1), Img: img, Clip: clip}

	// A quarter of a cell past the left edge: shown at its own size,
	// from a copy sent at its cells' size, 37 pixels of it.
	var k kittyImages
	reg.ShiftX = -0.25
	out := string(k.place(nil, []ImageRegion{reg}, 10, 20, false))
	if !strings.Contains(out, "s=40,v=20") || !strings.Contains(out, "x=3,y=0,w=37,h=20") || strings.Contains(out, ",c=") {
		t.Errorf("a picture cut within a cell sent %q", out)
	}

	// A whole cell past the edge: scaled to the three cells left.
	k = kittyImages{}
	reg.ShiftX = -1
	out = string(k.place(nil, []ImageRegion{reg}, 10, 20, false))
	if !strings.Contains(out, "c=3,r=1") || !strings.Contains(out, "x=10,y=0,w=30,h=20") {
		t.Errorf("a picture cut on a cell sent %q", out)
	}

	// Wholly outside: not shown.
	k = kittyImages{}
	reg.ShiftX = -5
	if out := string(k.place(nil, []ImageRegion{reg}, 10, 20, false)); strings.Contains(out, "a=p") {
		t.Errorf("a picture outside its clip was placed: %q", out)
	}

	// Inside: placed as usual.
	k = kittyImages{}
	reg.ShiftX = 0.5
	if out := string(k.place(nil, []ImageRegion{reg}, 10, 20, false)); !strings.Contains(out, "c=4,r=1") || !strings.Contains(out, "X=5") {
		t.Errorf("a picture inside its clip sent %q", out)
	}
}

func TestWholeCellShiftsClip(t *testing.T) {
	clip := cell.NewRect(0, 0, 10, 5)
	in := ImageRegion{Area: cell.NewRect(2, 2, 2, 1), ShiftX: 1, Clip: clip}
	out := ImageRegion{Area: cell.NewRect(8, 2, 2, 1), ShiftX: 1, Clip: clip}
	if got := wholeCellShifts([]ImageRegion{in, out}); len(got) != 1 || got[0].Area.X != 3 {
		t.Errorf("got %+v, want only the picture that fits, moved a cell", got)
	}
}

package terminal

// Fancyband fork: tests for placing only the pictures that changed.

import (
	"image"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/graphics"
)

// tileFrames draws frames of small pictures, one per two-cell tile, the way
// a tile map does, and returns what each frame sent.
func tileFrames(t *testing.T, term *Terminal, out func() []byte, frames ...[]image.Image) []string {
	t.Helper()
	var sent []string
	for _, tiles := range frames {
		before := len(out())
		err := term.Draw(func(f *Frame) {
			for i, img := range tiles {
				area := cell.NewRect(uint16(2*i), 1, 2, 1)
				for x := area.X; x < area.X+area.Width; x++ {
					f.Buffer.SetCell(x, 1, cell.Cell{Content: cell.RuneImage})
				}
				f.RegisterImageRegion(ImageRegion{Area: area, Img: img})
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		sent = append(sent, string(out()[before:]))
	}
	return sent
}

func TestKittyChangesOnlyTheTilesThatChanged(t *testing.T) {
	term, io := kittyTerminal(t)
	a, b, c := picture(16, 16), picture(16, 16), picture(16, 16)
	c.Pix[0] = 1 // a picture of its own
	sent := tileFrames(t, term, io.Output,
		[]image.Image{a, b, a},
		[]image.Image{a, c, a}, // the middle tile changes
		[]image.Image{a, c},    // the last tile goes
	)

	if n := strings.Count(sent[0], "a=p,"); n != 3 {
		t.Fatalf("first frame made %d placements, want 3", n)
	}
	if strings.Contains(sent[1], "d=a") || strings.Contains(sent[2], "d=a") {
		t.Fatal("a frame deleted every placement")
	}
	if n := strings.Count(sent[1], "a=p,"); n != 1 {
		t.Errorf("changing one tile made %d placements, want 1: %q", n, sent[1])
	}
	if !strings.Contains(sent[1], "a=d,d=i,") {
		t.Errorf("the changed tile's old placement was not deleted: %q", sent[1])
	}
	if n := strings.Count(sent[1], "a=t,"); n != 1 {
		t.Errorf("changing one tile sent %d pictures, want only the new one", n)
	}
	if strings.Contains(sent[2], "a=p,") || !strings.Contains(sent[2], "a=d,d=i,") {
		t.Errorf("removing a tile should only delete its placement: %q", sent[2])
	}
	// b is no longer on screen anywhere, so kitty frees it.
	if !strings.Contains(sent[1], "d=I") {
		t.Errorf("a picture no longer shown was not freed: %q", sent[1])
	}
}

func TestKittyRedrawsEveryTileAfterAFullRedraw(t *testing.T) {
	term, io := kittyTerminal(t)
	a := picture(16, 16)
	tileFrames(t, term, io.Output, []image.Image{a, a})
	term.ForceFullRedraw()
	sent := tileFrames(t, term, io.Output, []image.Image{a, a})
	if n := strings.Count(sent[0], "a=p,"); n != 2 {
		t.Errorf("after a full redraw %d tiles were placed, want 2", n)
	}
}

func TestSixelSendsOnlyTheTilesThatChanged(t *testing.T) {
	term, io := kittyTerminal(t)
	caps := term.Capabilities()
	caps.GraphicsProto = graphics.ProtocolSixel
	term.SetCapabilities(caps)
	a, b := picture(16, 16), picture(16, 16)
	sent := tileFrames(t, term, io.Output,
		[]image.Image{a, a, a},
		[]image.Image{a, b, a},
	)
	if n := strings.Count(sent[0], "\x1bP"); n != 3 {
		t.Fatalf("first frame sent %d Sixel pictures, want 3", n)
	}
	if n := strings.Count(sent[1], "\x1bP"); n != 1 {
		t.Errorf("changing one tile sent %d Sixel pictures, want 1", n)
	}
}

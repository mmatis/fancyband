package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
	"fancyband/internal/session"
)

func TestTilesClaimed(t *testing.T) {
	m := New(Config{Width: 80, Height: 24, Tiles: 2})
	caps := m.caps()
	if len(caps) != 2 || caps[1] != "tiles:2" {
		t.Errorf("caps = %q", caps)
	}
	if caps := New(Config{Width: 80, Height: 24}).caps(); len(caps) != 1 {
		t.Errorf("caps without tiles = %q", caps)
	}
}

// TestPlaceTiles feeds the model a real tile set and checks that View
// places one picture per tile, two cells wide, and leaves those cells
// blank.
func TestPlaceTiles(t *testing.T) {
	sheet, err := filepath.Abs("../../../engine/lib/tiles/adam-bolt/16x16.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sheet); err != nil {
		t.Skip("no engine tiles in this checkout")
	}
	m := New(Config{Width: 80, Height: 24, Tiles: 2})
	m.handleBatch(session.Batch{Msgs: []proto.Message{
		proto.TermOpen{Term: 0, Width: 80, Height: 24},
		proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1, Name: "Adam Bolt's tiles", Path: sheet},
		proto.Text{Term: 0, X: 0, Y: 1, Text: "Human"},
		proto.Pict{Term: 0, X: 20, Y: 5, Cells: []proto.PictCell{{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}}},
		proto.Pict{Term: 0, X: 22, Y: 5, Cells: []proto.PictCell{{FgRow: 3, FgCol: 4, BgRow: 0, BgCol: 1}}},
		// A tile that would run off the right edge is left out.
		proto.Pict{Term: 0, X: 79, Y: 5, Cells: []proto.PictCell{{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}}},
		proto.Fresh{Term: 0},
	}})
	if m.sheet == nil {
		t.Fatal("the model did not load the tile set")
	}

	buf := buffer.NewBuffer(cell.NewRect(0, 0, 80, 24))
	f := terminal.NewFrame(buf, terminal.NewFocusManager())
	m.View(f)
	regions := f.ImageRegionsSnapshot()
	if len(regions) != 2 {
		t.Fatalf("got %d pictures, want 2", len(regions))
	}
	if a := regions[0].Area; a.X != 20 || a.Y != 5 || a.Width != 2 || a.Height != 1 {
		t.Errorf("first picture at %+v", a)
	}
	if regions[0].Img == regions[1].Img {
		t.Error("two different tiles share a picture")
	}
	// Cells under pictures hold Limoni's marker, so it never paints text
	// over them.
	for x := uint16(20); x < 24; x++ {
		if r := buf.CellAt(x, 5).Content; r != cell.RuneImage {
			t.Errorf("cell %d under a tile holds %q, not cell.RuneImage", x, r)
		}
	}
	if r := buf.CellAt(0, 1).Content; r != 'H' {
		t.Errorf("text beside the tiles shows %q", r)
	}
}

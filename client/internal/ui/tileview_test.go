package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
	"fancyband/internal/session"
	"fancyband/internal/tiles"
)

func TestTilesClaimed(t *testing.T) {
	m := New(Config{Width: 80, Height: 24, Tiles: 2})
	caps := m.caps()
	if !slices.Contains(caps, "tiles:2") {
		t.Errorf("caps = %q", caps)
	}
	if caps := New(Config{Width: 80, Height: 24}).caps(); slices.ContainsFunc(caps, func(c string) bool { return strings.HasPrefix(c, "tiles:") }) {
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

// TestSaveListTiles opens the load screen in tile mode and checks that
// each character gets its race and class's tile, and that the map's
// tiles under the screen are left out.
func TestSaveListTiles(t *testing.T) {
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
		// A map tile under the middle of the screen.
		proto.Pict{Term: 0, X: 40, Y: 12, Cells: []proto.PictCell{{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}}},
		proto.Fresh{Term: 0},
	}})
	m.saves = []proto.SaveData{
		{Name: "Ann", Description: "Ann, L3 Elf Mage, at DL2", Race: "Elf", Class: "Mage"},
		{Name: "Bob", Description: "Bob, L9 Human Warrior, at DL5", Race: "Human", Class: "Warrior"},
	}
	m.openSaveList(false)
	// The list opens when SAVEFILES is answered.
	m.pending[m.nextID](proto.EventData{OK: true})
	if len(m.stack) != 1 {
		t.Fatal("no load screen")
	}

	buf := buffer.NewBuffer(cell.NewRect(0, 0, 80, 24))
	f := terminal.NewFrame(buf, terminal.NewFocusManager())
	m.View(f)
	regions := f.ImageRegionsSnapshot()
	if len(regions) != 2 {
		t.Fatalf("got %d pictures, want one for each character and none from the map", len(regions))
	}
	if regions[0].Img == regions[1].Img {
		t.Error("an Elf Mage and a Human Warrior share a tile")
	}
	// Bob's row is not selected, so his sprite sits on the screen's
	// background, with no black around it.
	want, _ := tiles.PlayerTile(*m.terms.TileSet, "Human", "Warrior")
	r, g, b := screenBg.Bg.RGB()
	bg := color.RGBA{r, g, b, 255}
	if regions[1].Img != m.sheet.Sprite(want.FgRow, want.FgCol, bg) {
		t.Error("Bob does not have the Human Warrior tile on the screen's background")
	}
	if got := color.RGBAModel.Convert(regions[1].Img.At(0, 0)); got != bg {
		t.Errorf("the corner of Bob's tile is %v, want the background %v", got, bg)
	}
	for _, r := range regions {
		if r.Area.Width != 2 || r.Area.Height != 1 {
			t.Errorf("a character's tile covers %+v, want 2 cells by 1", r.Area)
		}
	}
}

package ui

import (
	"image"
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
	caps := m.Caps()
	if !slices.Contains(caps, "tiles:2") {
		t.Errorf("caps = %q", caps)
	}
	if caps := New(Config{Width: 80, Height: 24}).Caps(); slices.ContainsFunc(caps, func(c string) bool { return strings.HasPrefix(c, "tiles:") }) {
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
		// A tile that runs off the right edge shows its first cell.
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
	if len(regions) != 3 {
		t.Fatalf("got %d pictures, want 2 tiles and the visible half of a third", len(regions))
	}
	if a := regions[2].Area; a.X != 79 || a.Y != 5 || a.Width != 1 || a.Height != 1 {
		t.Errorf("the half tile at the edge is at %+v", a)
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
	if r := buf.CellAt(79, 5).Content; r != cell.RuneImage {
		t.Errorf("the half tile's cell holds %q, not cell.RuneImage", r)
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

	// A map tile across the screen's left edge, its first cell outside.
	sb := m.stack[0].box(Box{0, 0, 80, 24})
	m.handleBatch(session.Batch{Msgs: []proto.Message{
		proto.Pict{Term: 0, X: uint16(sb.X - 1), Y: uint16(sb.Y + 2), Cells: []proto.PictCell{{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}}},
		proto.Fresh{Term: 0},
	}})

	buf := buffer.NewBuffer(cell.NewRect(0, 0, 80, 24))
	f := terminal.NewFrame(buf, terminal.NewFocusManager())
	m.View(f)
	// The map's pictures come first: only the half of the tile across
	// the screen's edge that is outside it. Then a tile for each
	// character.
	all := f.ImageRegionsSnapshot()
	if len(all) != 3 {
		t.Fatalf("got %d pictures, want half a map tile and one for each character", len(all))
	}
	if a := all[0].Area; int(a.X) != sb.X-1 || int(a.Y) != sb.Y+2 || a.Width != 1 || a.Height != 1 {
		t.Errorf("the map tile beside the screen shows at %+v, want its outside cell only", a)
	}
	if all[0].Img.Bounds().Dx() != 8 {
		t.Errorf("the outside cell's picture is %d pixels wide, want half of 16", all[0].Img.Bounds().Dx())
	}
	if buf.CellAt(uint16(sb.X-1), uint16(sb.Y+2)).Content != cell.RuneImage {
		t.Error("the outside cell of the tile beside the screen has no picture")
	}
	regions := all[1:]
	if regions[0].Img == regions[1].Img {
		t.Error("an Elf Mage and a Human Warrior share a tile")
	}
	// The map tile under the screen is left out, and its cells are not
	// left for a picture: around the screen's edge that showed as a
	// border in the terminal's background color.
	for x := uint16(40); x < 42; x++ {
		if buf.CellAt(x, 12).Content == cell.RuneImage {
			t.Errorf("cell %d,12 under the screen is left for a picture", x)
		}
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

// TestScaleFits checks which tile sizes leave enough of the map in view.
func TestScaleFits(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	m.terms.TileSet = &proto.TileSet{ID: 2, CellsW: 2, CellsH: 1}
	if !m.scaleFits(2) || m.scaleFits(3) || m.fittingScale(3) != 2 {
		t.Errorf("80x24: size 2 fits %v, size 3 fits %v, fitting %d", m.scaleFits(2), m.scaleFits(3), m.fittingScale(3))
	}
	m.layout = Compute(160, 50, false, false, false)
	if m.fittingScale(3) != 3 {
		t.Errorf("160x50: fitting size %d, want 3", m.fittingScale(3))
	}
	// At size 2 the engine reports 4x2 cells; the size-1 width is still 2.
	m.terms.TileSet = &proto.TileSet{ID: 2, CellsW: 4, CellsH: 2}
	if m.baseCells() != 2 {
		t.Errorf("base cells %d at size 2, want 2", m.baseCells())
	}
}

// TestWallShadows checks that a floor tile under a wall gets a shaded
// picture, that open floor keeps its own, and that the setting turns
// shadows off.
func TestWallShadows(t *testing.T) {
	sheet, err := filepath.Abs("../../../engine/lib/tiles/adam-bolt/16x16.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sheet); err != nil {
		t.Skip("no engine tiles in this checkout")
	}
	floor := proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}
	wall := proto.PictCell{FgRow: 0, FgCol: 4, BgRow: 0, BgCol: 4}
	m := New(Config{Width: 80, Height: 24, Tiles: 2})
	m.handleBatch(session.Batch{Msgs: []proto.Message{
		proto.TermOpen{Term: 0, Width: 80, Height: 24},
		proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1, Name: "Adam Bolt's tiles", Path: sheet},
		proto.Pict{Term: 0, X: 20, Y: 4, Cells: []proto.PictCell{wall}},
		proto.Pict{Term: 0, X: 20, Y: 5, Cells: []proto.PictCell{floor}},
		proto.Pict{Term: 0, X: 40, Y: 10, Cells: []proto.PictCell{floor}},
		proto.Fresh{Term: 0},
	}})
	g := m.terms.Terms[0]
	if mask := m.shadowMask(g, 20, 5); mask != tiles.ShadowN {
		t.Errorf("floor under a wall has mask %08b, want north", mask)
	}
	under, open := m.tilePicture(g, 20, 5, floor), m.tilePicture(g, 40, 10, floor)
	if under == open || open != m.sheet.Picture(floor) {
		t.Error("the floor under the wall is not shaded, or open floor is")
	}
	if m.tilePicture(g, 20, 4, wall) != m.sheet.Picture(wall) {
		t.Error("the wall itself is shaded")
	}
	off := false
	m.settings.Shadows = &off
	if m.tilePicture(g, 20, 5, floor) != open {
		t.Error("shadows still show with the setting off")
	}
}

func TestEquippyRows(t *testing.T) {
	gear := make([]*proto.Glyph, 12)
	for i := range gear {
		if i != 3 {
			gear[i] = &proto.Glyph{Char: "|", Color: 1, Tile: &proto.TileSpot{Row: 1, Col: 2}}
		}
	}
	style := func(int) cell.Style { return cell.NewStyle() }
	sprite := func(row, col uint8, bg color.RGBA) image.Image { return image.NewRGBA(image.Rect(0, 0, 1, 1)) }
	if rows := equippyRows(gear, style, sprite, 22, nil); len(rows) != 2 {
		t.Errorf("12 tiles in 22 columns take %d rows, want 2", len(rows))
	}
	if rows := equippyRows(gear, style, nil, 22, nil); len(rows) != 1 {
		t.Errorf("letters take %d rows, want 1", len(rows))
	}

	buf := buffer.NewBuffer(cell.NewRect(0, 0, 30, 3))
	f := terminal.NewFrame(buf, terminal.NewFocusManager())
	equippyRows(gear, style, sprite, 22, nil)[0].draw(f, 1, 0, 22)
	// Six slots on the first row, one of them empty.
	if n := len(f.ImageRegionsSnapshot()); n != 5 {
		t.Errorf("the first row has %d tiles, want 5", n)
	}
	if r := buf.CellAt(1+3*gearTileCells, 0).Content; r == cell.RuneImage {
		t.Error("the empty slot holds a picture marker")
	}
}

// TestGearTransparent checks that the gear tiles keep their see-through
// parts, so the terminal's background shows through them, and that the
// stats bar keeps the terminal's background too.
func TestGearTransparent(t *testing.T) {
	gear := []*proto.Glyph{{Char: "|", Color: 1, Tile: &proto.TileSpot{Row: 1, Col: 2}}}
	st := &proto.StatusData{Name: "Tester", Equippy: gear}
	var got []color.RGBA
	sprite := func(row, col uint8, bg color.RGBA) image.Image {
		got = append(got, bg)
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	m := New(Config{Width: 130, Height: 36})
	buf := buffer.NewBuffer(cell.NewRect(0, 0, 130, 36))
	f := terminal.NewFrame(buf, terminal.NewFocusManager())
	drawStats(f, m.computeLayout(130, 36, true).Stats, st, newPalette(nil), sprite, nil)
	if len(got) != 1 || got[0].A != 0 {
		t.Errorf("the tile was drawn on %v, want a see-through background", got)
	}
	regions := f.ImageRegionsSnapshot()
	if len(regions) != 1 || !regions[0].Transparent {
		t.Errorf("gear pictures %+v, want one placed with transparency", regions)
	}
	if c := buf.CellAt(1, 0).Style.Bg; c != cell.NewColorDefault() {
		t.Errorf("the stats bar has background %v, not the terminal's", c)
	}
}

// TestSharpPictures places tiles with the cell size known: each picture goes
// to Limoni at its cells' exact size in pixels with no new colors, and is
// the same value on the next frame, so it is not sent again.
func TestSharpPictures(t *testing.T) {
	sheet, err := filepath.Abs("../../../engine/lib/tiles/adam-bolt/16x16.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sheet); err != nil {
		t.Skip("no engine tiles in this checkout")
	}
	build := func(cellPixels func() (uint16, uint16, error)) []terminal.ImageRegion {
		m := New(Config{Width: 80, Height: 24, Tiles: 2, cellPixels: cellPixels})
		m.handleBatch(session.Batch{Msgs: []proto.Message{
			proto.TermOpen{Term: 0, Width: 80, Height: 24},
			proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1, Name: "Adam Bolt's tiles", Path: sheet},
			proto.Pict{Term: 0, X: 20, Y: 5, Cells: []proto.PictCell{{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}}},
			proto.Fresh{Term: 0},
		}})
		first := view(m)
		second := view(m)
		if len(first) != 1 || len(second) != 1 {
			t.Fatalf("%d and %d pictures, want 1", len(first), len(second))
		}
		if first[0].Img != second[0].Img {
			t.Error("the picture changed between frames, so it would be sent again")
		}
		return first
	}
	plain := build(nil)[0].Img
	if b := plain.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Fatalf("with no cell size the picture is %v, want the tile's own size", b)
	}
	palette := func(img image.Image) map[color.RGBA]bool {
		seen := map[color.RGBA]bool{}
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				seen[color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)] = true
			}
		}
		return seen
	}
	want := palette(plain)
	for _, c := range [][2]uint16{{9, 18}, {10, 20}, {16, 32}} {
		r := build(func() (uint16, uint16, error) { return c[0], c[1], nil })[0]
		w, h := 2*int(c[0]), int(c[1])
		if b := r.Img.Bounds(); b.Dx() != w || b.Dy() != h {
			t.Errorf("cells of %v: the picture is %v, want %dx%d", c, b, w, h)
		}
		for col := range palette(r.Img) {
			if !want[col] {
				t.Errorf("cells of %v: new color %v", c, col)
				break
			}
		}
	}
}

// TestSharpLeavesLargePictures checks that a picture larger than its cells
// is left for Limoni to shrink.
func TestSharpLeavesLargePictures(t *testing.T) {
	m := New(Config{Width: 80, Height: 24, cellPixels: func() (uint16, uint16, error) { return 8, 16, nil }})
	view(m)
	big := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if got := m.sharp(big, 2, 1); got != image.Image(big) {
		t.Error("a picture larger than its cells was changed")
	}
	exact := image.NewRGBA(image.Rect(0, 0, 16, 16))
	if got := m.sharp(exact, 2, 1); got != image.Image(exact) {
		t.Error("a picture that fits its cells was changed")
	}
}

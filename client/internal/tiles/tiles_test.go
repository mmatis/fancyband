package tiles

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fancyband/internal/proto"
)

// writeSheet makes a 2x2 sheet of 2x2-pixel tiles. Tile (0,0) is red,
// (0,1) green, (1,0) blue, and (1,1) is see-through with one white
// pixel at its top left.
func writeSheet(t *testing.T) string {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	fill := func(x0, y0 int, c color.NRGBA) {
		for y := y0; y < y0+2; y++ {
			for x := x0; x < x0+2; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
	}
	fill(0, 0, color.NRGBA{255, 0, 0, 255})
	fill(2, 0, color.NRGBA{0, 255, 0, 255})
	fill(0, 2, color.NRGBA{0, 0, 255, 255})
	img.SetNRGBA(2, 2, color.NRGBA{255, 255, 255, 255})
	path := filepath.Join(t.TempDir(), "sheet.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func rgba(img image.Image, x, y int) [4]uint32 {
	r, g, b, a := img.At(x, y).RGBA()
	return [4]uint32{r >> 8, g >> 8, b >> 8, a >> 8}
}

func TestPicture(t *testing.T) {
	path := writeSheet(t)
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, Alpha: true, Path: path})
	if err != nil {
		t.Fatal(err)
	}

	// Terrain alone: the green tile.
	green := s.Picture(proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1})
	if got := rgba(green, 1, 1); got != [4]uint32{0, 255, 0, 255} {
		t.Errorf("terrain pixel = %v", got)
	}

	// The see-through tile over blue: white where it has a pixel, blue
	// elsewhere.
	over := s.Picture(proto.PictCell{FgRow: 1, FgCol: 1, BgRow: 1, BgCol: 0})
	if got := rgba(over, 0, 0); got != [4]uint32{255, 255, 255, 255} {
		t.Errorf("foreground pixel = %v", got)
	}
	if got := rgba(over, 1, 1); got != [4]uint32{0, 0, 255, 255} {
		t.Errorf("terrain under a see-through pixel = %v", got)
	}

	// The same cell gives the same picture, so Limoni sends it once.
	again := s.Picture(proto.PictCell{FgRow: 1, FgCol: 1, BgRow: 1, BgCol: 0})
	if again != over || s.Len() != 2 {
		t.Errorf("the cache did not keep the picture (%d cached)", s.Len())
	}
}

// TestBlendsWithoutAlphaFlag checks that a set not flagged for alpha
// blending still shows the terrain under a tile's see-through pixels,
// as the SDL2 front end does, and that nothing stays see-through.
func TestBlendsWithoutAlphaFlag(t *testing.T) {
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, Path: writeSheet(t)})
	if err != nil {
		t.Fatal(err)
	}
	// The see-through tile over red: red shows where it has no pixel.
	p := s.Picture(proto.PictCell{FgRow: 1, FgCol: 1, BgRow: 0, BgCol: 0})
	if got := rgba(p, 1, 1); got != [4]uint32{255, 0, 0, 255} {
		t.Errorf("terrain under a see-through pixel = %v, want red", got)
	}
	// The see-through tile alone: black, not see-through.
	p = s.Picture(proto.PictCell{FgRow: 1, FgCol: 1, BgRow: 1, BgCol: 1})
	if got := rgba(p, 1, 1); got != [4]uint32{0, 0, 0, 255} {
		t.Errorf("an empty pixel = %v, want opaque black", got)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(proto.TileSet{TileW: 2, TileH: 2, Path: "/no/such/sheet.png"}); err == nil {
		t.Error("loaded a missing sheet")
	}
	if _, err := Load(proto.TileSet{Path: writeSheet(t)}); err == nil {
		t.Error("loaded a zero tile size")
	}
}

// TestRealSheets loads every sheet the engine ships, when the checkout
// has them.
func TestRealSheets(t *testing.T) {
	sets := []proto.TileSet{
		{Name: "old", TileW: 8, TileH: 8, Path: "../../../engine/lib/tiles/old/8x8.png"},
		{Name: "adam-bolt", TileW: 16, TileH: 16, Path: "../../../engine/lib/tiles/adam-bolt/16x16.png"},
		{Name: "gervais", TileW: 32, TileH: 32, Path: "../../../engine/lib/tiles/gervais/32x32.png"},
		{Name: "nomad", TileW: 8, TileH: 16, Path: "../../../engine/lib/tiles/nomad/8x16.png"},
		{Name: "shockbolt", TileW: 64, TileH: 64, Path: "../../../engine/lib/tiles/shockbolt/64x64.png"},
	}
	for _, ts := range sets {
		if _, err := os.Stat(ts.Path); err != nil {
			t.Skip("no engine tiles in this checkout")
		}
		s, err := Load(ts)
		if err != nil {
			t.Errorf("%s: %v", ts.Name, err)
			continue
		}
		if b := s.Picture(proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}).Bounds(); b.Dx() != int(ts.TileW) || b.Dy() != int(ts.TileH) {
			t.Errorf("%s: picture is %v", ts.Name, b)
		}
	}
}

// TestFlip mirrors a creature's tile and leaves the terrain under it alone,
// and a cell showing only terrain has the same picture either way.
func TestFlip(t *testing.T) {
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, CellsW: 2, CellsH: 1, Alpha: true, Path: writeSheet(t)})
	if err != nil {
		t.Fatal(err)
	}
	// Tile (1,1) has one white pixel at its top left, over red terrain.
	white, red := color.RGBA{255, 255, 255, 255}, color.RGBA{255, 0, 0, 255}
	at := func(img image.Image, x, y int) color.RGBA { return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) }

	c := proto.PictCell{FgRow: 1, FgCol: 1}
	right := s.Picture(c)
	if at(right, 0, 0) != white || at(right, 1, 0) != red {
		t.Errorf("facing right: %v, %v", at(right, 0, 0), at(right, 1, 0))
	}
	c.Flip = true
	left := s.Picture(c)
	if at(left, 0, 0) != red || at(left, 1, 0) != white {
		t.Errorf("facing left: %v, %v", at(left, 0, 0), at(left, 1, 0))
	}
	if got := s.At(c, 0, 0, 0, 0, LightEdges{}); got != left {
		t.Error("At and Picture give different pictures for a flipped cell")
	}

	terrain := proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}
	flipped := terrain
	flipped.Flip = true
	if s.Picture(terrain) != s.Picture(flipped) {
		t.Error("a cell of terrain alone has two pictures")
	}

	none := color.RGBA{}
	if sp := s.FlippedSprite(1, 1, none); at(sp, 1, 0) != white || at(sp, 0, 0) != none {
		t.Errorf("flipped sprite: %v, %v", at(sp, 0, 0), at(sp, 1, 0))
	}
	if sp := s.Sprite(1, 1, none); at(sp, 0, 0) != white {
		t.Errorf("sprite: %v", at(sp, 0, 0))
	}
}

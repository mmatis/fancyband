package tiles

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"

	"fancyband/internal/proto"
)

func TestShadowMask(t *testing.T) {
	walls := func(at ...[2]int) func(dx, dy int) bool {
		return func(dx, dy int) bool {
			for _, a := range at {
				if a == [2]int{dx, dy} {
					return true
				}
			}
			return false
		}
	}
	for _, tc := range []struct {
		name string
		at   [][2]int
		want uint8
	}{
		{"open floor", nil, 0},
		{"wall to the north", [][2]int{{0, -1}}, ShadowN},
		{"a lone corner", [][2]int{{1, 1}}, ShadowSE},
		// The side already shades the corner beside it.
		{"side and corner", [][2]int{{0, -1}, {1, -1}}, ShadowN},
		{"inside corner", [][2]int{{0, -1}, {-1, 0}, {-1, -1}}, ShadowN | ShadowW},
	} {
		if got := ShadowMask(walls(tc.at...)); got != tc.want {
			t.Errorf("%s: mask %08b, want %08b", tc.name, got, tc.want)
		}
	}
}

func TestShade(t *testing.T) {
	gray := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 20, 20))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{200, 200, 200, 255}), image.Point{}, draw.Src)
		return img
	}
	img := gray()
	shade(img, ShadowN)
	top, mid, bottom := img.RGBAAt(10, 0).R, img.RGBAAt(10, 4).R, img.RGBAAt(10, 19).R
	if !(top < mid && mid < 200 && bottom == 200) {
		t.Errorf("north shadow: top %d, row 4 %d, bottom %d; want darkest at the top, fading out", top, mid, bottom)
	}
	img = gray()
	shade(img, ShadowSE)
	if c, far := img.RGBAAt(19, 19).R, img.RGBAAt(10, 19).R; !(c < 200 && far == 200) {
		t.Errorf("corner shadow: corner %d, middle of the bottom %d", c, far)
	}
}

func TestShaded(t *testing.T) {
	path := writeSheet(t)
	// Tile (0,0) is a wall and (0,1) a floor; (1,1) has a see-through
	// white pixel, which stands on the floor.
	prf := "feat:GRANITE:*:0x80:0x80\nfeat:FLOOR:*:0x80:0x81\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "graf-test.prf"), []byte(prf), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	wall := proto.PictCell{BgRow: 0, BgCol: 0, FgRow: 0, FgCol: 0}
	floor := proto.PictCell{BgRow: 0, BgCol: 1, FgRow: 0, FgCol: 1}
	if !s.IsWall(wall) || s.IsFloor(wall) || !s.IsFloor(floor) {
		t.Fatal("IsWall and IsFloor do not follow the prf file")
	}
	if s.Shaded(wall, ShadowN) != s.Picture(wall) || s.Shaded(floor, 0) != s.Picture(floor) {
		t.Error("a wall, or a floor with no wall beside it, should get the plain picture")
	}
	shaded := s.Shaded(floor, ShadowN)
	if r := rgba(shaded, 0, 0); r[1] >= 255 {
		t.Errorf("shaded floor = %v, not darker", r)
	}
	if s.Shaded(floor, ShadowN) != shaded {
		t.Error("the shaded picture is not cached")
	}
	on := s.Shaded(proto.PictCell{BgRow: 0, BgCol: 1, FgRow: 1, FgCol: 1}, ShadowN)
	if r := rgba(on, 0, 0); r != [4]uint32{255, 255, 255, 255} {
		t.Errorf("the tile on the floor = %v, want white", r)
	}
}

// TestRealShadowFeats checks that every shipped tile set names walls and
// floors.
func TestRealShadowFeats(t *testing.T) {
	for _, dir := range []string{"adam-bolt", "gervais", "nomad", "old", "shockbolt"} {
		matches, _ := filepath.Glob(filepath.Join("../../../engine/lib/tiles", dir, "*.png"))
		if len(matches) == 0 {
			t.Skip("no engine tiles in this checkout")
		}
		walls, floors := loadShadowFeats(loadFeats(matches[0]))
		if len(walls) == 0 || len(floors) == 0 {
			t.Errorf("%s: %d wall tiles, %d floor tiles", dir, len(walls), len(floors))
		}
	}
}

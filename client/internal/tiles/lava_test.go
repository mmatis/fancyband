package tiles

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"fancyband/internal/proto"
)

func TestShiftTowardYellow(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 1))
	img.SetRGBA(0, 0, color.RGBA{200, 0, 0, 255})     // red
	img.SetRGBA(1, 0, color.RGBA{0, 0, 200, 255})     // blue
	img.SetRGBA(2, 0, color.RGBA{100, 100, 100, 255}) // gray
	img.SetRGBA(3, 0, color.RGBA{80, 0, 0, 255})      // dark red
	shiftTowardYellow(img, 1)
	if got := img.RGBAAt(0, 0); got != (color.RGBA{255, 255, 0, 255}) {
		t.Errorf("red shifted all the way = %v, want bright yellow", got)
	}
	if got := img.RGBAAt(1, 0); got != (color.RGBA{0, 0, 200, 255}) {
		t.Errorf("blue changed to %v", got)
	}
	if got := img.RGBAAt(2, 0); got != (color.RGBA{100, 100, 100, 255}) {
		t.Errorf("gray changed to %v", got)
	}
	if got := img.RGBAAt(3, 0); got != (color.RGBA{160, 160, 0, 255}) {
		t.Errorf("dark red shifted all the way = %v, want yellow twice as bright", got)
	}
}

func TestShifted(t *testing.T) {
	path := writeSheet(t)
	// Tile (0,0), the red one, is lava.
	prf := "# lava\nfeat:LAVA:*:0x80:0x80\nfeat:FLOOR:*:0x80:0x81\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "graf-test.prf"), []byte(prf), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	lava := proto.PictCell{FgRow: 0, FgCol: 0, BgRow: 0, BgCol: 0}
	floor := proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}
	if !s.IsLava(lava) || s.IsLava(floor) {
		t.Fatal("IsLava does not follow the prf file")
	}
	if s.Shifted(lava, 0) != s.Picture(lava) || s.Shifted(floor, 5) != s.Picture(floor) {
		t.Error("step 0, or a cell that is not lava, should get the plain picture")
	}
	last := s.Shifted(lava, ShiftSteps-1)
	if r := rgba(last, 1, 1); r[0] != 255 || r[1] == 0 || r[2] != 0 {
		t.Errorf("most shifted lava pixel = %v, want orange to yellow", r)
	}
	if s.Shifted(lava, ShiftSteps-1) != last {
		t.Error("the shifted picture is not cached")
	}
	// Something standing on lava keeps its colors: the see-through tile's
	// white pixel over the shifted red.
	on := s.Shifted(proto.PictCell{FgRow: 1, FgCol: 1, BgRow: 0, BgCol: 0}, ShiftSteps-1)
	if r := rgba(on, 0, 0); r != [4]uint32{255, 255, 255, 255} {
		t.Errorf("the tile on the lava = %v, want white", r)
	}
	if r := rgba(on, 1, 1); r[1] == 0 {
		t.Errorf("the lava under it = %v, not shifted", r)
	}
}

// TestRealLava checks that the shipped tile sets name their lava tiles.
func TestRealLava(t *testing.T) {
	for _, dir := range []string{"adam-bolt", "gervais", "nomad", "old", "shockbolt"} {
		matches, _ := filepath.Glob(filepath.Join("../../../engine/lib/tiles", dir, "*.png"))
		if len(matches) == 0 {
			t.Skip("no engine tiles in this checkout")
		}
		if len(loadLava(matches[0])) == 0 {
			t.Errorf("%s: no lava tiles found", dir)
		}
	}
}

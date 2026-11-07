package tiles

import (
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"fancyband/internal/proto"
)

// writeStoneSheet makes a sheet of four 2x2 tiles: gray granite, a gray
// vein with one gold pixel, a gray floor, and a gray door, named so in a
// prf file beside it.
func writeStoneSheet(t *testing.T) string {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 2))
	gray := color.NRGBA{128, 128, 128, 255}
	for y := 0; y < 2; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, gray)
		}
	}
	img.SetNRGBA(2, 0, color.NRGBA{230, 180, 40, 255})
	dir := t.TempDir()
	path := filepath.Join(dir, "sheet.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	prf := "feat:GRANITE:lit:0x80:0x80\nfeat:MAGMA_K:lit:0x80:0x81\nfeat:FLOOR:lit:0x80:0x82\nfeat:CLOSED:lit:0x80:0x83\n"
	if err := os.WriteFile(filepath.Join(dir, "graf-test.prf"), []byte(prf), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLevelColors(t *testing.T) {
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, Path: writeStoneSheet(t)})
	if err != nil {
		t.Fatal(err)
	}
	cell := func(col uint8) proto.PictCell { return proto.PictCell{BgCol: col, FgCol: col} }
	wall, vein, floor, door := cell(0), cell(1), cell(2), cell(3)
	plain := map[string]image.Image{"wall": s.Picture(wall), "vein": s.Picture(vein), "floor": s.Picture(floor), "door": s.Picture(door)}

	s.SetLevelColors(LevelColors{}) // the town: nothing changes
	if s.Picture(wall) != plain["wall"] {
		t.Error("setting the same colors again dropped the cached pictures")
	}

	// Walls toward blue, the floor toward red.
	s.SetLevelColors(LevelColors{Wall: Shift{Hue: 240, Saturation: 0.5}, Floor: Shift{Hue: 0, Saturation: 0.5}})
	w := rgba(s.Picture(wall), 0, 0)
	if !(w[2] > w[0] && w[2] > w[1]) {
		t.Errorf("wall = %v, want a blue tint", w)
	}
	fl := rgba(s.Picture(floor), 0, 0)
	if !(fl[0] > fl[1] && fl[0] > fl[2]) {
		t.Errorf("floor = %v, want a red tint", fl)
	}
	if v := rgba(s.Picture(vein), 1, 0); !(v[2] > v[0]) {
		t.Errorf("vein's stone = %v, want the walls' blue tint", v)
	}
	if v := rgba(s.Picture(vein), 0, 0); v != [4]uint32{230, 180, 40, 255} {
		t.Errorf("vein's gold = %v, want it unchanged", v)
	}
	if d := rgba(s.Picture(door), 0, 0); d != rgba(plain["door"], 0, 0) {
		t.Errorf("door = %v, want it unchanged", d)
	}
	// A creature on the floor keeps its own colors.
	on := s.Picture(proto.PictCell{BgCol: 2, FgCol: 3})
	if r := rgba(on, 0, 0); r != rgba(plain["door"], 0, 0) {
		t.Errorf("tile on the floor = %v, want its own colors", r)
	}
	// Sprites, as lists and the Tile Editor show them, keep the sheet's.
	if r := rgba(s.Sprite(0, 0, color.RGBA{}), 0, 0); r != rgba(plain["wall"], 0, 0) {
		t.Errorf("wall sprite = %v, want the sheet's colors", r)
	}

	s.SetLevelColors(LevelColors{})
	if r := rgba(s.Picture(wall), 0, 0); r != rgba(plain["wall"], 0, 0) {
		t.Errorf("wall back in the town = %v, want the sheet's colors", r)
	}
}

func TestRollLevelColors(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	same := 0
	for i := 0; i < 200; i++ {
		c := RollLevelColors(r)
		for _, sh := range []Shift{c.Wall, c.Floor} {
			if sh.Hue < 0 || sh.Hue >= 360 || sh.Lightness < lightMin || sh.Lightness > lightMax {
				t.Fatalf("roll out of range: %+v", sh)
			}
		}
		if c.Wall.Saturation < wallTintMin || c.Wall.Saturation > wallTintMax ||
			c.Floor.Saturation < floorTintMin || c.Floor.Saturation > floorTintMax {
			t.Fatalf("tint out of range: %+v", c)
		}
		if c.Wall.Hue == c.Floor.Hue {
			same++
		}
	}
	if same > 0 {
		t.Errorf("walls and floor rolled the same hue %d times; they should roll apart", same)
	}
}

// TestRealLevelKinds checks that every shipped tile set names the walls
// and the floor that level colors change.
func TestRealLevelKinds(t *testing.T) {
	for _, dir := range []string{"adam-bolt", "gervais", "nomad", "old", "shockbolt"} {
		matches, _ := filepath.Glob(filepath.Join("../../../engine/lib/tiles", dir, "*.png"))
		if len(matches) == 0 {
			t.Skip("no engine tiles in this checkout")
		}
		got := map[levelKind]int{}
		for _, k := range loadLevelKinds(loadFeats(matches[0])) {
			got[k]++
		}
		if got[levelWall] == 0 || got[levelFloor] == 0 {
			t.Errorf("%s: %d wall tiles, %d floor tiles", dir, got[levelWall], got[levelFloor])
		}
	}
}

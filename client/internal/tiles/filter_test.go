package tiles

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"fancyband/internal/proto"
)

// sprite returns a w by w test picture: a red square in the middle on
// see-through pixels, with one pixel of margin.
func sprite(w int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, w))
	for y := 1; y < w-1; y++ {
		for x := 1; x < w-1; x++ {
			img.SetRGBA(x, y, color.RGBA{200, 0, 0, 255})
		}
	}
	return img
}

// seeThroughKept fails t if a pixel see-through in before is not in after.
func seeThroughKept(t *testing.T, name string, before, after *image.RGBA) {
	t.Helper()
	b := before.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if before.RGBAAt(x, y).A == 0 && after.RGBAAt(x, y) != (color.RGBA{}) {
				t.Errorf("%s changed the see-through pixel %d,%d to %v", name, x, y, after.RGBAAt(x, y))
				return
			}
		}
	}
}

func TestColorSteps(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 1))
	img.SetRGBA(0, 0, color.RGBA{200, 0, 0, 255})     // red
	img.SetRGBA(1, 0, color.RGBA{0, 0, 200, 255})     // blue
	img.SetRGBA(2, 0, color.RGBA{100, 100, 100, 255}) // gray
	img.SetRGBA(3, 0, color.RGBA{80, 0, 0, 255})      // dark red
	// Reds to yellows turned toward yellow and brightened.
	warm := NewFilter()
	warm.Range = &Range{From: -60, To: 60}
	warm.Hue, warm.Lightness = 42, 0.7
	warm.apply(img, 0, 0, true)
	if got := img.RGBAAt(0, 0); got.R != 255 || got.G < 150 || got.B != 0 {
		t.Errorf("red turned toward yellow = %v, want bright orange to yellow", got)
	}
	if got := img.RGBAAt(1, 0); got != (color.RGBA{0, 0, 200, 255}) {
		t.Errorf("blue, outside the range, changed to %v", got)
	}
	if got := img.RGBAAt(2, 0); got != (color.RGBA{100, 100, 100, 255}) {
		t.Errorf("gray changed to %v", got)
	}

	blue := NewFilter()
	blue.Hue = 240
	red := image.NewRGBA(image.Rect(0, 0, 1, 1))
	red.SetRGBA(0, 0, color.RGBA{200, 0, 0, 255})
	blue.apply(red, 0, 0, false)
	if got := red.RGBAAt(0, 0); got != (color.RGBA{0, 0, 200, 255}) {
		t.Errorf("red turned 240 degrees = %v, want blue", got)
	}

	gray := NewFilter()
	gray.Saturation, gray.Lightness = -1, -0.5
	red.SetRGBA(0, 0, color.RGBA{200, 0, 0, 255})
	gray.apply(red, 0, 0, false)
	if got := red.RGBAAt(0, 0); got != (color.RGBA{100, 100, 100, 255}) {
		t.Errorf("red without saturation, half as bright = %v, want gray 100", got)
	}
}

func TestFade(t *testing.T) {
	f := NewFilter()
	f.Fade = 0.5
	img := sprite(4)
	f.apply(img, 0, 0, false)
	if got := img.RGBAAt(1, 1); got.A < 126 || got.A > 129 {
		t.Errorf("half see-through alpha = %d, want about 128", got.A)
	}
}

func TestOutlineAndGlowStayOnTheTile(t *testing.T) {
	for _, l := range []Look{{Outline: 1}, {Glow: 1}, {Outline: 1, Glow: 1}} {
		f := NewFilter()
		f.Look = l
		before := sprite(16)
		img := sprite(16)
		f.apply(img, 0, 0, false)
		seeThroughKept(t, "the edge", before, img)
		if l.Outline > 0 {
			// The outline is one pixel wide at this size: the square's
			// edge turns gold, the pixel inside it stays red.
			if got := img.RGBAAt(1, 5); got != (color.RGBA{255, 200, 40, 255}) {
				t.Errorf("edge pixel = %v, want gold", got)
			}
			if l.Glow == 0 {
				if got := img.RGBAAt(2, 5); got != (color.RGBA{200, 0, 0, 255}) {
					t.Errorf("pixel inside the outline = %v, want red", got)
				}
			}
		}
		if l.Glow > 0 && l.Outline == 0 {
			edge, deep := img.RGBAAt(1, 8), img.RGBAAt(8, 8)
			if edge.G <= deep.G || deep != (color.RGBA{200, 0, 0, 255}) {
				t.Errorf("glow: edge %v, middle %v; want the edge lighter and the middle red", edge, deep)
			}
		}
	}
	// An opaque tile has no edge, so it gets no outline.
	solidTile := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range solidTile.Pix {
		solidTile.Pix[i] = 200
	}
	solidTile.Pix[3] = 255
	before := append([]uint8(nil), solidTile.Pix...)
	f := NewFilter()
	f.Outline = 1
	f.apply(solidTile, 0, 0, true)
	if string(before) != string(solidTile.Pix) {
		t.Error("an opaque tile got an outline")
	}
}

func TestBlurAndWaverSkipTerrain(t *testing.T) {
	for _, l := range []Look{{Blur: 1}, {Waver: 1}} {
		f := NewFilter()
		f.Look = l
		img := sprite(16)
		f.apply(img, 3, 0, true)
		if string(img.Pix) != string(sprite(16).Pix) {
			t.Errorf("%+v changed terrain", l)
		}
		f.apply(img, 3, 0, false)
		if string(img.Pix) == string(sprite(16).Pix) {
			t.Errorf("%+v did not change a monster", l)
		}
		f.Terrain = true
		img = sprite(16)
		f.apply(img, 3, 0, true)
		if string(img.Pix) == string(sprite(16).Pix) {
			t.Errorf("%+v did not change terrain it was turned on for", l)
		}
	}
}

func TestCurvesRepeat(t *testing.T) {
	for _, c := range []string{CurveSmooth, CurveTriangle, CurveBlink, CurveFlicker} {
		lo, hi := 1.0, 0.0
		for s := 0; s < AnimSteps; s++ {
			v := curve(c, s, 1)
			if v != curve(c, s+AnimSteps, 1) || v != curve(c, s-AnimSteps, 1) {
				t.Errorf("%s does not repeat at step %d", c, s)
			}
			if v < 0 || v > 1 {
				t.Errorf("%s at step %d = %v, outside 0 to 1", c, s, v)
			}
			lo, hi = min(lo, v), max(hi, v)
		}
		// Flicker is random, so it need not reach the very ends.
		if lo > 0.2 || hi < 0.9 {
			t.Errorf("%s only moves from %v to %v", c, lo, hi)
		}
	}
	// Sparkle repeats too: the same step and variant light the same pixels.
	f := NewFilter()
	f.Sparkle = 1
	a, b, c := sprite(16), sprite(16), sprite(16)
	f.apply(a, 2, 1, false)
	f.apply(b, 2+AnimSteps, 1, false)
	f.apply(c, 3, 1, false)
	if string(a.Pix) != string(b.Pix) {
		t.Error("sparkle does not repeat after AnimSteps steps")
	}
	if string(a.Pix) == string(c.Pix) || string(a.Pix) == string(sprite(16).Pix) {
		t.Error("sparkle does not change from step to step")
	}
	seeThroughKept(t, "sparkle", sprite(16), a)
}

func TestStepAt(t *testing.T) {
	f := NewFilter()
	if s, v := f.StepAt(5e9, 3, 4); s != 0 || v != 0 {
		t.Errorf("a still filter is at step %d, variant %d", s, v)
	}
	f.Anim = &Anim{Curve: CurveSmooth, Period: 1.2, Spread: SpreadTogether}
	if s, _ := f.StepAt(300e6, 0, 0); s != 3 {
		t.Errorf("a quarter of the way through = step %d, want 3", s)
	}
	if a, _ := f.StepAt(300e6, 0, 0); a != func() int { s, _ := f.StepAt(300e6, 5, 9); return s }() {
		t.Error("grids that move together are out of step")
	}
	f.Anim.Spread = SpreadRipple
	if a, _ := f.StepAt(0, 1, 2); a != 3 {
		t.Errorf("ripple at grid 1, 2 = step %d, want 3", a)
	}
}

// TestVirtualTiles checks that two monsters sharing one sheet tile can
// carry different filters, through virtual tiles.
func TestVirtualTiles(t *testing.T) {
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, Path: writeSheet(t)})
	if err != nil {
		t.Fatal(err)
	}
	blue, faded := NewFilter(), NewFilter()
	blue.Hue = 240
	faded.Fade = 1
	e := NewEdits()
	a, ok := e.FreeTile(2)
	if !ok || a != [2]uint8{2, 0} {
		t.Fatalf("first free tile = %v, %v; want row 2, column 0", a, ok)
	}
	e.Virtual[a] = Virtual{Base: [2]uint8{0, 0}, Filter: blue}
	b, _ := e.FreeTile(2)
	e.Virtual[b] = Virtual{Base: [2]uint8{0, 0}, Filter: faded}
	s.SetEdits(e)
	// Each stands on the green tile at 0, 1.
	on := func(t [2]uint8) proto.PictCell { return proto.PictCell{FgRow: t[0], FgCol: t[1], BgRow: 0, BgCol: 1} }
	plain := s.Picture(on([2]uint8{0, 0}))
	if r := rgba(s.Picture(on(a)), 0, 0); r != [4]uint32{0, 0, 255, 255} {
		t.Errorf("the blue one = %v, want blue", r)
	}
	if r := rgba(s.Picture(on(b)), 0, 0); r != [4]uint32{0, 255, 0, 255} {
		t.Errorf("the invisible one = %v, want the green terrain", r)
	}
	if r := rgba(plain, 0, 0); r != [4]uint32{255, 0, 0, 255} {
		t.Errorf("the tile itself = %v, want its own red", r)
	}
}

func TestEditsFile(t *testing.T) {
	dir := t.TempDir()
	path := EditsPath(filepath.Join(dir, "tiles", "adam-bolt.prf"))
	if filepath.Base(path) != "adam-bolt.json" {
		t.Fatalf("EditsPath = %s", path)
	}
	e, err := LoadEdits(path)
	if err != nil || len(e.Virtual) != 0 {
		t.Fatalf("a missing file = %+v, %v; want no edits", e, err)
	}
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("saving no edits wrote a file")
	}

	f := NewFilter()
	f.Outline = 1
	f.Anim = &Anim{To: Look{Glow: 1}, Curve: CurveBlink, Period: 2, Spread: SpreadRandom}
	e.Virtual[[2]uint8{40, 3}] = Virtual{Base: [2]uint8{5, 6}, Filter: f}
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadEdits(path)
	if err != nil {
		t.Fatal(err)
	}
	v := got.Virtual[[2]uint8{40, 3}]
	if v.Base != [2]uint8{5, 6} || !sameFilter(v.Filter, f) {
		t.Errorf("read back %+v, want base 5, 6 and %+v", v, f)
	}
}

// TestForget checks that a change to one filter drops only the pictures
// that use its tile, so the rest of the map is not sent again.
func TestForget(t *testing.T) {
	s, err := Load(proto.TileSet{TileW: 2, TileH: 2, Path: writeSheet(t)})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEdits()
	v := [2]uint8{2, 0}
	e.Virtual[v] = Virtual{Base: [2]uint8{0, 0}, Filter: NewFilter()}
	s.SetEdits(e)
	other := s.Picture(proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1})
	old := s.Picture(proto.PictCell{FgRow: 2, FgCol: 0, BgRow: 0, BgCol: 1})
	f := e.Virtual[v]
	f.Filter.Hue = 120
	e.Virtual[v] = f
	s.Forget(v)
	if s.Picture(proto.PictCell{FgRow: 0, FgCol: 1, BgRow: 0, BgCol: 1}) != other {
		t.Error("a picture without the changed tile was made again")
	}
	if s.Picture(proto.PictCell{FgRow: 2, FgCol: 0, BgRow: 0, BgCol: 1}) == old {
		t.Error("the changed tile kept its old picture")
	}
}

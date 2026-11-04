package tiles

import (
	"image"
	"image/color"
	"testing"

	"github.com/thebanri/limoni/graphics"
)

// twoColors is a 16x16 checker of hard-edged two-pixel squares, offset in
// its bounds as a cut from a sheet is.
func twoColors() *image.RGBA {
	img := image.NewRGBA(image.Rect(32, 48, 48, 64))
	for y := 48; y < 64; y++ {
		for x := 32; x < 48; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if (x/2+y/2)%2 == 0 {
				c = color.RGBA{}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func colors(img image.Image) map[color.RGBA]bool {
	seen := map[color.RGBA]bool{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			seen[color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)] = true
		}
	}
	return seen
}

// TestEnlargeKeepsColors checks that enlarging adds no colors at any size,
// whole multiple or not, and that Limoni then sends the picture unchanged:
// its own resize would blend them.
func TestEnlargeKeepsColors(t *testing.T) {
	src := twoColors()
	for _, sz := range [][2]int{{16, 16}, {18, 18}, {20, 20}, {32, 32}, {36, 36}, {40, 40}} {
		out := Enlarge(src, sz[0], sz[1])
		if out.Bounds() != image.Rect(0, 0, sz[0], sz[1]) {
			t.Fatalf("%v: bounds %v", sz, out.Bounds())
		}
		if got := colors(out); len(got) != 2 {
			t.Errorf("%v: %d colors after enlarging, want 2", sz, len(got))
		}
		if got := colors(graphics.ResizeImageContain(out, sz[0], sz[1], true)); len(got) != 2 {
			t.Errorf("%v: %d colors after Limoni's fit, want 2", sz, len(got))
		}
	}
}

// TestEnlargeOtherImageTypes checks the path for pictures that are not RGBA.
func TestEnlargeOtherImageTypes(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	src.SetNRGBA(1, 0, color.NRGBA{0, 0, 255, 255})
	out := Enlarge(src, 4, 2)
	if got := out.RGBAAt(1, 1); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("left half: %v", got)
	}
	if got := out.RGBAAt(2, 0); got != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("right half: %v", got)
	}
}

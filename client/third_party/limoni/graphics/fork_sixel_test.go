package graphics

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// Fancyband fork: a transparent Sixel picture asks the terminal to leave
// the pixels it skips alone (P2=1); an opaque one keeps the old header.
func TestSixelTransparentHeader(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	img.SetRGBA(1, 1, color.RGBA{255, 0, 0, 255})
	if s := EncodeSixel(img, 1, 1, 4, 6, true); !strings.HasPrefix(s, "\x1bP0;1q\"1;1;") {
		t.Errorf("transparent picture starts %q", s[:min(len(s), 12)])
	}
	if s := EncodeSixel(img, 1, 1, 4, 6, false); !strings.HasPrefix(s, "\x1bPq\"1;1;") {
		t.Errorf("opaque picture starts %q", s[:min(len(s), 12)])
	}
}

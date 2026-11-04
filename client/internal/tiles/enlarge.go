package tiles

import (
	"image"
	"image/color"
)

// Enlarge returns img at w by h pixels, each new pixel a copy of the nearest
// old one, so tiles keep their hard edges and exact colors. img must not be
// larger than w by h.
func Enlarge(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	srcX := make([]int, w)
	for x := range srcX {
		srcX[x] = b.Min.X + x*b.Dx()/w
	}
	src, _ := img.(*image.RGBA)
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		row := out.Pix[y*out.Stride : y*out.Stride+4*w]
		for x, sx := range srcX {
			if src != nil {
				copy(row[4*x:4*x+4], src.Pix[src.PixOffset(sx, sy):])
				continue
			}
			c := color.RGBAModel.Convert(img.At(sx, sy)).(color.RGBA)
			row[4*x], row[4*x+1], row[4*x+2], row[4*x+3] = c.R, c.G, c.B, c.A
		}
	}
	return out
}

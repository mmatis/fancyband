package backdrop

import (
	"fmt"
	"image"
	_ "image/gif" // decoders for LoadImage
	_ "image/jpeg"
	_ "image/png"
	"os"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

// Image is a still picture as a scene: a wallpaper. It covers the screen —
// scaled until no edge is left bare, the overflow cropped evenly — at two
// pixels a cell, one above the other with half blocks, which makes the
// pixels about square in most fonts.
//
// It is made of coloured cells like every scene, so it works in any
// terminal with 256 colours or more, and looks like pixel art rather than a
// photograph. The picture is scaled once per window size; after that a frame
// costs a copy, and since it never moves, nothing is sent while the
// application is idle.
func Image(img image.Image) terminal.Backdrop { return &picture{img: img} }

// LoadImage reads a PNG, JPEG or GIF (its first frame) as an Image scene.
func LoadImage(path string) (terminal.Backdrop, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return Image(img), nil
}

type picture struct {
	grid
	img   image.Image
	cells []cell.Cell
}

func (p *picture) Interval() time.Duration { return 0 }

func (p *picture) Render(dst *buffer.Buffer, _ time.Duration) {
	if p.resized(dst) {
		p.scale()
	}
	copy(dst.Content, p.cells)
}

// scale samples the picture onto the grid: each half-cell pixel averages a
// few points spread over the part of the picture it covers, which is enough
// to keep thin lines and noise from flickering in and out, without reading
// every pixel of a large photograph.
func (p *picture) scale() {
	w, h := p.w, p.h
	p.cells = make([]cell.Cell, w*h)
	b := p.img.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	if w == 0 || h == 0 || iw == 0 || ih == 0 {
		return
	}
	// Cover: the larger scale, with the rest cropped evenly on both sides.
	pw, ph := float64(w), float64(2*h) // in half-cell pixels
	s := max(pw/iw, ph/ih)
	cropX, cropY := (iw*s-pw)/2, (ih*s-ph)/2
	step := 1 / s // picture pixels per screen pixel

	const n = 3 // samples per side
	sample := func(px, py int) rgb {
		x0 := (float64(px)+cropX)*step + float64(b.Min.X)
		y0 := (float64(py)+cropY)*step + float64(b.Min.Y)
		var sum rgb
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				x := int(x0 + (float64(i)+0.5)*step/n)
				y := int(y0 + (float64(j)+0.5)*step/n)
				r, g, bl, _ := p.img.At(min(x, b.Max.X-1), min(y, b.Max.Y-1)).RGBA()
				sum.r += float64(r >> 8)
				sum.g += float64(g >> 8)
				sum.b += float64(bl >> 8)
			}
		}
		return sum.scale(1.0 / (n * n))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p.cells[y*w+x] = halfCell(sample(x, 2*y), sample(x, 2*y+1))
		}
	}
}

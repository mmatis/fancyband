package tiles

import (
	"bufio"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fancyband/internal/proto"
)

// ShiftSteps is how many pictures each lava tile has, from its own colors
// (step 0) to the most yellow (step ShiftSteps-1).
const ShiftSteps = 8

// maxShift is how far the last step moves a red pixel toward yellow, as a
// share of the way.
const maxShift = 0.7

// loadFeats reads which terrain each sheet tile shows from the tile
// set's graf-*.prf files, which sit beside its sheet. Lines look like
// "feat:LAVA:torch:0x98:0xB8": the terrain's name, the light, and the
// row and column with the high bit set. A tile two kinds of terrain
// share keeps the first name.
func loadFeats(sheetPath string) map[[2]uint8]string {
	feats := map[[2]uint8]string{}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(sheetPath), "graf-*.prf"))
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Split(strings.TrimSpace(sc.Text()), ":")
			if len(fields) != 5 || fields[0] != "feat" {
				continue
			}
			row, err1 := strconv.ParseUint(fields[3], 0, 8)
			col, err2 := strconv.ParseUint(fields[4], 0, 8)
			at := [2]uint8{uint8(row) & 0x7F, uint8(col) & 0x7F}
			if _, seen := feats[at]; err1 == nil && err2 == nil && !seen {
				feats[at] = strings.ToUpper(fields[1])
			}
		}
		f.Close()
	}
	return feats
}

// loadLava finds the lava tiles. A set without them simply has no lava
// to shift.
func loadLava(feats map[[2]uint8]string) map[[2]uint8]bool {
	lava := map[[2]uint8]bool{}
	for at, name := range feats {
		if name == "LAVA" {
			lava[at] = true
		}
	}
	return lava
}

// IsLava reports whether a cell's terrain is lava.
func (s *Sheet) IsLava(c proto.PictCell) bool {
	return s.lava[[2]uint8{c.BgRow, c.BgCol}]
}

type shiftKey struct {
	cell proto.PictCell
	step int
}

// Shifted returns the picture for a cell with its lava terrain shifted
// toward yellow by step, from 0 to ShiftSteps-1. Whatever stands on the
// lava keeps its colors. A cell that is not lava, or step 0, gets its
// plain picture.
func (s *Sheet) Shifted(c proto.PictCell, step int) image.Image {
	step = min(max(step, 0), ShiftSteps-1)
	if step == 0 || !s.IsLava(c) {
		return s.Picture(c)
	}
	k := shiftKey{c, step}
	if p, ok := s.shifted[k]; ok {
		return p
	}
	out := image.NewRGBA(image.Rect(0, 0, s.tileW, s.tileH))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	s.draw(out, c.BgRow, c.BgCol)
	shiftTowardYellow(out, maxShift*float64(step)/float64(ShiftSteps-1))
	if c.FgRow != c.BgRow || c.FgCol != c.BgCol {
		s.draw(out, c.FgRow, c.FgCol)
	}
	s.shifted[k] = out
	return out
}

// shiftTowardYellow moves the hue of each red to yellow pixel the share t
// of the way to yellow, and brightens it by the share t, so the yellow end
// glows instead of fading to brown. Scaling the brightness, instead of
// adding to it, keeps the tile's light and dark texture. Other colors, and
// grays, stay as they are.
func shiftTowardYellow(img *image.RGBA, t float64) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := img.RGBAAt(x, y)
			h, sat, v := toHSV(p)
			if h > 300 {
				h -= 360 // treat magenta-reds as just below red
			}
			if sat < 0.2 || h < -60 || h > 60 {
				continue
			}
			h += t * (60 - h)
			v = min(v*(1+t), 1)
			img.SetRGBA(x, y, fromHSV(h, sat, v, p.A))
		}
	}
}

func toHSV(p color.RGBA) (h, s, v float64) {
	r, g, b := float64(p.R)/255, float64(p.G)/255, float64(p.B)/255
	hi, lo := max(r, g, b), min(r, g, b)
	v = hi
	if hi == 0 {
		return 0, 0, 0
	}
	s = (hi - lo) / hi
	if hi == lo {
		return 0, s, v
	}
	switch hi {
	case r:
		h = 60 * (g - b) / (hi - lo)
	case g:
		h = 60 * (2 + (b-r)/(hi-lo))
	default:
		h = 60 * (4 + (r-g)/(hi-lo))
	}
	if h < 0 {
		h += 360
	}
	return h, s, v
}

func fromHSV(h, s, v float64, a uint8) color.RGBA {
	for h < 0 {
		h += 360
	}
	c := v * s
	hp := h / 60
	x := c * (1 - abs(mod2(hp)-1))
	var r, g, b float64
	switch int(hp) % 6 {
	case 0:
		r, g = c, x
	case 1:
		r, g = x, c
	case 2:
		g, b = c, x
	case 3:
		g, b = x, c
	case 4:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := v - c
	to8 := func(f float64) uint8 { return uint8(min(max((f+m)*255+0.5, 0), 255)) }
	return color.RGBA{to8(r), to8(g), to8(b), a}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// mod2 returns f modulo 2, for f >= 0.
func mod2(f float64) float64 {
	return f - 2*float64(int(f/2))
}

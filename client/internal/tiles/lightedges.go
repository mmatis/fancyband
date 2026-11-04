package tiles

import (
	"image"
	"math"

	"fancyband/internal/proto"
)

// Light edges soften the steps where the floor's light changes from grid
// to grid: the line of sight, and the edge of a lit room. Each floor tile
// is drawn knowing the light on the floor around it. Like wall shadows and
// filters, they change pictures as the client draws them, never the sheet,
// and only the floor: whatever stands on it is drawn on top afterward. Walls
// and unknown grids take no part, so floor does not fade into them.

// LightStyle is how light edges are drawn.
type LightStyle uint8

const (
	// LightOff draws each tile with one light.
	LightOff LightStyle = iota
	// LightCorners blends each tile between the light at its four corners,
	// each the average of the tiles that meet there.
	LightCorners
	// LightCuts fills the corner of a tile between two brighter neighbors
	// up to its diagonal, which turns a step into a slanted line.
	LightCuts
	// LightDither is LightCorners drawn as a pattern of whole light levels.
	LightDither
)

// lightLevels is the kinds of light a floor tile shows: dark, lit, and in
// view.
const lightLevels = 3

// lightNone marks a neighbor that takes no part, in LightEdges.Around.
const lightNone = 3

// lightOf is the light a terrain tile shows, and which of the tile set's
// graf-*.prf files gives it, since Shockbolt's two have floors of their own.
type lightOf struct{ level, group uint8 }

// LightEdges says how to draw one floor tile: the style, and the light
// level of each neighbor, two bits each, in the order of the shadow mask:
// N, E, S, W, NE, SE, SW, NW. The zero value draws the plain tile.
type LightEdges struct {
	Style  LightStyle
	Around uint16
}

// loadLights reads the light each terrain tile shows, and measures the
// brightness of the plain floor at each light, by file.
func loadLights(img image.Image, sheetPath string, tileW, tileH int) (map[[2]uint8]lightOf, [][lightLevels]float64) {
	lights := map[[2]uint8]lightOf{}
	var lums [][lightLevels]float64
	levels := map[string]uint8{"dark": 0, "lit": 1, "los": 2, "torch": 2}
	scanFeats(sheetPath, func(group int, name, light string, at [2]uint8) {
		for group >= len(lums) {
			lums = append(lums, [lightLevels]float64{})
		}
		level, ok := levels[light]
		if !ok {
			return
		}
		if _, seen := lights[at]; !seen {
			lights[at] = lightOf{level, uint8(group)}
		}
		if name == "FLOOR" && lums[group][level] == 0 {
			lums[group][level] = tileBrightness(img, at, tileW, tileH)
		}
	})
	return lights, lums
}

// tileBrightness is a tile's mean brightness, from 0 to 255, on black.
func tileBrightness(img image.Image, at [2]uint8, tileW, tileH int) float64 {
	b := img.Bounds()
	x0, y0 := b.Min.X+int(at[1])*tileW, b.Min.Y+int(at[0])*tileH
	total := 0.0
	for y := y0; y < y0+tileH; y++ {
		for x := x0; x < x0+tileW; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			total += float64(r+g+bl) / 3 / 257
		}
	}
	return total / float64(tileW*tileH)
}

// LightLevel returns the light a cell's floor shows. ok is false for
// anything but floor, which light edges leave alone.
func (s *Sheet) LightLevel(c proto.PictCell) (level uint8, ok bool) {
	if !s.IsFloor(c) {
		return 0, false
	}
	l, ok := s.lights[s.base(c.BgRow, c.BgCol)]
	return l.level, ok
}

// LightAround returns how to draw a floor tile of the given level in style,
// from neighbor, which gives the cell dx, dy grids away and whether there is
// one. A tile whose neighbors all share its light gets the zero value, and so
// the same picture as any other.
func (s *Sheet) LightAround(style LightStyle, own uint8, neighbor func(dx, dy int) (proto.PictCell, bool)) LightEdges {
	if style == LightOff {
		return LightEdges{}
	}
	var around uint16
	differs := false
	for i, d := range [8][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}, {1, -1}, {1, 1}, {-1, 1}, {-1, -1}} {
		level := uint8(lightNone)
		if c, ok := neighbor(d[0], d[1]); ok {
			if l, ok := s.LightLevel(c); ok {
				level = l
				differs = differs || l != own
			}
		}
		around |= uint16(level) << (2 * i)
	}
	if !differs {
		return LightEdges{}
	}
	return LightEdges{style, around}
}

// relight draws the light edges on img, the floor tile at, of which only the
// terrain has been drawn.
func (s *Sheet) relight(img *image.RGBA, at [2]uint8, e LightEdges) {
	own, ok := s.lights[at]
	if !ok || int(own.group) >= len(s.lums) {
		return
	}
	lums := s.lums[own.group]
	for _, l := range lums {
		if l == 0 {
			return // the file gives no floor at some light
		}
	}
	f := newLightField(e, own.level, lums)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	o := lums[own.level]
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			u, v := (float64(x)+0.5)/float64(w), (float64(y)+0.5)/float64(h)
			light := f.at(u, v, 1.5/float64(w))
			if e.Style == LightDither {
				light = dithered(light, x, y, lums)
			}
			if light == o {
				continue
			}
			i := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			for j := 0; j < 3; j++ {
				img.Pix[i+j] = uint8(math.Min(float64(img.Pix[i+j])*light/o+0.5, 255))
			}
		}
	}
}

// lightField is the light across one tile.
type lightField struct {
	style LightStyle
	o     float64
	// Each side's and each corner's neighbor, in LightEdges order: its
	// brightness, and whether it takes part.
	val  [8]float64
	here [8]bool
	// corner is the light at each corner: NE, SE, SW, NW.
	corner [4]float64
}

// cornerSides gives each corner's two sides and its diagonal, by index in
// LightEdges order: NE, SE, SW, NW.
var cornerSides = [4][3]int{{0, 1, 4}, {2, 1, 5}, {2, 3, 6}, {0, 3, 7}}

func newLightField(e LightEdges, own uint8, lums [lightLevels]float64) *lightField {
	f := &lightField{style: e.Style, o: lums[own]}
	for i := range f.val {
		f.val[i] = f.o
		if l := e.Around >> (2 * i) & 3; l != lightNone {
			f.val[i], f.here[i] = lums[l], true
		}
	}
	for c, ids := range cornerSides {
		sum, n := f.o, 1.0
		for _, i := range ids {
			if f.here[i] {
				sum += f.val[i]
				n++
			}
		}
		f.corner[c] = sum / n
	}
	return f
}

// at returns the light at u, v across the tile, each from 0 at the west or
// north side to 1. soft is how wide a diagonal cut's edge is.
func (f *lightField) at(u, v, soft float64) float64 {
	if f.style == LightCuts {
		return f.cuts(u, v, soft)
	}
	return f.blend(u, v)
}

// blend blends between the four corners, whose light the tiles that meet
// there share, so nothing shows where tiles meet. Keeping the tile's own
// light in its middle left a row of bright and dark spots along a slope.
func (f *lightField) blend(u, v float64) float64 {
	top := f.corner[3]*(1-u) + f.corner[0]*u
	bottom := f.corner[2]*(1-u) + f.corner[1]*u
	return top*(1-v) + bottom*v
}

// cuts fills each corner that lies between two brighter sides up to the
// diagonal. Only the darker tile is cut, so that a run of steps makes one
// straight line and not two.
func (f *lightField) cuts(u, v, soft float64) float64 {
	light := f.o
	for c, ids := range cornerSides {
		a, b := ids[0], ids[1]
		if !f.here[a] || !f.here[b] || f.val[a] <= f.o || f.val[b] <= f.o {
			continue
		}
		du, dv := u, v
		if c == 0 || c == 1 { // the east corners
			du = 1 - u
		}
		if c == 1 || c == 2 { // the south corners
			dv = 1 - v
		}
		cover := math.Min(math.Max((1-(du+dv))/soft+0.5, 0), 1)
		light = math.Max(light, f.o+(math.Min(f.val[a], f.val[b])-f.o)*cover)
	}
	return light
}

// bayer is a 4 by 4 ordered dither pattern, as thresholds from 0 to 1.
var bayer = [4][4]float64{
	{0.5 / 16, 8.5 / 16, 2.5 / 16, 10.5 / 16},
	{12.5 / 16, 4.5 / 16, 14.5 / 16, 6.5 / 16},
	{3.5 / 16, 11.5 / 16, 1.5 / 16, 9.5 / 16},
	{15.5 / 16, 7.5 / 16, 13.5 / 16, 5.5 / 16},
}

// dithered turns light into one of the two light levels around it, chosen
// by pixel so that their mix looks like light.
func dithered(light float64, x, y int, lums [lightLevels]float64) float64 {
	best := lums[0]
	for i := 0; i+1 < lightLevels; i++ {
		lo, hi := math.Min(lums[i], lums[i+1]), math.Max(lums[i], lums[i+1])
		if light < lo || light > hi {
			continue
		}
		if hi == lo {
			return lo
		}
		if (light-lo)/(hi-lo) > bayer[y%4][x%4] {
			return hi
		}
		return lo
	}
	if light > best {
		return lums[lightLevels-1]
	}
	return best
}

package backdrop

import (
	"math"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

const synthwaveFPS = 24

var (
	synthSkyTop  = rgb{8, 3, 24}
	synthSkyMid  = rgb{40, 10, 62}
	synthSkyLow  = rgb{96, 24, 84}
	sunTop       = rgb{255, 214, 102}
	sunBottom    = rgb{245, 60, 130}
	floorDark    = rgb{12, 4, 28}
	gridLine     = rgb{255, 70, 210}
	horizonGlow  = rgb{255, 110, 190}
	synthStarCol = rgb{150, 120, 200}
)

// Synthwave is a retro sunset: a striped sun sinking behind the horizon and
// a neon grid rolling toward the viewer, at twice the vertical resolution
// with half blocks.
//
// Every line of the grid moves, but in four colour steps and in half rows,
// so a cell changes only when a line crosses it. Measured at 120×40 in
// truecolor over ten seconds (TestSceneTraffic): 44 KB a second, 10% of
// repainting every frame; 165 µs to draw a frame.
func Synthwave() terminal.Backdrop { return &synthwave{} }

type synthwave struct {
	grid
	sky []cell.Cell // sky, stars and sun: the upper half never moves but for the stripes
}

func (s *synthwave) Interval() time.Duration { return time.Second / synthwaveFPS }

// horizon is the horizon in half rows.
func (s *synthwave) horizon() float64 { return math.Floor(float64(s.h)*0.58) * 2 }

func (s *synthwave) skyAt(sy float64) rgb {
	t := clamp01(sy / s.horizon())
	if t < 0.6 {
		return synthSkyTop.mix(synthSkyMid, t/0.6)
	}
	return synthSkyMid.mix(synthSkyLow, (t-0.6)/0.4)
}

func (s *synthwave) build() {
	w, h := s.w, s.h
	s.sky = make([]cell.Cell, w*h)
	hz := int(s.horizon() / 2)
	for y := 0; y < min(hz, h); y++ {
		for x := 0; x < w; x++ {
			top, bot := s.skyAt(float64(2*y)+0.5), s.skyAt(float64(2*y)+1.5)
			s.sky[y*w+x] = halfCell(top, bot)
		}
	}
	for n := 0; n < w*hz/45; n++ {
		x, y := int(hash(n, 21)%uint32(w)), int(hash(n, 22)%uint32(max(1, hz*2/3)))
		bg := toRGB(s.sky[y*w+x].Style.Bg)
		s.sky[y*w+x] = glyphCell('·', synthStarCol.mix(bg, unit(n, 23)*0.6), bg)
	}
}

// sun reports whether the half row sy at column x is inside the sun at
// time secs, and its colour there.
func (s *synthwave) sun(x, sy, secs float64) (rgb, bool) {
	hz := s.horizon()
	r := float64(s.h) * 0.62 // radius in half rows, which are as tall as a column is wide; see Render
	cx, cy := float64(s.w)/2, hz-r*0.28
	dx, dy := x+0.5-cx, sy-cy
	if dx*dx+dy*dy > r*r || sy >= hz {
		return rgb{}, false
	}
	// Below its middle the sun is cut by bands that thicken toward the
	// horizon and slide slowly down.
	if dy > -r*0.1 {
		depth := (dy + r*0.1) / (r * 1.1)
		const period = 5.0
		phase := math.Mod(dy-secs*1.2, period)
		if phase < 0 {
			phase += period
		}
		if phase < period*(0.12+0.5*depth) {
			return rgb{}, false
		}
	}
	return sunTop.mix(sunBottom, clamp01((sy-(cy-r))/(r*1.5))), true
}

// floor returns the colour of the grid at column x and half row sy.
func (s *synthwave) floor(x, sy, secs float64) rgb {
	hz := s.horizon()
	d := sy - hz + 0.5 // half rows below the horizon
	if d <= 0 {
		return horizonGlow
	}
	cx := math.Floor(float64(s.w)/2) + 0.5 // a line runs up the middle column
	const camera = 110.0                   // perspective: how deep the floor looks
	z := camera / d                        // distance into the floor
	// Lines across roll toward the viewer; their width is measured in half
	// rows, which are where the eye sees it.
	u := z*1.1 - secs*0.9
	gap := d * d / (1.1 * camera) // half rows between two lines across
	across := math.Abs(u-math.Round(u)) * gap
	// Lines along converge on the vanishing point, one every 14 columns at
	// the bottom row.
	span := 14.0 * d / (float64(s.h) * 2 * 0.42)
	v := (x + 0.5 - cx) / span
	along := math.Abs(v-math.Round(v)) * span
	// A line that leans far crosses several columns per half row; it has to
	// be as wide as that, or it breaks into dots.
	slope := math.Abs(math.Round(v)*span) / d
	width := 0.55 + 0.5*slope
	// Where lines come closer together than the cells that draw them, they
	// would only alias into noise: they fade out there instead.
	line := math.Max(clamp01(1-across/0.8)*clamp01((gap-1.5)/3), clamp01(1-along/width)*clamp01((span-3)/5))
	fade := clamp01(d / (float64(s.h) * 0.5))
	haze := clamp01(1 - d/10)
	col := floorDark.mix(horizonGlow, haze*0.35)
	return col.mix(gridLine, quantize(line*(0.35+0.65*fade), 4))
}

func (s *synthwave) Render(dst *buffer.Buffer, t time.Duration) {
	if s.resized(dst) {
		s.build()
	}
	w, h := s.w, s.h
	if w == 0 || h == 0 {
		return
	}
	frame := frames(t, synthwaveFPS)
	secs := float64(frame) / synthwaveFPS
	out := dst.Content
	copy(out, s.sky)
	hz := s.horizon()
	// Above the horizon only the sun changes; the columns it spans are the
	// only ones worth looking at there.
	r := float64(s.h) * 0.62
	sunX0 := max(0, int(float64(w)/2-r)-1)
	sunX1 := min(w, int(float64(w)/2+r)+2)
	for y := 0; y < h; y++ {
		top, bot := float64(2*y)+0.5, float64(2*y)+1.5
		x0, x1 := 0, w
		if bot < hz {
			x0, x1 = sunX0, sunX1
		}
		for x := x0; x < x1; x++ {
			fx := float64(x)
			i := y*w + x
			var ct, cb rgb
			var st, sb bool
			if top < hz {
				ct, st = s.sun(fx, top, secs)
				if !st {
					ct = s.skyAt(top)
				}
			} else {
				ct, st = s.floor(fx, top, secs), true
			}
			if bot < hz {
				cb, sb = s.sun(fx, bot, secs)
				if !sb {
					cb = s.skyAt(bot)
				}
			} else {
				cb, sb = s.floor(fx, bot, secs), true
			}
			if st || sb {
				out[i] = halfCell(ct, cb)
			}
		}
	}
}

// halfCell draws two stacked colours in one cell: an upper half block over a
// background, or a space when the halves agree, which is cheaper to send.
func halfCell(top, bot rgb) cell.Cell {
	tc, bc := top.color(), bot.color()
	if tc == bc {
		return cell.Cell{Content: ' ', Style: cell.Style{Bg: bc}}
	}
	return cell.Cell{Content: '▀', Style: cell.Style{Fg: tc, Bg: bc}}
}

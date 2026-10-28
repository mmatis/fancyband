package backdrop

import (
	"math"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

const starfieldFPS = 30

var (
	spaceDeep   = rgb{2, 3, 10}
	nebulaRose  = rgb{70, 24, 70}
	nebulaTeal  = rgb{16, 54, 72}
	nebulaWhite = rgb{120, 110, 150}
)

// Starfield is deep space drifting past: three layers of stars moving at
// different speeds in front of a still nebula, the nearest ones twinkling.
//
// Only stars move, so a frame changes a few dozen cells at most. Measured at
// 120×40 in truecolor over ten seconds (TestSceneTraffic): 14 KB a second,
// 2% of repainting every frame; 12 µs to draw a frame.
func Starfield() terminal.Backdrop { return &starfield{} }

type driftStar struct {
	x, y  float64 // position at t = 0, in cells
	layer int
	seed  int
}

type starfield struct {
	grid
	base  []cell.Cell
	stars []driftStar
}

// Cells a second each layer drifts left, and how it looks.
var (
	layerSpeed = [3]float64{0.5, 1.6, 4.2}
	layerGlyph = [3]rune{'·', '•', '✦'}
	layerColor = [3]rgb{{90, 95, 130}, {170, 175, 210}, {245, 240, 255}}
)

func (s *starfield) Interval() time.Duration { return time.Second / starfieldFPS }

func (s *starfield) build() {
	w, h := s.w, s.h
	s.base = make([]cell.Cell, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Cells are about twice as tall as wide: stretch x so the nebula
			// is not squashed.
			fx, fy := float64(x)/2, float64(y)
			n := 0.6*noise(fx/9, fy/9, 1) + 0.3*noise(fx/4, fy/4, 2) + 0.1*noise(fx/2, fy/2, 3)
			band := math.Exp(-math.Pow((fy-float64(h)*0.5-(fx-float64(w)/4)*0.35)/(float64(h)*0.35), 2))
			v := clamp01((n - 0.35) * 2.2 * band)
			hue := noise(fx/14, fy/14, 4)
			col := spaceDeep.mix(nebulaTeal.mix(nebulaRose, hue), quantize(v, 12))
			if v > 0.8 {
				col = col.mix(nebulaWhite, (v-0.8)*0.8)
			}
			s.base[y*w+x] = bgCell(col)
		}
	}
	s.stars = s.stars[:0]
	for n := 0; n < w*h/18; n++ {
		layer := 0
		switch r := hash(n, 9) % 10; {
		case r >= 9:
			layer = 2
		case r >= 6:
			layer = 1
		}
		s.stars = append(s.stars, driftStar{
			x:     unit(n, 1) * float64(w),
			y:     unit(n, 2) * float64(h),
			layer: layer,
			seed:  n,
		})
	}
}

func (s *starfield) Render(dst *buffer.Buffer, t time.Duration) {
	if s.resized(dst) {
		s.build()
	}
	w, h := s.w, s.h
	if w == 0 || h == 0 {
		return
	}
	frame := frames(t, starfieldFPS)
	secs := float64(frame) / starfieldFPS
	out := dst.Content
	copy(out, s.base)
	fw := float64(w)
	for _, st := range s.stars {
		x := math.Mod(st.x-secs*layerSpeed[st.layer], fw)
		if x < 0 {
			x += fw
		}
		i := int(st.y)*w + int(x)
		fg := layerColor[st.layer]
		r := layerGlyph[st.layer]
		if st.layer == 2 && (frame+st.seed*7)%90 < 4 {
			fg, r = rgb{255, 255, 255}, '✧'
		}
		bg := toRGB(out[i].Style.Bg)
		out[i] = glyphCell(r, fg, bg)
	}
}

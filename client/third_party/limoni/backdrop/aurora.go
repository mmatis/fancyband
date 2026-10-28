package backdrop

import (
	"math"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

const auroraFPS = 20

var (
	auroraSkyTop = rgb{3, 5, 16}
	auroraSkyLow = rgb{8, 20, 36}
	auroraGreen  = rgb{70, 240, 150}
	auroraTeal   = rgb{40, 190, 200}
	auroraViolet = rgb{150, 80, 210}
	ridgeFar     = rgb{16, 22, 42}
	ridgeNear    = rgb{7, 10, 20}
)

// Aurora is the northern lights over a mountain range: two curtains of light
// that drift, fold and shimmer above a still, starry sky.
//
// The curtains are background colour behind spaces, quantised to twelve
// levels, so a cell changes only when the light over it crosses a level.
// Light that moves everywhere is still the dearest scene to send. Measured
// at 120×40 in truecolor over ten seconds (TestSceneTraffic): 76 KB a
// second, 15% of repainting every frame; 120 µs to draw a frame.
func Aurora() terminal.Backdrop { return &aurora{} }

type auroraStar struct {
	i              int
	period, offset int
}

type aurora struct {
	grid
	sky   []rgb // per row
	ridge []int // per column: the first row of the mountains
	near  []int // per column: the first row of the nearer range
	stars []auroraStar
	star  []int32 // per cell: index into stars, or -1
}

func (a *aurora) Interval() time.Duration { return time.Second / auroraFPS }

func (a *aurora) build() {
	w, h := a.w, a.h
	a.sky = make([]rgb, h)
	for y := range a.sky {
		t := float64(y) / float64(max(1, h-1))
		a.sky[y] = auroraSkyTop.mix(auroraSkyLow, t*t)
	}
	a.ridge = make([]int, w)
	a.near = make([]int, w)
	for x := 0; x < w; x++ {
		fx := float64(x)
		far := 0.18 + 0.1*noise(fx/22, 0, 3) + 0.05*noise(fx/7, 1, 5)
		near := 0.08 + 0.07*noise(fx/15, 2, 9) + 0.02*noise(fx/4, 3, 11)
		a.ridge[x] = h - int(far*float64(h))
		a.near[x] = h - int(near*float64(h))
	}
	a.stars = a.stars[:0]
	a.star = make([]int32, w*h)
	for i := range a.star {
		a.star[i] = -1
	}
	for n := 0; n < w*h/40; n++ {
		x, y := int(hash(n, 1)%uint32(w)), int(hash(n, 2)%uint32(max(1, h*2/3)))
		if y >= a.ridge[x] {
			continue
		}
		i := y*w + x
		a.star[i] = int32(len(a.stars))
		a.stars = append(a.stars, auroraStar{i: i, period: 60 + int(hash(n, 3)%160), offset: int(hash(n, 4) % 200)})
	}
}

// curtain returns the row of a curtain's lower edge at column x, its
// brightness there, and how far the light reaches up from it, in rows.
func (a *aurora) curtain(k int, x, s float64) (edge, bright, reach float64) {
	h := float64(a.h)
	ph := float64(k) * 2.1
	edge = h*(0.36+0.1*float64(k)) +
		h*0.1*math.Sin(x*0.027+s*0.17+ph) +
		h*0.05*math.Sin(x*0.061-s*0.11+ph*1.7) +
		h*0.015*math.Sin(x*0.17+s*0.4)
	// Rays: brighter streaks that slide slowly along the curtain, over a
	// glow that never quite goes out.
	ray := 0.5 + 0.5*math.Sin(x*0.29+s*0.3+2.5*math.Sin(x*0.023+s*0.07+ph))
	ray2 := 0.5 + 0.5*math.Sin(x*0.71-s*0.45+ph)
	fold := 0.5 + 0.5*math.Sin(x*0.016+s*0.09-ph)
	bright = (0.45 + 0.4*ray*ray + 0.15*ray2) * (0.35 + 0.65*fold) * (1 - 0.3*float64(k))
	reach = h * (0.28 + 0.1*math.Sin(x*0.035+s*0.13+ph) + 0.05*ray)
	return edge, bright, reach
}

func (a *aurora) Render(dst *buffer.Buffer, t time.Duration) {
	if a.resized(dst) {
		a.build()
	}
	w, h := a.w, a.h
	if w == 0 || h == 0 {
		return
	}
	// Time moves in whole frames, so the frame the application draws and the
	// ones in between agree.
	frame := frames(t, auroraFPS)
	s := float64(frame) / auroraFPS
	out := dst.Content

	for x := 0; x < w; x++ {
		fx := float64(x)
		e0, b0, r0 := a.curtain(0, fx, s)
		e1, b1, r1 := a.curtain(1, fx, s)
		// The upward fade exp((y-e)/r), carried down the column by one
		// multiplication a row instead of an exponential a cell.
		up0, step0 := math.Exp(-e0/r0), math.Exp(1/r0)
		up1, step1 := math.Exp(-e1/r1), math.Exp(1/r1)
		for y := 0; y < h; y++ {
			fade0, fade1 := up0, up1
			up0 *= step0
			up1 *= step1
			i := y*w + x
			switch {
			case y >= a.near[x]:
				out[i] = bgCell(ridgeNear)
				continue
			case y >= a.ridge[x]:
				// The far range catches a little of the light above it.
				glow := quantize(0.5*b0*math.Exp(-math.Abs(float64(a.ridge[x])-e0)/6), 4)
				out[i] = bgCell(ridgeFar.mix(auroraTeal, glow*0.12))
				continue
			}
			fy := float64(y)
			light, height := 0.0, 0.0
			for k := 0; k < 2; k++ {
				e, b, r, fade := e0, b0, r0, fade0
				if k == 1 {
					e, b, r, fade = e1, b1, r1, fade1
				}
				d := fy - e
				var v float64
				switch {
				case d >= 4 || d < -3.5*r:
					continue // too far from the curtain to show
				case d >= 0:
					v = math.Exp(-d * d / 3) // a sharp lower edge
				default:
					v = fade // a long fade upward
				}
				v *= b
				if v > light {
					height = clamp01(-d / (r * 1.4))
				}
				light = 1 - (1-light)*(1-v) // two curtains add up
			}
			light = quantize(light, 12)
			col := a.sky[y]
			if light > 0 {
				glow := auroraGreen.mix(auroraTeal, clamp01(height*1.6))
				glow = glow.mix(auroraViolet, clamp01(height*1.4-0.4))
				col = col.mix(glow, light*0.5)
			}
			if si := a.star[i]; si >= 0 && light < 0.3 {
				st := a.stars[si]
				fg := rgb{120, 130, 170}
				r := '·'
				if (frame+st.offset)%st.period < 3 {
					fg, r = rgb{235, 240, 255}, '+'
				}
				out[i] = cell.Cell{Content: r, Style: cell.Style{Fg: fg.color(), Bg: col.color()}}
				continue
			}
			out[i] = bgCell(col)
		}
	}
}

package tiles

import (
	"image"
	"image/color"
	"math"
	"time"
)

// Filters change a tile's picture as the client draws it, as wall
// shadows do; the sheet never changes. Each filter is a chain of steps,
// all drawn inside the tile's own picture: hue, saturation, and
// lightness, limited to a color range if one is set, then blur and
// waver, outline and inner glow, sparkle, and opacity. An animation
// moves the settings toward a second set over time, in AnimSteps steps,
// so each animated tile has a small, fixed set of pictures.

// AnimSteps is how many pictures an animated filter has in one period.
const AnimSteps = 12

// variants is how many random patterns flicker and sparkle have. Each
// grid takes one, so neighbors differ while the pictures still cache.
const variants = 4

// defaultPeriod is how long a waver or sparkle takes to repeat when the
// filter has no animation of its own.
const defaultPeriod = 1.5

// Look holds the settings an animation moves between.
type Look struct {
	// Hue turns colors around the color wheel, in degrees.
	Hue float64 `json:"hue,omitempty"`
	// Saturation and Lightness scale a color's saturation and
	// brightness: -1 takes them away, 0 keeps them, and 1 doubles them.
	Saturation float64 `json:"saturation,omitempty"`
	Lightness  float64 `json:"lightness,omitempty"`
	// Fade is how see-through the tile is, from 0 for none to 1 for
	// invisible. People see it as opacity, 1 - Fade.
	Fade float64 `json:"fade,omitempty"`
	// Outline, Glow, Blur, Waver, and Sparkle are strengths from 0 to 1.
	Outline float64 `json:"outline,omitempty"`
	Glow    float64 `json:"glow,omitempty"`
	Blur    float64 `json:"blur,omitempty"`
	Waver   float64 `json:"waver,omitempty"`
	Sparkle float64 `json:"sparkle,omitempty"`
}

// Range limits the color steps to some pixels, by three pairs of limits:
// hue, from From to To degrees going up the color wheel, wrapping past
// red when To is below From, and every hue when they are equal;
// lightness (HSV value), from LightFrom to LightTo; and colorfulness
// (HSV saturation), from SatFrom to SatTo. Lightness and colorfulness go
// from 0 to 1. A gray has no hue, so it is in the range only when the
// range has every hue. A turned hue may leave the range, so reds can
// turn green.
//
// A gray has no hue to turn either, so for a gray in a range Hue picks a
// tint, as on the color wheel from red at 0 degrees, and Saturation adds
// that much of it: 50% gives a half-strength tint.
//
// Limits marks a range with all three pairs. Older ranges had hues alone,
// with Greys for the grays, and are read as they meant.
type Range struct {
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	LightFrom float64 `json:"light_from,omitempty"`
	LightTo   float64 `json:"light_to,omitempty"`
	SatFrom   float64 `json:"sat_from,omitempty"`
	SatTo     float64 `json:"sat_to,omitempty"`
	Limits    bool    `json:"limits,omitempty"`
	Greys     bool    `json:"greys,omitempty"`
}

// grayLimit is the saturation below which a pixel counts as gray.
const grayLimit = 0.2

// GrayLimit is the colorfulness below which a pixel counts as gray, as a
// fraction.
const GrayLimit = grayLimit

// Normal returns the range with all three pairs of limits, its hues from
// 0 up to 360 degrees: an older range as it was meant.
func (r Range) Normal() Range {
	if !r.Limits {
		if r.Greys {
			r = Range{SatTo: grayLimit}
		} else {
			r = Range{From: r.From, To: r.To, SatFrom: grayLimit, SatTo: 1}
		}
		r.LightTo, r.Limits = 1, true
	}
	r.From, r.To = math.Mod(math.Mod(r.From, 360)+360, 360), math.Mod(math.Mod(r.To, 360)+360, 360)
	return r
}

// AllHues reports whether the range has every hue.
func (r Range) AllHues() bool {
	n := r.Normal()
	return n.From == n.To
}

// HasHue reports whether hue h, in degrees, is in the range's hues.
func (r Range) HasHue(h float64) bool {
	n := r.Normal()
	if n.From == n.To {
		return true
	}
	d := math.Mod(math.Mod(h-n.From, 360)+360, 360)
	w := math.Mod(n.To-n.From+360, 360)
	return d <= w
}

// Has reports whether a color, as hue, saturation, and value, is in the
// range.
func (r Range) Has(h, s, v float64) bool {
	const eps = 1e-9
	n := r.Normal()
	if v < n.LightFrom-eps || v > n.LightTo+eps || s < n.SatFrom-eps || s > n.SatTo+eps {
		return false
	}
	if s < grayLimit {
		return n.From == n.To
	}
	return r.HasHue(h)
}

// FromHSV returns the color with hue h in degrees, and saturation s and
// value v from 0 to 1.
func FromHSV(h, s, v float64) color.RGBA { return fromHSV(math.Mod(h+720, 360), s, v, 255) }

// RGB is a color for an outline or a glow.
type RGB [3]uint8

// Filter is everything one tile's filter does.
type Filter struct {
	Look
	Range        *Range `json:"range,omitempty"`
	OutlineColor RGB    `json:"outline_color"`
	GlowColor    RGB    `json:"glow_color"`
	// SparkleColor is what the sparkles light pixels toward, or nil for
	// white, as filters made before it had.
	SparkleColor *RGB `json:"sparkle_color,omitempty"`
	// Terrain lets blur and waver change terrain, which they leave alone
	// otherwise: on terrain they show seams between grids.
	Terrain bool  `json:"terrain,omitempty"`
	Anim    *Anim `json:"anim,omitempty"`
}

// Curves an animation can follow, from its filter's own settings (0) to
// its second set (1) and back.
const (
	CurveSmooth   = "smooth"   // eases there and back
	CurveTriangle = "triangle" // straight there and back
	CurveBlink    = "blink"    // half the time each
	CurveFlicker  = "flicker"  // a random amount at each step
)

// Spreads say how the grids that show a tile differ in time.
const (
	SpreadTogether = "together" // all in step
	SpreadRipple   = "ripple"   // a wave along the diagonals
	SpreadRandom   = "random"   // each grid its own phase
)

// Anim moves a filter's settings toward To and back, once a Period.
type Anim struct {
	To     Look    `json:"to"`
	Curve  string  `json:"curve"`
	Period float64 `json:"period"` // seconds
	Spread string  `json:"spread"`
}

// Colors for outlines and glows to start with.
var (
	Gold  = RGB{255, 200, 40}
	White = RGB{255, 255, 255}
)

// NewFilter returns a filter that changes nothing, with a gold outline
// and a white glow ready for when they are turned up.
func NewFilter() Filter {
	return Filter{OutlineColor: Gold, GlowColor: White}
}

// Animated reports whether the filter changes over time.
func (f *Filter) Animated() bool {
	if f == nil {
		return false
	}
	return f.Anim != nil || f.Waver > 0 || f.Sparkle > 0
}

// random reports whether the filter's pictures differ from grid to grid.
func (f *Filter) random() bool {
	if f == nil {
		return false
	}
	return f.Sparkle > 0 || f.Anim != nil && (f.Anim.Curve == CurveFlicker || f.Anim.To.Sparkle > 0)
}

// Plain reports whether the filter changes nothing.
func (f *Filter) Plain() bool {
	return f == nil || f.Look == (Look{}) && f.Anim == nil
}

// StepAt returns which of the AnimSteps pictures a grid at gx, gy shows
// at time t, and which random variant.
func (f *Filter) StepAt(t time.Duration, gx, gy int) (step, variant int) {
	if f.random() {
		variant = int(gridHash(gx, gy, 0) % variants)
	}
	if !f.Animated() {
		return 0, variant
	}
	period, spread := defaultPeriod, SpreadTogether
	if f.Anim != nil {
		period, spread = max(f.Anim.Period, 0.1), f.Anim.Spread
	}
	step = int(t.Seconds() / period * AnimSteps)
	switch spread {
	case SpreadRipple:
		step += gx + gy
	case SpreadRandom:
		step += int(gridHash(gx, gy, 1) % AnimSteps)
	}
	return ((step % AnimSteps) + AnimSteps) % AnimSteps, variant
}

// lookAt returns the settings at a step.
func (f *Filter) lookAt(step, variant int) Look {
	if f.Anim == nil {
		return f.Look
	}
	c := curve(f.Anim.Curve, step, variant)
	a, b := f.Look, f.Anim.To
	mix := func(x, y float64) float64 { return x + c*(y-x) }
	return Look{
		Hue: mix(a.Hue, b.Hue), Saturation: mix(a.Saturation, b.Saturation),
		Lightness: mix(a.Lightness, b.Lightness), Fade: mix(a.Fade, b.Fade),
		Outline: mix(a.Outline, b.Outline), Glow: mix(a.Glow, b.Glow),
		Blur: mix(a.Blur, b.Blur), Waver: mix(a.Waver, b.Waver), Sparkle: mix(a.Sparkle, b.Sparkle),
	}
}

// curve returns how far toward the second settings an animation is at a
// step, from 0 to 1. Every curve repeats after AnimSteps steps.
func curve(name string, step, variant int) float64 {
	step = ((step % AnimSteps) + AnimSteps) % AnimSteps
	x := float64(step) / AnimSteps
	switch name {
	case CurveTriangle:
		return 1 - math.Abs(2*x-1)
	case CurveBlink:
		if step < AnimSteps/2 {
			return 0
		}
		return 1
	case CurveFlicker:
		return float64(gridHash(step, variant, 2)%1000) / 999
	}
	return (1 - math.Cos(2*math.Pi*x)) / 2
}

// gridHash mixes three numbers into one, the same each time.
func gridHash(a, b, c int) uint32 {
	h := uint32(2166136261)
	for _, v := range []int{a, b, c} {
		h ^= uint32(v)
		h *= 16777619
		h ^= h >> 13
	}
	return h
}

// apply runs the filter's steps on img, a tile alone on see-through
// pixels, at a step and variant. terrain says the tile is terrain, so
// blur and waver leave it alone unless the filter says otherwise.
func (f *Filter) apply(img *image.RGBA, step, variant int, terrain bool) {
	l := f.lookAt(step, variant)
	if l.Hue != 0 || l.Saturation != 0 || l.Lightness != 0 {
		recolor(img, l, f.Range)
	}
	if !terrain || f.Terrain {
		if l.Blur > 0 {
			blur(img, int(math.Round(l.Blur*float64(img.Bounds().Dx())/6)))
		}
		if l.Waver > 0 {
			waver(img, l.Waver, step)
		}
	}
	if l.Outline > 0 || l.Glow > 0 {
		edges(img, l, f.OutlineColor, f.GlowColor)
	}
	if l.Sparkle > 0 {
		c := White
		if f.SparkleColor != nil {
			c = *f.SparkleColor
		}
		sparkle(img, l.Sparkle, c, step, variant)
	}
	if l.Fade > 0 {
		keep := 1 - min(l.Fade, 1)
		for i := 0; i < len(img.Pix); i++ {
			img.Pix[i] = uint8(float64(img.Pix[i])*keep + 0.5)
		}
	}
}

// recolor turns hues and scales saturation and brightness, for the
// pixels in r, or every colored pixel without one. img holds
// premultiplied colors, so each pixel is unpremultiplied first.
func recolor(img *image.RGBA, l Look, r *Range) {
	for i := 0; i+3 < len(img.Pix); i += 4 {
		a := img.Pix[i+3]
		if a == 0 {
			continue
		}
		p := unpremul(img.Pix[i : i+4 : i+4])
		h, s, v := toHSV(p)
		switch {
		case r != nil && !r.Has(h, s, v):
			continue
		case r != nil && s < grayLimit:
			// A tint of the hue Hue names, as strong as Saturation.
			h = l.Hue
			s = min(max(s+l.Saturation, 0), 1)
		default:
			h += l.Hue
			s = min(max(s*(1+l.Saturation), 0), 1)
		}
		v = min(max(v*(1+l.Lightness), 0), 1)
		premul(img.Pix[i:i+4:i+4], fromHSV(math.Mod(h+720, 360), s, v, a))
	}
}

func unpremul(px []uint8) color.RGBA {
	a := px[3]
	if a == 255 {
		return color.RGBA{px[0], px[1], px[2], a}
	}
	un := func(c uint8) uint8 { return uint8(min(int(c)*255/int(a), 255)) }
	return color.RGBA{un(px[0]), un(px[1]), un(px[2]), a}
}

func premul(px []uint8, c color.RGBA) {
	m := func(v uint8) uint8 { return uint8((int(v)*int(c.A) + 127) / 255) }
	px[0], px[1], px[2], px[3] = m(c.R), m(c.G), m(c.B), c.A
}

// blur averages each pixel with those within radius, across and then
// down. Past the tile's edge is see-through.
func blur(img *image.RGBA, radius int) {
	if radius < 1 {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	src := append([]uint8(nil), img.Pix...)
	pass := func(n, lines int, at func(line, i int) int) {
		for line := 0; line < lines; line++ {
			for i := 0; i < n; i++ {
				var sum [4]int
				for d := -radius; d <= radius; d++ {
					if j := i + d; j >= 0 && j < n {
						o := at(line, j)
						for c := 0; c < 4; c++ {
							sum[c] += int(src[o+c])
						}
					}
				}
				o := at(line, i)
				for c := 0; c < 4; c++ {
					img.Pix[o+c] = uint8(sum[c] / (2*radius + 1))
				}
			}
		}
		copy(src, img.Pix)
	}
	pass(w, h, func(y, x int) int { return img.PixOffset(b.Min.X+x, b.Min.Y+y) })
	pass(h, w, func(x, y int) int { return img.PixOffset(b.Min.X+x, b.Min.Y+y) })
}

// waver pushes each row sideways along a wave that moves with the step.
func waver(img *image.RGBA, strength float64, step int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	amp := strength * float64(w) / 8
	src := append([]uint8(nil), img.Pix...)
	for y := 0; y < h; y++ {
		phase := 2 * math.Pi * (2*float64(y)/float64(h) + float64(step)/AnimSteps)
		dx := int(math.Round(amp * math.Sin(phase)))
		for x := 0; x < w; x++ {
			o := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			sx := x - dx
			if sx < 0 || sx >= w {
				copy(img.Pix[o:o+4], []uint8{0, 0, 0, 0})
				continue
			}
			so := img.PixOffset(b.Min.X+sx, b.Min.Y+y)
			copy(img.Pix[o:o+4], src[so:so+4])
		}
	}
}

// solid is how opaque a pixel must be to count as part of the tile's
// colored shape for outlines and glows.
const solid = 128

// edges draws the outline and the inner glow. Both sit on the tile's
// colored pixels, measured from where they meet see-through ones; the
// tile's border does not count as an edge, so opaque terrain gets none.
// See-through pixels never change.
func edges(img *image.RGBA, l Look, outline, glow RGB) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dist := edgeDistance(img)
	width := max(1, w/32)
	reach := max(2, w/5)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := dist[y*w+x]
			if d == 0 {
				continue
			}
			o := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			p := unpremul(img.Pix[o : o+4 : o+4])
			if l.Glow > 0 && d <= reach {
				p = mixRGB(p, glow, l.Glow*(1-float64(d-1)/float64(reach)))
			}
			if l.Outline > 0 && d <= width {
				p = mixRGB(p, outline, min(l.Outline, 1))
			}
			premul(img.Pix[o:o+4:o+4], p)
		}
	}
}

// edgeDistance returns, for each pixel, how many pixels away the nearest
// see-through pixel is, counting diagonals as one, or 0 for see-through
// pixels themselves. Pixels with no see-through pixel in the tile get a
// large number.
func edgeDistance(img *image.RGBA) []int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	far := w + h
	d := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if img.Pix[img.PixOffset(b.Min.X+x, b.Min.Y+y)+3] >= solid {
				d[y*w+x] = far
			}
		}
	}
	at := func(x, y int) int {
		if x < 0 || y < 0 || x >= w || y >= h {
			return far
		}
		return d[y*w+x]
	}
	// Two passes of a chessboard distance transform.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if d[y*w+x] != 0 {
				d[y*w+x] = min(d[y*w+x], at(x-1, y)+1, at(x, y-1)+1, at(x-1, y-1)+1, at(x+1, y-1)+1)
			}
		}
	}
	for y := h - 1; y >= 0; y-- {
		for x := w - 1; x >= 0; x-- {
			if d[y*w+x] != 0 {
				d[y*w+x] = min(d[y*w+x], at(x+1, y)+1, at(x, y+1)+1, at(x+1, y+1)+1, at(x-1, y+1)+1)
			}
		}
	}
	return d
}

func mixRGB(p color.RGBA, c RGB, t float64) color.RGBA {
	m := func(a, b uint8) uint8 { return uint8(float64(a) + t*(float64(b)-float64(a)) + 0.5) }
	return color.RGBA{m(p.R, c[0]), m(p.G, c[1]), m(p.B, c[2]), p.A}
}

// sparkle lights a few of the colored pixels, different ones at each
// step and in each variant.
func sparkle(img *image.RGBA, strength float64, c RGB, step, variant int) {
	b := img.Bounds()
	limit := uint32(strength * 60) // in a thousand
	step = ((step % AnimSteps) + AnimSteps) % AnimSteps
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			o := img.PixOffset(x, y)
			if img.Pix[o+3] < solid || gridHash(x*131+y, step, variant+3)%1000 >= limit {
				continue
			}
			premul(img.Pix[o:o+4:o+4], mixRGB(unpremul(img.Pix[o:o+4:o+4]), c, 0.85))
		}
	}
}

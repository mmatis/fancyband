package backdrop

import (
	"math"
	"math/rand"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

const cityFPS = 30

var (
	citySkyTop     = rgb{6, 8, 24}
	citySkyHorizon = rgb{52, 26, 70}
	moonLight      = rgb{246, 236, 196}
	windowWarm     = rgb{255, 206, 120}
	windowCool     = rgb{170, 220, 255}
	windowOff      = rgb{34, 34, 54}
	railSteel      = rgb{70, 76, 96}
	trainBody      = rgb{44, 50, 70}
	waterDeep      = rgb{4, 8, 22}
)

// craters on the moon: centre and radius, in moon radii.
var craters = [...][3]float64{
	{-0.35, -0.25, 0.24}, {0.32, 0.2, 0.2}, {0.05, -0.55, 0.13},
	{-0.25, 0.5, 0.16}, {0.55, -0.3, 0.1},
}

// City is a city at night over water: a moon, twinkling stars, lit windows
// that go on and off, a train crossing an elevated line, a beacon, a shooting
// star now and then, and the whole skyline reflected in the water with
// glints. It is the scene of examples/xray, without the X-ray.
//
// Most of it is still from one frame to the next; the train is what costs.
// Measured at 120×40 in truecolor over ten seconds (TestSceneTraffic):
// 40 KB a second, 3% of repainting every frame; 17 µs to draw a frame.
func City() terminal.Backdrop { return &city{} }

type cityStar struct {
	i              int // cell index
	period, offset int
}

type cityWindow struct {
	i      int
	cool   bool
	period int // frames between the window's chances to change
	offset int
}

type city struct {
	grid
	horizon  int
	base     []cell.Cell // sky, moon, buildings and rail: what never moves
	sky      []bool      // cells of base that are open sky
	stars    []cityStar
	windows  []cityWindow
	antennaX int
	antennaY int
	trainLen int
}

func (c *city) Interval() time.Duration { return time.Second / cityFPS }

func (c *city) skyAt(y int) rgb {
	t := float64(y) / float64(max(1, c.horizon-1))
	return citySkyTop.mix(citySkyHorizon, t*t)
}

// build lays out the city for a new window size. The layout is random, but
// always from the same seed, so a size always gets the same city.
func (c *city) build() {
	w, h := c.w, c.h
	rng := rand.New(rand.NewSource(7))
	water := min(max(3, h/4), h/2) // a tiny window is mostly sky
	c.horizon = h - water
	c.base = make([]cell.Cell, w*h)
	c.sky = make([]bool, w*h)
	c.stars, c.windows = c.stars[:0], c.windows[:0]

	set := func(x, y int, cl cell.Cell) {
		if x >= 0 && y >= 0 && x < w && y < h {
			c.base[y*w+x] = cl
			c.sky[y*w+x] = false
		}
	}

	// Sky, with a halo around the moon.
	moonR := math.Max(2, float64(h)/9)
	moonX, moonY := float64(w)*0.8, float64(h)*0.2
	for y := 0; y < c.horizon; y++ {
		base := c.skyAt(y)
		for x := 0; x < w; x++ {
			dx := (float64(x) - moonX) / 2
			dy := float64(y) - moonY
			col := base
			if d := math.Sqrt(dx*dx+dy*dy) / (moonR * 3.2); d < 1 {
				col = base.mix(rgb{60, 58, 84}, (1-d)*(1-d)*0.8)
			}
			c.base[y*w+x] = bgCell(col)
			c.sky[y*w+x] = true
		}
	}

	// The moon, at twice the vertical resolution with half blocks.
	for y := int(moonY - moonR - 1); y <= int(moonY+moonR+1); y++ {
		for x := int(moonX - moonR*2 - 2); x <= int(moonX+moonR*2+2); x++ {
			if x < 0 || y < 0 || x >= w || y >= c.horizon {
				continue
			}
			at := func(sub float64) (mx, my float64) {
				return (float64(x) - moonX) / 2 / moonR, (float64(y) + sub - moonY) / moonR
			}
			in := func(sub float64) bool {
				mx, my := at(sub)
				return mx*mx+my*my <= 1
			}
			shade := func(sub float64) rgb {
				mx, my := at(sub)
				k := 1 - 0.2*(mx*mx+my*my)
				for _, cr := range craters {
					dx, dy := mx-cr[0], my-cr[1]
					if dx*dx+dy*dy < cr[2]*cr[2] {
						k -= 0.13
					}
				}
				return moonLight.scale(k)
			}
			upper, lower := in(0.25), in(0.75)
			bg := toRGB(c.base[y*w+x].Style.Bg)
			switch {
			case upper && lower:
				set(x, y, glyphCell('▀', shade(0.25), shade(0.75)))
			case upper:
				set(x, y, glyphCell('▀', shade(0.25), bg))
			case lower:
				set(x, y, glyphCell('▄', shade(0.75), bg))
			}
		}
	}

	// Stars, on open sky only.
	for i := 0; i < w*c.horizon/28; i++ {
		x, y := rng.Intn(w), rng.Intn(max(1, c.horizon*2/3))
		c.stars = append(c.stars, cityStar{i: y*w + x, period: 40 + rng.Intn(120), offset: rng.Intn(160)})
	}

	// Skyline: buildings stand on the row above the water.
	tallest, tallestX := 0, 0
	for x := 0; x < w; {
		bw := 4 + rng.Intn(8)
		minH := c.horizon / 4
		bh := minH + rng.Intn(max(1, c.horizon*3/5-minH))
		if rng.Intn(6) == 0 {
			bh = c.horizon * 3 / 4 // an occasional tower
		}
		top := c.horizon - bh
		x1 := min(w, x+bw)
		shade := 16 + rng.Float64()*14
		body := rgb{shade, shade + 2, shade + 16}
		for y := max(0, top); y < c.horizon; y++ {
			for bx := x; bx < x1; bx++ {
				set(bx, y, bgCell(body))
			}
		}
		if top >= 0 {
			for bx := x; bx < x1; bx++ {
				// The roof edge catches a little moonlight.
				set(bx, top, glyphCell('▁', body.mix(moonLight, 0.25), c.skyAt(top)))
			}
		}
		if bh > tallest {
			tallest, tallestX = bh, x+bw/2
		}
		for wy := top + 1; wy < c.horizon-3; wy += 2 {
			for wx := x + 1; wx < x1-1; wx += 2 {
				if wy >= 0 {
					c.windows = append(c.windows, cityWindow{
						i:      wy*w + wx,
						cool:   rng.Intn(5) == 0,
						period: cityFPS * (60 + rng.Intn(180)),
						offset: rng.Intn(cityFPS * 240),
					})
				}
			}
		}
		x += bw + rng.Intn(2)
	}
	c.antennaX, c.antennaY = tallestX, c.horizon-tallest-3
	if c.antennaY >= 0 && c.antennaX < w {
		for y := c.antennaY + 1; y < c.antennaY+3; y++ {
			set(c.antennaX, y, glyphCell('│', rgb{90, 90, 110}, c.skyAt(y)))
		}
	}

	// The elevated rail.
	rail := c.horizon - 1
	for x := 0; x < w; x++ {
		set(x, rail, glyphCell('▀', railSteel, toRGB(c.base[rail*w+x].Style.Bg)))
	}

	// Keep the stars that landed on open sky.
	n := 0
	for _, s := range c.stars {
		if c.sky[s.i] {
			c.stars[n] = s
			n++
		}
	}
	c.stars = c.stars[:n]
	c.trainLen = max(18, w/3)
}

func (c *city) Render(dst *buffer.Buffer, t time.Duration) {
	if c.resized(dst) {
		c.build()
	}
	w, h := c.w, c.h
	if w == 0 || h == 0 {
		return
	}
	frame := frames(t, cityFPS)
	out := dst.Content
	copy(out, c.base)
	set := func(x, y int, r rune, fg, bg rgb) {
		if x >= 0 && y >= 0 && x < w && y < h {
			out[y*w+x] = glyphCell(r, fg, bg)
		}
	}
	bgAt := func(x, y int) rgb { return toRGB(out[y*w+x].Style.Bg) }

	// Stars twinkle one at a time.
	for _, s := range c.stars {
		bg := toRGB(out[s.i].Style.Bg)
		switch phase := (frame + s.offset) % s.period; {
		case phase < 3:
			out[s.i] = glyphCell('✦', rgb{255, 250, 220}, bg)
		case phase < 6:
			out[s.i] = glyphCell('+', rgb{200, 200, 230}, bg)
		default:
			out[s.i] = glyphCell('·', rgb{130, 130, 170}, bg)
		}
	}

	// A shooting star in some six-second windows, drawn over open sky.
	const shootCycle = cityFPS * 6
	if k := frame / shootCycle; hash(k, 0)%3 == 0 {
		start := int(hash(k, 1) % (shootCycle - 30))
		if age := frame%shootCycle - start; age >= 0 && age <= 26 {
			x0 := float64(hash(k, 2) % uint32(max(1, w/2)))
			y0 := float64(hash(k, 3) % uint32(max(1, c.horizon/4)))
			for i := 0; i < 7; i++ {
				x := int(x0 + float64(age)*1.6 - float64(i)*1.6)
				y := int(y0 + float64(age)*0.55 - float64(i)*0.55)
				if x < 0 || y < 0 || x >= w || y >= c.horizon-2 || !c.sky[y*w+x] {
					continue
				}
				r := '━'
				if i == 0 {
					r = '✦'
				}
				k := 1 - float64(i)/7
				set(x, y, r, rgb{255, 245, 210}.scale(0.35+0.65*k), c.skyAt(y))
			}
		}
	}

	// Windows: each gets a chance to change every minute or few.
	for n, wn := range c.windows {
		bg := toRGB(out[wn.i].Style.Bg)
		lit := hash(n, (frame+wn.offset)/wn.period)%3 != 0
		switch {
		case lit && wn.cool:
			out[wn.i] = glyphCell('▪', windowCool, bg)
		case lit:
			out[wn.i] = glyphCell('▪', windowWarm, bg)
		default:
			out[wn.i] = glyphCell('▪', windowOff, bg)
		}
	}

	// A slow red beacon on the tallest tower.
	if c.antennaY >= 0 && c.antennaX < w {
		beacon := rgb{90, 30, 40}
		if (frame/cityFPS)%2 == 0 {
			beacon = rgb{255, 60, 70}
		}
		set(c.antennaX, c.antennaY, '●', beacon, c.skyAt(c.antennaY))
	}

	// The train crosses right to left, then waits two seconds.
	rail := c.horizon - 1
	const speed = 0.9 // cells a frame
	travel := int(float64(w+c.trainLen+10) / speed)
	if f := frame % (travel + cityFPS*2); f < travel && rail >= 3 {
		tx := int(math.Round(float64(w+4) - speed*float64(f)))
		for i := 0; i < c.trainLen; i++ {
			x := tx + i
			if x < 0 || x >= w {
				continue
			}
			car := i % 12
			roof, body := rail-3, rail-2
			switch {
			case i == 0:
				set(x, roof, '▗', trainBody, bgAt(x, roof))
				set(x, body, '█', trainBody, trainBody)
			case car == 11:
				// the gap between two cars
			default:
				set(x, roof, '▄', trainBody, bgAt(x, roof))
				if car%3 == 1 {
					set(x, body, '█', windowCool.mix(windowWarm, 0.3), trainBody)
				} else {
					set(x, body, ' ', trainBody, trainBody)
				}
			}
			set(x, rail, '▀', railSteel, trainBody.scale(0.7))
		}
		// The headlight.
		for i := 1; i <= 4; i++ {
			if x := tx - i; x >= 0 && x < w {
				bg := bgAt(x, rail-2)
				set(x, rail-2, ' ', bg, bg.mix(rgb{255, 240, 180}, 0.5/float64(i)))
			}
		}
	}

	// Water: the city mirrored, darkened and blued.
	for y := c.horizon; y < h; y++ {
		src := c.horizon - 1 - (y - c.horizon)
		if src < 0 {
			src = 0
		}
		depth := float64(y-c.horizon+1) / float64(h-c.horizon+1)
		for x := 0; x < w; x++ {
			s := out[src*w+x]
			fg := toRGB(s.Style.Fg).mix(waterDeep, 0.45+0.35*depth)
			bg := toRGB(s.Style.Bg).mix(waterDeep, 0.5+0.4*depth)
			r := s.Content
			switch r {
			case '▀':
				r = '▄'
			case '▄':
				r = '▀'
			case '▗':
				r = '▝'
			case '▁':
				r = '▔'
			}
			if r == ' ' || fg == bg {
				out[y*w+x] = bgCell(bg)
			} else {
				out[y*w+x] = glyphCell(r, fg, bg)
			}
		}
	}
	// Ripples break the reflection up on fixed columns.
	for y := c.horizon; y < h; y++ {
		for x := (y * 7) % 11; x < w; x += 11 {
			bg := bgAt(x, y)
			set(x, y, '─', bg.mix(rgb{120, 140, 200}, 0.25), bg)
		}
	}
	// Glints: sixteen slots, each moving to another spot every few frames.
	if water := (h - c.horizon) * w; water > 0 {
		for j := 0; j < 16; j++ {
			period := 5 + j%7
			i := c.horizon*w + int(hash(j+1000, (frame+j*3)/period)%uint32(water))
			bg := toRGB(out[i].Style.Bg)
			out[i] = glyphCell('━', bg.mix(moonLight, 0.55), bg)
		}
	}
}

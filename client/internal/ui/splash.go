package ui

import (
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
)

// splashName is the splash screen's name in nativeScreens and in the
// settings' list of classic screens. It has no capability: the engine
// waits at its own splash screen either way, and the client draws over
// it.
const splashName = "splash"

// nativeSplash reports whether the client draws its own splash screen.
func (m *Model) nativeSplash() bool { return !slices.Contains(m.settings.Classic, splashName) }

// The block letters' glyphs, from the FIGlet font ANSI Shadow, six rows
// each. Block cells are the letters' faces; the line-drawing ones are
// their shadows.
var blockGlyphs = map[rune][6]string{
	'F': {
		"███████╗",
		"██╔════╝",
		"█████╗  ",
		"██╔══╝  ",
		"██║     ",
		"╚═╝     ",
	},
	'A': {
		" █████╗ ",
		"██╔══██╗",
		"███████║",
		"██╔══██║",
		"██║  ██║",
		"╚═╝  ╚═╝",
	},
	'N': {
		"███╗   ██╗",
		"████╗  ██║",
		"██╔██╗ ██║",
		"██║╚██╗██║",
		"██║ ╚████║",
		"╚═╝  ╚═══╝",
	},
	'C': {
		" ██████╗",
		"██╔════╝",
		"██║     ",
		"██║     ",
		"╚██████╗",
		" ╚═════╝",
	},
	'Y': {
		"██╗   ██╗",
		"╚██╗ ██╔╝",
		" ╚████╔╝ ",
		"  ╚██╔╝  ",
		"   ██║   ",
		"   ╚═╝   ",
	},
	'B': {
		"██████╗ ",
		"██╔══██╗",
		"██████╔╝",
		"██╔══██╗",
		"██████╔╝",
		"╚═════╝ ",
	},
	'D': {
		"██████╗ ",
		"██╔══██╗",
		"██║  ██║",
		"██║  ██║",
		"██████╔╝",
		"╚═════╝ ",
	},
}

// blockText spells word in the block letters, one string a row, every
// row as wide as the others.
func blockText(word string) []string {
	rows := make([]string, 6)
	for _, r := range word {
		g, ok := blockGlyphs[r]
		if !ok {
			continue
		}
		w := 0
		for _, line := range g {
			w = max(w, len([]rune(line)))
		}
		for i, line := range g {
			rows[i] += line + strings.Repeat(" ", w-len([]rune(line)))
		}
	}
	return rows
}

// splashTitle is "FANCYBAND" in block letters.
var splashTitle = blockText("FANCYBAND")

// The text under the title, as the game's own splash screen,
// lib/screens/news.txt, has it.
var (
	splashQuote = []string{
		`"When the world is old and the Powers grow weary, then Morgoth,`,
		` seeing that the guard sleepeth, shall come back through the`,
		` Door of Night out of the Timeless Void.  Then shall the Last`,
		` Battle be gathered..."`,
	}
	splashCredits = []string{
		"A variant of Angband 4.2.6: http://rephial.org/",
		"Forums: https://angband.live/forums/",
		"For help press '?' in-game",
	}
)

// firePalette turns heat into color, black through red, orange, and
// yellow to white: the palette of the "Doom fire" method, with heat 0
// as the black of the screen behind it.
var firePalette = [...][3]uint8{
	{0x00, 0x00, 0x00}, {0x1F, 0x07, 0x07}, {0x2F, 0x0F, 0x07}, {0x47, 0x0F, 0x07},
	{0x57, 0x17, 0x07}, {0x67, 0x1F, 0x07}, {0x77, 0x1F, 0x07}, {0x8F, 0x27, 0x07},
	{0x9F, 0x2F, 0x07}, {0xAF, 0x3F, 0x07}, {0xBF, 0x47, 0x07}, {0xC7, 0x47, 0x07},
	{0xDF, 0x4F, 0x07}, {0xDF, 0x57, 0x07}, {0xDF, 0x57, 0x07}, {0xD7, 0x5F, 0x07},
	{0xD7, 0x5F, 0x07}, {0xD7, 0x67, 0x0F}, {0xCF, 0x6F, 0x0F}, {0xCF, 0x77, 0x0F},
	{0xCF, 0x7F, 0x0F}, {0xCF, 0x87, 0x17}, {0xC7, 0x87, 0x17}, {0xC7, 0x8F, 0x17},
	{0xC7, 0x97, 0x1F}, {0xBF, 0x9F, 0x1F}, {0xBF, 0x9F, 0x1F}, {0xBF, 0xA7, 0x27},
	{0xBF, 0xA7, 0x27}, {0xBF, 0xAF, 0x2F}, {0xB7, 0xAF, 0x2F}, {0xB7, 0xB7, 0x2F},
	{0xB7, 0xB7, 0x37}, {0xCF, 0xCF, 0x6F}, {0xDF, 0xDF, 0x9F}, {0xEF, 0xEF, 0xC7},
	{0xFF, 0xFF, 0xFF},
}

// maxHeat is the hottest heat, white.
const maxHeat = len(firePalette) - 1

// fireSeed seeds the fire, so its still frame is always the same.
const fireSeed = 4242

// stillSteps is how many steps the still frame burns for, enough for
// flames to reach the top.
const stillSteps = 60

// fire is a "Doom fire": a grid of heat that rises from a source row
// under it, drifting sideways and cooling at random. Each cell shows two
// rows of it. The source burns unevenly, with hot spots that drift
// along it, so the flames rise in tongues rather than as a wall.
type fire struct {
	w, h int
	// heat holds h+1 rows, top first; the last is the source, which
	// does not show.
	heat []uint8
	// cool is how much a row cools by at the least; it cools by up to
	// two more at random.
	cool int
	// t is the time, in steps, which moves the hot spots.
	t   int
	rng *rand.Rand
}

// newFire makes a fire w pixels wide and h high, burning from its
// source, and lets it burn to its still frame.
func newFire(w, h int) *fire {
	f := &fire{w: w, h: h, heat: make([]uint8, w*(h+1)), rng: rand.New(rand.NewPCG(fireSeed, fireSeed))}
	// A row cools by cool+1 on average, so the hottest tongues reach
	// the top and the rest die out on the way.
	f.cool = max(maxHeat/h-2, 0)
	for i := 0; i < stillSteps; i++ {
		f.step()
	}
	return f
}

// fuel is how hot the source burns at column x at time t, from 0 to 1:
// two waves that drift opposite ways, so hot spots grow, merge, and
// part.
func fuel(x, t int) float64 {
	fx, ft := float64(x), float64(t)
	v := 0.5 + 0.3*math.Sin(fx*0.42+ft*0.31) + 0.2*math.Sin(fx*0.17-ft*0.23+1.7)
	return min(max(v, 0), 1)
}

// step moves the fire on by one frame: the source's hot spots drift,
// and each pixel takes the heat of the one under it, a little cooler,
// drifting left or right.
func (f *fire) step() {
	f.t++
	for x := 0; x < f.w; x++ {
		v := fuel(x, f.t)
		f.heat[f.h*f.w+x] = uint8(float64(maxHeat) * (0.3 + 0.7*v*v))
	}
	for y := 0; y < f.h; y++ {
		for x := 0; x < f.w; x++ {
			src := int(f.heat[(y+1)*f.w+x])
			dx := x - f.rng.IntN(3) + 1
			if dx < 0 || dx >= f.w {
				dx = x
			}
			f.heat[y*f.w+dx] = uint8(max(src-f.cool-f.rng.IntN(3), 0))
		}
	}
}

// at returns the heat at x, y, where y may be the source row, h.
func (f *fire) at(x, y int) int {
	if x < 0 || x >= f.w || y < 0 || y > f.h {
		return 0
	}
	return int(f.heat[y*f.w+x])
}

// fireColor is heat's color.
func fireColor(heat int) cell.Color {
	c := firePalette[min(max(heat, 0), maxHeat)]
	return limoni.RGB(c[0], c[1], c[2])
}

// splashScreen is Fancyband's own splash screen: the title in block
// letters with fire above it, and the game's quote and credits under it.
// The start menu sits below, as a screen of its own.
type splashScreen struct {
	m    *Model
	fire *fire
}

func newSplash(m *Model) *splashScreen { return &splashScreen{m: m} }

// splashPlan says where each part of the splash screen goes, in rows
// from the top of the area, or -1 for a part left out.
type splashPlan struct {
	fireRows                     int
	fire, title, version         int
	quote, credits, menu, height int
}

// Layout sizes.
const (
	splashFireRows    = 6
	splashMinFireRows = 2
)

// splashLayout fits the splash screen and a menu menuH rows high into h rows,
// leaving out the quote and then the credits, and shrinking the fire,
// when they do not fit.
func splashLayout(h, menuH int) splashPlan {
	quote, credits, fireRows := true, true, splashFireRows
	need := func() int {
		n := fireRows + len(splashTitle) + 1 + 1 + menuH
		if quote {
			n += len(splashQuote) + 1
		}
		if credits {
			n += len(splashCredits) + 1
		}
		return n
	}
fitting:
	for need() > h {
		switch {
		case quote:
			quote = false
		case credits:
			credits = false
		case fireRows > splashMinFireRows:
			fireRows--
		default:
			// Too short even so: the menu may cover the version.
			break fitting
		}
	}
	p := splashPlan{fireRows: fireRows, quote: -1, credits: -1}
	top := max((h-need())/2, 0)
	p.fire = top
	p.title = top + fireRows
	p.version = p.title + len(splashTitle)
	y := p.version + 2
	if quote {
		p.quote = y
		y += len(splashQuote) + 1
	}
	if credits {
		p.credits = y
		y += len(splashCredits) + 1
	}
	p.menu = min(y, max(h-menuH, 0))
	p.height = need()
	return p
}

// menuHeight is the start menu's height, or a guess before it opens.
func (s *splashScreen) menuHeight(area Box) int {
	for _, sc := range s.m.stack {
		if l, ok := sc.(*listScreen); ok && l.id == startMenuID {
			return l.box(area).H
		}
	}
	return 10
}

// menuBox is where the start menu goes: under the credits, centered.
func (s *splashScreen) menuBox(area Box, w, h int) Box {
	p := splashLayout(area.H, h)
	return Box{area.X + (area.W-w)/2, area.Y + p.menu, w, min(h, area.H)}
}

func (s *splashScreen) key(driver.KeyEvent) {}
func (s *splashScreen) click(int, int)      {}
func (s *splashScreen) box(area Box) Box    { return area }

// animated makes animation ticks redraw the fire.
func (s *splashScreen) animated() bool { return true }

// step burns the fire on, once a tick.
func (s *splashScreen) step() {
	if s.fire != nil {
		s.fire.step()
	}
}

func (s *splashScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	black := cell.NewStyle().WithBg(limoni.RGB(0, 0, 0))
	for y := b.Y; y < b.Y+b.H; y++ {
		for x := b.X; x < b.X+b.W; x++ {
			f.Buffer.SetCell(uint16(x), uint16(y), cell.Cell{Content: ' ', Style: black})
		}
	}
	p := splashLayout(b.H, s.menuHeight(b))
	tw := len([]rune(splashTitle[0]))
	left := b.X + (b.W-tw)/2
	if s.fire == nil || s.fire.h != 2*p.fireRows || s.fire.w != tw {
		s.fire = newFire(tw, 2*p.fireRows)
	}

	// The fire, two pixels a cell.
	for row := 0; row < p.fireRows; row++ {
		y := b.Y + p.fire + row
		for x := 0; x < tw; x++ {
			up, down := s.fire.at(x, 2*row), s.fire.at(x, 2*row+1)
			c := cell.Cell{Content: ' ', Style: black}
			if up > 0 || down > 0 {
				c = cell.Cell{Content: '▀', Style: cell.NewStyle().WithFg(fireColor(up)).WithBg(fireColor(down))}
			}
			f.Buffer.SetCell(uint16(left+x), uint16(y), c)
		}
	}

	// The title: faces from red at the foot to orange at the top, which
	// catches the fire's glow, and shadows in a dark ember red.
	shadow := limoni.RGB(0x5a, 0x14, 0x0a)
	for row, line := range splashTitle {
		y := b.Y + p.title + row
		for x, r := range []rune(line) {
			if r == ' ' {
				continue
			}
			st := cell.NewStyle().WithFg(shadow).WithBg(limoni.RGB(0, 0, 0))
			if r == '█' {
				st = st.WithFg(titleFace(row, s.fire.at(x, s.fire.h-1)))
			}
			f.Buffer.SetCell(uint16(left+x), uint16(y), cell.Cell{Content: r, Style: st})
		}
	}
	f.RegisterAccessibility(accessibility.Node{Role: accessibility.RoleImage, Label: "Fancyband",
		Bounds: rect(Box{left, b.Y + p.title, tw, len(splashTitle)})})

	slate := limoni.Fg(limoni.RGB(0x8c, 0x9a, 0xb0)).WithBg(limoni.RGB(0, 0, 0))
	dim := limoni.Fg(limoni.RGB(0x70, 0x70, 0x80)).WithBg(limoni.RGB(0, 0, 0))
	if v := s.m.version(); v != "" {
		f.Text(left+tw-limoni.StringWidth(v), b.Y+p.version, v, dim)
	}
	if p.quote >= 0 {
		qw := 0
		for _, l := range splashQuote {
			qw = max(qw, limoni.StringWidth(l))
		}
		for i, l := range splashQuote {
			f.Text(b.X+(b.W-qw)/2, b.Y+p.quote+i, l, slate)
		}
	}
	if p.credits >= 0 {
		for i, l := range splashCredits {
			st := slate
			if i == len(splashCredits)-1 {
				st = dim
			}
			f.Text(b.X+(b.W-limoni.StringWidth(l))/2, b.Y+p.credits+i, l, st)
		}
	}
}

// titleFace is the color of a letter's face on row (0 at the top), lit
// by heat from the fire just above it: the top rows take some of the
// fire's color.
func titleFace(row, heat int) cell.Color {
	base := [6][3]float64{
		{0xf0, 0x8a, 0x24}, {0xe0, 0x62, 0x1c}, {0xc8, 0x40, 0x16},
		{0xb0, 0x2c, 0x12}, {0x96, 0x20, 0x10}, {0x7c, 0x18, 0x0e},
	}[min(row, 5)]
	glow := [6]float64{0.55, 0.25}[min(row, 5)]
	fc := firePalette[min(max(heat, 0), maxHeat)]
	mix := func(i int) uint8 { return uint8(base[i]*(1-glow) + float64(fc[i])*glow) }
	return limoni.RGB(mix(0), mix(1), mix(2))
}

// version is the engine's version, from its HELLO, as the game's own
// splash screen shows it.
func (m *Model) version() string {
	if m.sess == nil {
		return ""
	}
	return strings.TrimPrefix(m.sess.Server.Name, "Fancyband ")
}

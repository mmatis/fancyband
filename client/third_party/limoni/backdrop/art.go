package backdrop

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

// Art is a scene made from text: ASCII art, box drawing, Braille — any
// characters one column wide — still, flipped through frame by frame, or
// drifting across the screen. It is read from a plain text file by
// ParseArt; docs/backdrop-art.md describes the format and how to animate one.
//
// In short: the file is the picture. Lines that are exactly a directive
// change how it is shown:
//
//	@fps 6                  frames a second, for a file with several frames
//	@frame                  starts the next frame
//	@color #ffd166 #ef476f  one colour, or a gradient through several
//	@gradient vertical      vertical, horizontal or diagonal
//	@align center           center, top, bottom, left, right, top-left, ...
//	@offset 2 -1            moves it from where align puts it, in cells
//	@tile                   repeats it to fill the screen; @tile 6 2 leaves gaps
//	@scroll 4 0             drifts it, in cells a second, wrapping around
//	@background #101020     a colour behind the whole screen
//
// A space in the art is transparent. Colours can also come from ANSI escape
// sequences in the text itself, as tools such as chafa, jp2a and lolcat
// write them; a colour from the text beats @color.
type Art struct {
	frames []artFrame
	w, h   int // the largest frame, which alignment uses so frames do not jump

	fps           float64
	stops         []rgb // @color
	gradient      string
	align         string
	offX, offY    int
	tile          bool
	gapX, gapY    int
	scrollX       float64
	scrollY       float64
	background    cell.Color
	hasBackground bool
	interval      time.Duration
}

// artFrame is one frame: rows of cells, a space being transparent.
type artFrame struct {
	rows [][]artCell
}

type artCell struct {
	r      rune
	fg, bg cell.Color // zero: from @color, and no background
	bold   bool
}

// LoadArt reads an art file; see Art.
func LoadArt(path string) (*Art, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	a, err := ParseArt(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return a, nil
}

// ParseArt reads art from r; see Art for the format.
func ParseArt(r io.Reader) (*Art, error) {
	a := &Art{fps: 4, align: "center", gradient: "vertical"}
	var cur [][]artCell
	flush := func() {
		// Blank lines at the end of a frame are only the space before the
		// next @frame.
		for len(cur) > 0 && len(cur[len(cur)-1]) == 0 {
			cur = cur[:len(cur)-1]
		}
		a.frames = append(a.frames, artFrame{rows: cur})
		cur = nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	var pen artCell // carried across lines, as a terminal would
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		if ok, err := a.directive(text, flush); ok {
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			continue
		}
		var row []artCell
		row, pen = parseArtLine(text, pen)
		if len(cur) == 0 && len(row) == 0 {
			continue // blank lines before a frame's first line
		}
		cur = append(cur, row)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()
	// A trailing @frame leaves an empty frame behind; so can a file that
	// starts with one.
	frames := a.frames[:0]
	for _, f := range a.frames {
		if len(f.rows) > 0 {
			frames = append(frames, f)
		}
	}
	a.frames = frames
	if len(a.frames) == 0 {
		return nil, fmt.Errorf("no art in the file")
	}
	for _, f := range a.frames {
		a.h = max(a.h, len(f.rows))
		for _, r := range f.rows {
			a.w = max(a.w, len(r))
		}
	}
	a.interval = a.pace()
	return a, nil
}

// directive handles a line that is one, and reports whether it was.
func (a *Art) directive(text string, flush func()) (bool, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "@") || strings.TrimSpace(text) != text {
		return false, nil
	}
	args := fields[1:]
	num := func(i int) (float64, error) {
		if i >= len(args) {
			return 0, fmt.Errorf("%s needs %d numbers", fields[0], i+1)
		}
		return strconv.ParseFloat(args[i], 64)
	}
	switch fields[0] {
	case "@frame":
		if len(args) != 0 {
			return false, nil
		}
		flush()
	case "@fps":
		v, err := num(0)
		if err != nil || v <= 0 || v > 60 {
			return true, fmt.Errorf("@fps wants a number from 0 to 60")
		}
		a.fps = v
	case "@color", "@colour":
		if len(args) == 0 {
			return true, fmt.Errorf("@color needs at least one #rrggbb")
		}
		a.stops = a.stops[:0]
		for _, s := range args {
			c, err := parseHex(s)
			if err != nil {
				return true, err
			}
			a.stops = append(a.stops, c)
		}
	case "@gradient":
		if len(args) != 1 || (args[0] != "vertical" && args[0] != "horizontal" && args[0] != "diagonal") {
			return true, fmt.Errorf("@gradient is vertical, horizontal or diagonal")
		}
		a.gradient = args[0]
	case "@align":
		if len(args) != 1 || !validAlign(args[0]) {
			return true, fmt.Errorf("@align is center, top, bottom, left, right, top-left, top-right, bottom-left or bottom-right")
		}
		a.align = args[0]
	case "@offset":
		x, err1 := num(0)
		y, err2 := num(1)
		if err1 != nil || err2 != nil {
			return true, fmt.Errorf("@offset wants two whole numbers: columns and rows")
		}
		a.offX, a.offY = int(x), int(y)
	case "@tile":
		a.tile = true
		if len(args) > 0 {
			x, err1 := num(0)
			y, err2 := num(1)
			if err1 != nil || err2 != nil || x < 0 || y < 0 {
				return true, fmt.Errorf("@tile takes nothing, or the gap between copies: columns and rows")
			}
			a.gapX, a.gapY = int(x), int(y)
		}
	case "@scroll":
		x, err1 := num(0)
		y, err2 := num(1)
		if err1 != nil || err2 != nil {
			return true, fmt.Errorf("@scroll wants two numbers: columns and rows a second")
		}
		a.scrollX, a.scrollY = x, y
	case "@background", "@bg":
		if len(args) != 1 {
			return true, fmt.Errorf("@background needs one #rrggbb")
		}
		c, err := parseHex(args[0])
		if err != nil {
			return true, err
		}
		a.background, a.hasBackground = c.color(), true
	default:
		// Not a directive we know: it is part of the picture, the way
		// "@@@@" is in a lot of ASCII art.
		return false, nil
	}
	return true, nil
}

func validAlign(s string) bool {
	switch s {
	case "center", "top", "bottom", "left", "right", "top-left", "top-right", "bottom-left", "bottom-right":
		return true
	}
	return false
}

func parseHex(s string) (rgb, error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil || len(h) != 6 {
		return rgb{}, fmt.Errorf("%q is not a colour; write it as #rrggbb", s)
	}
	return rgb{float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)}, nil
}

// pace is how often the art changes: its frame rate, or as often as the
// drift moves it a whole cell, whichever is sooner. Zero for still art.
func (a *Art) pace() time.Duration {
	var iv time.Duration
	if len(a.frames) > 1 {
		iv = time.Duration(float64(time.Second) / a.fps)
	}
	if speed := math.Max(math.Abs(a.scrollX), math.Abs(a.scrollY)); speed > 0 {
		s := time.Duration(float64(time.Second) / speed)
		if iv == 0 || s < iv {
			iv = s
		}
	}
	if iv > 0 && iv < time.Second/30 {
		iv = time.Second / 30
	}
	return iv
}

// Interval implements terminal.Backdrop.
func (a *Art) Interval() time.Duration { return a.interval }

// Render implements terminal.Backdrop.
func (a *Art) Render(dst *buffer.Buffer, t time.Duration) {
	w, h := int(dst.Area.Width), int(dst.Area.Height)
	out := dst.Content
	blank := cell.Cell{Content: ' '}
	if a.hasBackground {
		blank.Style.Bg = a.background
	}
	for i := range out {
		out[i] = blank
	}
	if w == 0 || h == 0 {
		return
	}
	secs := t.Seconds()
	frame := a.frames[0]
	if len(a.frames) > 1 {
		frame = a.frames[int(secs*a.fps)%len(a.frames)]
	}
	x0, y0 := a.origin(w, h)
	x0 += int(math.Floor(secs * a.scrollX))
	y0 += int(math.Floor(secs * a.scrollY))

	if a.tile {
		// Every copy that can touch the screen, one art-size apart.
		sx, sy := max(1, a.w+a.gapX), max(1, a.h+a.gapY)
		x0, y0 = mod(x0, sx)-sx, mod(y0, sy)-sy
		for ty := y0; ty < h; ty += sy {
			for tx := x0; tx < w; tx += sx {
				a.stamp(out, w, h, frame, tx, ty)
			}
		}
		return
	}
	if a.scrollX != 0 || a.scrollY != 0 {
		// Drifting art leaves one side and comes back on the other: it
		// wraps around the screen plus its own size.
		x0 = mod(x0+a.w, w+a.w) - a.w
		y0 = mod(y0+a.h, h+a.h) - a.h
	}
	a.stamp(out, w, h, frame, x0, y0)
}

func mod(a, b int) int {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// origin is where @align and @offset put the art's top-left corner.
func (a *Art) origin(w, h int) (x, y int) {
	x, y = (w-a.w)/2, (h-a.h)/2
	if strings.Contains(a.align, "left") {
		x = 0
	}
	if strings.Contains(a.align, "right") {
		x = w - a.w
	}
	if strings.HasPrefix(a.align, "top") {
		y = 0
	}
	if strings.HasPrefix(a.align, "bottom") {
		y = h - a.h
	}
	return x + a.offX, y + a.offY
}

// stamp draws a frame with its top-left corner at (x0, y0).
func (a *Art) stamp(out []cell.Cell, w, h int, f artFrame, x0, y0 int) {
	for ry, row := range f.rows {
		y := y0 + ry
		if y < 0 || y >= h {
			continue
		}
		for rx, c := range row {
			x := x0 + rx
			if x < 0 || x >= w || c.r == ' ' && c.bg == 0 {
				continue
			}
			fg := c.fg
			if fg == 0 {
				fg = a.colorAt(rx, ry)
			}
			bg := c.bg
			if bg == 0 && a.hasBackground {
				bg = a.background
			}
			st := cell.Style{Fg: fg, Bg: bg}
			if c.bold {
				st.Modifier = cell.ModifierBold
			}
			out[y*w+x] = cell.Cell{Content: c.r, Style: st}
		}
	}
}

// artDefault is the colour of art with no @color and no colours of its
// own: quieter than the terminal's text, so the art does not read as
// output, and a colour Fade can fade.
var artDefault = rgb{138, 148, 184}

// colorAt is the @color at a cell of the art: the one colour, or where the
// cell falls on the gradient.
func (a *Art) colorAt(x, y int) cell.Color {
	switch len(a.stops) {
	case 0:
		return artDefault.color()
	case 1:
		return a.stops[0].color()
	}
	var t float64
	switch a.gradient {
	case "horizontal":
		t = float64(x) / float64(max(1, a.w-1))
	case "diagonal":
		t = (float64(x)/2 + float64(y)) / float64(max(1, a.w/2+a.h-1))
	default:
		t = float64(y) / float64(max(1, a.h-1))
	}
	t = clamp01(t) * float64(len(a.stops)-1)
	i := min(int(t), len(a.stops)-2)
	return a.stops[i].mix(a.stops[i+1], t-float64(i)).color()
}

// parseArtLine turns a line of the file into cells, reading the ANSI colour
// sequences in it (SGR) and dropping any other escape sequence. Tabs go to
// the next multiple of eight; characters wider than one column become a
// space, since a scene's cells are one column each.
func parseArtLine(text string, pen artCell) ([]artCell, artCell) {
	var row []artCell
	for i := 0; i < len(text); {
		if text[i] == 0x1b {
			n, params, final := escape(text[i:])
			if final == 'm' {
				pen = applySGR(pen, params)
			}
			i += n
			continue
		}
		cluster, width, _ := cell.NextCluster(text[i:])
		i += len(cluster)
		switch {
		case cluster == "\t":
			for n := 8 - len(row)%8; n > 0; n-- {
				row = append(row, artCell{r: ' ', fg: pen.fg, bg: pen.bg})
			}
			continue
		case width != 1:
			cluster = " "
			if width == 0 {
				continue // a control character or a lone combining mark
			}
		}
		c := pen
		c.r = cell.ClusterContent(cluster, 1)
		row = append(row, c)
		for k := 1; k < width; k++ {
			row = append(row, artCell{r: ' ', fg: pen.fg, bg: pen.bg})
		}
	}
	return row, pen
}

// escape measures the escape sequence at the start of s and returns its
// length, and for a CSI sequence its parameters and final byte.
func escape(s string) (n int, params string, final byte) {
	if len(s) < 2 {
		return len(s), "", 0
	}
	switch s[1] {
	case '[':
		for j := 2; j < len(s); j++ {
			if c := s[j]; c >= 0x40 && c <= 0x7e {
				return j + 1, s[2:j], c
			}
		}
		return len(s), "", 0
	case ']':
		// OSC, up to BEL or ST.
		for j := 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1, "", 0
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2, "", 0
			}
		}
		return len(s), "", 0
	}
	return 2, "", 0
}

// applySGR applies the colour and bold parameters of an SGR sequence.
// Palette colours are turned into RGB with the xterm defaults, so Fade can
// fade them like any other colour.
func applySGR(pen artCell, params string) artCell {
	if params == "" {
		params = "0"
	}
	ps := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	for i := 0; i < len(ps); i++ {
		p, _ := strconv.Atoi(ps[i])
		switch {
		case p == 0:
			pen = artCell{}
		case p == 1:
			pen.bold = true
		case p == 22:
			pen.bold = false
		case p >= 30 && p <= 37:
			pen.fg = xterm(p - 30)
		case p >= 90 && p <= 97:
			pen.fg = xterm(p - 90 + 8)
		case p == 39:
			pen.fg = 0
		case p >= 40 && p <= 47:
			pen.bg = xterm(p - 40)
		case p >= 100 && p <= 107:
			pen.bg = xterm(p - 100 + 8)
		case p == 49:
			pen.bg = 0
		case p == 38 || p == 48:
			var c cell.Color
			if i+2 < len(ps) && ps[i+1] == "5" {
				n, _ := strconv.Atoi(ps[i+2])
				c = xterm(n)
				i += 2
			} else if i+4 < len(ps) && ps[i+1] == "2" {
				r, _ := strconv.Atoi(ps[i+2])
				g, _ := strconv.Atoi(ps[i+3])
				b, _ := strconv.Atoi(ps[i+4])
				c = cell.NewColorRGB(uint8(r), uint8(g), uint8(b))
				i += 4
			} else {
				continue
			}
			if p == 38 {
				pen.fg = c
			} else {
				pen.bg = c
			}
		}
	}
	return pen
}

// xterm16 is xterm's default palette for the first sixteen colours.
var xterm16 = [16][3]uint8{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
	{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

// xterm is palette colour n as RGB: the sixteen, the 6×6×6 cube, the greys.
func xterm(n int) cell.Color {
	switch {
	case n < 0 || n > 255:
		return 0
	case n < 16:
		c := xterm16[n]
		return cell.NewColorRGB(c[0], c[1], c[2])
	case n < 232:
		n -= 16
		level := func(v int) uint8 {
			if v == 0 {
				return 0
			}
			return uint8(55 + 40*v)
		}
		return cell.NewColorRGB(level(n/36), level(n/6%6), level(n%6))
	}
	g := uint8(8 + 10*(n-232))
	return cell.NewColorRGB(g, g, g)
}

var _ terminal.Backdrop = (*Art)(nil)

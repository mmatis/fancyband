package ui

import (
	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// Palette slots the drawing uses: black for the normal background and the
// shade the game uses for dark backgrounds (COLOUR_SHADE in z-color.h).
const (
	colorBlack = 0
	colorShade = 28
)

// palette holds the engine's colors as Limoni colors.
type palette []cell.Color

func newPalette(rgb []proto.RGB) palette {
	p := make(palette, 32)
	for i := range p {
		p[i] = cell.NewColorRGB(255, 255, 255)
	}
	p[colorBlack] = cell.NewColorRGB(0, 0, 0)
	for i, c := range rgb {
		if i < len(p) {
			p[i] = cell.NewColorRGB(c.R, c.G, c.B)
		}
	}
	return p
}

func (p palette) color(i int) cell.Color {
	if i < 0 || i >= len(p) {
		return p[1]
	}
	return p[i]
}

// style turns an Angband attribute into a Limoni style.
func (p palette) style(a proto.Attr) cell.Style {
	fg := p.color(a.Color())
	bg := p[colorBlack]
	switch a.Background() {
	case proto.BgSame:
		bg = fg
	case proto.BgDark:
		bg = p.color(colorShade)
	}
	if a.Reversed() {
		fg, bg = bg, fg
	}
	return cell.NewStyle().WithFg(fg).WithBg(bg)
}

// termView draws one term. Each Angband cell takes one column; a glyph the
// terminal would draw two columns wide is replaced so the map stays
// aligned.
type termView struct {
	grid   *grid.Grid
	pal    palette
	cursor bool // draw the cursor, as reverse video
	flash  bool // invert the whole term, for the bell
}

func (v *termView) Draw(ctx cell.Context, buf *buffer.Buffer) {
	area := ctx.Area
	w := min(int(area.Width), v.grid.Width)
	h := min(int(area.Height), v.grid.Height)
	cur := v.grid.Cursor
	for y := 0; y < int(area.Height); y++ {
		for x := 0; x < int(area.Width); x++ {
			c := cell.Cell{Content: ' ', Style: v.pal.style(0)}
			if x < w && y < h {
				gc := v.grid.Cell(x, y)
				r := gc.Rune
				if r == 0 || limoni.RuneWidth(r) != 1 {
					r = '?'
					if gc.Rune == ' ' || gc.Rune == 0 {
						r = ' '
					}
				}
				c = cell.Cell{Content: r, Style: v.pal.style(gc.Attr)}
			}
			if v.flash || (v.cursor && cur.Visible && x == cur.X && y == cur.Y) {
				c.Style = c.Style.Reverse()
			}
			buf.SetCell(area.X+uint16(x), area.Y+uint16(y), c)
		}
	}
}

func (v *termView) SizeHint(max cell.Rect) (uint16, uint16) {
	return uint16(v.grid.Width), uint16(v.grid.Height)
}

func rect(b Box) cell.Rect {
	return cell.NewRect(uint16(b.X), uint16(b.Y), uint16(max(b.W, 0)), uint16(max(b.H, 0)))
}

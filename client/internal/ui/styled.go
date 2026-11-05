package ui

import (
	"image/color"
	"slices"
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
)

// styledView draws the game's styled text, wrapped at spaces to fit its
// area, from line Offset on.
type styledView struct {
	text   proto.StyledText
	pal    palette
	Offset int
}

// styledRun is part of a wrapped row: text in one color.
type styledRun struct {
	text  string
	color int
}

// wrapPlain breaks plain text into rows no wider than width, as
// wrapStyled does.
func wrapPlain(text string, width int) []string {
	var out []string
	for _, row := range wrapStyled(proto.PlainText(colorWhite, text), width) {
		var b strings.Builder
		for _, r := range row {
			b.WriteString(r.text)
		}
		out = append(out, b.String())
	}
	return out
}

// wrapStyled breaks styled text into rows no wider than width. A word
// longer than a row is cut. Spaces at the start of a game line are kept,
// for indented text; spaces where a line wraps are dropped.
func wrapStyled(text proto.StyledText, width int) [][]styledRun {
	if width <= 0 {
		return nil
	}
	var rows [][]styledRun
	for _, line := range text {
		var row []styledRun
		used := 0
		wrapped := false
		add := func(s string, color int) {
			if n := len(row); n > 0 && row[n-1].color == color {
				row[n-1].text += s
			} else {
				row = append(row, styledRun{s, color})
			}
			used += limoni.StringWidth(s)
		}
		wrap := func() {
			trimTrailingSpace(row)
			rows = append(rows, row)
			row, used, wrapped = nil, 0, true
		}
		for _, sp := range line {
			for _, word := range splitKeepSpaces(sp.Text) {
				w := limoni.StringWidth(word)
				if word[0] == ' ' {
					// Keep indentation and spaces between words, but
					// not spaces that would start a wrapped row.
					if (used > 0 || !wrapped) && used+w <= width {
						add(word, sp.Color)
					}
					continue
				}
				if used > 0 && used+w > width {
					wrap()
				}
				for w > width {
					cut := cutWidth(word, width)
					add(word[:cut], sp.Color)
					wrap()
					word = word[cut:]
					w = limoni.StringWidth(word)
				}
				add(word, sp.Color)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// splitKeepSpaces splits s into words and runs of spaces, in order.
func splitKeepSpaces(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || (s[i] == ' ') != (s[start] == ' ') {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
}

func trimTrailingSpace(row []styledRun) {
	if n := len(row); n > 0 {
		row[n-1].text = strings.TrimRight(row[n-1].text, " ")
	}
}

// cutWidth returns how many bytes of s fit in width columns, at least
// one rune.
func cutWidth(s string, width int) int {
	w := 0
	for i, r := range s {
		w += limoni.RuneWidth(r)
		if w > width && i > 0 {
			return i
		}
	}
	return len(s)
}

func (v *styledView) Draw(ctx cell.Context, buf *buffer.Buffer) {
	area := ctx.Area
	rows := wrapStyled(v.text, int(area.Width))
	for y := 0; y < int(area.Height); y++ {
		i := v.Offset + y
		if i >= len(rows) {
			break
		}
		x := int(area.X)
		for _, run := range rows[i] {
			style := cell.NewStyle().WithFg(v.pal.color(run.color))
			for _, r := range run.text {
				if x >= int(area.X+area.Width) {
					break
				}
				buf.SetCell(uint16(x), area.Y+uint16(y), cell.Cell{Content: r, Style: style})
				x += max(limoni.RuneWidth(r), 1)
			}
		}
	}
}

// headTileRoom is what roomForHeadTile adds to a line: a space and the tile's
// two cells. They are non-breaking, so they wrap with the line's last word.
const headTileRoom = "\u00a0\u00a0\u00a0"

// roomForHeadTile returns text with room for a tile at the end of line head.
func roomForHeadTile(text proto.StyledText, head int) proto.StyledText {
	if head >= len(text) || len(text[head]) == 0 {
		return text
	}
	out := slices.Clone(text)
	line := slices.Clone(out[head])
	line[len(line)-1].Text += headTileRoom
	out[head] = line
	return out
}

// drawHeadTile draws a tile in the room roomForHeadTile left in text, shown in
// box scrolled by offset, if that row is showing.
func drawHeadTile(f *terminal.Frame, box Box, text proto.StyledText, head, offset int, tile *proto.PictCell, env *drawEnv) {
	if head >= len(text) || len(text[head]) == 0 {
		return
	}
	rows := wrapStyled(text[head:head+1], box.W)
	used := 0
	for _, run := range rows[len(rows)-1] {
		used += limoni.StringWidth(run.text)
	}
	y := box.Y + styledHeight(text[:head], box.W) + len(rows) - 1 - offset
	if y < box.Y || y >= box.Y+box.H || used < 2 {
		return
	}
	at := Box{box.X + used - 2, y, 2, 1}
	for x := at.X; x < at.X+at.W; x++ {
		f.Buffer.SetCell(uint16(x), uint16(y), cell.Cell{Content: cell.RuneImage})
	}
	r, g, b := screenBg.Bg.RGB()
	img := env.sprite(tile.FgRow, tile.FgCol, color.RGBA{r, g, b, 255})
	env.register(f, terminal.ImageRegion{Area: rect(at), Img: env.picture(img, at)})
}

// styledHeight returns how many rows the text takes at width.
func styledHeight(text proto.StyledText, width int) int {
	return len(wrapStyled(text, width))
}

func (v *styledView) SizeHint(area cell.Rect) (uint16, uint16) {
	return area.Width, uint16(styledHeight(v.text, int(area.Width)))
}

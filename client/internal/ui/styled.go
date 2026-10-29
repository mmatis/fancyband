package ui

import (
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"

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

// styledHeight returns how many rows the text takes at width.
func styledHeight(text proto.StyledText, width int) int {
	return len(wrapStyled(text, width))
}

func (v *styledView) SizeHint(area cell.Rect) (uint16, uint16) {
	return area.Width, uint16(styledHeight(v.text, int(area.Width)))
}

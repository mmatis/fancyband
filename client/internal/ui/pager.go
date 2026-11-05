package ui

import (
	"fmt"
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// pagerScreen shows styled text that scrolls, with tabs, keys, and an
// optional search that keeps only the lines that match. The character
// sheet, the message history, and help are pagers.
type pagerScreen struct {
	id, title string
	// tabs, if set, are drawn along the top, with tab the current one.
	// Tab, Shift+Tab, and clicks pick another and run onTab, which sets
	// text for it.
	tabs  []string
	tab   int
	onTab func(t int)
	text  proto.StyledText
	// keys act on the screen, and are shown in the footer.
	keys []listKey
	// note is a line under the text, such as the filter in use.
	note string
	// searchable lets '/' keep only the lines that contain some text.
	searchable bool
	query      string
	searching  bool
	// clickLine runs for a click on a line, by its index in text.
	clickLine func(line int)
	// head is text above the rest that stays put while it scrolls, as a
	// table's headings.
	head proto.StyledText
	// center puts the head and text, as wide as their widest line, in
	// the middle of the screen, as for a table.
	center bool
	// overlay, if set, draws over the first row of each line of head and
	// text shown, after the text, as tiles over letters.
	overlay func(f *terminal.Frame, line []proto.Span, row Box, env *drawEnv)
	// width is the box's width, or 0 for 80 columns.
	width int
	close func()

	// offset is the first wrapped row shown; atEnd keeps the view at the
	// end until the player scrolls, for the message history.
	offset int
	atEnd  bool
	// height is how many rows of text showed when last drawn.
	height   int
	hotspots []hotspot
	// wrapped and wrappedFrom are what layout last worked out, for wrapKey.
	wrapKey     pagerWrap
	wrapped     [][]styledRun
	wrappedFrom []int
}

// pagerWrap names what a pager's wrapped rows were made from. Setting text
// makes a new slice, so its first line tells one text from another.
type pagerWrap struct {
	width int
	query string
	lines int
	first *[]proto.Span
}

// shownLines returns the lines that match the search, by their index in
// text.
func (p *pagerScreen) shownLines() []int {
	q := strings.ToLower(p.query)
	var out []int
	for i, line := range p.text {
		if q == "" || strings.Contains(strings.ToLower(spansText(line)), q) {
			out = append(out, i)
		}
	}
	return out
}

// spansText is a line's text without its colors.
func spansText(line []proto.Span) string {
	var b strings.Builder
	for _, sp := range line {
		b.WriteString(sp.Text)
	}
	return b.String()
}

// layout wraps the shown lines to width, and returns the wrapped rows and,
// for each, the line of text it comes from. It keeps them until the text, the
// search, or the width changes: the message history can hold thousands of
// lines, and every draw needs them.
func (p *pagerScreen) layout(width int) ([][]styledRun, []int) {
	key := pagerWrap{width: width, query: p.query, lines: len(p.text)}
	if len(p.text) > 0 {
		key.first = &p.text[0]
	}
	if p.wrapped != nil && p.wrapKey == key {
		return p.wrapped, p.wrappedFrom
	}
	rows, from := [][]styledRun{}, []int(nil)
	for _, i := range p.shownLines() {
		for _, row := range wrapStyled(proto.StyledText{p.text[i]}, width) {
			rows = append(rows, row)
			from = append(from, i)
		}
	}
	p.wrapKey, p.wrapped, p.wrappedFrom = key, rows, from
	return rows, from
}

func (p *pagerScreen) scroll(d int) {
	p.offset = max(p.offset+d, 0)
	p.atEnd = false
}

func (p *pagerScreen) setTab(t int) {
	n := len(p.tabs)
	p.tab = (t%n + n) % n
	p.offset = 0
	if p.onTab != nil {
		p.onTab(p.tab)
	}
}

func (p *pagerScreen) key(ev driver.KeyEvent) {
	if p.searching {
		switch ev.Type {
		case driver.KeyEsc:
			p.searching, p.query = false, ""
		case driver.KeyEnter:
			p.searching = false
		case driver.KeyBackspace:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
		case driver.KeySpace:
			p.query += " "
		case driver.KeyRune:
			if !ev.Ctrl && !ev.Alt && ev.Ch >= ' ' {
				p.query += string(ev.Ch)
			}
		default:
			p.searching = false
			p.key(ev)
			return
		}
		p.offset = 0
		return
	}
	switch ev.Type {
	case driver.KeyEsc:
		if p.query != "" {
			p.query = ""
			return
		}
		p.close()
	case driver.KeyTab:
		if len(p.tabs) > 0 {
			d := 1
			if ev.Shift {
				d = -1
			}
			p.setTab(p.tab + d)
		}
	case driver.KeyArrowDown:
		p.scroll(1)
	case driver.KeyArrowUp:
		p.scroll(-1)
	case driver.KeyPageDown, driver.KeySpace:
		p.scroll(pageRows(p.height))
	case driver.KeyPageUp:
		p.scroll(-pageRows(p.height))
	case driver.KeyHome:
		p.offset, p.atEnd = 0, false
	case driver.KeyEnd:
		p.atEnd = true
	case driver.KeyRune:
		if ev.Ctrl || ev.Alt {
			return
		}
		if ev.Ch == '/' && p.searchable {
			p.searching = true
			return
		}
		for _, k := range p.keys {
			if ev.Ch == k.Key {
				k.Do(-1)
				return
			}
		}
		switch ev.Ch {
		case 'q':
			p.close()
		case 'j', '2':
			p.scroll(1)
		case 'k', '8':
			p.scroll(-1)
		case '-':
			p.scroll(-pageRows(p.height))
		}
	}
}

func (p *pagerScreen) box(area Box) Box {
	w := p.width
	if w == 0 {
		w = 80
	}
	w = min(area.W, w)
	h := min(area.H, max(area.H-2, 12))
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (p *pagerScreen) footer() []string {
	var parts []string
	if len(p.tabs) > 0 {
		parts = append(parts, "Tab next")
	}
	parts = append(parts, "↑↓ PgUp PgDn scroll")
	for _, k := range p.keys {
		parts = append(parts, string(k.Key)+" "+k.Label)
	}
	if p.searchable {
		parts = append(parts, "/ search")
	}
	return append(parts, "Esc close")
}

func (p *pagerScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	p.hotspots = append(p.hotspots, hotspot{box: b, do: do})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

func (p *pagerScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	p.hotspots = p.hotspots[:0]
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" "+p.title+" ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: p.id, Role: accessibility.RoleDialog, Label: p.title, Bounds: rect(b)})
	inner := Box{b.X + 2, b.Y + 1, b.W - 4, b.H - 2}

	if len(p.tabs) > 0 {
		x, y := inner.X, inner.Y
		for i, t := range p.tabs {
			label := " " + t + " "
			st := screenDim
			if i == p.tab {
				st = screenSelected
			}
			w := limoni.StringWidth(label)
			if x > inner.X && x+w > inner.X+inner.W {
				x, y = inner.X, y+1
			}
			f.Text(x, y, label, st)
			i := i
			p.hotspot(f, Box{x, y, w, 1}, func() { p.setTab(i) })
			x += w + 1
		}
		inner.H -= y - inner.Y + 2
		inner.Y = y + 2
	}

	// The footer, wrapped between its keys, and the search or note above
	// it.
	var lines []string
	line := ""
	for _, part := range p.footer() {
		switch {
		case line == "":
			line = part
		case limoni.StringWidth(line)+2+limoni.StringWidth(part) <= inner.W:
			line += "  " + part
		default:
			lines = append(lines, line)
			line = part
		}
	}
	lines = append(lines, line)
	for i, l := range lines {
		f.TextFit(inner.X, inner.Y+inner.H-len(lines)+i, inner.W, l, screenDim)
	}
	inner.H -= len(lines)
	if p.searching || p.query != "" {
		f.TextFit(inner.X, inner.Y+inner.H-1, inner.W, "Search: "+p.query+"_", screenText)
		inner.H -= 2
	} else if p.note != "" {
		f.TextFit(inner.X, inner.Y+inner.H-1, inner.W, p.note, screenText)
		inner.H -= 2
	}

	if p.center {
		widest := 0
		for _, line := range append(append(proto.StyledText{}, p.head...), p.text...) {
			widest = max(widest, limoni.StringWidth(spansText(line)))
		}
		if off := (inner.W - widest) / 2; off > 0 {
			inner.X += off
			inner.W -= off
		}
	}
	if len(p.head) > 0 {
		for _, line := range p.head {
			if inner.H <= 1 {
				break
			}
			f.RenderWidget(&styledView{text: proto.StyledText{line}, pal: env.pal}, rect(Box{inner.X, inner.Y, inner.W, 1}))
			if p.overlay != nil {
				p.overlay(f, line, Box{inner.X, inner.Y, inner.W, 1}, env)
			}
			inner.Y++
			inner.H--
		}
	}
	wrapped, from := p.layout(inner.W)
	rows := len(from)
	p.height = inner.H
	if p.atEnd {
		p.offset = rows
	}
	p.offset = min(p.offset, max(rows-inner.H, 0))
	f.RenderWidget(&styledView{rows: wrapped, pal: env.pal, Offset: p.offset}, rect(inner))
	f.RenderWidget(wheelArea{scroll: func(d int) { p.scroll(d * wheelStep) }, pointer: env.pointer}, rect(inner))
	if p.overlay != nil {
		for y := 0; y < inner.H && p.offset+y < rows; y++ {
			n := p.offset + y
			if n == 0 || from[n] != from[n-1] {
				p.overlay(f, p.text[from[n]], Box{inner.X, inner.Y + y, inner.W, 1}, env)
			}
		}
	}
	if p.clickLine != nil {
		for y := 0; y < inner.H && p.offset+y < rows; y++ {
			line := from[p.offset+y]
			p.hotspot(f, Box{inner.X, inner.Y + y, inner.W, 1}, func() { p.clickLine(line) })
		}
	}
	if rows > inner.H && rows > 0 {
		// Where the view is, at the right of the top border.
		at := fmt.Sprintf(" %d%% ", min(100, (p.offset+inner.H)*100/rows))
		f.Text(b.X+b.W-2-len(at), b.Y, at, screenDim)
	}
}

func (p *pagerScreen) controls() []hotspot { return p.hotspots }

func (p *pagerScreen) click(x, y int) {
	for _, h := range p.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

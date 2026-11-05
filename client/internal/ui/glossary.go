package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// Descriptions on click and hover. The engine's GLOSSARY gives every
// term the game has rules for: races, classes, stats, skills, abilities,
// elements, curses, and status effects, each with its text and the names
// a screen may show it by. After the screens draw, View finds those names
// in what the top screen, the stats bar, and the status line show, and
// underlines them: a mark. A click on a mark, or the mouse resting on one
// for hoverDelay, opens a box with the term's text. Screens that draw a
// label for a term themselves, such as the comparison's column headings,
// mark it by the term's id through drawEnv.mark.

const (
	descID = "description"
	// markID starts the accessibility id of each name marked on screen.
	markID = "term-"
	// hoverDelay is how long the mouse rests on a name before its box
	// opens.
	hoverDelay = 500 * time.Millisecond
	// descWidth is the widest a description box gets.
	descWidth = 62
)

// glossName is one name a term goes by, or a series' lead.
type glossName struct {
	runes []rune
	id    string
	// label marks a common word, matched only in labels.
	label bool
	// items are a series' items, longest first, for a lead.
	items []*glossName
}

// glossary holds GLOSSARY's terms for finding their names on screen.
type glossary struct {
	terms map[string]*proto.GlossaryTerm
	// first holds every name and lead by its first word, longest first.
	first map[string][]*glossName
	// byName is each name's term, for labels a screen draws itself.
	byName map[string]string
}

// isWordRune reports whether r is part of a word: a name matches only
// where it is not part of a longer word, so "Elf" is not found in
// "Half-Elf".
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' }

// firstWord is the first word of s, or its first rune when it starts
// with none.
func firstWord(s []rune) string {
	i := 0
	for i < len(s) && isWordRune(s[i]) {
		i++
	}
	return string(s[:max(i, 1)])
}

func newGlossary(terms []proto.GlossaryTerm, series []proto.GlossarySeries) *glossary {
	g := &glossary{terms: map[string]*proto.GlossaryTerm{}, first: map[string][]*glossName{}, byName: map[string]string{}}
	seen := map[string]bool{}
	add := func(n *glossName) {
		if len(n.runes) == 0 {
			return
		}
		k := firstWord(n.runes)
		g.first[k] = append(g.first[k], n)
	}
	for i := range terms {
		t := &terms[i]
		g.terms[t.ID] = t
		for _, n := range t.Names {
			// The first term to claim a name keeps it.
			if seen[n.Name] {
				continue
			}
			seen[n.Name] = true
			g.byName[n.Name] = t.ID
			add(&glossName{runes: []rune(n.Name), id: t.ID, label: n.Label})
		}
	}
	for _, s := range series {
		lead := &glossName{runes: []rune(s.Lead)}
		for _, it := range s.Items {
			if g.terms[it.ID] != nil {
				lead.items = append(lead.items, &glossName{runes: []rune(it.Name), id: it.ID})
			}
		}
		if len(lead.items) == 0 {
			continue
		}
		slices.SortStableFunc(lead.items, func(a, b *glossName) int { return len(b.runes) - len(a.runes) })
		add(lead)
	}
	for _, ns := range g.first {
		slices.SortStableFunc(ns, func(a, b *glossName) int { return len(b.runes) - len(a.runes) })
	}
	return g
}

// lookup returns the term a screen's label names, if any.
func (g *glossary) lookup(name string) string {
	if g == nil {
		return ""
	}
	return g.byName[strings.TrimSpace(name)]
}

// glossMark is a name found on screen: the cells it takes on one row,
// and the term it names. A name wrapped onto two rows is two marks.
type glossMark struct {
	box Box
	id  string
	// passive is set for a label inside a control, such as a column
	// heading a click selects: the control takes the click, and hover
	// still opens the box.
	passive bool
}

// matchAt reports where name ends if it starts at text[i], as a whole
// word, or -1.
func matchAt(text []rune, i int, name []rune) int {
	if i+len(name) > len(text) {
		return -1
	}
	for k, r := range name {
		if text[i+k] != r {
			return -1
		}
	}
	end := i + len(name)
	if isWordRune(name[len(name)-1]) && end < len(text) && isWordRune(text[end]) {
		return -1
	}
	return end
}

// longest returns the first of names, longest first, that starts at
// text[i], and where it ends.
func longest(text []rune, i int, names []*glossName) (*glossName, int) {
	for _, n := range names {
		if end := matchAt(text, i, n.runes); end >= 0 {
			return n, end
		}
	}
	return nil, -1
}

// hasRunes reports whether text has p at i.
func hasRunes(text []rune, i int, p string) bool {
	for _, r := range p {
		if i >= len(text) || text[i] != r {
			return false
		}
		i++
	}
	return true
}

// span is a match in scanned text: runes start to end name term id.
type span struct {
	start, end int
	id         string
}

// find returns the names in text, leftmost first and, where several
// start at the same place, the longest. With labels false, a label-only
// name counts only as a label in the text, followed by ':' or '/', as
// "Save:" or "Hit/Shoot/Throw:" in the birth screen's panels.
func (g *glossary) find(text []rune, labels bool) []span {
	var out []span
	for i := 0; i < len(text); i++ {
		if !isWordRune(text[i]) || i > 0 && isWordRune(text[i-1]) {
			continue
		}
		cands := g.first[firstWord(text[i:])]
		for _, n := range cands {
			end := matchAt(text, i, n.runes)
			if end < 0 {
				continue
			}
			if n.label && !labels && !(end < len(text) && (text[end] == ':' || text[end] == '/')) {
				continue
			}
			if n.items == nil {
				out = append(out, span{i, end, n.id})
				i = end - 1
				break
			}
			// A lead: the list after it, as "fire, cold, and acid".
			items := g.series(text, end, n.items)
			if len(items) == 0 {
				continue
			}
			out = append(out, items...)
			i = items[len(items)-1].end - 1
			break
		}
	}
	return out
}

// series finds the list of items after a lead ending at k.
func (g *glossary) series(text []rune, k int, items []*glossName) []span {
	var out []span
	for k < len(text) && text[k] == ' ' {
		n, end := longest(text, k+1, items)
		if n == nil {
			break
		}
		out = append(out, span{k + 1, end, n.id})
		k = end
		switch {
		case hasRunes(text, k, ", and"):
			k += len(", and")
		case hasRunes(text, k, ","):
			k++
		case hasRunes(text, k, " and"):
			k += len(" and")
		case hasRunes(text, k, " or"):
			k += len(" or")
		default:
			return out
		}
	}
	return out
}

// scan finds the names drawn in area of buf. The rows are joined with a
// space, so a name wrapped onto the next row is found too. Cells skip
// reports are left out.
func (g *glossary) scan(buf *buffer.Buffer, area Box, labels bool, skip func(x, y int) bool) []glossMark {
	if g == nil || area.Empty() {
		return nil
	}
	type at struct{ x, y int }
	var text []rune
	var pos []at
	for y := area.Y; y < area.Y+area.H; y++ {
		start := len(text)
		for x := area.X; x < area.X+area.W; x++ {
			c := buf.Get(uint16(x), uint16(y))
			if c == nil || c.Content == 0 {
				// The second cell of a wide letter
				continue
			}
			r := c.Content
			if r == cell.RuneImage || !utf8.ValidRune(r) || skip != nil && skip(x, y) {
				r = ' '
			}
			text = append(text, r)
			pos = append(pos, at{x, y})
		}
		for len(text) > start && text[len(text)-1] == ' ' {
			text, pos = text[:len(text)-1], pos[:len(pos)-1]
		}
		text = append(text, ' ')
		pos = append(pos, at{-1, -1})
	}
	var marks []glossMark
	for _, s := range g.find(text, labels) {
		var cur *glossMark
		for i := s.start; i < s.end; i++ {
			p := pos[i]
			if p.x < 0 {
				cur = nil
				continue
			}
			if cur != nil && cur.box.Y == p.y && cur.box.X+cur.box.W == p.x {
				cur.box.W++
				continue
			}
			marks = append(marks, glossMark{box: Box{p.x, p.y, 1, 1}, id: s.id})
			cur = &marks[len(marks)-1]
		}
	}
	return marks
}

// inBox reports whether x, y is in b.
func inBox(b Box, x, y int) bool { return x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H }

// ------------------------------------------------------------------------
// The marks on screen

// glossLabels is a screen whose text is labels, such as the character
// sheet's, so common words such as "Save" are marked in it too.
type glossLabels interface{ glossLabels() bool }

// fetchGlossary asks the engine for its terms, once per engine.
func (m *Model) fetchGlossary() {
	m.query("GLOSSARY", nil, func(a proto.EventData) {
		if len(a.Terms) > 0 {
			m.gloss = newGlossary(a.Terms, a.Series)
		}
	})
}

// describeOn reports whether the mouse resting on a name opens its box.
func (m *Model) describeOn() bool { return m.settings.Describe == nil || *m.settings.Describe }

// markLabel marks a label a screen draws for term id, for drawEnv.mark.
func (m *Model) markLabel(b Box, id string) {
	if id == "" || b.Empty() || m.gloss == nil || m.gloss.terms[id] == nil {
		return
	}
	m.drawnMarks = append(m.drawnMarks, glossMark{box: b, id: id})
}

// findMarks finds the names on the top screen, and on the stats bar and
// the status line where no screen covers them. A name inside a row or a
// button is left out, since a click there is the control's.
func (m *Model) findMarks(f *terminal.Frame) []glossMark {
	marks := m.drawnMarks
	m.drawnMarks = nil
	if m.gloss == nil {
		return marks
	}
	var ctl []Box
	if n := len(m.stack); n > 0 {
		if c, ok := m.stack[n-1].(interface{ controls() []hotspot }); ok {
			for _, h := range c.controls() {
				if !h.region {
					ctl = append(ctl, h.box)
				}
			}
		}
	}
	inControl := func(mk glossMark) bool {
		return slices.ContainsFunc(ctl, func(b Box) bool { return b.Overlaps(mk.box) })
	}
	for i := range marks {
		marks[i].passive = inControl(marks[i])
	}
	drawn := func(x, y int) bool {
		for _, mk := range marks {
			if inBox(mk.box, x, y) {
				return true
			}
		}
		return false
	}
	area := Box{0, 0, m.layout.Width, m.layout.Height}
	var covered []Box
	for _, s := range m.stack {
		covered = append(covered, s.box(area))
	}
	if n := len(m.stack); n > 0 {
		top := m.stack[n-1]
		labels := false
		if l, ok := top.(glossLabels); ok {
			labels = l.glossLabels()
		}
		for _, mk := range m.gloss.scan(f.Buffer, covered[n-1], labels, drawn) {
			if !inControl(mk) {
				marks = append(marks, mk)
			}
		}
	}
	if m.layout.Native() && m.inGame {
		under := func(x, y int) bool {
			return drawn(x, y) || slices.ContainsFunc(covered, func(b Box) bool { return inBox(b, x, y) })
		}
		marks = append(marks, m.gloss.scan(f.Buffer, m.layout.Stats, true, under)...)
		// The status line names status effects; its "Light 1" is the
		// light's radius, not the element.
		for _, mk := range m.gloss.scan(f.Buffer, m.layout.StatusLine, true, under) {
			if m.gloss.terms[mk.id].Kind == "timed" {
				marks = append(marks, mk)
			}
		}
	}
	return marks
}

// markAt returns the mark at x, y in the last frame.
func (m *Model) markAt(x, y int) (glossMark, bool) {
	for _, mk := range m.marks {
		if inBox(mk.box, x, y) {
			return mk, true
		}
	}
	return glossMark{}, false
}

// drawGlossary finds and underlines the names on screen, draws the open
// box, and takes the clicks on both.
func (m *Model) drawGlossary(f *terminal.Frame, pal palette) {
	if m.desc != nil && m.desc.under != m.topScreen() {
		// The screen it was opened on has closed or another covers it.
		m.closeDesc()
	}
	m.marks = m.findMarks(f)
	open := ""
	if m.desc != nil {
		open = m.desc.id
	}
	for i, mk := range m.marks {
		// Each name is a button in the accessibility tree, named as it
		// shows, with its term's id as the value.
		var label strings.Builder
		for x := mk.box.X; x < mk.box.X+mk.box.W; x++ {
			if c := f.Buffer.Get(uint16(x), uint16(mk.box.Y)); c != nil && c.Content != 0 && c.Content != cell.RuneImage {
				label.WriteRune(c.Content)
			}
		}
		f.RegisterAccessibility(accessibility.Node{ID: fmt.Sprintf("%s%d", markID, i), Role: accessibility.RoleButton,
			Label: label.String(), Value: mk.id, Bounds: rect(mk.box)})
		for x := mk.box.X; x < mk.box.X+mk.box.W; x++ {
			c := f.Buffer.Get(uint16(x), uint16(mk.box.Y))
			if c == nil || c.Content == cell.RuneImage {
				continue
			}
			c.Style = c.Style.Underline()
			if mk.id == open && m.desc.anchor.Overlaps(mk.box) {
				c.Style = c.Style.WithBg(hoverBg)
			}
		}
	}
	if m.desc != nil {
		// A click anywhere but the box closes it, or opens another name's.
		f.RegisterClickHandler(rect(Box{0, 0, m.layout.Width, m.layout.Height}), func(ev driver.MouseEvent) {
			m.clickAt(int(ev.X), int(ev.Y))
		})
	} else {
		for _, mk := range m.marks {
			if !mk.passive {
				f.RegisterClickHandler(rect(mk.box), func(driver.MouseEvent) { m.openDesc(mk, false) })
			}
		}
	}
	m.drawDesc(f, pal)
}

// glossClick handles a left click that reached Update rather than the
// click regions drawGlossary registered, and reports whether it took it.
func (m *Model) glossClick(x, y int) bool {
	if m.desc != nil {
		m.clickAt(x, y)
		return true
	}
	if mk, ok := m.markAt(x, y); ok && !mk.passive {
		m.openDesc(mk, false)
		return true
	}
	return false
}

// clickAt handles a click while a box is open: on the box it does
// nothing, on its own name it closes it, on another name it opens that
// one's, and anywhere else it closes the box.
func (m *Model) clickAt(x, y int) {
	if m.desc == nil {
		return
	}
	if inBox(m.desc.box, x, y) {
		return
	}
	mk, ok := m.markAt(x, y)
	if !ok || mk.id == m.desc.id && m.desc.anchor.Overlaps(mk.box) {
		m.closeDesc()
		return
	}
	m.openDesc(mk, false)
}

// ------------------------------------------------------------------------
// The box

// descBox is an open description box.
type descBox struct {
	id string
	// anchor is the name the box describes, which it opens beside.
	anchor Box
	// hover is set for a box the mouse opened, which closes when the
	// mouse leaves the name and the box.
	hover bool
	// offset is how far the text is scrolled, and box where the box was
	// last placed.
	offset int
	box    Box
	shown  int
	// under is the top screen when the box opened, or nil.
	under screen
}

// topScreen is the top screen, or nil.
func (m *Model) topScreen() screen {
	if n := len(m.stack); n > 0 {
		return m.stack[n-1]
	}
	return nil
}

// openDesc opens the box for the name mk.
func (m *Model) openDesc(mk glossMark, hover bool) {
	if m.gloss == nil || m.gloss.terms[mk.id] == nil {
		return
	}
	m.desc = &descBox{id: mk.id, anchor: mk.box, hover: hover, under: m.topScreen()}
	m.placeDesc()
}

func (m *Model) closeDesc() { m.desc = nil }

// descRows is the box's text wrapped to fit width.
func (m *Model) descRows(width int) [][]styledRun {
	t := m.gloss.terms[m.desc.id]
	return wrapStyled(t.Text, width)
}

// placeDesc puts the box beside its name: below it when it fits, else
// above, inside the window.
func (m *Model) placeDesc() {
	d := m.desc
	if d == nil {
		return
	}
	W, H := m.layout.Width, m.layout.Height
	w := min(descWidth, W-2)
	rows := len(m.descRows(w - 4))
	h := min(rows+2, H-2)
	x := min(max(d.anchor.X-2, 0), max(W-w, 0))
	y := d.anchor.Y + 1
	switch {
	case y+h <= H:
	case d.anchor.Y-h >= 0:
		y = d.anchor.Y - h
	default:
		y = max(H-h, 0)
	}
	d.box = Box{x, y, w, h}
	d.shown = h - 2
	d.offset = min(d.offset, max(rows-d.shown, 0))
}

// scrollDesc scrolls the box's text by n rows.
func (m *Model) scrollDesc(n int) {
	d := m.desc
	rows := len(m.descRows(d.box.W - 4))
	d.offset = min(max(d.offset+n, 0), max(rows-d.shown, 0))
}

func (m *Model) drawDesc(f *terminal.Frame, pal palette) {
	d := m.desc
	if d == nil {
		return
	}
	m.placeDesc()
	b := d.box
	t := m.gloss.terms[d.id]
	fillBox(f, b, screenBg)
	block := widgets.NewBlock().Rounded().WithTitle(" " + t.Title + " ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus)
	f.RenderWidget(block, rect(b))
	rows := m.descRows(b.W - 4)
	text := Box{b.X + 2, b.Y + 1, b.W - 4, b.H - 2}
	f.RenderWidget(&styledView{text: t.Text, pal: pal, Offset: d.offset}, rect(text))
	if len(rows) > d.shown {
		if d.offset > 0 {
			f.Text(b.X+b.W-2, b.Y, "▲", screenFocus)
		}
		if d.offset+d.shown < len(rows) {
			f.Text(b.X+b.W-2, b.Y+b.H-1, "▼", screenFocus)
		}
	}
	var plain []string
	for _, line := range t.Text {
		plain = append(plain, spansText(line))
	}
	f.RegisterAccessibility(accessibility.Node{ID: descID, Role: accessibility.RoleDialog, Label: t.Title,
		Value: strings.Join(plain, "\n"), Bounds: rect(b)})
	// The box takes the wheel, and the pointer's moves for hover.
	f.RenderWidget(wheelArea{scroll: func(n int) { m.scrollDesc(n * wheelStep) }, pointer: m.pointerAt}, rect(b))
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) {})
}

// descKey handles a key while the box is open, and reports whether it
// took it: Escape closes the box, and the arrows and page keys scroll
// it. Any other key closes it and goes on to the screen.
func (m *Model) descKey(ev driver.KeyEvent) bool {
	d := m.desc
	switch ev.Type {
	case driver.KeyEsc:
		m.closeDesc()
		return true
	case driver.KeyArrowUp:
		m.scrollDesc(-1)
		return true
	case driver.KeyArrowDown:
		m.scrollDesc(1)
		return true
	case driver.KeyPageUp:
		m.scrollDesc(-max(d.shown-1, 1))
		return true
	case driver.KeyPageDown:
		m.scrollDesc(max(d.shown-1, 1))
		return true
	}
	m.closeDesc()
	return false
}

// ------------------------------------------------------------------------
// Hover

// hoverDue says the mouse may have rested on a name for hoverDelay.
type hoverDue struct{ seq int }

// glossPointer follows the mouse for hover: it closes a box the mouse
// opened once the mouse leaves the name and the box, and starts the
// wait to open the box of a name the mouse comes to rest on.
func (m *Model) glossPointer(x, y int) {
	if d := m.desc; d != nil && d.hover && !inBox(d.box, x, y) && !inBox(d.anchor, x, y) {
		m.closeDesc()
	}
	mk, ok := m.markAt(x, y)
	if !ok || !m.describeOn() {
		m.hoverMark = nil
		return
	}
	if m.hoverMark != nil && *m.hoverMark == mk {
		return
	}
	m.hoverMark = &mk
	m.hoverSeq++
	seq, wake := m.hoverSeq, m.hoverWake
	time.AfterFunc(hoverDelay, func() {
		// Only the latest wait matters; drop an older one still queued.
		select {
		case <-wake:
		default:
		}
		select {
		case wake <- seq:
		default:
		}
	})
}

// waitHover is a command that ends when a hover wait does.
func (m *Model) waitHover() engine.Cmd {
	wake := m.hoverWake
	return func(ctx context.Context) engine.Msg {
		select {
		case seq := <-wake:
			return hoverDue{seq}
		case <-ctx.Done():
			return nil
		}
	}
}

// hoverEnded opens the box of the name the mouse rests on, if it still
// does.
func (m *Model) hoverEnded(seq int) bool {
	if seq != m.hoverSeq || m.hoverMark == nil || !m.describeOn() {
		return false
	}
	mk := *m.hoverMark
	if !m.pointerSet || !inBox(mk.box, int(m.pointer.X), int(m.pointer.Y)) {
		return false
	}
	if d := m.desc; d != nil && d.id == mk.id && d.anchor == mk.box {
		return false
	}
	m.openDesc(mk, true)
	return true
}

// ------------------------------------------------------------------------
// The keyboard

const termsID = "terms"

// takesText is a screen that is taking typed text, such as a search,
// where ' is a letter.
type takesText interface{ takesText() bool }

// usesQuote is a screen with a use of its own for ', as the aim screen's
// closest target.
type usesQuote interface{ usesQuote() bool }

// listTerms opens a list of the terms the top screen names, for the '
// key; choosing one opens its box.
func (m *Model) listTerms() bool {
	if m.gloss == nil || len(m.stack) == 0 {
		return false
	}
	top := m.stack[len(m.stack)-1]
	if t, ok := top.(takesText); ok && t.takesText() {
		return false
	}
	if q, ok := top.(usesQuote); ok && q.usesQuote() {
		return false
	}
	if l, ok := top.(*listScreen); ok && l.id == termsID {
		return false
	}
	var marks []glossMark
	seen := map[string]bool{}
	for _, mk := range m.marks {
		if !seen[mk.id] {
			seen[mk.id] = true
			marks = append(marks, mk)
		}
	}
	if len(marks) == 0 {
		m.notify("Nothing here has a description.")
		return true
	}
	var items []listItem
	for _, mk := range marks {
		t := m.gloss.terms[mk.id]
		items = append(items, listItem{Label: t.Title, Hint: kindNames[t.Kind], Detail: t.Text})
	}
	s := newListScreen(termsID, "Described here", items)
	s.detail = true
	s.searchable = true
	s.cancel = m.pop
	s.choose = func(i int) {
		m.pop()
		m.openDesc(marks[i], false)
	}
	m.push(s)
	return true
}

// kindNames are the kinds of term, as the list of terms shows them.
var kindNames = map[string]string{
	"race": "race", "class": "class", "stat": "stat", "skill": "skill", "ability": "ability",
	"element": "element", "curse": "curse", "timed": "status",
}

func (s *listScreen) takesText() bool  { return s.searching }
func (p *pagerScreen) takesText() bool { return p.searching }
func (d *textDialog) takesText() bool  { return true }
func (s *birthScreen) takesText() bool { return s.step == stepName }

// The character sheet's text is labels.
func (p *pagerScreen) glossLabels() bool { return p.id == characterID }

// withManual puts the manual's paragraph about a race or a class, for
// the birth list id, above help, the game's own summary of it.
func (m *Model) withManual(list, name string, help proto.StyledText) proto.StyledText {
	kind := "race"
	if list == birthClassesID {
		kind = "class"
	}
	if m.gloss == nil {
		return help
	}
	t := m.gloss.terms[kind+":"+name]
	if t == nil || !t.Manual {
		return help
	}
	out := append(proto.StyledText{}, t.Text...)
	return append(append(out, nil), help...)
}

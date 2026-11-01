package ui

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// The equipment comparison: every item the character could wear, from
// the equipment, the pack, the floor, the home, and the stores, in rows,
// against every property the game compares, in columns, as the game's
// own comparison screen (ui-equip-cmp.c) shows them.

// compareName is the comparison's name in nativeScreens and the
// settings' list of classic screens. It has no capability: the knowledge
// screen opens it.
const compareName = "compare"

// compareID names the comparison in Limoni's accessibility tree, and
// compareRowID its selected row.
const (
	compareID    = "compare"
	compareRowID = "compare-row"
)

// compareSources are the comparison's tabs: which items it lists, as the
// game's own screen cycles through them with c.
var compareSources = []struct {
	label string
	has   func(src string) bool
}{
	{"Yours", func(s string) bool { return s != "s" }},
	{"In stores", func(s string) bool { return s == "s" }},
	{"Everything", func(string) bool { return true }},
	{"Carried", func(s string) bool { return s == "e" || s == "p" }},
}

// compareWhere names an item's source, as the game's letters for them.
var compareWhere = map[string]string{"e": "worn", "p": "in the pack", "f": "on the floor", "h": "at home", "s": "in a store"}

// compareHeadings are the categories' headings over their columns.
var compareHeadings = map[string]string{
	"resistances": "Resists", "abilities": "Abilities", "hindrances": "Hinders",
	"modifiers": "Modifiers", "stat_modifiers": "Stats",
}

// nativeCompare reports whether the knowledge screen opens Fancyband's
// own equipment comparison.
func (m *Model) nativeCompare() bool { return !slices.Contains(m.settings.Classic, compareName) }

// unknownValue is the value of a property the character does not know
// yet, which the game shows as "?".
const unknownValue = math.MaxInt32

// compareMinName is the narrowest the item names may get; past that,
// the columns scroll sideways.
const compareMinName = 24

// compareMaxName is the widest the names get while columns are left
// out, and compareMinCols the fewest columns shown beside them.
const (
	compareMaxName = 34
	compareMinCols = 6
)

// compareColW is each property column's width: room for a value such as
// "+10" with space either side, and for half of a label.
const compareColW = 5

// compareLabels are short names for the properties, at most eight
// letters, for the column headings. A property not here shows its own
// name, cut to fit.
var compareLabels = map[string]string{
	"Disenchant": "Disench",

	"Fear resistance": "No fear", "Blindess resistance": "No blind", "Blindness resistance": "No blind",
	"Confusion resistance": "No conf", "Stun resistance": "No stun", "Hold life": "HoldLife",
	"Regeneration": "Regen", "See invisible": "See inv", "Free action": "Free act",
	"Feather falling": "Feather", "Slow digestion": "Slow dig", "Trap immunity": "Trap imm",

	"Impaired HP recovery": "Slow HP", "Impaired mana recovery": "Slow SP", "Aggravate": "Aggrav",
	"No teleportation": "No tele", "Drain experience": "Drain XP", "Sticky curse": "Sticky",

	"Infravision": "Infra", "Tunneling": "Tunnel", "Attack speed": "Blows", "Shooting speed": "Shots",
	"Shooting power": "Might", "Damage reduction": "Dmg red", "Movement speed": "Moves",
}

// compareLabel is property p's heading.
func compareLabel(p proto.CompareProp) string {
	if l, ok := compareLabels[p.Name]; ok {
		return l
	}
	r := []rune(p.Name)
	return string(r[:min(len(r), 2*compareColW-2)])
}

// numeric reports whether a category's values are numbers, shown as
// such, rather than the game's symbols.
func numeric(category string) bool { return category == "modifiers" || category == "stat_modifiers" }

// compareScreen is the native equipment comparison.
type compareScreen struct {
	m    *Model
	data *proto.CompareData
	// source is the tab, and shown the indexes into data.Items of the
	// rows it lists, filtered and sorted.
	source int
	shown  []int
	// sel is the selected row, an index into shown, and offset the first
	// row in view; col is the selected column, an index into data.Props.
	sel, offset, col int
	// filter is the property a filter keeps items good in, or -1, and
	// filterNot keeps those not good in it instead.
	filter    int
	filterNot bool
	// sortBy is the column the rows are sorted by, best first, or -1 for
	// the game's order, and reverse turns the order round.
	sortBy  int
	reverse bool
	// mark is the item marked to compare with another, an index into
	// data.Items, or -1.
	mark int
	// left is the first column in view, and across how many showed when
	// last drawn: the columns scroll sideways when they do not all fit.
	// room is how many cells the columns had then.
	left, across, room int
	// rowsShown is how many rows showed when last drawn.
	rowsShown int
	hotspots  []hotspot
	// The last row clicked, and when, for a double click.
	clickedRow int
	clickedAt  time.Time
}

// openCompare opens the equipment comparison.
func (m *Model) openCompare() {
	m.query("COMPARE", nil, func(a proto.EventData) {
		if a.Compare == nil {
			m.notify("There is no equipment to compare yet.")
			return
		}
		m.push(newCompare(m, a.Compare))
	})
}

func newCompare(m *Model, data *proto.CompareData) *compareScreen {
	s := &compareScreen{m: m, data: data, filter: -1, sortBy: -1, mark: -1, clickedRow: -1}
	s.refilter()
	return s
}

// good reports whether value v is good for property p, as the game's
// quick filter takes it: a resistance at least resists, an ability is
// there, a hindrance is not, and a modifier is above 0. A value the
// character does not know counts as there, as in the game.
func (s *compareScreen) good(p, v int) bool {
	switch s.data.Props[p].Category {
	case "resistances":
		return v >= 1
	case "abilities":
		return v != 0
	case "hindrances":
		return v == 0
	}
	return v > 0
}

// filterText says what the filter keeps, to follow "only items that".
func (s *compareScreen) filterText() string {
	if s.filter < 0 {
		return ""
	}
	p := s.data.Props[s.filter]
	good := !s.filterNot
	switch p.Category {
	case "resistances":
		return map[bool]string{true: "resist ", false: "do not resist "}[good] + p.Name
	case "abilities":
		return map[bool]string{true: "have ", false: "lack "}[good] + p.Name
	case "hindrances":
		return map[bool]string{true: "are free of ", false: "have "}[good] + p.Name
	}
	return "have " + p.Name + map[bool]string{true: " above 0", false: " of 0 or less"}[good]
}

// refilter works out the rows: the tab's items that pass the filter, in
// order. The selection stays on the same item when it is still listed.
func (s *compareScreen) refilter() {
	keep := -1
	if s.sel >= 0 && s.sel < len(s.shown) {
		keep = s.shown[s.sel]
	}
	s.shown = s.shown[:0]
	for i, it := range s.data.Items {
		if !compareSources[s.source].has(it.Source) {
			continue
		}
		if s.filter >= 0 && s.filter < len(it.Values) && s.good(s.filter, it.Values[s.filter]) == s.filterNot {
			continue
		}
		s.shown = append(s.shown, i)
	}
	if s.sortBy >= 0 {
		p := s.sortBy
		rank := func(i int) int {
			v := s.data.Items[i].Values[p]
			switch {
			case v == unknownValue:
				// Unknown sorts last.
				return math.MinInt32
			case s.data.Props[p].Category == "hindrances":
				return -v
			}
			return v
		}
		slices.SortStableFunc(s.shown, func(a, b int) int { return rank(b) - rank(a) })
	}
	if s.reverse {
		slices.Reverse(s.shown)
	}
	s.sel = 0
	for n, i := range s.shown {
		if i == keep {
			s.sel = n
		}
	}
}

// visible returns how many columns fit in room cells from column left,
// with a rule before each new category.
func (s *compareScreen) visible(left, room int) int {
	props := s.data.Props
	n, x := 0, 0
	for p := left; p < len(props); p++ {
		if p > left && props[p].Category != props[p-1].Category {
			x++
		}
		if x+compareColW > room {
			break
		}
		x += compareColW
		n++
	}
	return max(n, 1)
}

// maxLeft is the last first column: the one from which the rest all fit.
func (s *compareScreen) maxLeft() int {
	if s.room <= 0 {
		return len(s.data.Props) - 1
	}
	l := len(s.data.Props) - 1
	for l > 0 && s.visible(l-1, s.room) >= len(s.data.Props)-(l-1) {
		l--
	}
	return l
}

// fit scrolls the columns so the selected one shows.
func (s *compareScreen) fit() {
	s.left = min(max(s.left, 0), s.maxLeft())
	if s.col < s.left {
		s.left = s.col
	}
	if s.room > 0 {
		for s.left < s.col && s.col >= s.left+s.visible(s.left, s.room) {
			s.left++
		}
	}
}

// scrollCols scrolls the columns to start at left, moving the selected
// column into view.
func (s *compareScreen) scrollCols(left int) {
	s.left = min(max(left, 0), s.maxLeft())
	n := len(s.data.Props) - s.left
	if s.room > 0 {
		n = s.visible(s.left, s.room)
	}
	s.col = min(max(s.col, s.left), s.left+n-1)
}

// nextCategory moves the column cursor to the first column of the next
// category, or the first one after the last.
func (s *compareScreen) nextCategory(d int) {
	props := s.data.Props
	cat := props[s.col].Category
	i := s.col
	for {
		i = (i + d + len(props)) % len(props)
		if props[i].Category != cat || i == s.col {
			break
		}
	}
	// Going back lands on the category's first column.
	for d < 0 && i > 0 && props[i-1].Category == props[i].Category {
		i--
	}
	// The group starts at the left, so as much of it shows as fits.
	s.col = i
	s.left = i
	s.fit()
}

func (s *compareScreen) box(area Box) Box { return area }

// moveCol moves the column cursor by d.
func (s *compareScreen) moveCol(d int) {
	s.col = min(max(s.col+d, 0), len(s.data.Props)-1)
	s.fit()
}

// moveRow moves the selection by d rows.
func (s *compareScreen) moveRow(d int) {
	if len(s.shown) == 0 {
		return
	}
	s.sel = min(max(s.sel+d, 0), len(s.shown)-1)
}

func (s *compareScreen) key(ev driver.KeyEvent) {
	switch ev.Type {
	case driver.KeyEsc:
		if s.mark >= 0 {
			s.mark = -1
			return
		}
		s.m.pop()
	case driver.KeyArrowDown:
		s.moveRow(1)
	case driver.KeyArrowUp:
		s.moveRow(-1)
	case driver.KeyArrowRight:
		if ev.Shift {
			s.nextCategory(1)
		} else {
			s.moveCol(1)
		}
	case driver.KeyArrowLeft:
		if ev.Shift {
			s.nextCategory(-1)
		} else {
			s.moveCol(-1)
		}
	case driver.KeyPageDown:
		s.moveRow(pageRows(s.rowsShown))
	case driver.KeyPageUp:
		s.moveRow(-pageRows(s.rowsShown))
	case driver.KeyHome:
		s.sel = 0
	case driver.KeyEnd:
		s.sel = max(len(s.shown)-1, 0)
	case driver.KeyTab:
		d := 1
		if ev.Shift {
			d = len(compareSources) - 1
		}
		s.setSource((s.source + d) % len(compareSources))
	case driver.KeyEnter:
		s.describe()
	case driver.KeyRune:
		if ev.Ctrl || ev.Alt {
			return
		}
		switch ev.Ch {
		case 'j', '2':
			s.moveRow(1)
		case 'k', '8':
			s.moveRow(-1)
		case 'l', '6':
			s.moveCol(1)
		case 'h', '4':
			s.moveCol(-1)
		case 'n', ' ':
			s.moveRow(pageRows(s.rowsShown))
		case 'p':
			s.moveRow(-pageRows(s.rowsShown))
		case 'c':
			s.setSource((s.source + 1) % len(compareSources))
		case 'v':
			s.nextCategory(1)
		case 'q', '!':
			s.setFilter(s.col, ev.Ch == '!')
		case 's':
			if s.sortBy == s.col {
				s.sortBy = -1
			} else {
				s.sortBy = s.col
			}
			s.refilter()
		case 'r':
			s.reverse = !s.reverse
			s.refilter()
		case 'R':
			s.source, s.filter, s.filterNot, s.sortBy, s.reverse, s.mark = 0, -1, false, -1, false, -1
			s.refilter()
		case 'I':
			s.describe()
		case 'x':
			s.compareMarked()
		}
	}
}

// setSource picks a tab.
func (s *compareScreen) setSource(t int) {
	s.source = t
	s.refilter()
}

// setFilter keeps the items good in property p, or with not those that
// are not; the same filter again turns it off.
func (s *compareScreen) setFilter(p int, not bool) {
	if s.filter == p && s.filterNot == not {
		s.filter = -1
	} else {
		s.filter, s.filterNot = p, not
	}
	s.refilter()
}

// selected returns the selected item's index in data.Items, or -1.
func (s *compareScreen) selected() int {
	if s.sel < 0 || s.sel >= len(s.shown) {
		return -1
	}
	return s.shown[s.sel]
}

// recall asks the game to describe item i, then runs done with its
// title and description.
func (s *compareScreen) recall(i int, done func(title string, text proto.StyledText)) {
	n := i
	s.m.query("RECALL", map[string]any{"item": proto.ItemRef{Compare: &n}}, func(r proto.EventData) {
		if r.Title == nil {
			s.m.notify("The game cannot describe that item now.")
			return
		}
		done(capitalize(*r.Title), r.Description)
	})
}

// showText opens text over the comparison.
func (s *compareScreen) showText(title string, text proto.StyledText) {
	t := &textScreen{id: recallID, title: title, text: text}
	t.close = func() {
		if i := slices.Index(s.m.stack, screen(t)); i >= 0 {
			s.m.stack = slices.Delete(s.m.stack, i, i+1)
		}
	}
	s.m.push(t)
}

// describe shows the selected item's description.
func (s *compareScreen) describe() {
	if i := s.selected(); i >= 0 {
		s.recall(i, s.showText)
	}
}

// compareMarked marks the selected item, or, with one marked, shows the
// two items' descriptions one after the other, as the game's x does.
func (s *compareScreen) compareMarked() {
	i := s.selected()
	if i < 0 {
		return
	}
	if s.mark < 0 || s.mark == i {
		s.mark = i
		return
	}
	first := s.mark
	s.mark = -1
	s.recall(first, func(t1 string, d1 proto.StyledText) {
		s.recall(i, func(t2 string, d2 proto.StyledText) {
			heading := func(t string) proto.StyledText { return proto.PlainText(colorYellow, t) }
			text := slices.Concat(heading(t1), d1, proto.StyledText{nil}, heading(t2), d2)
			s.showText("Object comparison", text)
		})
	})
}

// hotspot makes part of the screen clickable.
func (s *compareScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	s.hotspots = append(s.hotspots, hotspot{box: b, do: do})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

func (s *compareScreen) controls() []hotspot { return s.hotspots }

func (s *compareScreen) click(x, y int) {
	for _, h := range s.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

// clickRow selects row n; a second click on it soon after describes it.
func (s *compareScreen) clickRow(n int) {
	now := time.Now()
	if n == s.clickedRow && now.Sub(s.clickedAt) < doubleClick {
		s.sel = n
		s.clickedRow = -1
		s.describe()
		return
	}
	s.sel, s.clickedRow, s.clickedAt = n, n, now
}

// cellStyle is a value's cell, in its palette color, on bg.
func cellStyle(pal palette, color int, bg cell.Color) cell.Style {
	return pal.style(proto.Attr(color)).WithBg(bg)
}

// cellText is what a cell shows for property p with game symbol r and
// value v: the game's symbol, or for a modifier its number.
func (s *compareScreen) cellText(p int, r rune, v int, known bool) string {
	if !known || !numeric(s.data.Props[p].Category) {
		return string(r)
	}
	switch v {
	case unknownValue:
		return "?"
	case 0:
		return "."
	}
	return fmt.Sprintf("%+d", v)
}

func (s *compareScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	pal := env.pal
	s.hotspots = s.hotspots[:0]
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" Compare equipment ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: compareID, Role: accessibility.RoleDialog,
		Label: "Compare equipment", Value: compareSources[s.source].label, Bounds: rect(b)})
	in := Box{b.X + 2, b.Y + 1, b.W - 4, b.H - 2}
	props := s.data.Props
	bg := screenBg.Bg

	// The tabs.
	x := in.X
	for i, src := range compareSources {
		label := " " + src.label + " "
		st := screenDim
		if i == s.source {
			st = screenSelected
		}
		f.Text(x, in.Y, label, st)
		i := i
		s.hotspot(f, Box{x, in.Y, limoni.StringWidth(label), 1}, func() { s.setSource(i) })
		x += limoni.StringWidth(label) + 1
	}

	// The columns in view, from left: as many as fit beside the names,
	// each compareColW wide, with a rule between categories.
	// The names take what they need, from compareMinName to
	// compareMaxName, or less when that would leave too few columns.
	longest := limoni.StringWidth("You, with your equipment") + 1
	for _, it := range s.data.Items {
		longest = max(longest, limoni.StringWidth(it.Name)+1)
	}
	nameW := min(max(longest, compareMinName), compareMaxName, in.W-4-compareMinCols*compareColW)
	nameW = max(nameW, 10)
	left := in.X + 4 + nameW
	room := in.X + in.W - left
	if room != s.room {
		s.room = room
		s.fit()
	}
	colX := map[int]int{}
	var cols []int
	cx := 0
	for p := s.left; p < len(props); p++ {
		if p > s.left && props[p].Category != props[p-1].Category {
			cx++
		}
		if cx+compareColW > room {
			break
		}
		colX[p] = left + cx
		cols = append(cols, p)
		cx += compareColW
	}
	s.across = max(len(cols), 1)
	scrolls := len(cols) < len(props)

	// The headings: each category's name over its columns, then each
	// property's label, on two rows that take turns, so a label may run
	// over its neighbours' columns.
	headY := in.Y + 2
	rule := cell.NewStyle().WithFg(limoni.RGB(70, 70, 90)).WithBg(bg)
	for n, p := range cols {
		prop := props[p]
		first := n == 0 || prop.Category != props[cols[n-1]].Category
		if first {
			end := len(cols)
			for k := n; k < len(cols); k++ {
				if props[cols[k]].Category != prop.Category {
					end = k
					break
				}
			}
			heading := compareHeadings[prop.Category]
			w := colX[cols[end-1]] + compareColW - colX[p]
			hw := limoni.StringWidth(heading)
			f.TextFit(colX[p]+max((w-hw)/2, 0), headY, min(hw, w), heading, screenTitle)
			if n > 0 {
				// The rule leaves the label rows to labels that overhang
				// their columns.
				for y := headY; y < in.Y+in.H-2; y++ {
					if y == headY+1 || y == headY+2 {
						continue
					}
					f.Buffer.SetCell(uint16(colX[p]-1), uint16(y), cell.Cell{Content: '│', Style: rule})
				}
			}
		}
		st := screenText
		if p == s.col {
			st = screenSelected
		}
		label := compareLabel(prop)
		lw := limoni.StringWidth(label)
		ly := headY + 1 + p%2
		lx := colX[p] + (compareColW-lw)/2
		// Labels stay between the names and the border.
		lx = min(max(lx, in.X+4+nameW-1), in.X+in.W-lw)
		f.Text(lx, ly, label, st)
		// The column's own cells under the labels, for the cursor.
		p := p
		s.hotspot(f, Box{colX[p], headY + 1, compareColW, 2}, func() { s.col = p; s.fit() })
	}

	// One row of cells.
	row := func(y int, text string, colors, values []int, rowBg cell.Color) {
		runes := []rune(text)
		for _, p := range cols {
			r, c := ' ', colorWhite
			if p < len(runes) {
				r = runes[p]
			}
			if p < len(colors) {
				c = colors[p]
			}
			v, known := 0, p < len(values)
			if known {
				v = values[p]
			}
			cellBg := rowBg
			if p == s.col && rowBg == bg {
				cellBg = limoni.RGB(40, 40, 58)
			}
			fillBox(f, Box{colX[p], y, compareColW, 1}, cell.NewStyle().WithBg(cellBg))
			t := s.cellText(p, r, v, known)
			st := cellStyle(pal, c, cellBg)
			if t == "." {
				st = cell.NewStyle().WithFg(limoni.RGB(80, 80, 96)).WithBg(cellBg)
			}
			f.Text(colX[p]+(compareColW-limoni.StringWidth(t))/2, y, t, st)
		}
	}
	y := headY + 3
	f.Text(in.X, y, "@", cellStyle(pal, colorWhite, bg))
	f.TextFit(in.X+4, y, nameW-1, "You, with your equipment", screenTitle)
	row(y, s.data.Player.Text, s.data.Player.Colors, s.data.Player.Values, bg)
	y++

	// Under the items: a scroll bar for the columns when they do not all
	// fit, what the cursor's column is, and the keys.
	listTop := y + 1
	bottom := in.Y + in.H - 2
	if scrolls {
		bottom--
	}
	listH := max(bottom-listTop, 1)
	s.rowsShown = listH
	if s.sel < s.offset {
		s.offset = s.sel
	}
	if s.sel >= s.offset+listH {
		s.offset = s.sel - listH + 1
	}
	s.offset = min(s.offset, max(len(s.shown)-listH, 0))
	if len(s.shown) == 0 {
		f.TextFit(in.X+4, listTop, in.W-4, "No items. Press c for other sources, or q or ! again to turn the filter off.", screenDim)
	}
	for n := s.offset; n < len(s.shown) && n < s.offset+listH; n++ {
		it := s.data.Items[s.shown[n]]
		ry := listTop + n - s.offset
		rowBg := bg
		nameSt := screenText
		switch {
		case n == s.sel:
			rowBg = screenSelected.Bg
			nameSt = screenSelected
		case s.shown[n] == s.mark:
			nameSt = screenTitle
		}
		fillBox(f, Box{in.X, ry, 4 + nameW, 1}, cell.NewStyle().WithBg(rowBg))
		glyph := []rune(it.Char + " ")[0]
		f.Buffer.SetCell(uint16(in.X), uint16(ry), cell.Cell{Content: glyph, Style: cellStyle(pal, it.Color, rowBg)})
		f.Text(in.X+2, ry, it.Source, nameSt)
		name := it.Name
		if s.shown[n] == s.mark {
			name = "* " + name
		}
		f.TextFit(in.X+4, ry, nameW-1, name, nameSt)
		row(ry, it.Text, it.Colors, it.Values, rowBg)
		n := n
		s.hotspot(f, Box{in.X, ry, in.W, 1}, func() { s.clickRow(n) })
		if n == s.sel {
			f.RegisterAccessibility(accessibility.Node{ID: compareRowID, Role: accessibility.RoleListItem,
				Label: it.Name, Value: it.Text, State: accessibility.StateSelected, Bounds: rect(Box{in.X, ry, in.W, 1})})
		}
	}
	if len(s.shown) > listH {
		f.RenderWidget(scrollBar{vertical: true, total: len(s.shown), shown: listH, offset: s.offset, set: func(o int) {
			s.offset = min(max(o, 0), len(s.shown)-listH)
			s.sel = min(max(s.sel, s.offset), s.offset+listH-1)
		}}, rect(Box{b.X + b.W - 1, listTop, 1, listH}))
	}
	if scrolls {
		f.RenderWidget(scrollBar{total: len(props), shown: len(cols), offset: s.left, set: s.scrollCols},
			rect(Box{left, bottom, in.X + in.W - left, 1}))
	}
	// The wheel scrolls the rows; with Shift, or sideways, the columns.
	f.RenderWidget(mouseArea{handle: func(ev driver.MouseEvent) {
		switch ev.Button {
		case driver.MouseScrollUp, driver.MouseScrollDown:
			d := 1
			if ev.Button == driver.MouseScrollUp {
				d = -1
			}
			if ev.Shift {
				s.scrollCols(s.left + d*wheelStep)
				return
			}
			s.offset = min(max(s.offset+d*wheelStep, 0), max(len(s.shown)-listH, 0))
			s.sel = min(max(s.sel, s.offset), s.offset+listH-1)
		case driver.MouseScrollLeft:
			s.scrollCols(s.left - wheelStep)
		case driver.MouseScrollRight:
			s.scrollCols(s.left + wheelStep)
		case driver.MouseNone:
			if env.pointer != nil {
				env.pointer(int(ev.X), int(ev.Y))
			}
		}
	}}, rect(Box{in.X, headY, in.W, bottom - headY}))

	f.TextFit(in.X, in.Y+in.H-2, in.W, s.columnInfo(), screenText)
	keys := "←→ property  Shift+←→ or v group  ↑↓ item  Enter describe  x compare two  q ! filter  s sort  r reverse  c source  R reset  Esc back"
	if limoni.StringWidth(keys) > in.W {
		keys = "←→↑↓ move  v group  Enter describe  x compare  q ! filter  s sort  Esc back"
	}
	f.TextFit(in.X, in.Y+in.H-1, in.W, keys, screenDim)
}

// columnInfo says what the cursor's column is, what the selected item
// has in it, and what the filter and sort are.
func (s *compareScreen) columnInfo() string {
	if s.col < 0 || s.col >= len(s.data.Props) {
		return ""
	}
	p := s.data.Props[s.col]
	parts := []string{fmt.Sprintf("%s (%s)", p.Name, strings.ToLower(compareHeadings[p.Category]))}
	if i := s.selected(); i >= 0 {
		it := s.data.Items[i]
		parts = append(parts, compareWhere[it.Source])
	}
	if f := s.filterText(); f != "" {
		parts = append(parts, "only items that "+f)
	}
	if s.sortBy >= 0 {
		parts = append(parts, "best "+s.data.Props[s.sortBy].Name+" first")
	}
	if s.mark >= 0 {
		parts = append(parts, "x on another item compares it with the marked one")
	}
	return strings.Join(parts, " · ")
}

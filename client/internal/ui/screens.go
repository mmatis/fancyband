package ui

import (
	"image"
	"image/color"
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

// A screen is a native screen drawn over the streamed one. Screens stack:
// the top one gets every key, and the engine's screen shows underneath.
type screen interface {
	// key handles one key press.
	key(ev driver.KeyEvent)
	// box returns where the screen draws within area, so tiles under it
	// can be left out.
	box(area Box) Box
	// draw draws the screen in box.
	draw(f *terminal.Frame, box Box, env *drawEnv)
	// click handles a left click at x, y, for a click that reaches
	// Update rather than the click regions draw registered.
	click(x, y int)
}

// hotspot is a part of a screen that a click acts on. A region, such as
// a whole column that a click gives the keys, is not lit up under the
// mouse as a control is.
type hotspot struct {
	box    Box
	do     func()
	region bool
}

// drawEnv is what screens draw with: the game's palette, and its tiles
// when the map uses them.
type drawEnv struct {
	pal palette
	// sprite returns a tile's sprite on a background color, or is nil
	// in text mode.
	sprite func(row, col uint8, bg color.RGBA) image.Image
	// sharp, if set, enlarges a picture for the cells it goes on.
	sharp func(img image.Image, cols, rows int) image.Image
	// place, if set, takes the screen's pictures; see Model.place.
	place func(f *terminal.Frame, r terminal.ImageRegion)
	// pointer, if set, takes the mouse pointer's moves that Limoni gives
	// to a wheelArea, for hover.
	pointer func(x, y int)
	// mark, if set, marks a label the screen draws for a term, by the
	// term's id, so a click or hover describes it; gloss finds the id.
	mark  func(b Box, id string)
	gloss *glossary
}

// picture returns img as it should go on the cells of at.
func (e *drawEnv) picture(img image.Image, at Box) image.Image {
	if e.sharp == nil {
		return img
	}
	return e.sharp(img, at.W, at.H)
}

// tile draws a sprite on the cells of at, on the background bg.
func (e *drawEnv) tile(f *terminal.Frame, at Box, row, col uint8, bg cell.Color) {
	for y := at.Y; y < at.Y+at.H; y++ {
		for x := at.X; x < at.X+at.W; x++ {
			f.Buffer.SetCell(uint16(x), uint16(y), cell.Cell{Content: cell.RuneImage})
		}
	}
	r, g, b := bg.RGB()
	img := e.sprite(row, col, color.RGBA{r, g, b, 255})
	e.register(f, terminal.ImageRegion{Area: rect(at), Img: e.picture(img, at)})
}

// register places a picture the screen draws.
func (e *drawEnv) register(f *terminal.Frame, r terminal.ImageRegion) {
	if e.place != nil {
		e.place(f, r)
		return
	}
	f.RegisterImageRegion(r)
}

// markID marks a label the screen draws in b for term id.
func (e *drawEnv) markID(b Box, id string) {
	if e.mark != nil {
		e.mark(b, id)
	}
}

// doubleClick is how quickly a second click on a row must follow the
// first to count as a double click.
const doubleClick = 500 * time.Millisecond

// Colors for native screens.
var (
	screenBorder   = limoni.Fg(limoni.RGB(120, 120, 150))
	screenTitle    = limoni.Fg(limoni.RGB(230, 200, 120)).Bold()
	screenText     = limoni.Fg(limoni.RGB(210, 210, 220))
	screenDim      = limoni.Fg(limoni.RGB(120, 120, 135))
	screenWarn     = limoni.Fg(limoni.RGB(240, 150, 80))
	screenSelected = limoni.Fg(limoni.RGB(20, 20, 30)).WithBg(limoni.RGB(230, 200, 120))
	screenBg       = cell.NewStyle().WithBg(limoni.RGB(16, 16, 24))
	// screenFocus is the border of the part of a screen that has the keys.
	screenFocus = limoni.Fg(limoni.RGB(230, 200, 120))
)

// listItem is one row of a list screen.
type listItem struct {
	Label string
	// Icon, if set, is drawn in IconColor (a palette index) before the
	// label.
	Icon      rune
	IconColor int
	// Tile, if set, is a tile drawn before the label when the map uses
	// tiles.
	Tile *proto.PictCell
	// More are tiles drawn at the right of the row, before the hint,
	// when the map uses tiles; a nil one is drawn as a dot.
	More []*proto.PictCell
	// Hint is short text drawn at the right of the row.
	Hint string
	// Detail is shown in the detail panel while the item is selected.
	Detail proto.StyledText
	// DetailTile, if set, is a tile drawn after line DetailHead of Detail,
	// the name of what it describes, when the map uses tiles.
	DetailTile *proto.PictCell
	DetailHead int
	// Dim draws the label dimmed, for a choice that cannot be taken now.
	Dim bool
	// LabelColor, if set, is the label's color (a palette index) when it
	// is not selected.
	LabelColor *int
	// Header makes the row a group's heading, which the selection skips
	// and a search leaves out.
	Header bool
}

// setGlyph shows the tile, or without one the letter ch in color.
func (it *listItem) setGlyph(tile *proto.TileSpot, ch string, color int) {
	if tile != nil {
		it.Tile = &proto.PictCell{FgRow: tile.Row, FgCol: tile.Col}
	} else if r := []rune(ch); len(r) > 0 {
		it.Icon, it.IconColor = r[0], color
	}
}

// listKey is a key that acts on the selected item, shown in the footer.
type listKey struct {
	Key   rune
	Label string
	Do    func(item int)
}

// listButton is a button under a list, which acts on the selected item.
type listButton struct {
	Label string
	Do    func(item int)
}

// listCheck is a checkbox above a list, such as a switch that changes
// what the list shows.
type listCheck struct {
	Label  string
	On     func() bool
	Toggle func()
}

// listScreen is the shared list screen: a list you move through and
// search, with icons and a detail panel. Most native screens are one.
type listScreen struct {
	// id names the list in Limoni's accessibility tree, for tests.
	id    string
	title string
	items []listItem
	// choose runs on Enter, with the item's index in items.
	choose func(item int)
	// cancel runs on Escape. Nil closes nothing.
	cancel func()
	// keys act on the selected item.
	keys []listKey
	// hints are more keys to show in the footer, such as "/ pack or
	// equipment", for keys that pick handles.
	hints []string
	// searchable lets '/' filter the list.
	searchable bool
	// detail shows a panel with the selected item's Detail.
	detail bool
	// width and height are the box's size, or 0 to fit.
	width, height int
	// place, if set, puts the box of size w by h in area; otherwise it
	// is centered.
	place func(area Box, w, h int) Box
	// note is a line of text under the list.
	note string
	// intro is text above the list, such as a dialog's question.
	intro proto.StyledText
	// buttons are drawn under the list, on as many rows as they need.
	buttons []listButton
	// checks are drawn above the list.
	checks []listCheck
	// clickChooses makes one click on a row choose it, as in a menu.
	// Otherwise one click selects and a double click chooses.
	clickChooses bool
	// tabs, if set, are drawn along the top, with tab the current one.
	// Tab, Shift+Tab, and clicks pick another and run onTab, which fills
	// items for it.
	tabs  []string
	tab   int
	onTab func(t int)
	// focused draws the border in screenFocus, for a list that shares
	// its screen with other parts, such as the Tile Editor's.
	focused bool
	// clicked, if set, runs before a click on the screen acts.
	clicked func()
	// changed, if set, runs when the wheel or a click has moved the
	// selection. They reach the list itself, not a screen built around it.
	changed func()
	// pick, if set, sees each key first, outside a search, and returns
	// whether it took it, as a key that picks an item by its letter.
	pick func(ev driver.KeyEvent) bool
	// detailOffset is how far the detail panel is scrolled, and
	// detailHeight how many rows it showed when last drawn.
	detailOffset, detailHeight int
	// detailFor is the item the detail panel was scrolled for.
	detailFor int

	// listHeight is how many rows the list showed when last drawn.
	listHeight int
	// The last row clicked, by its index in items, and when.
	clickedItem int
	clickedAt   time.Time
	// hotspots are the rows and buttons last drawn.
	hotspots []hotspot

	state     *widgets.ListState
	query     string
	searching bool
}

func newListScreen(id, title string, items []listItem) *listScreen {
	s := &listScreen{id: id, title: title, items: items, state: widgets.NewListState()}
	if len(items) > 0 {
		s.state.Select(0)
		s.settle(1)
	}
	return s
}

// shown returns the indexes of the items that match the search. A
// search leaves out the headings.
func (s *listScreen) shown() []int {
	q := strings.ToLower(s.query)
	var out []int
	for i, it := range s.items {
		if q == "" || !it.Header && strings.Contains(strings.ToLower(it.Label), q) {
			out = append(out, i)
		}
	}
	return out
}

// settle moves the selection off a heading, in direction dir (1 down,
// -1 up), or the other way when there is nothing that way.
func (s *listScreen) settle(dir int) {
	shown := s.shown()
	at := func(n int) bool { return n >= 0 && n < len(shown) && !s.items[shown[n]].Header }
	n := s.state.Selected
	if n < 0 || n >= len(shown) || at(n) {
		return
	}
	for _, d := range []int{dir, -dir} {
		for i := n + d; i >= 0 && i < len(shown); i += d {
			if at(i) {
				s.state.Select(i)
				return
			}
		}
	}
}

// selected returns the selected item's index in items, or -1.
func (s *listScreen) selected() int {
	shown := s.shown()
	if s.state.Selected < 0 || s.state.Selected >= len(shown) || s.items[shown[s.state.Selected]].Header {
		return -1
	}
	return shown[s.state.Selected]
}

// selectItem selects the item with index i in items, if it is shown.
func (s *listScreen) selectItem(i int) {
	for n, j := range s.shown() {
		if j == i {
			s.state.Select(n)
		}
	}
}

func (s *listScreen) key(ev driver.KeyEvent) {
	shown := s.shown()
	if s.searching {
		switch ev.Type {
		case driver.KeyEsc:
			s.searching, s.query = false, ""
		case driver.KeyEnter:
			s.searching = false
			return
		case driver.KeyBackspace:
			if r := []rune(s.query); len(r) > 0 {
				s.query = string(r[:len(r)-1])
			}
		case driver.KeyArrowUp, driver.KeyArrowDown, driver.KeyPageUp, driver.KeyPageDown, driver.KeyHome, driver.KeyEnd:
			s.move(ev, len(shown))
			return
		case driver.KeySpace:
			s.query += " "
		case driver.KeyRune:
			if ev.Ctrl || ev.Alt || ev.Ch < ' ' {
				return
			}
			s.query += string(ev.Ch)
		default:
			return
		}
		s.state.Select(0)
		if len(s.shown()) == 0 {
			s.state.Select(-1)
		}
		s.settle(1)
		return
	}
	if s.pick != nil && s.pick(ev) {
		return
	}
	if s.detail && ev.Shift {
		// Shift with the arrows and page keys scrolls the detail panel.
		switch ev.Type {
		case driver.KeyArrowDown:
			s.detailOffset++
			return
		case driver.KeyArrowUp:
			s.detailOffset = max(s.detailOffset-1, 0)
			return
		case driver.KeyPageDown:
			s.detailOffset += pageRows(s.detailHeight)
			return
		case driver.KeyPageUp:
			s.detailOffset = max(s.detailOffset-pageRows(s.detailHeight), 0)
			return
		}
	}
	switch ev.Type {
	case driver.KeyTab:
		if len(s.tabs) > 0 {
			d := 1
			if ev.Shift {
				d = -1
			}
			s.setTab(s.tab + d)
		}
		return
	case driver.KeyEsc:
		if s.query != "" {
			s.query = ""
			s.state.Select(0)
			s.settle(1)
			return
		}
		if s.cancel != nil {
			s.cancel()
		}
		return
	case driver.KeyEnter:
		if i := s.selected(); i >= 0 && s.choose != nil {
			s.choose(i)
		}
		return
	case driver.KeyRune:
		if ev.Ch == '/' && s.searchable {
			s.searching = true
			return
		}
		for _, k := range s.keys {
			if ev.Ch == k.Key && !ev.Ctrl && !ev.Alt {
				k.Do(s.selected())
				return
			}
		}
		switch ev.Ch {
		case 'j', '2':
			ev.Type = driver.KeyArrowDown
		case 'k', '8':
			ev.Type = driver.KeyArrowUp
		}
	}
	s.move(ev, len(shown))
}

// setTab makes tab t current, wrapping around, and lets onTab fill the
// list for it.
func (s *listScreen) setTab(t int) {
	n := len(s.tabs)
	s.tab = (t%n + n) % n
	if s.onTab != nil {
		s.onTab(s.tab)
	}
	s.state.Select(0)
	if len(s.shown()) == 0 {
		s.state.Select(-1)
	}
	s.settle(1)
}

// moreWidth is the cells the More tiles take on a row.
func moreWidth(it listItem) int { return len(it.More) * 3 }

// move handles the keys that move the selection.
func (s *listScreen) move(ev driver.KeyEvent, n int) {
	if n == 0 {
		return
	}
	switch ev.Type {
	case driver.KeyArrowDown:
		s.state.Select((s.state.Selected + 1) % n)
	case driver.KeyArrowUp:
		s.state.Select((s.state.Selected - 1 + n) % n)
	case driver.KeyHome:
		s.state.Select(0)
	case driver.KeyEnd:
		s.state.Select(n - 1)
	case driver.KeyPageDown:
		s.state.Select(min(s.state.Selected+pageRows(s.listHeight), n-1))
	case driver.KeyPageUp:
		s.state.Select(max(s.state.Selected-pageRows(s.listHeight), 0))
	}
	switch ev.Type {
	case driver.KeyArrowUp, driver.KeyPageUp, driver.KeyEnd:
		s.settle(-1)
	default:
		s.settle(1)
	}
}

// wheel scrolls the list by d notches of the mouse wheel.
func (s *listScreen) wheel(d int) {
	wheelList(s.state, len(s.shown()), s.listHeight, d, nil)
	s.settle(d)
	if s.changed != nil {
		s.changed()
	}
}

// listWidth is the width of the list part: room for the longest label.
func (s *listScreen) listWidth() int {
	w := limoni.StringWidth(s.title) + 4
	for _, it := range s.items {
		iw := limoni.StringWidth(it.Label) + 4
		if it.Icon != 0 {
			iw += 2
		}
		if it.Tile != nil {
			iw += 3
			if it.Icon != 0 {
				iw++
			}
		}
		if len(it.More) > 0 {
			iw += moreWidth(it) + 1
		}
		if it.Hint != "" {
			iw += limoni.StringWidth(it.Hint) + 2
		}
		w = max(w, iw)
	}
	return w
}

func (s *listScreen) footer() string {
	return strings.Join(s.footerParts(), "  ")
}

// footerLines is the footer wrapped to width, between its keys.
func (s *listScreen) footerLines(width int) []string {
	var lines []string
	line := ""
	for _, p := range s.footerParts() {
		switch {
		case line == "":
			line = p
		case limoni.StringWidth(line)+2+limoni.StringWidth(p) <= width:
			line += "  " + p
		default:
			lines = append(lines, line)
			line = p
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func (s *listScreen) footerParts() []string {
	var parts []string
	if len(s.tabs) > 0 {
		parts = append(parts, "Tab next")
	}
	for _, k := range s.keys {
		parts = append(parts, string(k.Key)+" "+k.Label)
	}
	parts = append(parts, s.hints...)
	if s.searchable {
		parts = append(parts, "/ search")
	}
	if s.cancel != nil {
		parts = append(parts, "Esc back")
	}
	return parts
}

func (s *listScreen) box(area Box) Box {
	w, h := s.width, s.height
	if w == 0 {
		w = s.listWidth() + 2
		if s.detail {
			w = max(w+40, 72)
		}
		w = max(w, limoni.StringWidth(s.footer())+4, limoni.StringWidth(s.note)+4)
		// Room for the buttons on one row.
		bw := 0
		for _, bt := range s.buttons {
			bw += limoni.StringWidth(bt.Label) + 5
		}
		w = max(w, bw+3)
	}
	if h == 0 {
		h = len(s.items) + 2
		if s.note != "" {
			h += 2
		}
		if s.footer() != "" || s.searchable {
			h++
		}
		if len(s.buttons) > 0 {
			h += len(buttonRows(s.buttons, min(w, area.W)-3)) + 1
		}
		if len(s.checks) > 0 {
			h += 2
		}
		if s.detail {
			h = max(h, 16)
		}
		if len(s.intro) > 0 {
			h += styledHeight(s.intro, min(w, area.W)-4) + 1
		}
		if len(s.tabs) > 0 {
			h += 2
		}
	}
	w, h = min(w, area.W), min(h, area.H)
	if s.place != nil {
		return s.place(area, w, h)
	}
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

// hotspot makes part of the screen clickable, both through Limoni's
// click regions, which it tries before the widgets' own, and through
// click().
func (s *listScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	if s.clicked != nil {
		act := do
		do = func() {
			s.clicked()
			act()
		}
	}
	s.hotspots = append(s.hotspots, hotspot{box: b, do: do})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

// controls returns the parts a click acts on, as last drawn.
func (s *listScreen) controls() []hotspot { return s.hotspots }

func (s *listScreen) click(x, y int) {
	for _, h := range s.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

// clickItem handles a click on the row showing item.
func (s *listScreen) clickItem(item int) {
	if s.items[item].Header {
		return
	}
	again := item == s.clickedItem && time.Since(s.clickedAt) <= doubleClick
	s.clickedItem, s.clickedAt = item, time.Now()
	s.selectItem(item)
	if s.changed != nil {
		s.changed()
	}
	if (s.clickChooses || again) && s.choose != nil {
		s.clickedAt = time.Time{}
		s.choose(item)
	}
}

func (s *listScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	pal := env.pal
	s.hotspots = s.hotspots[:0]
	r := rect(b)
	fillBox(f, b, screenBg)
	border := screenBorder
	if s.focused {
		border = screenFocus
	}
	block := widgets.NewBlock().Rounded().WithTitle(" " + s.title + " ").
		WithTitleStyle(screenTitle).WithBorderStyle(border)
	f.RenderWidget(block, r)
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}

	// The tabs, the current one highlighted, on as many rows as they
	// need.
	if len(s.tabs) > 0 {
		x, y := inner.X+1, inner.Y
		for i, t := range s.tabs {
			label := " " + t + " "
			st := screenDim
			if i == s.tab {
				st = screenSelected
			}
			w := limoni.StringWidth(label)
			if x > inner.X+1 && x+w > inner.X+inner.W-1 {
				x, y = inner.X+1, y+1
			}
			f.Text(x, y, label, st)
			i := i
			s.hotspot(f, Box{x, y, w, 1}, func() { s.setTab(i) })
			x += w + 1
		}
		inner.H -= y - inner.Y + 2
		inner.Y = y + 2
	}

	// The bottom row holds the search line or the footer.
	bottom := inner.Y + inner.H - 1
	if s.searching || s.query != "" {
		f.TextFit(inner.X+1, bottom, inner.W-2, "Search: "+s.query+"_", screenText)
		inner.H--
	} else if lines := s.footerLines(inner.W - 2); len(lines) > 0 {
		for i, l := range lines {
			f.TextFit(inner.X+1, bottom-len(lines)+1+i, inner.W-2, l, screenDim)
		}
		inner.H -= len(lines)
	}
	if len(s.buttons) > 0 {
		// Buttons sit at the right of their row, the last one rightmost,
		// and wrap onto more rows above when they do not fit.
		rows := buttonRows(s.buttons, inner.W-1)
		y := inner.Y + inner.H - len(rows)
		for _, row := range rows {
			x := inner.X + inner.W - 1
			for i := len(row) - 1; i >= 0; i-- {
				bt := row[i]
				w := limoni.StringWidth(bt.Label) + 4
				x -= w
				// No ID: with one, Limoni registers a click region that
				// only takes the focus, over the one that presses.
				at := Box{x, y, w, 1}
				press := func() { bt.Do(s.selected()) }
				f.RenderWidget(widgets.Button{
					Label:        bt.Label,
					Style:        screenTitle.WithBg(screenBg.Bg),
					FocusedStyle: screenTitle.WithBg(screenBg.Bg),
					OnPress:      press,
				}, rect(at))
				s.hotspot(f, at, press)
				x--
			}
			y++
		}
		inner.H -= len(rows) + 1
	}
	if s.note != "" {
		f.TextFit(inner.X+1, inner.Y+inner.H-1, inner.W-2, s.note, screenDim)
		inner.H -= 2
	}

	if len(s.intro) > 0 {
		ih := styledHeight(s.intro, inner.W-2)
		f.RenderWidget(&styledView{text: s.intro, pal: pal}, rect(Box{inner.X + 1, inner.Y, inner.W - 2, ih}))
		inner.Y += ih + 1
		inner.H -= ih + 1
	}

	// The checkboxes, on one row, or more if they do not fit.
	if len(s.checks) > 0 {
		x, y := inner.X+1, inner.Y
		for _, c := range s.checks {
			box := "[ ] "
			if c.On() {
				box = "[x] "
			}
			w := limoni.StringWidth(box + c.Label)
			if x > inner.X+1 && x+w > inner.X+inner.W-1 {
				x, y = inner.X+1, y+1
			}
			f.Text(x, y, box+c.Label, screenText)
			at := Box{x, y, w, 1}
			node := accessibility.Node{Role: accessibility.RoleCheckbox, Label: c.Label, Value: "false", Bounds: rect(at)}
			if c.On() {
				node.Value, node.State = "true", accessibility.StateChecked
			}
			f.RegisterAccessibility(node)
			s.hotspot(f, at, c.Toggle)
			x += w + 2
		}
		inner.H -= y - inner.Y + 2
		inner.Y = y + 2
	}

	listBox := inner
	if s.detail {
		listBox.W = min(s.listWidth(), inner.W*3/5)
		detailBox := Box{listBox.X + listBox.W + 1, inner.Y, inner.W - listBox.W - 2, inner.H}
		for y := 0; y < inner.H; y++ {
			f.Text(listBox.X+listBox.W, inner.Y+y, "│", screenBorder)
		}
		if i := s.selected(); i >= 0 && !detailBox.Empty() {
			text := Box{detailBox.X + 1, detailBox.Y, detailBox.W - 1, detailBox.H}
			if i != s.detailFor {
				s.detailFor, s.detailOffset = i, 0
			}
			detail := s.items[i].Detail
			headTile := s.items[i].DetailTile
			if env.sprite == nil {
				headTile = nil
			}
			if headTile != nil {
				detail = roomForHeadTile(detail, s.items[i].DetailHead)
			}
			rows := styledHeight(detail, text.W)
			// Text longer than the panel gets a scroll bar in the
			// column next to the border, with a space before it.
			bar := Box{text.X + text.W, text.Y, 1, text.H}
			scrolls := rows > text.H && text.H >= 3
			if scrolls {
				text.W--
				rows = styledHeight(detail, text.W)
			}
			s.detailHeight = text.H
			s.detailOffset = min(s.detailOffset, max(rows-text.H, 0))
			f.RenderWidget(&styledView{text: detail, pal: pal, Offset: s.detailOffset}, rect(text))
			if headTile != nil {
				drawHeadTile(f, text, detail, s.items[i].DetailHead, s.detailOffset, headTile, env)
			}
			f.RenderWidget(wheelArea{scroll: func(d int) {
				s.detailOffset = min(max(s.detailOffset+d*wheelStep, 0), max(rows-text.H, 0))
			}, pointer: env.pointer}, rect(text))
			if scrolls {
				f.RenderWidget(scrollBar{vertical: true, total: rows, shown: text.H, offset: s.detailOffset, set: func(o int) {
					s.detailOffset = min(max(o, 0), max(rows-text.H, 0))
				}}, rect(bar))
			}
		}
	}

	shown := s.shown()
	labels := make([]string, len(shown))
	tiles := env.sprite != nil
	for n, i := range shown {
		it := s.items[i]
		label := it.Label
		if it.Tile != nil && tiles {
			// Room for the tile, two cells wide.
			label = "   " + label
		}
		if it.Icon != 0 {
			// The icon is in the label too, so the accessibility tree
			// has it; it is redrawn in its color below. Two spaces keep
			// it clear of the tile.
			sep := " "
			if it.Tile != nil && tiles {
				sep = "  "
			}
			label = string(it.Icon) + sep + label
		}
		labels[n] = " " + label
	}
	rowStyle := screenText.WithBg(screenBg.Bg)
	list := &widgets.List{
		ID:            s.id,
		Label:         s.title,
		Items:         labels,
		Style:         rowStyle,
		SelectedStyle: screenSelected,
		// Limoni applies FocusedStyle to every row of a focused list, so
		// it must be the plain row style; only the selection stands out.
		FocusedStyle: rowStyle,
		Scrollbar:    true,
		State:        s.state,
	}
	f.RenderWidget(list, rect(listBox))
	s.listHeight = listBox.H
	f.RenderWidget(wheelArea{scroll: s.wheel, pointer: env.pointer}, rect(listBox))
	// Icons, tiles, and hints go over the list's rows, and so do click
	// handlers, which Limoni tries before the list's own.
	for y := 0; y < listBox.H; y++ {
		n := s.state.Offset + y
		if n >= len(shown) {
			break
		}
		item := shown[n]
		it := s.items[item]
		row := Box{listBox.X, listBox.Y + y, listBox.W, 1}
		s.hotspot(f, row, func() { s.clickItem(item) })
		if it.Header {
			fillBox(f, Box{row.X, row.Y, row.W - 1, 1}, screenBg)
			f.TextFit(row.X+1, row.Y, row.W-2, it.Label, screenTitle.WithBg(screenBg.Bg))
			continue
		}
		if it.Dim && n != s.state.Selected {
			// Short of the scrollbar, in the last column.
			f.TextFit(row.X, row.Y, row.W-1, labels[n], screenDim.WithBg(screenBg.Bg))
		}
		if it.LabelColor != nil && n != s.state.Selected {
			f.TextFit(row.X, row.Y, row.W-1, labels[n], cell.NewStyle().WithFg(pal.color(*it.LabelColor)).WithBg(screenBg.Bg))
		}
		if it.Tile != nil && tiles {
			at := Box{listBox.X + 1, listBox.Y + y, 2, 1}
			if it.Icon != 0 {
				at.X += 3
			}
			// The sprite sits on the row's own background, highlighted
			// or not.
			bg := screenBg.Bg
			if n == s.state.Selected {
				bg = screenSelected.Bg
			}
			env.tile(f, at, it.Tile.FgRow, it.Tile.FgCol, bg)
		}
		if it.Icon != 0 {
			st := cell.NewStyle().WithFg(pal.color(it.IconColor))
			if n == s.state.Selected {
				st = st.WithBg(screenSelected.Bg)
			}
			f.Text(listBox.X+1, listBox.Y+y, string(it.Icon), st)
		}
		hintX := listBox.X + listBox.W - 2
		if it.Hint != "" {
			st := screenDim
			if n == s.state.Selected {
				st = screenSelected
			}
			hintX -= limoni.StringWidth(it.Hint)
			// A space keeps a label too long for the row off the hint.
			f.Text(hintX-1, listBox.Y+y, " "+it.Hint, st)
		}
		if len(it.More) > 0 && tiles {
			bg := screenBg.Bg
			dot := screenDim
			if n == s.state.Selected {
				bg, dot = screenSelected.Bg, screenSelected
			}
			x := hintX - 1 - moreWidth(it)
			for _, t := range it.More {
				if t == nil {
					f.Text(x, listBox.Y+y, " · ", dot)
				} else {
					env.tile(f, Box{x, listBox.Y + y, 2, 1}, t.FgRow, t.FgCol, bg)
				}
				x += 3
			}
		}
	}
}

// buttonRows lays buttons out in rows no wider than width, in order.
func buttonRows(buttons []listButton, width int) [][]listButton {
	var rows [][]listButton
	used := 0
	for _, bt := range buttons {
		w := limoni.StringWidth(bt.Label) + 5
		if n := len(rows); n == 0 || used+w > width && len(rows[n-1]) > 0 {
			rows = append(rows, nil)
			used = 0
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], bt)
		used += w
	}
	return rows
}

// fillBox clears a box to style.
func fillBox(f *terminal.Frame, b Box, style cell.Style) {
	line := strings.Repeat(" ", max(b.W, 0))
	for y := b.Y; y < b.Y+b.H; y++ {
		f.Text(b.X, y, line, style)
	}
}

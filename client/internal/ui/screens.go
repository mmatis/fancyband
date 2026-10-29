package ui

import (
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/thebanri/limoni"
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

// hotspot is a part of a screen that a click acts on.
type hotspot struct {
	box Box
	do  func()
}

// drawEnv is what screens draw with: the game's palette, and its tiles
// when the map uses them.
type drawEnv struct {
	pal palette
	// sprite returns a tile's sprite on a background color, or is nil
	// in text mode.
	sprite func(row, col uint8, bg color.RGBA) image.Image
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
	screenSelected = limoni.Fg(limoni.RGB(20, 20, 30)).WithBg(limoni.RGB(230, 200, 120))
	screenBg       = cell.NewStyle().WithBg(limoni.RGB(16, 16, 24))
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
	// searchable lets '/' filter the list.
	searchable bool
	// detail shows a panel with the selected item's Detail.
	detail bool
	// width and height are the box's size, or 0 to fit.
	width, height int
	// note is a line of text under the list.
	note string
	// intro is text above the list, such as a dialog's question.
	intro proto.StyledText
	// buttons are drawn under the list.
	buttons []listButton
	// clickChooses makes one click on a row choose it, as in a menu.
	// Otherwise one click selects and a double click chooses.
	clickChooses bool
	// tabs, if set, are drawn along the top, with tab the current one.
	// Tab, Shift+Tab, and clicks pick another and run onTab, which fills
	// items for it.
	tabs  []string
	tab   int
	onTab func(t int)

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
	}
	return s
}

// shown returns the indexes of the items that match the search.
func (s *listScreen) shown() []int {
	q := strings.ToLower(s.query)
	var out []int
	for i, it := range s.items {
		if q == "" || strings.Contains(strings.ToLower(it.Label), q) {
			out = append(out, i)
		}
	}
	return out
}

// selected returns the selected item's index in items, or -1.
func (s *listScreen) selected() int {
	shown := s.shown()
	if s.state.Selected < 0 || s.state.Selected >= len(shown) {
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
		case driver.KeyBackspace:
			if r := []rune(s.query); len(r) > 0 {
				s.query = string(r[:len(r)-1])
			}
		case driver.KeyArrowUp, driver.KeyArrowDown:
			s.move(ev, len(shown))
			return
		case driver.KeySpace:
			s.query += " "
		case driver.KeyRune:
			if !ev.Ctrl && !ev.Alt && ev.Ch >= ' ' {
				s.query += string(ev.Ch)
			}
		}
		s.state.Select(0)
		if len(s.shown()) == 0 {
			s.state.Select(-1)
		}
		return
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
		s.state.Select(min(s.state.Selected+10, n-1))
	case driver.KeyPageUp:
		s.state.Select(max(s.state.Selected-10, 0))
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
	var parts []string
	if len(s.tabs) > 0 {
		parts = append(parts, "Tab next")
	}
	for _, k := range s.keys {
		parts = append(parts, string(k.Key)+" "+k.Label)
	}
	if s.searchable {
		parts = append(parts, "/ search")
	}
	if s.cancel != nil {
		parts = append(parts, "Esc back")
	}
	return strings.Join(parts, "  ")
}

func (s *listScreen) box(area Box) Box {
	w, h := s.width, s.height
	if w == 0 {
		w = s.listWidth() + 2
		if s.detail {
			w = max(w+40, 72)
		}
		w = max(w, limoni.StringWidth(s.footer())+4, limoni.StringWidth(s.note)+4)
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
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

// hotspot makes part of the screen clickable, both through Limoni's
// click regions, which it tries before the widgets' own, and through
// click().
func (s *listScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	s.hotspots = append(s.hotspots, hotspot{b, do})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

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
	again := item == s.clickedItem && time.Since(s.clickedAt) <= doubleClick
	s.clickedItem, s.clickedAt = item, time.Now()
	s.selectItem(item)
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
	block := widgets.NewBlock().Rounded().WithTitle(" " + s.title + " ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenBorder)
	f.RenderWidget(block, r)
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}

	// The tabs, the current one highlighted.
	if len(s.tabs) > 0 {
		x := inner.X + 1
		for i, t := range s.tabs {
			label := " " + t + " "
			st := screenDim
			if i == s.tab {
				st = screenSelected
			}
			w := limoni.StringWidth(label)
			f.Text(x, inner.Y, label, st)
			i := i
			s.hotspot(f, Box{x, inner.Y, w, 1}, func() { s.setTab(i) })
			x += w + 1
		}
		inner.Y += 2
		inner.H -= 2
	}

	// The bottom row holds the search line or the footer.
	bottom := inner.Y + inner.H - 1
	if s.searching || s.query != "" {
		f.TextFit(inner.X+1, bottom, inner.W-2, "Search: "+s.query+"_", screenText)
		inner.H--
	} else if ft := s.footer(); ft != "" {
		f.TextFit(inner.X+1, bottom, inner.W-2, ft, screenDim)
		inner.H--
	}
	if len(s.buttons) > 0 {
		// Buttons sit at the right of their row, the last one rightmost.
		x := inner.X + inner.W - 1
		y := inner.Y + inner.H - 1
		for i := len(s.buttons) - 1; i >= 0; i-- {
			bt := s.buttons[i]
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
		inner.H -= 2
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

	listBox := inner
	if s.detail {
		listBox.W = min(s.listWidth(), inner.W/2)
		detailBox := Box{listBox.X + listBox.W + 1, inner.Y, inner.W - listBox.W - 2, inner.H}
		for y := 0; y < inner.H; y++ {
			f.Text(listBox.X+listBox.W, inner.Y+y, "│", screenBorder)
		}
		if i := s.selected(); i >= 0 && !detailBox.Empty() {
			f.RenderWidget(&styledView{text: s.items[i].Detail, pal: pal}, rect(Box{detailBox.X + 1, detailBox.Y, detailBox.W - 1, detailBox.H}))
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
		if it.Tile != nil && tiles {
			at := Box{listBox.X + 1, listBox.Y + y, 2, 1}
			if it.Icon != 0 {
				at.X += 3
			}
			for x := at.X; x < at.X+at.W; x++ {
				f.Buffer.SetCell(uint16(x), uint16(at.Y), cell.Cell{Content: cell.RuneImage})
			}
			// The sprite sits on the row's own background, highlighted
			// or not.
			bg := screenBg.Bg
			if n == s.state.Selected {
				bg = screenSelected.Bg
			}
			r, g, b := bg.RGB()
			img := env.sprite(it.Tile.FgRow, it.Tile.FgCol, color.RGBA{r, g, b, 255})
			f.RegisterImageRegion(terminal.ImageRegion{Area: rect(at), Img: img})
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
			f.Text(hintX, listBox.Y+y, it.Hint, st)
		}
		if len(it.More) > 0 && tiles {
			bg := screenBg.Bg
			dot := screenDim
			if n == s.state.Selected {
				bg, dot = screenSelected.Bg, screenSelected
			}
			r, g, b := bg.RGB()
			x := hintX - 1 - moreWidth(it)
			for _, t := range it.More {
				if t == nil {
					f.Text(x, listBox.Y+y, " · ", dot)
				} else {
					at := Box{x, listBox.Y + y, 2, 1}
					for cx := at.X; cx < at.X+at.W; cx++ {
						f.Buffer.SetCell(uint16(cx), uint16(at.Y), cell.Cell{Content: cell.RuneImage})
					}
					f.RegisterImageRegion(terminal.ImageRegion{Area: rect(at), Img: env.sprite(t.FgRow, t.FgCol, color.RGBA{r, g, b, 255})})
				}
				x += 3
			}
		}
	}
}

// fillBox clears a box to style.
func fillBox(f *terminal.Frame, b Box, style cell.Style) {
	line := strings.Repeat(" ", max(b.W, 0))
	for y := b.Y; y < b.Y+b.H; y++ {
		f.Text(b.X, y, line, style)
	}
}

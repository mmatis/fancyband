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

// IDs of the store screen and its lists in Limoni's accessibility tree,
// which tests use.
const (
	storeID      = "store"
	storeStockID = "store-stock"
	storeGearID  = "store-gear"
)

// storeMessages is how many of the game's last messages the store screen
// shows.
const storeMessages = 2

// storeScreen is the native store: what the store sells beside what the
// character carries, the selected object's description under them, and
// the game's last messages. The engine runs the store's commands while
// the screen stays open, and the screen's reply leaves the store.
type storeScreen struct {
	m *Model
	// id is the store screen's request.
	id   int
	info proto.StoreInfo
	// stock and gear are the two lists, and focus the one with the keys:
	// 0 for the stock, 1 for the gear.
	stock, gear *listScreen
	focus       int
	// messages are the game's messages since the store opened, the
	// newest last.
	messages []string
	// busy is set while a command runs, which takes no more keys.
	busy bool
	// detailOffset is how far the description is scrolled, detailHeight
	// how many rows it showed when last drawn, and detailFor the object
	// it was scrolled for.
	detailOffset, detailHeight int
	detailFor                  string
	hotspots                   []hotspot
}

// openStore opens the store screen for the screen request id. If the
// engine cannot describe the store, the classic one opens instead.
func (m *Model) openStore(id int) {
	classic := m.classicElse(id)
	m.queryElse("STORE", nil, func(a proto.EventData) {
		if a.Store == nil {
			classic()
			return
		}
		s := &storeScreen{m: m, id: id}
		s.fill(*a.Store)
		m.store = s
		m.push(s)
	}, classic)
}

// verbs returns what buying, buying one, and selling are called here.
func (s *storeScreen) verbs() (buy, one, sell string) {
	switch {
	case s.info.Home:
		return "Take", "Take one", "Drop"
	case s.info.NoSelling:
		return "Buy", "Buy one", "Give"
	}
	return "Buy", "Buy one", "Sell"
}

// storeRow makes a list row for an object in the store or carried: its
// tile or letter, name, and hint.
func storeRow(it proto.StoreItem, label, hint string) listItem {
	row := listItem{Label: label, Hint: hint, Dim: it.Max == 0}
	row.setGlyph(it.Tile, it.Char, it.Color)
	return row
}

// fill shows a STORE answer, keeping each list's selection where it was.
func (s *storeScreen) fill(info proto.StoreInfo) {
	s.info = info
	stock := make([]listItem, len(info.Stock))
	for i, it := range info.Stock {
		hint := weightText(it.Weight)
		if !info.Home {
			hint = fmt.Sprintf("%d gold", it.Price)
		}
		stock[i] = storeRow(it, it.Name, hint)
		s.m.recallInto(&stock[i], map[string]any{"item": it.Ref.Map()})
	}
	gear := make([]listItem, len(info.Gear))
	for i, it := range info.Gear {
		label := fmt.Sprintf("%c) %s", itemLetter(it.Ref), it.Name)
		if it.Ref.Equip != nil {
			label = "worn: " + it.Name
		}
		hint := ""
		if len(it.Prices) > 0 && !info.NoSelling {
			hint = fmt.Sprintf("%d gold", it.Prices[0])
		}
		gear[i] = storeRow(it, label, hint)
		s.m.recallInto(&gear[i], map[string]any{"item": it.Ref.Map()})
	}

	title := "For sale"
	if info.Home {
		title = "In your home"
	}
	s.stock = s.keepList(s.stock, storeStockID, title, stock)
	s.gear = s.keepList(s.gear, storeGearID, "What you carry", gear)
	s.stock.choose = func(int) { s.buy(false) }
	s.gear.choose = func(int) { s.sell() }
	s.stock.clicked = func() { s.focus = 0 }
	s.gear.clicked = func() { s.focus = 1 }
}

// keepList makes a list of items, selecting the row old had selected.
func (s *storeScreen) keepList(old *listScreen, id, title string, items []listItem) *listScreen {
	l := newListScreen(id, title, items)
	if old != nil {
		l.state.Offset = old.state.Offset
		l.selectItem(min(old.selected(), len(items)-1))
	}
	return l
}

// focused returns the list with the keys.
func (s *storeScreen) focused() *listScreen {
	if s.focus == 1 {
		return s.gear
	}
	return s.stock
}

// say shows a line in the store's messages.
func (s *storeScreen) say(text string) {
	s.messages = append(s.messages, text)
}

// leave closes the store screen, and the character leaves the store.
func (s *storeScreen) leave() {
	s.m.pop()
	s.m.store = nil
	s.m.send(proto.NewReply(s.id, map[string]any{}))
}

// run sends one of the store's commands on an object, and shows the
// store as it is once the command has run.
func (s *storeScreen) run(cmd string, it proto.StoreItem, n int) {
	s.busy = true
	s.m.command(cmd, map[string]any{"item": it.Ref.Map(), "quantity": n}, func(r proto.EventData) {
		if !r.OK {
			s.busy = false
			s.say("You cannot do that: " + r.Error)
			return
		}
		s.refresh()
	})
}

// refresh asks for the store again.
func (s *storeScreen) refresh() {
	s.m.queryElse("STORE", nil, func(a proto.EventData) {
		s.busy = false
		if a.Store != nil {
			s.fill(*a.Store)
		}
	}, func() { s.busy = false })
}

// howMany asks how many of an object to buy or sell, up to max, unless
// there is only one; then runs do with the number.
func (s *storeScreen) howMany(text string, max int, do func(n int)) {
	if max <= 1 {
		do(1)
		return
	}
	q := &quantityDialog{text: text, max: max, value: 1}
	q.done = func(n int, ok bool) {
		s.m.pop()
		if ok {
			do(n)
		}
	}
	s.m.push(q)
}

// buy buys the selected object from the store, or takes it from the
// home: one, or as many as the player says, after a check of the price.
func (s *storeScreen) buy(one bool) {
	i := s.stock.selected()
	if i < 0 || s.busy {
		return
	}
	it := s.info.Stock[i]
	verb, _, _ := s.verbs()
	if it.Max == 0 {
		s.say(it.Why)
		return
	}
	text := fmt.Sprintf("%s how many? (at most %d)", verb, it.Max)
	if it.Have > 0 {
		text = fmt.Sprintf("%s how many? (you have %d)", verb, it.Have)
	}
	max := it.Max
	if one {
		max = 1
	}
	s.howMany(text, max, func(n int) {
		if s.info.Home {
			s.run("RETRIEVE", it, n)
			return
		}
		price := it.Prices[n-1]
		s.m.ask(fmt.Sprintf("Buy %s for %d gold?", countOf(it.Name, n), price), func(yes bool) {
			if yes {
				s.run("BUY", it, n)
			}
		})
	})
}

// sell sells the selected object to the store, or drops it in the home.
func (s *storeScreen) sell() {
	i := s.gear.selected()
	if i < 0 || s.busy {
		return
	}
	it := s.info.Gear[i]
	_, _, verb := s.verbs()
	if it.Max == 0 {
		s.say(it.Why)
		return
	}
	s.howMany(fmt.Sprintf("%s how many?", verb), it.Max, func(n int) {
		if s.info.Home {
			s.run("STASH", it, n)
			return
		}
		question := fmt.Sprintf("Sell %s for %d gold?", countOf(it.Name, n), it.Prices[n-1])
		if s.info.NoSelling {
			question = fmt.Sprintf("Give %s to the store?", countOf(it.Name, n))
		}
		s.m.ask(question, func(yes bool) {
			if yes {
				s.run("SELL", it, n)
			}
		})
	})
}

// countOf names n of an object whose name starts with how many there
// are, such as "5 Flasks of Oil": "2 Flasks of Oil", or "one of the 5
// Flasks of Oil". All of them, or an object alone, keep its own name.
func countOf(name string, n int) string {
	var have int
	if _, err := fmt.Sscanf(name, "%d ", &have); err != nil || have <= 1 || n == have {
		return name
	}
	if n == 1 {
		return "one of the " + name
	}
	_, rest, _ := strings.Cut(name, " ")
	return fmt.Sprintf("%d %s", n, rest)
}

func (s *storeScreen) key(ev driver.KeyEvent) {
	if s.busy {
		return
	}
	if ev.Shift {
		// Shift with the arrows and page keys scrolls the description.
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
	case driver.KeyEsc:
		s.leave()
		return
	case driver.KeyTab, driver.KeyArrowLeft, driver.KeyArrowRight:
		s.focus = 1 - s.focus
		return
	case driver.KeyRune:
		if !ev.Ctrl && !ev.Alt {
			switch ev.Ch {
			case 'p', 'g':
				s.focus = 0
				s.buy(false)
				return
			case 'o':
				s.focus = 0
				s.buy(true)
				return
			case 'd', 's':
				s.focus = 1
				s.sell()
				return
			}
		}
	}
	s.focused().key(ev)
}

// box fills the window, less a margin, up to a comfortable width.
func (s *storeScreen) box(area Box) Box {
	w := min(area.W, 120)
	h := area.H
	if area.H > 30 {
		h = area.H - 2
	}
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (s *storeScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	s.hotspots = s.hotspots[:0]
	fillBox(f, b, screenBg)
	title := s.info.Name
	if !s.info.Home && s.info.Owner != "" {
		title += ": " + s.info.Owner
	}
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" "+title+" ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenBorder), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: storeID, Role: accessibility.RoleDialog, Label: title, Bounds: rect(b)})
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}

	// The gold, and what the owner pays at most.
	money := fmt.Sprintf("Gold %d", s.info.Gold)
	if !s.info.Home && !s.info.NoSelling {
		money += fmt.Sprintf("   The owner pays up to %d for one object", s.info.Purse)
	}
	f.TextFit(inner.X+1, inner.Y, inner.W-2, money, screenText)

	// The keys, the buttons, and the messages, from the bottom up.
	buy, one, sell := s.verbs()
	keys := fmt.Sprintf("p %s  o %s  d %s  Tab other list  Shift+↑↓ description  Esc leave", buy, one, sell)
	if s.busy {
		keys = "…"
	}
	bottom := inner.Y + inner.H - 1
	f.TextFit(inner.X+1, bottom, inner.W-2, keys, screenDim)
	s.drawButtons(f, Box{inner.X, bottom - 1, inner.W, 1}, buy, one, sell)
	msgs := s.messages[max(len(s.messages)-storeMessages, 0):]
	for i, msg := range msgs {
		f.TextFit(inner.X+1, bottom-2-len(msgs)+i, inner.W-2, msg, screenWarn)
	}

	// The two lists side by side, and the description under them.
	rest := Box{inner.X, inner.Y + 2, inner.W, inner.H - 2 - 3 - storeMessages}
	detailH := min(max(rest.H/3, 5), 12)
	lists := Box{rest.X, rest.Y, rest.W, rest.H - detailH}
	// What the store sells has the longer names, with prices.
	left := Box{lists.X, lists.Y, lists.W * 3 / 5, lists.H}
	right := Box{lists.X + left.W, lists.Y, lists.W - left.W, lists.H}
	s.stock.focused = s.focus == 0
	s.gear.focused = s.focus == 1
	s.stock.draw(f, left, env)
	s.gear.draw(f, right, env)
	s.drawDetail(f, Box{rest.X + 1, lists.Y + lists.H, rest.W - 2, detailH}, env)
}

// drawDetail draws the description of the object selected in the list
// with the keys.
func (s *storeScreen) drawDetail(f *terminal.Frame, b Box, env *drawEnv) {
	l := s.focused()
	i := l.selected()
	if i < 0 || b.Empty() {
		return
	}
	key := fmt.Sprintf("%d/%d", s.focus, i)
	if key != s.detailFor {
		s.detailFor, s.detailOffset = key, 0
	}
	text := l.items[i].Detail
	rows := styledHeight(text, b.W)
	s.detailHeight = b.H
	s.detailOffset = min(s.detailOffset, max(rows-b.H, 0))
	f.RenderWidget(&styledView{text: text, pal: env.pal, Offset: s.detailOffset}, rect(b))
	f.RenderWidget(wheelArea{scroll: func(d int) {
		s.detailOffset = min(max(s.detailOffset+d*wheelStep, 0), max(rows-b.H, 0))
	}, pointer: env.pointer}, rect(b))
}

// drawButtons draws the buttons at the right of row b.
func (s *storeScreen) drawButtons(f *terminal.Frame, b Box, buy, one, sell string) {
	x := b.X + b.W - 1
	buttons := []struct {
		label string
		do    func()
	}{
		{buy, func() { s.focus = 0; s.buy(false) }},
		{one, func() { s.focus = 0; s.buy(true) }},
		{sell, func() { s.focus = 1; s.sell() }},
		{"Leave", s.leave},
	}
	for i := len(buttons) - 1; i >= 0; i-- {
		bt := buttons[i]
		w := limoni.StringWidth(bt.label) + 4
		x -= w
		at := Box{x, b.Y, w, 1}
		do := func() {
			if !s.busy {
				bt.do()
			}
		}
		f.RenderWidget(widgets.Button{Label: bt.label, Style: screenTitle.WithBg(screenBg.Bg),
			FocusedStyle: screenTitle.WithBg(screenBg.Bg), OnPress: do}, rect(at))
		s.hotspots = append(s.hotspots, hotspot{box: at, do: do})
		f.RegisterClickHandler(rect(at), func(driver.MouseEvent) { do() })
		x--
	}
}

// controls returns the parts a click acts on, as last drawn.
func (s *storeScreen) controls() []hotspot {
	return append(append(append([]hotspot(nil), s.stock.hotspots...), s.gear.hotspots...), s.hotspots...)
}

func (s *storeScreen) click(x, y int) {
	if s.busy {
		return
	}
	for _, h := range s.controls() {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

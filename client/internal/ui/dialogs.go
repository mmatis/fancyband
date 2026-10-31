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

// IDs of the small dialogs in Limoni's accessibility tree, which tests
// use.
const (
	checkID     = "check"
	quantityID  = "quantity"
	directionID = "direction"
)

// promptText is a prompt's text without the key help the game's own
// prompt adds, such as "[y/n]".
func promptText(text string) string {
	text = strings.TrimSpace(text)
	for _, help := range []string{"[y/n]", "[Y/n]", "[y/N]"} {
		text = strings.TrimSpace(strings.TrimSuffix(text, help))
	}
	return text
}

// openCheck answers a yes-or-no prompt with a dialog. Y and N answer
// it, and Escape says no, as in the game.
func (m *Model) openCheck(d proto.RequestData) {
	m.ask(promptText(d.Text), func(yes bool) {
		m.send(proto.NewReply(d.ID, map[string]any{"value": yes}))
	})
}

// ask asks a yes-or-no question in a dialog, and runs answer with the
// answer once the dialog closes. Escape says no.
func (m *Model) ask(text string, answer func(yes bool)) {
	done := func(yes bool) {
		m.pop()
		answer(yes)
	}
	ask := newMenu(checkID, "Are you sure?", []menuEntry{
		{label: "Yes", hint: "y", do: func() { done(true) }},
		{label: "No", hint: "n", do: func() { done(false) }},
	})
	if text == "" {
		text = "Go ahead?"
	}
	ask.intro = proto.PlainText(colorWhite, text)
	ask.cancel = func() { done(false) }
	ask.pick = func(ev driver.KeyEvent) bool {
		if ev.Type != driver.KeyRune || ev.Ctrl || ev.Alt {
			return false
		}
		switch ev.Ch {
		case 'y', 'Y':
			done(true)
			return true
		case 'n', 'N':
			done(false)
			return true
		}
		return false
	}
	ask.width = min(max(40, limoni.StringWidth(text)+6), 72)
	// No is the safe answer, so it starts selected.
	ask.selectItem(1)
	m.push(ask)
}

// quantityDialog asks for a number from 1 to max.
type quantityDialog struct {
	text  string
	max   int
	value int
	// typed is set once a digit replaces the starting value.
	typed bool
	// done answers with the value, or cancels when ok is false.
	done     func(value int, ok bool)
	hotspots []hotspot
}

// openQuantity answers a quantity prompt with a dialog.
func (m *Model) openQuantity(d proto.RequestData) {
	text := promptText(d.Text)
	if text == "" {
		text = "How many?"
	}
	q := &quantityDialog{text: text, max: max(d.Max, 1), value: 1}
	q.done = func(v int, ok bool) {
		m.pop()
		if !ok {
			m.send(proto.NewReply(d.ID, nil))
			return
		}
		m.send(proto.NewReply(d.ID, map[string]any{"value": v}))
	}
	m.push(q)
}

func (q *quantityDialog) set(v int) {
	q.value = min(max(v, 1), q.max)
}

func (q *quantityDialog) key(ev driver.KeyEvent) {
	switch ev.Type {
	case driver.KeyEsc:
		q.done(0, false)
	case driver.KeyEnter:
		q.done(q.value, true)
	case driver.KeyArrowUp, driver.KeyArrowRight:
		q.set(q.value + 1)
	case driver.KeyArrowDown, driver.KeyArrowLeft:
		q.set(q.value - 1)
	case driver.KeyPageUp:
		q.set(q.value + 10)
	case driver.KeyPageDown:
		q.set(q.value - 10)
	case driver.KeyHome:
		q.set(1)
	case driver.KeyEnd:
		q.set(q.max)
	case driver.KeyBackspace:
		q.typed = true
		q.value /= 10
	case driver.KeyRune:
		switch {
		case ev.Ch >= '0' && ev.Ch <= '9':
			v := int(ev.Ch - '0')
			if q.typed {
				v += q.value * 10
			}
			q.typed = true
			q.value = min(v, q.max)
		case ev.Ch == '+':
			q.set(q.value + 1)
		case ev.Ch == '-':
			q.set(q.value - 1)
		case ev.Ch == 'a' || ev.Ch == '*':
			// All of them.
			q.set(q.max)
		}
	}
}

// wheel changes the number by d notches of the mouse wheel, up for more.
func (q *quantityDialog) wheel(d int) { q.set(q.value - d) }

func (q *quantityDialog) box(area Box) Box {
	// Room for the key help on its row.
	w := min(max(52, limoni.StringWidth(q.text)+6), area.W)
	h := min(8, area.H)
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (q *quantityDialog) draw(f *terminal.Frame, b Box, env *drawEnv) {
	q.hotspots = q.hotspots[:0]
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" How many? ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: quantityID, Role: accessibility.RoleDialog, Label: "How many?", Bounds: rect(b)})
	x, w := b.X+2, b.W-4
	f.TextFit(x, b.Y+1, w, q.text, screenText)

	// The number, with buttons for less and more on either side.
	y := b.Y + 3
	shown := fmt.Sprintf(" %d ", q.value)
	if q.value == 0 {
		shown = " _ "
	}
	at := Box{x + 4, y, limoni.StringWidth(shown), 1}
	f.Text(x, y, "[-]", screenTitle)
	q.hotspots = append(q.hotspots, hotspot{box: Box{x, y, 3, 1}, do: func() { q.set(q.value - 1) }})
	f.Text(at.X, y, shown, screenSelected)
	f.RegisterAccessibility(accessibility.Node{Role: accessibility.RoleInput, Label: q.text, Value: fmt.Sprint(q.value), Bounds: rect(at)})
	px := at.X + at.W + 1
	f.Text(px, y, "[+]", screenTitle)
	q.hotspots = append(q.hotspots, hotspot{box: Box{px, y, 3, 1}, do: func() { q.set(q.value + 1) }})
	f.TextFit(px+4, y, b.X+b.W-px-6, fmt.Sprintf("of %d", q.max), screenDim)
	f.RenderWidget(wheelArea{scroll: q.wheel, pointer: env.pointer}, rect(Box{x, y, w, 1}))

	// OK, All, and Cancel at the bottom right.
	bx := b.X + b.W - 2
	by := b.Y + b.H - 3
	for _, bt := range []struct {
		label string
		do    func()
	}{
		{"Cancel", func() { q.done(0, false) }},
		{"All", func() { q.set(q.max) }},
		{"OK", func() { q.done(q.value, true) }},
	} {
		bw := limoni.StringWidth(bt.label) + 4
		bx -= bw
		box := Box{bx, by, bw, 1}
		do := bt.do
		f.RenderWidget(widgets.Button{Label: bt.label, Style: screenTitle.WithBg(screenBg.Bg),
			FocusedStyle: screenTitle.WithBg(screenBg.Bg), OnPress: do}, rect(box))
		q.hotspots = append(q.hotspots, hotspot{box: box, do: do})
		f.RegisterClickHandler(rect(box), func(driver.MouseEvent) { do() })
		bx--
	}
	f.TextFit(x, b.Y+b.H-2, w, "↑↓ change  PgUp/PgDn by 10  Enter OK  Esc cancel", screenDim)
}

func (q *quantityDialog) controls() []hotspot { return q.hotspots }

func (q *quantityDialog) click(x, y int) {
	for _, h := range q.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

// directionDialog asks for a direction, 1 to 9 as on the number pad.
type directionDialog struct {
	// allow5 admits 5, which means here or no direction.
	allow5   bool
	done     func(dir int, ok bool)
	hotspots []hotspot
}

// openDirection answers a direction prompt with a dialog.
func (m *Model) openDirection(d proto.RequestData) {
	p := &directionDialog{allow5: d.AllowNone}
	p.done = func(dir int, ok bool) {
		m.pop()
		if !ok {
			m.send(proto.NewReply(d.ID, nil))
			return
		}
		m.send(proto.NewReply(d.ID, map[string]any{"value": dir}))
	}
	m.push(p)
}

// directionKeys maps the keys that name a direction, beyond the digits:
// the arrows, the keypad's other keys, and the roguelike keys.
var directionKeys = map[driver.KeyType]int{
	driver.KeyArrowUp: 8, driver.KeyArrowDown: 2, driver.KeyArrowLeft: 4, driver.KeyArrowRight: 6,
	driver.KeyHome: 7, driver.KeyPageUp: 9, driver.KeyEnd: 1, driver.KeyPageDown: 3,
}

var directionLetters = map[rune]int{'h': 4, 'j': 2, 'k': 8, 'l': 6, 'y': 7, 'u': 9, 'b': 1, 'n': 3}

func (p *directionDialog) key(ev driver.KeyEvent) {
	if ev.Type == driver.KeyEsc {
		p.done(0, false)
		return
	}
	if dir, ok := directionKeys[ev.Type]; ok {
		p.done(dir, true)
		return
	}
	if ev.Type != driver.KeyRune || ev.Ctrl || ev.Alt {
		return
	}
	switch {
	case ev.Ch >= '1' && ev.Ch <= '9':
		p.pick(int(ev.Ch - '0'))
	case ev.Ch == '.':
		p.pick(5)
	default:
		if dir, ok := directionLetters[ev.Ch]; ok {
			p.done(dir, true)
		}
	}
}

func (p *directionDialog) pick(dir int) {
	if dir == 5 && !p.allow5 {
		return
	}
	p.done(dir, true)
}

func (p *directionDialog) box(area Box) Box {
	w, h := min(38, area.W), min(9, area.H)
	// Near the top, so the map around the character stays in sight.
	return Box{area.X + (area.W-w)/2, area.Y + 1, w, h}
}

// directionArrows are the dialog's cells, by direction.
var directionArrows = [10]string{1: "↙", 2: "↓", 3: "↘", 4: "←", 5: "·", 6: "→", 7: "↖", 8: "↑", 9: "↗"}

func (p *directionDialog) draw(f *terminal.Frame, b Box, _ *drawEnv) {
	p.hotspots = p.hotspots[:0]
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" Which direction? ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: directionID, Role: accessibility.RoleDialog, Label: "Which direction?", Bounds: rect(b)})
	gx := b.X + 3
	for row, dirs := range [3][3]int{{7, 8, 9}, {4, 5, 6}, {1, 2, 3}} {
		for col, dir := range dirs {
			st := screenTitle
			if dir == 5 && !p.allow5 {
				st = screenDim
			}
			at := Box{gx + col*5, b.Y + 2 + row*2, 4, 1}
			f.Text(at.X, at.Y, fmt.Sprintf("%d %s", dir, directionArrows[dir]), st)
			dir := dir
			p.hotspots = append(p.hotspots, hotspot{box: at, do: func() { p.pick(dir) }})
			f.RegisterClickHandler(rect(at), func(driver.MouseEvent) { p.pick(dir) })
			f.RegisterAccessibility(accessibility.Node{Role: accessibility.RoleButton, Label: fmt.Sprint(dir), Bounds: rect(at)})
		}
	}
	tx := gx + 16
	for i, line := range []string{"Digits, arrows,", "or a click.", "", "Esc cancels."} {
		f.TextFit(tx, b.Y+2+i, b.X+b.W-tx-1, line, screenDim)
	}
}

func (p *directionDialog) controls() []hotspot { return p.hotspots }

func (p *directionDialog) click(x, y int) {
	for _, h := range p.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

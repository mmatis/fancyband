package ui

import (
	"unicode"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"
)

// fieldStyle is the background of text being typed, and the cursor is
// drawn in screenSelected.
var fieldStyle = screenText.WithBg(limoni.RGB(44, 44, 64))

// textField is text being typed, with a cursor, such as a character's
// name.
type textField struct {
	text   []rune
	cursor int
	// max is the most characters it takes.
	max int
	// fresh is set while the text is as given, before any key: as in the
	// game's own prompts, typing then replaces it.
	fresh bool
}

func (t *textField) set(s string) {
	t.text = []rune(s)
	if len(t.text) > t.max {
		t.text = t.text[:t.max]
	}
	t.cursor = len(t.text)
	t.fresh = true
}

func (t *textField) String() string { return string(t.text) }

// key edits the text, and returns whether it took the key.
func (t *textField) key(ev driver.KeyEvent) bool {
	if t.fresh {
		switch ev.Type {
		case driver.KeyBackspace, driver.KeyDelete, driver.KeySpace, driver.KeyRune:
			if ev.Type != driver.KeyRune || !ev.Ctrl && !ev.Alt && unicode.IsPrint(ev.Ch) {
				t.text, t.cursor = nil, 0
			}
		}
		t.fresh = false
	}
	switch ev.Type {
	case driver.KeyArrowLeft:
		t.cursor = max(t.cursor-1, 0)
	case driver.KeyArrowRight:
		t.cursor = min(t.cursor+1, len(t.text))
	case driver.KeyHome:
		t.cursor = 0
	case driver.KeyEnd:
		t.cursor = len(t.text)
	case driver.KeyBackspace:
		if t.cursor > 0 {
			t.text = append(t.text[:t.cursor-1], t.text[t.cursor:]...)
			t.cursor--
		}
	case driver.KeyDelete:
		if t.cursor < len(t.text) {
			t.text = append(t.text[:t.cursor], t.text[t.cursor+1:]...)
		}
	case driver.KeySpace:
		t.insert(' ')
	case driver.KeyRune:
		if ev.Ctrl || ev.Alt || !unicode.IsPrint(ev.Ch) {
			return false
		}
		t.insert(ev.Ch)
	default:
		return false
	}
	return true
}

func (t *textField) insert(r rune) {
	if len(t.text) >= t.max {
		return
	}
	t.text = append(t.text[:t.cursor], append([]rune{r}, t.text[t.cursor:]...)...)
	t.cursor++
}

// rows breaks the text into rows no wider than width, at a space where
// one is near enough, and returns where each row starts. The cell after
// the text, where the cursor goes to add more, counts.
func (t *textField) rows(width int) []int {
	starts := []int{0}
	for start := 0; len(t.text)+1-start > width; {
		cut := start + width
		for i := cut; i > start+width/2; i-- {
			if t.text[i-1] == ' ' {
				cut = i
				break
			}
		}
		starts = append(starts, cut)
		start = cut
	}
	return starts
}

// draw draws the text in b, broken into rows of b's width, with the
// cursor shown as a highlighted cell, and names it label for screen
// readers.
func (t *textField) draw(f *terminal.Frame, b Box, label string) {
	if b.Empty() {
		return
	}
	fillBox(f, b, fieldStyle)
	starts := t.rows(b.W)
	row := 0
	for i := 0; i <= len(t.text); i++ {
		for row+1 < len(starts) && i >= starts[row+1] {
			row++
		}
		col := i - starts[row]
		if row >= b.H {
			break
		}
		ch, st := " ", fieldStyle
		if i < len(t.text) {
			ch = string(t.text[i])
			if t.fresh {
				// Typing replaces it, so it shows as chosen.
				st = screenSelected
			}
		}
		if i == t.cursor {
			st = screenSelected
		}
		f.Text(b.X+col, b.Y+row, ch, st)
	}
	f.RegisterAccessibility(accessibility.Node{Role: accessibility.RoleInput, Label: label, Value: t.String(), Bounds: rect(b)})
}

// textDialog asks for text, such as a character's history, with OK and
// Cancel.
type textDialog struct {
	id, title string
	field     textField
	// rows is how many rows the text box has.
	rows     int
	done     func(text string, ok bool)
	hotspots []hotspot
}

func (d *textDialog) key(ev driver.KeyEvent) {
	switch ev.Type {
	case driver.KeyEsc:
		d.done("", false)
	case driver.KeyEnter:
		d.done(d.field.String(), true)
	default:
		d.field.key(ev)
	}
}

func (d *textDialog) box(area Box) Box {
	w := min(64, area.W)
	h := min(d.rows+6, area.H)
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (d *textDialog) draw(f *terminal.Frame, b Box, env *drawEnv) {
	d.hotspots = d.hotspots[:0]
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" "+d.title+" ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: d.id, Role: accessibility.RoleDialog, Label: d.title, Bounds: rect(b)})
	d.field.draw(f, Box{b.X + 2, b.Y + 2, b.W - 4, d.rows}, d.title)
	by := b.Y + b.H - 3
	bx := b.X + b.W - 2
	for _, bt := range []struct {
		label string
		do    func()
	}{
		{"Cancel", func() { d.done("", false) }},
		{"OK", func() { d.done(d.field.String(), true) }},
	} {
		w := len(bt.label) + 4
		bx -= w
		at := Box{bx, by, w, 1}
		do := bt.do
		f.RenderWidget(widgets.Button{Label: bt.label, Style: screenTitle.WithBg(screenBg.Bg),
			FocusedStyle: screenTitle.WithBg(screenBg.Bg), OnPress: do}, rect(at))
		d.hotspots = append(d.hotspots, hotspot{box: at, do: do})
		f.RegisterClickHandler(rect(at), func(driver.MouseEvent) { do() })
		bx--
	}
	f.TextFit(b.X+2, b.Y+b.H-2, b.W-4, "Enter OK  Esc cancel  ←→ Home End move", screenDim)
}

func (d *textDialog) controls() []hotspot { return d.hotspots }

func (d *textDialog) click(x, y int) {
	for _, h := range d.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

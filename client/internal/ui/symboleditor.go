package ui

import (
	"fmt"
	"slices"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// The Tile Editor's Symbol tab, in its column 3: the letter and color an
// entry has when the map draws letters instead of tiles. Every change
// goes to the engine at once with SET_SYMBOL; the editor's Save writes
// them to symbols.prf, which every tile set shares. In text mode an
// effect draws * or a line along its path, so it takes only a color.

// symbolID names the tab's list in Limoni's accessibility tree, for
// tests.
const symbolID = "tile-symbol"

// symbolScreen edits one entry's symbol, in column 3 of the Tile Editor.
type symbolScreen struct {
	ed *tileEditor
	// e is the entry the symbol is for.
	e int

	rows     []*optRow
	state    *widgets.ListState
	hotspots []hotspot
	// listHeight is how many rows the list showed when last drawn.
	listHeight int
}

// newSymbol makes the Symbol tab for entry e.
func (ed *tileEditor) newSymbol(e int) *symbolScreen {
	ss := &symbolScreen{ed: ed, e: e, state: widgets.NewListState()}
	ss.build()
	// An effect's first row is text only.
	ss.state.Select(slices.IndexFunc(ss.rows, func(r *optRow) bool { return r.kind != optInfo }))
	return ss
}

// openSymbol opens the Symbol tab in column 3 on entry e and gives it the
// keys.
func (ed *tileEditor) openSymbol(e int) {
	if ed.symbol == nil || ed.symbol.e != e {
		ed.symbol = ed.newSymbol(e)
	}
	ed.side, ed.focus = sideSymbol, focusSide
}

func (ss *symbolScreen) entry() proto.VisualData { return ss.ed.v.Entries[ss.e] }

// letterRow is the row that takes a typed letter.
func (ss *symbolScreen) letterRow() bool {
	return ss.state.Selected == 0 && ss.entry().Kind != "effect"
}

// build makes the rows for the symbol as it is.
func (ss *symbolScreen) build() {
	en := ss.entry()
	var rows []*optRow
	if en.Kind == "effect" {
		rows = append(rows, &optRow{kind: optInfo, label: "Letter: * or a line along its path"})
	} else {
		rows = append(rows, &optRow{kind: optButton, label: "Letter: " + en.Char + "   type a new one", editable: true,
			desc: "Type any letter, number, or sign to show this as when the map draws letters."})
	}
	colorRow := &optRow{kind: optChoice, label: "Color", editable: true, choices: ss.ed.v.SymbolColors,
		desc: "The letter's color. Click one below, or use Left and Right."}
	if len(colorRow.choices) == 0 {
		colorRow.choices = []string{fmt.Sprintf("Color %d", en.Color)}
	}
	colorRow.choice = min(max(en.Color, 0), len(colorRow.choices)-1)
	colorRow.set = func(r *optRow) { ss.set(ss.entry().Char, r.choice) }
	rows = append(rows, colorRow,
		&optRow{kind: optButton, label: "Put back the game's symbol", editable: true,
			desc: "Gives back the letter and color the game has for this.", press: ss.reset},
		&optRow{kind: optButton, label: "Done", editable: true,
			desc: "Back to the list. Save there keeps your symbols for next time.", press: ss.ed.back})
	ss.rows = rows
}

// set gives the entry letter ch in color c.
func (ss *symbolScreen) set(ch string, c int) {
	ss.ed.setSymbol(ss.e, ch, c)
	sel := ss.state.Selected
	ss.build()
	ss.state.Select(sel)
}

// reset gives back the game's own symbol.
func (ss *symbolScreen) reset() {
	ss.ed.resetSymbol(ss.e)
	sel := ss.state.Selected
	ss.build()
	ss.state.Select(sel)
}

func (ss *symbolScreen) selectedRow() *optRow {
	if ss.state.Selected < 0 || ss.state.Selected >= len(ss.rows) {
		return nil
	}
	return ss.rows[ss.state.Selected]
}

func (ss *symbolScreen) key(ev driver.KeyEvent) {
	n := len(ss.rows)
	// jump selects row i, or the nearest row past it in direction d
	// that is not text only.
	jump := func(i, d int) {
		i = min(max(i, 0), n-1)
		for ; i >= 0 && i < n; i += d {
			if ss.rows[i].kind != optInfo {
				ss.state.Select(i)
				return
			}
		}
	}
	move := func(d int) { jump(((ss.state.Selected+d)%n+n)%n, d) }
	if ev.Type == driver.KeyRune && ss.letterRow() && !ev.Ctrl && !ev.Alt && ev.Ch > ' ' {
		ss.set(string(ev.Ch), ss.entry().Color)
		return
	}
	page := pageRows(ss.listHeight)
	switch ev.Type {
	case driver.KeyEsc:
		ss.ed.back()
	case driver.KeyArrowDown:
		move(1)
	case driver.KeyArrowUp:
		move(-1)
	case driver.KeyPageDown:
		jump(ss.state.Selected+page, 1)
	case driver.KeyPageUp:
		jump(ss.state.Selected-page, -1)
	case driver.KeyHome:
		jump(0, 1)
	case driver.KeyEnd:
		jump(n-1, -1)
	case driver.KeyArrowLeft, driver.KeyArrowRight:
		d := 1
		if ev.Type == driver.KeyArrowLeft {
			d = -1
		}
		if r := ss.selectedRow(); r != nil && r.kind == optChoice {
			r.change(d)
		}
	case driver.KeyEnter, driver.KeySpace:
		if r := ss.selectedRow(); r != nil && !ss.letterRow() {
			r.change(1)
		}
	case driver.KeyRune:
		switch ev.Ch {
		case 'j':
			move(1)
		case 'k':
			move(-1)
		case 'h':
			ss.key(driver.KeyEvent{Type: driver.KeyArrowLeft})
		case 'l':
			ss.key(driver.KeyEvent{Type: driver.KeyArrowRight})
		}
	}
}

// hotspot makes part of the tab clickable; a click gives column 3 the
// keys before it acts.
func (ss *symbolScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	act := func() {
		ss.ed.focus = focusSide
		do()
	}
	ss.hotspots = append(ss.hotspots, hotspot{box: b, do: act})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { act() })
}

// draw draws the rows in b, the colors to click under them, and the
// selected row's description.
func (ss *symbolScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	ss.hotspots = ss.hotspots[:0]
	en := ss.entry()
	foot := "↑↓ move  ←→ color  Esc back"
	if en.Kind != "effect" {
		foot = "Type a letter  ↑↓ move  ←→ color  Esc back"
	}
	f.TextFit(b.X+1, b.Y+b.H-1, b.W-2, foot, screenDim)
	if r := ss.selectedRow(); r != nil {
		f.RenderWidget(&styledView{text: proto.PlainText(colorWhite, r.desc), pal: env.pal},
			rect(Box{b.X + 1, b.Y + b.H - 4, b.W - 2, 2}))
	}

	// The rows, then under them the colors, each a sample of the letter,
	// the current one in brackets.
	names := ss.ed.v.SymbolColors
	per := max((b.W-2)/4, 1)
	gridH := (len(names) + per - 1) / per
	listBox := Box{b.X, b.Y, b.W, max(min(len(ss.rows), b.H-6-gridH), 1)}
	gridY := listBox.Y + listBox.H + 1
	sample := en.Char
	if en.Kind == "effect" || sample == "" {
		sample = "*"
	}
	for i := range names {
		at := Box{b.X + 1 + (i%per)*4, gridY + i/per, 3, 1}
		mark := screenDim.WithBg(screenBg.Bg)
		left, right := " ", " "
		if i == en.Color {
			left, right = "[", "]"
			mark = screenTitle.WithBg(screenBg.Bg)
		}
		f.Text(at.X, at.Y, left, mark)
		f.Text(at.X+1, at.Y, sample, cell.NewStyle().WithFg(env.pal.color(i)).WithBg(limoni.RGB(0, 0, 0)))
		f.Text(at.X+2, at.Y, right, mark)
		c := i
		ss.hotspot(f, at, func() { ss.set(ss.entry().Char, c) })
	}

	labels := make([]string, len(ss.rows))
	for i, r := range ss.rows {
		labels[i] = " " + r.text()
	}
	rowStyle := screenText.WithBg(screenBg.Bg)
	f.RenderWidget(&widgets.List{
		ID: symbolID, Label: "Symbol", Items: labels,
		Style: rowStyle, SelectedStyle: screenSelected, FocusedStyle: rowStyle,
		Scrollbar: true, State: ss.state,
	}, rect(listBox))
	ss.listHeight = listBox.H
	f.RenderWidget(wheelArea{scroll: func(d int) {
		wheelList(ss.state, len(ss.rows), ss.listHeight, d, func(i int) bool { return ss.rows[i].kind != optInfo })
	}, pointer: env.pointer}, rect(listBox))
	for y := 0; y < listBox.H; y++ {
		n := ss.state.Offset + y
		if n >= len(ss.rows) {
			break
		}
		r := ss.rows[n]
		if r.kind == optInfo {
			f.TextFit(listBox.X, listBox.Y+y, listBox.W-1, labels[n], screenDim)
			continue
		}
		ss.hotspot(f, Box{listBox.X, listBox.Y + y, listBox.W, 1}, func() {
			ss.state.Select(n)
			if r.kind == optButton && n != 0 {
				r.change(1)
			}
		})
	}
}

// setSymbol gives entry e letter ch in color c for when the map draws
// letters, both here and in the engine.
func (ed *tileEditor) setSymbol(e int, ch string, c int) {
	en := &ed.v.Entries[e]
	if en.OwnSymbol == nil {
		en.OwnSymbol = &proto.Symbol{Char: en.Char, Color: en.Color}
	}
	en.Char, en.Color = ch, c
	if *en.OwnSymbol == (proto.Symbol{Char: ch, Color: c}) {
		en.OwnSymbol = nil
	}
	args := map[string]any{"kind": en.Kind, "id": en.ID, "color": c}
	if en.Kind != "effect" {
		args["char"] = ch
	}
	ed.dirty = true
	ed.refresh()
	ed.m.command("SET_SYMBOL", args, ed.checkSymbol)
}

// resetSymbol gives entry e back the game's own letter and color.
func (ed *tileEditor) resetSymbol(e int) {
	en := &ed.v.Entries[e]
	if en.OwnSymbol == nil {
		ed.m.notify("This still has the game's own symbol.")
		return
	}
	en.Char, en.Color, en.OwnSymbol = en.OwnSymbol.Char, en.OwnSymbol.Color, nil
	ed.dirty = true
	ed.refresh()
	ed.m.command("SET_SYMBOL", map[string]any{"kind": en.Kind, "id": en.ID, "reset": true}, ed.checkSymbol)
}

// checkSymbol reports a refused symbol and reads the entries again, so
// the editor shows what the engine has.
func (ed *tileEditor) checkSymbol(r proto.EventData) {
	if r.OK {
		return
	}
	ed.m.notify("Cannot change the symbol: " + r.Error)
	ed.reread()
}

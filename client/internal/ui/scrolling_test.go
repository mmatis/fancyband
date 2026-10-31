package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"
	"github.com/thebanri/limoni/widgets"
)

// wheel turns the mouse wheel one notch at x, y: down for d > 0.
func wheel(t *testing.T, term *testkit.Terminal, x, y, d int) {
	t.Helper()
	b := driver.MouseScrollDown
	if d < 0 {
		b = driver.MouseScrollUp
	}
	if !term.Mouse(driver.MouseEvent{X: uint16(x), Y: uint16(y), Button: b}) {
		t.Fatalf("nothing at %d, %d took the wheel", x, y)
	}
}

func TestWheelList(t *testing.T) {
	st := widgets.NewListState()
	st.Select(2)
	// The view moves; the selection stays while it is in view.
	wheelList(st, 50, 10, 1, nil)
	if st.Offset != 3 || st.Selected != 3 {
		t.Errorf("one notch down from the top: offset %d, selected %d; want 3, 3", st.Offset, st.Selected)
	}
	st.Select(8)
	wheelList(st, 50, 10, 1, nil)
	if st.Offset != 6 || st.Selected != 8 {
		t.Errorf("another notch: offset %d, selected %d; want 6, 8", st.Offset, st.Selected)
	}
	// The view stops at the ends.
	for i := 0; i < 20; i++ {
		wheelList(st, 50, 10, 1, nil)
	}
	if st.Offset != 40 || st.Selected != 40 {
		t.Errorf("at the end: offset %d, selected %d; want 40, 40", st.Offset, st.Selected)
	}
	wheelList(st, 50, 10, -1, nil)
	if st.Offset != 37 || st.Selected != 40 {
		t.Errorf("one notch up from the end: offset %d, selected %d; want 37, 40", st.Offset, st.Selected)
	}
	// A selection pushed off the top moves to the nearest row ok
	// allows.
	st.Offset, st.Selected = 0, 0
	wheelList(st, 50, 10, 1, func(i int) bool { return i != 3 })
	if st.Selected != 4 {
		t.Errorf("pushed onto a row it cannot take, the selection went to %d, want 4", st.Selected)
	}
	if pageRows(0) != 10 || pageRows(20) != 19 || pageRows(1) != 1 {
		t.Error("pageRows is not a screenful less one")
	}
}

// TestListScreenScrolls scrolls the shared list screen with the wheel
// and the page keys, and checks a click on a row still selects it.
func TestListScreenScrolls(t *testing.T) {
	var items []listItem
	for i := 0; i < 100; i++ {
		items = append(items, listItem{Label: fmt.Sprintf("item %02d", i)})
	}
	s := newListScreen("test-list", "Test", items)
	s.height = 20
	term := testkit.NewTerminal(60, 30)
	env := &drawEnv{pal: newPalette(nil)}
	draw := func() Box {
		b := s.box(Box{0, 0, 60, 30})
		term.Draw(func(f *terminal.Frame) { s.draw(f, b, env) })
		return b
	}
	b := draw()
	row := b.Y + 2 // the list's second row
	wheel(t, term, b.X+3, row, 1)
	if s.state.Offset != 3 || s.state.Selected != 3 {
		t.Errorf("after a notch down: offset %d, selected %d; want 3, 3", s.state.Offset, s.state.Selected)
	}
	draw()
	wheel(t, term, b.X+3, row, -1)
	if s.state.Offset != 0 || s.state.Selected != 3 {
		t.Errorf("after a notch up: offset %d, selected %d; want 0, 3", s.state.Offset, s.state.Selected)
	}
	s.key(driver.KeyEvent{Type: driver.KeyPageDown})
	if want := 3 + s.listHeight - 1; s.state.Selected != want {
		t.Errorf("Page Down selected %d, want %d, a screenful less one on", s.state.Selected, want)
	}
	s.key(driver.KeyEvent{Type: driver.KeyPageUp})
	s.key(driver.KeyEvent{Type: driver.KeyArrowDown})
	if s.state.Selected != 4 {
		t.Errorf("Page Up then Down selected %d, want 4", s.state.Selected)
	}
	draw()
	term.Click(uint16(b.X+3), uint16(b.Y+1+5))
	if s.state.Selected != 5 {
		t.Errorf("a click on the sixth row selected %d, want 5", s.state.Selected)
	}
}

// TestOptionsScroll scrolls the options screen with the wheel and the
// page keys.
func TestOptionsScroll(t *testing.T) {
	var rows []*optRow
	for i := 0; i < 80; i++ {
		rows = append(rows, &optRow{kind: optCheck, label: fmt.Sprintf("option %02d", i), editable: true})
	}
	s := &optionsScreen{m: New(Config{Width: 100, Height: 30}), tabs: []*optTab{{name: "Test", rows: rows}}, state: widgets.NewListState()}
	s.state.Select(0)
	term := testkit.NewTerminal(100, 30)
	env := &drawEnv{pal: newPalette(nil)}
	b := s.box(Box{0, 0, 100, 30})
	term.Draw(func(f *terminal.Frame) { s.draw(f, b, env) })
	if s.listHeight == 0 {
		t.Fatal("the options screen did not note its list's height")
	}
	wheel(t, term, b.X+5, b.Y+4, 1)
	if s.state.Offset != 3 || s.state.Selected != 3 {
		t.Errorf("after a notch down: offset %d, selected %d; want 3, 3", s.state.Offset, s.state.Selected)
	}
	s.key(driver.KeyEvent{Type: driver.KeyPageDown})
	if want := 3 + s.listHeight - 1; s.state.Selected != want {
		t.Errorf("Page Down selected %d, want %d", s.state.Selected, want)
	}
	s.key(driver.KeyEvent{Type: driver.KeyEnd})
	if s.state.Selected != 79 {
		t.Errorf("End selected %d, want 79", s.state.Selected)
	}
	s.key(driver.KeyEvent{Type: driver.KeyPageUp})
	s.key(driver.KeyEvent{Type: driver.KeyHome})
	if s.state.Selected != 0 {
		t.Errorf("Home selected %d, want 0", s.state.Selected)
	}
}

// TestTileEditorScrolls scrolls each of the editor's lists with the
// wheel: the list and the sheet whichever column has the keys, and the
// filter's settings.
func TestTileEditorScrolls(t *testing.T) {
	m, ed, term := testEditor(t, 1, 120, 30)
	list, _, side := ed.columns(ed.box(Box{0, 0, 120, 30}))
	drawEditor(m, ed, term)

	// The list, with the list's keys.
	// Over the first row, under the tabs and the checkbox.
	first := slices.IndexFunc(strings.Split(term.Snapshot(), "\n"), func(l string) bool {
		r := []rune(l)
		return len(r) >= list.X+list.W && strings.Contains(string(r[list.X:list.X+list.W]), "monster 00")
	})
	wheel(t, term, list.X+5, first, 1)
	if ed.state.Offset != 3 || ed.state.Selected != 3 {
		t.Errorf("the list after a notch down: offset %d, selected %d; want 3, 3", ed.state.Offset, ed.state.Selected)
	}
	drawEditor(m, ed, term)

	// The sheet, while the list keeps the keys.
	p := ed.picker
	if p == nil || p.e != ed.rows[3] {
		t.Fatal("column 3 does not follow the selected row")
	}
	top := p.top
	wheel(t, term, side.X+4, side.Y+4, 1)
	if p.top != top+3 || p.row < p.top {
		t.Errorf("the sheet after a notch down: top %d, cursor row %d; want top %d with the cursor in view", p.top, p.row, top+3)
	}
	if ed.focus != focusList {
		t.Error("the wheel over the sheet took the keys from the list")
	}
	p.key(driver.KeyEvent{Type: driver.KeyPageDown})
	if p.row < p.top+p.down-2 {
		t.Errorf("Page Down on the sheet left the cursor at row %d, top %d of %d", p.row, p.top, p.down)
	}

	// The filter's settings, with animation on, so a row of text sits
	// among them.
	ed.key(driver.KeyEvent{Type: driver.KeyRune, Ch: 'f'})
	fs := ed.filter
	for _, r := range fs.rows {
		if r.label == "Animation" {
			r.change(1)
		}
	}
	drawEditor(m, ed, term)
	for i := 0; i < 10; i++ {
		wheel(t, term, side.X+4, side.Y+5, 1)
		drawEditor(m, ed, term)
		if r := fs.selectedRow(); r == nil || r.kind == optInfo {
			t.Fatalf("after %d notches the filter's selection is on a row of text", i+1)
		}
	}
	if fs.state.Offset == 0 {
		t.Error("the wheel did not scroll the filter's settings")
	}
	fs.key(driver.KeyEvent{Type: driver.KeyHome})
	if fs.state.Selected != 0 {
		t.Errorf("Home selected %d, want 0", fs.state.Selected)
	}
	fs.key(driver.KeyEvent{Type: driver.KeyPageDown})
	if r := fs.selectedRow(); fs.state.Selected == 0 || r == nil || r.kind == optInfo {
		t.Errorf("Page Down selected row %d of the filter", fs.state.Selected)
	}
}

// findRune returns where r first shows on term.
func findRune(t *testing.T, term *testkit.Terminal, r rune) (int, int) {
	t.Helper()
	for y, line := range strings.Split(term.Snapshot(), "\n") {
		if x := slices.Index([]rune(line), r); x >= 0 {
			return x, y
		}
	}
	t.Fatalf("no %q on the screen:\n%s", r, term.Snapshot())
	return 0, 0
}

// TestSheetScrollBars scrolls a sheet too big for column 3 with its two
// scroll bars: their arrows, their tracks, and their thumbs dragged; and
// sideways with Shift and the wheel.
func TestSheetScrollBars(t *testing.T) {
	m, ed, term := testEditor(t, 1, 120, 40)
	drawEditor(m, ed, term)
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	drawEditor(m, ed, term)
	p := ed.picker
	cols, rows := p.size()
	if p.across >= cols || p.down >= rows {
		t.Fatalf("the sheet fits: %dx%d of %dx%d tiles", p.across, p.down, cols, rows)
	}
	click := func(r rune) {
		t.Helper()
		x, y := findRune(t, term, r)
		if !term.Click(uint16(x), uint16(y)) {
			t.Fatalf("nothing took a click on %q", r)
		}
		drawEditor(m, ed, term)
	}

	// Sideways: an arrow moves one column, the track a page, and the
	// thumb dragged to the end shows the last column.
	click('▶')
	if p.left != 1 || p.col != 1 {
		t.Errorf("after ▶: left %d, cursor column %d", p.left, p.col)
	}
	rx, ry := findRune(t, term, '▶')
	term.Click(uint16(rx-1), uint16(ry))
	drawEditor(m, ed, term)
	if want := min(1+p.across-1, cols-p.across); p.left != want {
		t.Errorf("a click on the track right of the thumb: left %d, want %d", p.left, want)
	}
	click('◀')
	lx, _ := findRune(t, term, '◀')
	thumb := slices.Index([]rune(strings.Split(term.Snapshot(), "\n")[ry])[lx:], '█') + lx
	term.Click(uint16(thumb), uint16(ry))
	term.Drag(uint16(rx-1), uint16(ry))
	drawEditor(m, ed, term)
	if p.left != cols-p.across {
		t.Errorf("the thumb dragged to the end: left %d, want %d", p.left, cols-p.across)
	}
	if p.col < p.left {
		t.Errorf("the cursor, at column %d, was left out of view from %d", p.col, p.left)
	}

	// Down and up.
	click('▼')
	if p.top != 1 {
		t.Errorf("after ▼: top %d", p.top)
	}
	click('▲')
	if p.top != 0 {
		t.Errorf("after ▲: top %d", p.top)
	}

	// Shift and the wheel over the sheet go sideways.
	sx, sy := p.hotspots[len(p.hotspots)-1].box.X, p.hotspots[len(p.hotspots)-1].box.Y
	term.Mouse(driver.MouseEvent{Button: driver.MouseScrollUp, Shift: true, X: uint16(sx), Y: uint16(sy)})
	if p.left != cols-p.across-wheelStep {
		t.Errorf("Shift and the wheel up: left %d, want %d", p.left, cols-p.across-wheelStep)
	}
}

// TestSheetZoom zooms the sheet by key, by Ctrl and the wheel, and by the
// panel's buttons, and back to the map's own size, and remembers it.
func TestSheetZoom(t *testing.T) {
	m, ed, term := testEditor(t, 1, 160, 50)
	drawEditor(m, ed, term)
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	drawEditor(m, ed, term)
	p := ed.picker
	if p.cw != 2 || p.ch != 1 || !strings.Contains(term.Snapshot(), "Zoom: size 1, as the map") {
		t.Fatalf("at first a tile takes %dx%d cells:\n%s", p.cw, p.ch, term.Snapshot())
	}
	ed.key(runeKey('+'))
	drawEditor(m, ed, term)
	if p.cw != 4 || p.ch != 2 || m.settings.PickerZoom != 2 || !strings.Contains(term.Snapshot(), "Zoom: size 2 ") {
		t.Errorf("after +: %dx%d cells, zoom %d", p.cw, p.ch, m.settings.PickerZoom)
	}
	ed.key(runeKey('-'))
	ed.key(runeKey('-'))
	drawEditor(m, ed, term)
	if m.settings.PickerZoom != 1 || p.cw != 2 {
		t.Errorf("- past the smallest: zoom %d, %d cells wide", m.settings.PickerZoom, p.cw)
	}
	sx, sy := p.hotspots[len(p.hotspots)-1].box.X, p.hotspots[len(p.hotspots)-1].box.Y
	term.Mouse(driver.MouseEvent{Button: driver.MouseScrollUp, Ctrl: true, X: uint16(sx), Y: uint16(sy)})
	drawEditor(m, ed, term)
	if m.settings.PickerZoom != 2 {
		t.Errorf("Ctrl and the wheel up: zoom %d", m.settings.PickerZoom)
	}
	clickText(t, term, "[+]")
	drawEditor(m, ed, term)
	if m.settings.PickerZoom != 3 || p.cw != 6 || p.ch != 3 {
		t.Errorf("after [+]: zoom %d, %dx%d cells", m.settings.PickerZoom, p.cw, p.ch)
	}
	ed.key(runeKey('0'))
	drawEditor(m, ed, term)
	if m.settings.PickerZoom != 0 || p.cw != 2 {
		t.Errorf("0: zoom %d, %d cells wide", m.settings.PickerZoom, p.cw)
	}
}

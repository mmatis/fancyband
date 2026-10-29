package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/uitest"

	"fancyband/internal/proto"
)

func TestLaunchArgs(t *testing.T) {
	base := []string{"-duser=U", "-utester", "-n"}
	cases := []struct {
		l            Launch
		args, module []string
	}{
		{Launch{}, []string{"-duser=U", "-utester"}, nil},
		{Launch{Name: "Bob"}, []string{"-duser=U", "-uBob"}, nil},
		{Launch{New: true}, []string{"-duser=U", "-n"}, []string{"--new-save"}},
		{Launch{Menu: true}, []string{"-duser=U", "-utester"}, nil},
	}
	for _, c := range cases {
		args, module := c.l.Args(base)
		if !slices.Equal(args, c.args) || !slices.Equal(module, c.module) {
			t.Errorf("%+v.Args = %q %q, want %q %q", c.l, args, module, c.args, c.module)
		}
	}
	if !hasNew(base) || hasNew(base[:2]) || !hasName(base) || hasName([]string{"-u"}) {
		t.Error("hasNew or hasName is wrong")
	}
}

// rowsText joins wrapped rows as plain text, one row per line.
func rowsText(rows [][]styledRun) string {
	var lines []string
	for _, row := range rows {
		var b strings.Builder
		for _, r := range row {
			b.WriteString(r.text)
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

func TestWrapStyled(t *testing.T) {
	text := proto.StyledText{
		{{Text: "It can ", Color: 1}, {Text: "breathe fire", Color: 3}, {Text: " and bite.", Color: 1}},
		{{Text: "  Indented", Color: 1}},
		{},
		{{Text: "abcdefghijkl", Color: 2}},
	}
	rows := wrapStyled(text, 10)
	want := "It can\nbreathe\nfire and\nbite.\n  Indented\n\nabcdefghij\nkl"
	if got := rowsText(rows); got != want {
		t.Errorf("wrapped:\n%s\nwant:\n%s", got, want)
	}
	// Colors survive the wrap: "fire" stays in color 3.
	if r := rows[2][0]; r.text != "fire" || r.color != 3 {
		t.Errorf("row 2 starts %+v, want fire in color 3", r)
	}
	if styledHeight(text, 10) != 8 {
		t.Errorf("height %d, want 8", styledHeight(text, 10))
	}
}

func TestListScreenKeys(t *testing.T) {
	var chose, acted []int
	cancelled := false
	s := newListScreen("test-list", "Things", []listItem{
		{Label: "Apple"}, {Label: "Banana"}, {Label: "Cherry"}, {Label: "Blueberry"},
	})
	s.searchable = true
	s.choose = func(i int) { chose = append(chose, i) }
	s.cancel = func() { cancelled = true }
	s.keys = []listKey{{Key: 'd', Label: "drop", Do: func(i int) { acted = append(acted, i) }}}

	press := func(keys ...any) {
		for _, k := range keys {
			switch k := k.(type) {
			case rune:
				s.key(driver.KeyEvent{Type: driver.KeyRune, Ch: k})
			case driver.KeyType:
				s.key(driver.KeyEvent{Type: k})
			}
		}
	}
	press(driver.KeyArrowDown, driver.KeyArrowDown, driver.KeyEnter)
	press(driver.KeyArrowUp, 'd')
	if !slices.Equal(chose, []int{2}) || !slices.Equal(acted, []int{1}) {
		t.Fatalf("chose %v, acted %v", chose, acted)
	}

	// Searching for "b" leaves Banana and Blueberry; Up wraps to the last.
	press('/', 'B', driver.KeyEnter, driver.KeyArrowUp, driver.KeyEnter)
	if !slices.Equal(chose, []int{2, 3}) {
		t.Fatalf("after the search, chose %v", chose)
	}
	if shown := s.shown(); !slices.Equal(shown, []int{1, 3}) {
		t.Errorf("shown %v", shown)
	}
	// Escape clears the search first, then cancels.
	press(driver.KeyEsc)
	if cancelled || len(s.shown()) != 4 {
		t.Fatal("the first Escape should clear the search")
	}
	press(driver.KeyEsc)
	if !cancelled {
		t.Error("the second Escape did not cancel")
	}
}

func TestListScreenDraw(t *testing.T) {
	s := newListScreen("test-list", "Things", []listItem{
		{Label: "Apple", Icon: '!', IconColor: 4, Hint: "2 lb", Detail: proto.PlainText(1, "A red fruit.")},
		{Label: "Banana", Detail: proto.PlainText(1, "A yellow fruit.")},
	})
	s.detail = true
	s.cancel = func() {}
	pal := newPalette(nil)
	page := uitest.Run(t, 80, 24, func(f *terminal.Frame, ev *driver.Event) bool {
		if ev != nil && ev.Type == driver.EventKey {
			s.key(ev.Key)
		}
		area := Box{0, 0, 80, 24}
		s.draw(f, s.box(area), &drawEnv{pal: pal})
		return true
	})
	page.Expect(page.GetByID("test-list")).ToHaveValue(" ! Apple")
	// Only the selected row is highlighted, even with the list focused.
	page.Expect(page.GetByID("test-list")).ToBeFocused()
	highlight := cell.Style{Bg: screenSelected.Bg}
	page.Expect(page.GetByRole("list-item", " ! Apple")).ToHaveStyle(highlight)
	page.Expect(page.GetByRole("list-item", " Banana")).Not().ToHaveStyle(highlight)
	screen := page.Screen()
	for _, want := range []string{"Things", "A red fruit.", "2 lb", "Esc back"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	page.Press("down")
	page.Expect(page.GetByID("test-list")).ToHaveValue(" Banana")
	if !strings.Contains(page.Screen(), "A yellow fruit.") {
		t.Errorf("the detail panel did not follow the selection:\n%s", page.Screen())
	}
}

// TestListScreenClicks checks the mouse: one click chooses a menu entry;
// in a list to look through, one click selects, a second chooses, and
// the buttons act on the selection.
func TestListScreenClicks(t *testing.T) {
	var chose, loaded []int
	cancelled := false
	menu := newListScreen("menu", "Menu", []listItem{{Label: "Resume"}, {Label: "Quit"}})
	menu.clickChooses = true
	menu.choose = func(i int) { chose = append(chose, i) }

	list := newListScreen("saves", "Saves", []listItem{{Label: "Ann"}, {Label: "Bob"}})
	list.choose = func(i int) { loaded = append(loaded, i) }
	list.buttons = []listButton{
		{Label: "Load", Do: func(i int) { loaded = append(loaded, i) }},
		{Label: "Cancel", Do: func(int) { cancelled = true }},
	}

	var top *listScreen = menu
	pal := newPalette(nil)
	page := uitest.Run(t, 80, 24, func(f *terminal.Frame, ev *driver.Event) bool {
		top.draw(f, top.box(Box{0, 0, 80, 24}), &drawEnv{pal: pal})
		return true
	})
	page.GetByRole("list-item", " Quit").Click()
	if !slices.Equal(chose, []int{1}) {
		t.Fatalf("a click on Quit chose %v", chose)
	}

	top = list
	page.Press("f1") // any event redraws
	page.GetByRole("list-item", " Bob").Click()
	if len(loaded) != 0 || list.selected() != 1 {
		t.Fatalf("one click: loaded %v, selected %d; want Bob selected only", loaded, list.selected())
	}
	page.GetByRole("list-item", " Bob").Click()
	if !slices.Equal(loaded, []int{1}) {
		t.Fatalf("a double click loaded %v, want Bob", loaded)
	}
	page.GetByRole("list-item", " Ann").Click()
	page.GetByRole("button", "Load").Click()
	if !slices.Equal(loaded, []int{1, 0}) {
		t.Errorf("Load loaded %v, want Bob then Ann", loaded)
	}
	page.GetByRole("button", "Cancel").Click()
	if !cancelled {
		t.Error("Cancel did nothing")
	}
}

// TestMenuClickThroughRegions clicks a menu the way the real program
// routes clicks: through the click regions the model's View registered,
// before Update sees them.
func TestMenuClickThroughRegions(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	hit := false
	m.push(newMenu("t-menu", "Test", []menuEntry{{label: "Stay", do: func() {}}, {label: "Go", do: func() { hit = true }}}))
	page := uitest.Run(t, 80, 24, func(f *terminal.Frame, ev *driver.Event) bool {
		m.View(f)
		return true
	})
	page.GetByRole("list-item", " Go").Click()
	if !hit {
		t.Error("clicking Go did not choose it")
	}
}

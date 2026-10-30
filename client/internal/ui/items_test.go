package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// gone waits until nothing with id is on screen.
func (p *player) gone(id string) {
	p.t.Helper()
	deadline := time.Now().Add(playWait)
	for p.page.GetByID(id).Count() > 0 {
		if time.Now().After(deadline) {
			p.t.Fatalf("%s still open:\n%s", id, p.page.Screen())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// open presses key until the screen with id opens. The game drops keys
// typed while it is busy, as after a potion's effect.
func (p *player) open(key, id string) {
	p.t.Helper()
	deadline := time.Now().Add(playWait)
	for {
		p.page.Press(key)
		wait := time.Now().Add(time.Second)
		for time.Now().Before(wait) {
			if p.page.GetByID(id).Count() > 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("%s never opened:\n%s", id, p.page.Screen())
		}
	}
}

// letterOf returns the letter a list row shows before name, as "c) name".
func letterOf(t *testing.T, screen, name string) string {
	t.Helper()
	m := regexp.MustCompile(`([a-z0-9])\) [^\n│]*` + regexp.QuoteMeta(name)).FindStringSubmatch(screen)
	if m == nil {
		t.Fatalf("no row for %q:\n%s", name, screen)
	}
	return m[1]
}

// TestInventoryPlay plays through the native inventory and prompts:
// open the pack, read a description, quaff a potion from its actions,
// look at the equipment and quiver, and open and cancel an item prompt,
// a direction prompt, and a yes-or-no prompt.
func TestInventoryPlay(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, _ := startPlayer(t, 110, 32, false, d.Args("-n"))
	p.createCharacter()

	p.page.Press("i")
	p.page.Expect(p.page.GetByID(inventoryID)).ToBeVisible()
	p.waitText("Burden")
	// The first row's description, once RECALL answers.
	p.waitText("When eaten")
	letter := letterOf(t, p.page.Screen(), "Berserk Strength")
	p.page.Press(letter)
	p.page.Expect(p.page.GetByID(actionsID)).ToBeVisible()
	p.waitText("Quaff")
	p.page.Press("q")
	p.gone(inventoryID)
	p.waitFor("the town", inTownScreen)

	p.open("i", inventoryID)
	p.waitText("Burden")
	// The game's message line still names the potions it used up.
	if strings.Contains(p.page.Screen(), "a Potion of Berserk Strength") {
		t.Fatalf("the potion is still in the pack:\n%s", p.page.Screen())
	}
	p.page.Press("/")
	p.waitText("Dagger")
	p.waitText("weapon")
	p.page.Press("|")
	p.page.Expect(p.page.GetByID(inventoryID)).ToBeVisible()
	p.page.Press("escape")
	p.gone(inventoryID)

	p.open("E", itemPromptID)
	p.waitText("Ration")
	p.page.Press("escape")
	p.gone(itemPromptID)

	p.open("+", directionID)
	p.page.Press("escape")
	p.gone(directionID)

	p.open("Q", checkID)
	p.waitText("Do you really want to retire?")
	p.page.Press("n")
	p.gone(checkID)
	p.waitFor("the town", inTownScreen)
}

// TestQuantityKeys checks the quantity dialog's keys: digits type a
// number, the arrows and page keys change it within its range, and
// Enter and Escape answer.
func TestQuantityKeys(t *testing.T) {
	var got int
	answered, ok := false, false
	q := &quantityDialog{text: "Drop how many?", max: 25, value: 1}
	q.done = func(v int, k bool) { got, answered, ok = v, true, k }
	keys := func(evs ...driver.KeyEvent) {
		for _, ev := range evs {
			q.key(ev)
		}
	}
	r := func(c rune) driver.KeyEvent { return driver.KeyEvent{Type: driver.KeyRune, Ch: c} }
	k := func(t driver.KeyType) driver.KeyEvent { return driver.KeyEvent{Type: t} }

	keys(r('1'), r('2'))
	if q.value != 12 {
		t.Errorf("typed 12, value %d", q.value)
	}
	keys(r('9'))
	if q.value != 25 {
		t.Errorf("typed 129 with a maximum of 25: %d", q.value)
	}
	keys(k(driver.KeyPageDown), k(driver.KeyPageDown), k(driver.KeyPageDown))
	if q.value != 1 {
		t.Errorf("page down past the start: %d", q.value)
	}
	keys(k(driver.KeyArrowUp), k(driver.KeyArrowUp), k(driver.KeyPageUp))
	if q.value != 13 {
		t.Errorf("up, up, page up from 1: %d", q.value)
	}
	keys(k(driver.KeyEnd))
	if q.value != 25 {
		t.Errorf("End: %d", q.value)
	}
	keys(k(driver.KeyEnter))
	if !answered || !ok || got != 25 {
		t.Errorf("Enter answered %v %v %d", answered, ok, got)
	}
	answered = false
	keys(k(driver.KeyEsc))
	if !answered || ok {
		t.Errorf("Escape answered %v %v", answered, ok)
	}

	// The wheel over the number changes it too.
	q.value = 5
	term := testkit.NewTerminal(60, 20)
	env := &drawEnv{pal: newPalette(nil)}
	b := q.box(Box{0, 0, 60, 20})
	term.Draw(func(f *terminal.Frame) { q.draw(f, b, env) })
	wheel(t, term, b.X+6, b.Y+3, -1)
	if q.value != 6 {
		t.Errorf("a notch up the wheel from 5: %d", q.value)
	}
}

// TestDirectionKeys checks the direction dialog's keys: digits, the
// arrows and the keypad's keys, and the roguelike letters, with 5 only
// where the prompt allows it.
func TestDirectionKeys(t *testing.T) {
	for _, c := range []struct {
		ev     driver.KeyEvent
		allow5 bool
		want   int
	}{
		{driver.KeyEvent{Type: driver.KeyRune, Ch: '7'}, false, 7},
		{driver.KeyEvent{Type: driver.KeyArrowLeft}, false, 4},
		{driver.KeyEvent{Type: driver.KeyPageDown}, false, 3},
		{driver.KeyEvent{Type: driver.KeyRune, Ch: 'k'}, false, 8},
		{driver.KeyEvent{Type: driver.KeyRune, Ch: '5'}, false, -1},
		{driver.KeyEvent{Type: driver.KeyRune, Ch: '5'}, true, 5},
		{driver.KeyEvent{Type: driver.KeyEsc}, false, 0},
	} {
		got := -1
		p := &directionDialog{allow5: c.allow5, done: func(dir int, ok bool) {
			got = dir
			if !ok {
				got = 0
			}
		}}
		p.key(c.ev)
		if got != c.want {
			t.Errorf("%+v with allow5 %v: %d, want %d", c.ev, c.allow5, got, c.want)
		}
	}
}

// TestDetailScrolls scrolls a long description in the detail panel with
// the wheel and with Shift and the page keys, and starts it again at
// the top for another row.
func TestDetailScrolls(t *testing.T) {
	var long proto.StyledText
	for i := 0; i < 60; i++ {
		long = append(long, []proto.Span{{Text: fmt.Sprintf("line %02d", i), Color: colorWhite}})
	}
	s := newListScreen("test-list", "Test", []listItem{{Label: "one", Detail: long}, {Label: "two", Detail: long}})
	s.detail = true
	s.width, s.height = 80, 20
	term := testkit.NewTerminal(80, 20)
	env := &drawEnv{pal: newPalette(nil)}
	draw := func() Box {
		b := s.box(Box{0, 0, 80, 20})
		term.Draw(func(f *terminal.Frame) { s.draw(f, b, env) })
		return b
	}
	b := draw()
	wheel(t, term, b.X+b.W-10, b.Y+3, 1)
	if s.detailOffset != wheelStep {
		t.Errorf("a notch of the wheel over the detail: offset %d", s.detailOffset)
	}
	if s.state.Selected != 0 {
		t.Errorf("the wheel over the detail moved the list to %d", s.state.Selected)
	}
	s.key(driver.KeyEvent{Type: driver.KeyPageDown, Shift: true})
	draw()
	if s.detailOffset != wheelStep+pageRows(s.detailHeight) {
		t.Errorf("Shift+Page Down: offset %d", s.detailOffset)
	}
	s.key(driver.KeyEvent{Type: driver.KeyArrowDown})
	draw()
	if s.state.Selected != 1 || s.detailOffset != 0 {
		t.Errorf("another row: selected %d, offset %d", s.state.Selected, s.detailOffset)
	}
}

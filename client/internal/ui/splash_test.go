package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"
)

// drawStack draws the model's open screens, bottom first, as View does.
func drawStack(m *Model, term *testkit.Terminal) {
	area := term.Area()
	b := Box{0, 0, int(area.Width), int(area.Height)}
	env := &drawEnv{pal: newPalette(nil)}
	term.Draw(func(f *terminal.Frame) {
		for _, s := range m.stack {
			s.draw(f, s.box(b), env)
		}
	})
}

// TestBlockLetters checks the title's size: six rows, all as wide, and
// narrow enough for the smallest window.
func TestBlockLetters(t *testing.T) {
	if len(splashTitle) != 6 {
		t.Fatalf("%d rows, want 6", len(splashTitle))
	}
	w := len([]rune(splashTitle[0]))
	for i, row := range splashTitle {
		if n := len([]rune(row)); n != w {
			t.Errorf("row %d is %d wide, row 0 %d", i, n, w)
		}
	}
	if w > MinWidth-2 || w < 60 {
		t.Errorf("the title is %d wide; it must fit in %d columns", w, MinWidth)
	}
	for r := range blockGlyphs {
		if !strings.ContainsRune("FANCYBD", r) {
			t.Errorf("unused glyph %q", r)
		}
	}
}

// TestFirePalette checks the palette runs from black to white, getting
// no more than a shade darker on the way, as the classic palette does in
// two places.
func TestFirePalette(t *testing.T) {
	if firePalette[0] != [3]uint8{0, 0, 0} || firePalette[maxHeat] != [3]uint8{255, 255, 255} {
		t.Errorf("palette runs from %v to %v, want black to white", firePalette[0], firePalette[maxHeat])
	}
	light := func(c [3]uint8) int { return 3*int(c[0]) + 6*int(c[1]) + int(c[2]) }
	for i := 1; i <= maxHeat; i++ {
		if light(firePalette[i])+60 < light(firePalette[i-1]) {
			t.Errorf("heat %d is darker than heat %d", i, i-1)
		}
	}
}

// TestFireStill checks the fire's still frame is always the same, burns
// in the bottom rows and not as a solid wall at the top, and that a step
// changes it.
func TestFireStill(t *testing.T) {
	a, b := newFire(77, 12), newFire(77, 12)
	if !slices.Equal(a.heat, b.heat) {
		t.Fatal("two fires' still frames differ")
	}
	rowHeat := func(f *fire, y int) (sum, lit int) {
		for x := 0; x < f.w; x++ {
			sum += f.at(x, y)
			if f.at(x, y) > 0 {
				lit++
			}
		}
		return
	}
	bottom, _ := rowHeat(a, a.h-1)
	top, lit := rowHeat(a, 0)
	if bottom <= top*3 || lit > a.w*3/4 {
		t.Errorf("bottom row heat %d, top row %d with %d of %d pixels lit", bottom, top, lit, a.w)
	}
	a.step()
	if slices.Equal(a.heat, b.heat) {
		t.Error("a step left the fire as it was")
	}
}

// TestSplashScreen draws the splash screen and start menu at the
// smallest window and a large one: the title shows, the menu sits under
// the title, and the quote shows when there is room. With Animations off
// ticks leave the fire still.
func TestSplashScreen(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 40}} {
		m := New(Config{Width: size[0], Height: size[1]})
		m.openStartMenu()
		if len(m.stack) != 2 {
			t.Fatalf("%d screens open, want the splash screen and the menu", len(m.stack))
		}
		term := testkit.NewTerminal(uint16(size[0]), uint16(size[1]))
		drawStack(m, term)
		snap := term.Snapshot()
		lines := strings.Split(snap, "\n")
		title := -1
		for i, l := range lines {
			if strings.Contains(l, "███████╗ █████╗ ███╗") {
				title = i
			}
		}
		menuTop := -1
		for i, l := range lines {
			if strings.Contains(l, "Fancyband") && strings.Contains(l, "╭") {
				menuTop = i
			}
		}
		if title < 0 || menuTop <= title+5 {
			t.Errorf("%dx%d: title at row %d, menu at %d:\n%s", size[0], size[1], title, menuTop, snap)
		}
		if quote := strings.Contains(snap, "Door of Night"); quote != (size[1] >= 40) {
			t.Errorf("%dx%d: quote shown %v:\n%s", size[0], size[1], quote, snap)
		}
		if !strings.Contains(snap, "▀") {
			t.Errorf("%dx%d: no fire:\n%s", size[0], size[1], snap)
		}

		// Animations off: a tick neither steps nor redraws.
		splash := m.stack[0].(*splashScreen)
		before := slices.Clone(splash.fire.heat)
		m.update(animTick{})
		if !slices.Equal(before, splash.fire.heat) {
			t.Errorf("%dx%d: the fire moved with Animations off", size[0], size[1])
		}
		m.cfg.Animate = true
		if r := m.update(animTick{}); !r.Redraw || slices.Equal(before, splash.fire.heat) {
			t.Errorf("%dx%d: a tick with Animations on did not move the fire", size[0], size[1])
		}
	}
}

// TestSplashClassic checks the switch: with the classic splash screen,
// the start menu opens alone over the game's own.
func TestSplashClassic(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	m.settings.Classic = []string{splashName}
	m.openStartMenu()
	if len(m.stack) != 1 {
		t.Fatalf("%d screens open, want only the menu", len(m.stack))
	}
	if slices.Contains(m.Caps(), "screen:splash") {
		t.Error("the splash screen claims a capability")
	}
}

// TestSplashIntro plays the intro on a clock the test sets: at first the
// title is above the window, the menu below its place, and the quote
// black; at the end each is where the still screen has it. Ticks run
// until the intro ends.
func TestSplashIntro(t *testing.T) {
	now := time.Unix(1000, 0)
	still := New(Config{Width: 100, Height: 40})
	still.openStartMenu()
	m := New(Config{Width: 100, Height: 40, Animate: true})
	m.now = func() time.Time { return now }
	m.openStartMenu()
	if !m.startIntro {
		t.Fatal("the intro asked for no ticks")
	}
	snap := func(m *Model) string {
		term := testkit.NewTerminal(100, 40)
		term.Draw(func(f *terminal.Frame) { m.View(f) })
		return term.Snapshot()
	}
	menuY := func(s string) int {
		for y, line := range strings.Split(s, "\n") {
			if strings.Contains(line, "New character") {
				return y
			}
		}
		return -1
	}
	want := snap(still)
	first := snap(m)
	if strings.Contains(first, "███████╗ █████╗") {
		t.Errorf("the title is in view on the first frame:\n%s", first)
	}
	if y := menuY(first); y >= 0 && y <= menuY(want) {
		t.Errorf("the menu starts at row %d, want below %d", y, menuY(want))
	}
	splash := m.stack[0].(*splashScreen)
	if v := fadeIn([3]uint8{0x8c, 0x9a, 0xb0}, splash.progress(introFadeStart, introEnd)); v.Fg != limoni.RGB(0, 0, 0) {
		t.Errorf("the quote starts in %v, want black", v.Fg)
	}
	if r := m.update(introTick{}); !r.Redraw || len(r.Commands) == 0 {
		t.Error("a tick during the intro asked for no redraw or no next tick")
	}
	// Halfway, the title is on its way down.
	now = now.Add(introTitleEnd / 2)
	if mid := snap(m); !strings.Contains(mid, "███████╗ █████╗") && !strings.Contains(mid, "╚═╝     ╚═╝") {
		t.Errorf("halfway, no part of the title shows:\n%s", mid)
	}
	now = now.Add(introEnd)
	if got := snap(m); got != want {
		t.Errorf("after the intro the screen differs from the still one:\n%s\nwant:\n%s", got, want)
	}
	if r := m.update(introTick{}); len(r.Commands) != 0 {
		t.Error("ticks go on after the intro")
	}
}

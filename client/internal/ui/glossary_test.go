package ui

import (
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// testGlossary is a few terms as GLOSSARY gives them.
func testGlossary() *glossary {
	term := func(id, title, text string, names ...proto.GlossaryName) proto.GlossaryTerm {
		return proto.GlossaryTerm{ID: id, Kind: strings.Split(id, ":")[0], Title: title, Text: proto.PlainText(colorWhite, text), Names: names}
	}
	run := func(s string) proto.GlossaryName { return proto.GlossaryName{Name: s} }
	label := func(s string) proto.GlossaryName { return proto.GlossaryName{Name: s, Label: true} }
	long := strings.Repeat("Hold life keeps your experience. ", 40)
	terms := []proto.GlossaryTerm{
		term("ability:HOLD_LIFE", "Hold Life", long, run("Hold Life"), run("hold life"), run("Sustains your life force")),
		term("race:Elf", "Elf", "Elves are fair.", run("Elf")),
		term("race:Half-Elf", "Half-Elf", "Half-elves are half fair.", run("Half-Elf")),
		term("ability:RES_FIRE", "Fire Resistance", "You resist fire.", run("Fire Resistance")),
		term("ability:RES_COLD", "Cold Resistance", "You resist cold.", run("Cold Resistance")),
		term("ability:SUST_STR", "Sustain Strength", "Your strength is sustained.", run("Sustain Strength")),
		term("element:FIRE", "Fire", "Fire burns.", label("Fire"), label("fire")),
		term("timed:AFRAID", "Afraid", "You are terrified!", label("Afraid"), label("fear")),
		term("skill:SAVE", "Saving Throw", "A saving throw resists spells.", run("Saving Throw"), label("Save")),
	}
	series := []proto.GlossarySeries{
		{Lead: "Provides resistance to", Items: []proto.GlossarySeriesItem{{Name: "fire", ID: "ability:RES_FIRE"}, {Name: "cold", ID: "ability:RES_COLD"}}},
		{Lead: "Sustains", Items: []proto.GlossarySeriesItem{{Name: "strength", ID: "ability:SUST_STR"}}},
	}
	return newGlossary(terms, series)
}

// TestGlossaryFind finds names in running text and labels: whole words
// only, the longest where several start at one place, common words only
// as labels, and the items of a series' list.
func TestGlossaryFind(t *testing.T) {
	g := testGlossary()
	cases := []struct {
		text   string
		labels bool
		want   []string
	}{
		{"It grants Hold Life.", false, []string{"Hold Life=ability:HOLD_LIFE"}},
		{"A Half-Elf, not an Elf; Elfish ways.", false, []string{"Half-Elf=race:Half-Elf", "Elf=race:Elf"}},
		{"You feel no fear.", false, nil},
		{"Fear fear", true, []string{"fear=timed:AFRAID"}},
		{"Save:  +30", false, []string{"Save=skill:SAVE"}},
		{"Save the day", false, nil},
		{"Provides resistance to fire, cold.", false, []string{"fire=ability:RES_FIRE", "cold=ability:RES_COLD"}},
		{"Provides resistance to acid.", false, nil},
		{"Sustains your life force.", false, []string{"Sustains your life force=ability:HOLD_LIFE"}},
		{"Sustains strength.", false, []string{"strength=ability:SUST_STR"}},
		{"Fire Resistance", true, []string{"Fire Resistance=ability:RES_FIRE"}},
		{"Fire", true, []string{"Fire=element:FIRE"}},
	}
	for _, c := range cases {
		text := []rune(c.text)
		var got []string
		for _, s := range g.find(text, c.labels) {
			got = append(got, string(text[s.start:s.end])+"="+s.id)
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("find(%q, labels %v) = %q, want %q", c.text, c.labels, got, c.want)
		}
	}
}

// TestGlossaryScanWraps finds a name wrapped onto the next row, as two
// marks.
func TestGlossaryScanWraps(t *testing.T) {
	g := testGlossary()
	term := testkit.NewTerminal(20, 4)
	var marks []glossMark
	term.Draw(func(f *terminal.Frame) {
		f.Text(0, 0, "You have Hold", screenText)
		f.Text(0, 1, "Life now.", screenText)
		marks = g.scan(f.Buffer, Box{0, 0, 20, 4}, false, nil)
	})
	want := []glossMark{{box: Box{9, 0, 4, 1}, id: "ability:HOLD_LIFE"}, {box: Box{0, 1, 4, 1}, id: "ability:HOLD_LIFE"}}
	if len(marks) != 2 || marks[0] != want[0] || marks[1] != want[1] {
		t.Errorf("marks %+v, want %+v", marks, want)
	}
}

// describedScreen opens a list whose detail panel names Hold Life, at
// the bottom right of an 80x24 window.
func describedScreen(t *testing.T) (*Model, *testkit.Terminal, func(), glossMark) {
	t.Helper()
	m := New(Config{Width: 80, Height: 24})
	m.gloss = testGlossary()
	items := []listItem{{Label: "Ring of Hold Life", Detail: proto.PlainText(colorWhite, "Some text. This ring grants Hold Life.")}}
	s := newListScreen("test-list", "Test", items)
	s.detail = true
	s.width, s.height = 80, 24
	m.push(s)
	term := testkit.NewTerminal(80, 24)
	draw := func() { term.Draw(func(f *terminal.Frame) { m.View(f) }) }
	draw()
	var found glossMark
	for _, mk := range m.marks {
		if mk.id == "ability:HOLD_LIFE" {
			found = mk
		}
	}
	if found.id == "" {
		t.Fatalf("no mark for Hold Life in the detail panel: %+v\n%s", m.marks, term.Snapshot())
	}
	// The list's row names Hold Life too, but a click there is the row's.
	for _, mk := range m.marks {
		for _, h := range s.hotspots {
			if h.box.Overlaps(mk.box) {
				t.Errorf("a mark %+v inside the row %+v", mk, h.box)
			}
		}
	}
	return m, term, draw, found
}

// TestDescriptionClick opens the box with a click and closes it with a
// second click, a click elsewhere, and Escape. The box keeps inside the
// window, its text scrolls, and the name is underlined.
func TestDescriptionClick(t *testing.T) {
	m, term, draw, mk := describedScreen(t)
	if c := term.Frame().Buffer.CellAt(uint16(mk.box.X), uint16(mk.box.Y)); !c.Style.HasModifier(cell.ModifierUnderline) {
		t.Error("the name is not underlined")
	}
	if !term.Click(uint16(mk.box.X+1), uint16(mk.box.Y)) || m.desc == nil || m.desc.id != "ability:HOLD_LIFE" {
		t.Fatalf("a click on the name opened %+v", m.desc)
	}
	draw()
	b := m.desc.box
	if b.X < 0 || b.Y < 0 || b.X+b.W > 80 || b.Y+b.H > 24 {
		t.Errorf("the box %+v leaves the 80x24 window", b)
	}
	if b.Overlaps(mk.box) {
		t.Errorf("the box %+v covers its name %+v", b, mk.box)
	}
	found := false
	for _, n := range term.AccessibilityTree() {
		if n.ID == descID && n.Label == "Hold Life" && strings.Contains(n.Value, "experience") {
			found = true
		}
	}
	if !found {
		t.Error("no description in the accessibility tree")
	}
	// The text is longer than the box: the keys and the wheel scroll it.
	m.descKey(driver.KeyEvent{Type: driver.KeyPageDown})
	if m.desc.offset == 0 {
		t.Error("Page Down did not scroll the box")
	}
	m.descKey(driver.KeyEvent{Type: driver.KeyArrowUp})
	off := m.desc.offset
	term.Mouse(driver.MouseEvent{X: uint16(b.X + 3), Y: uint16(b.Y + 2), Button: driver.MouseScrollDown})
	if m.desc.offset <= off {
		t.Error("the wheel did not scroll the box")
	}
	// A click in the box keeps it.
	draw()
	term.Click(uint16(b.X+3), uint16(b.Y+2))
	if m.desc == nil {
		t.Fatal("a click in the box closed it")
	}
	// A second click on the name closes it.
	term.Click(uint16(mk.box.X+1), uint16(mk.box.Y))
	if m.desc != nil {
		t.Error("a second click on the name left the box open")
	}
	// A click elsewhere closes it.
	draw()
	term.Click(uint16(mk.box.X+1), uint16(mk.box.Y))
	draw()
	term.Click(0, 0)
	if m.desc != nil {
		t.Error("a click elsewhere left the box open")
	}
	// Escape closes it, and goes no further.
	draw()
	term.Click(uint16(mk.box.X+1), uint16(mk.box.Y))
	if !m.descKey(driver.KeyEvent{Type: driver.KeyEsc}) || m.desc != nil {
		t.Error("Escape did not close the box")
	}
}

// TestDescriptionHover opens the box when the mouse rests on a name,
// keeps it while the mouse is in the box, and closes it when the mouse
// leaves both; with the setting off, only clicks open it.
func TestDescriptionHover(t *testing.T) {
	m, term, draw, mk := describedScreen(t)
	move := func(x, y int) engine.UpdateResult {
		return m.Update(engine.MouseMotionMsg{Position: pointAt(x, y)})
	}
	move(mk.box.X+1, mk.box.Y)
	if m.hoverMark == nil {
		t.Fatal("the pointer on the name started no wait")
	}
	// An old wait does nothing.
	if m.Update(hoverDue{m.hoverSeq - 1}); m.desc != nil {
		t.Error("a stale wait opened the box")
	}
	if r := m.Update(hoverDue{m.hoverSeq}); !r.Redraw || m.desc == nil || !m.desc.hover {
		t.Fatalf("resting on the name opened %+v", m.desc)
	}
	draw()
	b := m.desc.box
	move(b.X+2, b.Y+1)
	if m.desc == nil {
		t.Error("moving into the box closed it")
	}
	if r := move(0, 0); m.desc != nil || !r.Redraw {
		t.Errorf("moving away left the box open, or asked for no redraw: %+v", r)
	}

	// A wait that ends after the mouse moved on opens nothing.
	draw()
	move(mk.box.X+1, mk.box.Y)
	seq := m.hoverSeq
	move(0, 0)
	if m.Update(hoverDue{seq}); m.desc != nil {
		t.Error("the box opened after the mouse left")
	}

	// Off: hover opens nothing, and a click still does.
	off := false
	m.settings.Describe = &off
	move(mk.box.X+1, mk.box.Y)
	m.Update(hoverDue{m.hoverSeq})
	if m.desc != nil {
		t.Error("with descriptions under the mouse off, hover opened the box")
	}
	draw()
	if term.Click(uint16(mk.box.X+1), uint16(mk.box.Y)); m.desc == nil {
		t.Error("with descriptions under the mouse off, a click opened nothing")
	}
}

// TestDescriptionKeyList lists the terms a screen names for the ' key;
// choosing one opens its box over the screen.
func TestDescriptionKeyList(t *testing.T) {
	m, _, draw, _ := describedScreen(t)
	if !m.listTerms() {
		t.Fatal("' opened no list")
	}
	l, ok := m.topScreen().(*listScreen)
	if !ok || l.id != termsID || len(l.items) != 1 || l.items[0].Label != "Hold Life" {
		t.Fatalf("the list of terms %+v", m.topScreen())
	}
	l.choose(0)
	if m.desc == nil || m.desc.id != "ability:HOLD_LIFE" {
		t.Fatalf("choosing Hold Life opened %+v", m.desc)
	}
	draw()
	if m.desc == nil {
		t.Error("the box closed on the next frame")
	}
}

// TestDescriptionsPlay plays the real client: the birth screen shows a
// race's paragraph from the manual, Mushroom Lore in the Hobbit's panel
// opens its box with a click, and in a game the stats bar's stats are
// marked.
func TestDescriptionsPlay(t *testing.T) {
	d := enginetest.NewDirs(t)
	// Wide enough for the stats bar beside the map.
	p, _ := startPlayer(t, 120, 32, false, d.Args("-n"))
	p.choose(startMenuID, "New character")
	p.page.Expect(p.page.GetByID(birthRacesID)).ToBeVisible()
	p.page.Press("home")
	for i := 0; i < 3; i++ {
		p.page.Press("down") // Hobbit
	}
	p.waitText("Hobbits, or Halflings")
	p.page.GetByRole("button", "Mushroom Lore").Click()
	p.page.Expect(p.page.GetByID(descID)).ToBeVisible()
	if v := p.page.GetByID(descID).Node().Value; !strings.Contains(v, "mushrooms") {
		t.Errorf("Mushroom Lore's box says %q", v)
	}
	p.page.Press("esc")
	p.page.Expect(p.page.GetByID(descID)).Not().ToBeVisible()
	p.page.Expect(p.page.GetByID(birthRacesID)).ToBeVisible()

	p.page.Press("home")
	p.page.Press("enter")
	p.page.Expect(p.page.GetByID(birthClassesID)).ToBeVisible()
	p.waitText("hack-and-slash")
	p.page.Press("home")
	p.page.Press("enter")
	p.waitText("Points left")
	p.page.Press("enter")
	p.waitText("What is your character's name?")
	p.page.Type("Tester")
	p.page.Press("enter")
	p.waitText("Your character's history:")
	p.page.Press("enter")
	p.waitText("Start over")
	p.page.Press("enter")
	p.waitFor("the town", inTownScreen)

	p.page.GetByRole("button", "STR").Click()
	p.page.Expect(p.page.GetByID(descID)).ToBeVisible()
	if v := p.page.GetByID(descID).Node().Value; !strings.Contains(v, "Strength is important") {
		t.Errorf("STR's box says %q", v)
	}
}

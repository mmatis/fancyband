package ui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// fakeCompare is a COMPARE answer with a property of each category and
// items from every source.
func fakeCompare() *proto.CompareData {
	props := []proto.CompareProp{
		{Category: "resistances", Code: "Fi", Name: "Fire"},
		{Category: "resistances", Code: "Co", Name: "Cold"},
		{Category: "abilities", Code: "FA", Name: "Free action"},
		{Category: "hindrances", Code: "Ag", Name: "Aggravate"},
		{Category: "modifiers", Code: "Sp", Name: "Speed"},
		{Category: "stat_modifiers", Code: "ST", Name: "STR"},
	}
	item := func(i int, name, src string, text string, vals ...int) proto.CompareItem {
		n := i
		return proto.CompareItem{Item: proto.Item{Ref: proto.ItemRef{Compare: &n}, Name: name, Char: "(", Color: 1},
			Source: src, Quality: "average", Text: text, Colors: []int{1, 1, 1, 1, 1, 1}, Values: vals}
	}
	return &proto.CompareData{Props: props,
		Player: proto.CompareRow{Text: "+.....", Colors: []int{1, 1, 1, 1, 1, 1}, Values: []int{1, 0, 0, 0, 0, 0}},
		Items: []proto.CompareItem{
			item(0, "a Cloak", "e", "+.....", 1, 0, 0, 0, 0, 0),
			item(1, "Boots of Speed", "p", "....5.", 0, 0, 0, 0, 5, 0),
			item(2, "a Ring of the Dog", "f", "...*..", 0, 0, 0, 1, 0, 0),
			item(3, "Gauntlets of Free Action", "h", "..+...", 0, 0, 1, 0, 0, 0),
			item(4, "Boots of Slowness", "s", "....-.", 0, 0, 0, 0, -2, 0),
			item(5, "a Strange Amulet", "s", "??????", unknownValue, unknownValue, unknownValue, unknownValue, unknownValue, unknownValue),
		}}
}

func rowNames(s *compareScreen) string {
	var out []string
	for _, i := range s.shown {
		out = append(out, s.data.Items[i].Name)
	}
	return strings.Join(out, ",")
}

// TestCompareSources checks the tabs: yours, in stores, everything, and
// carried, cycled with c, Tab, and clicks.
func TestCompareSources(t *testing.T) {
	s := newCompare(New(Config{Width: 80, Height: 24}), fakeCompare())
	want := []string{
		"a Cloak,Boots of Speed,a Ring of the Dog,Gauntlets of Free Action",
		"Boots of Slowness,a Strange Amulet",
		"a Cloak,Boots of Speed,a Ring of the Dog,Gauntlets of Free Action,Boots of Slowness,a Strange Amulet",
		"a Cloak,Boots of Speed",
	}
	for i, w := range want {
		if got := rowNames(s); got != w {
			t.Errorf("tab %d lists %s, want %s", i, got, w)
		}
		if i%2 == 0 {
			s.key(runeKey('c'))
		} else {
			s.key(keyOf(driver.KeyTab))
		}
	}
	if s.source != 0 {
		t.Errorf("four steps left the tab at %d", s.source)
	}
}

// TestCompareFilters checks q and ! on each kind of property, as the
// game's quick filter takes them, and that unknown values pass q.
func TestCompareFilters(t *testing.T) {
	s := newCompare(New(Config{Width: 80, Height: 24}), fakeCompare())
	s.setSource(2)
	cases := []struct {
		col  int
		not  bool
		want string
		say  string
	}{
		{0, false, "a Cloak,a Strange Amulet", "resist Fire"},
		{0, true, "Boots of Speed,a Ring of the Dog,Gauntlets of Free Action,Boots of Slowness", "do not resist Fire"},
		{2, false, "Gauntlets of Free Action,a Strange Amulet", "have Free action"},
		{3, false, "a Cloak,Boots of Speed,Gauntlets of Free Action,Boots of Slowness", "are free of Aggravate"},
		{3, true, "a Ring of the Dog,a Strange Amulet", "have Aggravate"},
		{4, false, "Boots of Speed,a Strange Amulet", "have Speed above 0"},
	}
	for _, c := range cases {
		s.col = c.col
		key := 'q'
		if c.not {
			key = '!'
		}
		s.key(runeKey(key))
		if got := rowNames(s); got != c.want {
			t.Errorf("%c on %s lists %s, want %s", key, s.data.Props[c.col].Name, got, c.want)
		}
		if got := s.filterText(); got != c.say {
			t.Errorf("filter says %q, want %q", got, c.say)
		}
		// The same key again turns it off.
		s.key(runeKey(key))
		if s.filter != -1 || len(s.shown) != 6 {
			t.Errorf("%c twice left filter %d and %d rows", key, s.filter, len(s.shown))
		}
	}
}

// TestCompareSort sorts by a column, best first and unknown last, then
// reverses, keeping the selection on its item, and R resets.
func TestCompareSort(t *testing.T) {
	s := newCompare(New(Config{Width: 80, Height: 24}), fakeCompare())
	s.setSource(2)
	s.sel = 1 // Boots of Speed
	s.col = 4
	s.key(runeKey('s'))
	if got := rowNames(s); !strings.HasPrefix(got, "Boots of Speed,") || !strings.HasSuffix(got, "Boots of Slowness,a Strange Amulet") {
		t.Errorf("sorted by speed: %s", got)
	}
	if s.data.Items[s.selected()].Name != "Boots of Speed" {
		t.Errorf("the selection moved to %s", s.data.Items[s.selected()].Name)
	}
	s.key(runeKey('r'))
	if got := rowNames(s); !strings.HasPrefix(got, "a Strange Amulet,Boots of Slowness") {
		t.Errorf("reversed: %s", got)
	}
	s.key(runeKey('R'))
	if s.sortBy != -1 || s.reverse || s.source != 0 || rowNames(s) != "a Cloak,Boots of Speed,a Ring of the Dog,Gauntlets of Free Action" {
		t.Errorf("reset: sort %d, reverse %v, source %d, %s", s.sortBy, s.reverse, s.source, rowNames(s))
	}
}

// realProps are the game's 50 properties, as COMPARE gives them.
func realProps() []proto.CompareProp {
	var props []proto.CompareProp
	for _, c := range []struct {
		cat   string
		names string
	}{
		{"resistances", "Acid,Elec,Fire,Cold,Poison,Light,Dark,Sound,Shard,Nexus,Nether,Chaos,Disenchant"},
		{"abilities", "Fear resistance,Blindess resistance,Confusion resistance,Stun resistance,Hold life,Regeneration,ESP,See invisible,Free action,Feather falling,Slow digestion,Trap immunity,Bless"},
		{"hindrances", "Impaired HP recovery,Impaired mana recovery,Fear,Aggravate,No teleportation,Drain experience,Sticky curse,Fragile"},
		{"modifiers", "Stealth,Search,Infravision,Tunneling,Speed,Attack speed,Shooting speed,Shooting power,Light,Damage reduction,Movement speed"},
		{"stat_modifiers", "STR,INT,WIS,DEX,CON"},
	} {
		for _, n := range strings.Split(c.names, ",") {
			props = append(props, proto.CompareProp{Category: c.cat, Code: n[:2], Name: n})
		}
	}
	return props
}

// drawCompare draws s alone in a w by h terminal and returns its lines.
func drawCompare(s *compareScreen, w, h int) []string {
	term := testkit.NewTerminal(uint16(w), uint16(h))
	drawStack(s.m, term)
	return strings.Split(term.Snapshot(), "\n")
}

// TestCompareTiles draws the character's and each item's tile in place
// of its letter, on the row's own background, with the place and the
// name clear of it.
func TestCompareTiles(t *testing.T) {
	m := New(Config{Width: 100, Height: 24})
	data := fakeCompare()
	data.Player.Glyph = proto.Glyph{Char: "@", Tile: &proto.TileSpot{Row: 7, Col: 8}}
	for i := range data.Items {
		data.Items[i].Tile = &proto.TileSpot{Row: 3, Col: uint8(i)}
	}
	s := newCompare(m, data)
	var bgs []color.RGBA
	sprite := func(row, col uint8, bg color.RGBA) image.Image {
		bgs = append(bgs, bg)
		return image.NewRGBA(image.Rect(0, 0, 16, 16))
	}
	term := testkit.NewTerminal(100, 24)
	b := Box{0, 0, 100, 24}
	term.Draw(func(f *terminal.Frame) { s.draw(f, b, &drawEnv{pal: newPalette(nil), sprite: sprite}) })
	regions := term.Frame().ImageRegionsSnapshot()
	if want := len(s.shown) + 1; len(regions) != want {
		t.Fatalf("%d pictures, want %d", len(regions), want)
	}
	for _, r := range regions {
		if r.Area.X != uint16(b.X+2) || r.Area.Width != 2 || r.Area.Height != 1 {
			t.Errorf("picture at %d,%d, %dx%d", r.Area.X, r.Area.Y, r.Area.Width, r.Area.Height)
		}
	}
	sr, sg, sb := screenSelected.Bg.RGB()
	selected := false
	for _, bg := range bgs {
		selected = selected || bg == color.RGBA{sr, sg, sb, 255}
	}
	if !selected {
		t.Errorf("no tile on the selected row's background: %v", bgs)
	}
	snap := term.Snapshot()
	if strings.Contains(snap, "@") || strings.Contains(snap, "( e") {
		t.Errorf("letters beside the tiles:\n%s", snap)
	}
	for _, want := range []string{" e a Cloak", "You, with your equipment"} {
		if !strings.Contains(snap, want) {
			t.Errorf("no %q in:\n%s", want, snap)
		}
	}
}

// TestCompareDraw draws the comparison at 80 and 100 columns: the
// readable labels, the game's symbols under the right labels, numbers
// for modifiers, and the border whole on every row.
func TestCompareDraw(t *testing.T) {
	for _, w := range []int{80, 100} {
		m := New(Config{Width: w, Height: 24})
		s := newCompare(m, fakeCompare())
		m.push(s)
		lines := drawCompare(s, w, 24)
		snap := strings.Join(lines, "\n")
		for _, want := range []string{"Compare equipment", "Yours", "Resists", "Fire", "Cold", "Free act", "Aggrav", "Speed", "STR",
			"You, with your equipment", "a Cloak", "Boots of Speed", "+5", "Fire (resists)", "worn", "Esc back"} {
			if !strings.Contains(snap, want) {
				t.Errorf("%d columns: no %q:\n%s", w, want, snap)
			}
		}
		for i, l := range lines[:len(lines)-1] {
			r := []rune(strings.TrimRight(l, " "))
			if len(r) == 0 || !strings.ContainsRune("│╮╯▲▼█░", r[len(r)-1]) {
				t.Errorf("%d columns: row %d lost its border: %q", w, i, l)
			}
		}
		// The cloak's + sits under Fire, centered in its column.
		at := func(want string) int {
			for i, l := range lines {
				if strings.Contains(l, want) {
					return i
				}
			}
			t.Fatalf("no %q", want)
			return -1
		}
		fire := strings.Index(lines[at("Fire  ")], "Fire")
		fire = len([]rune(lines[at("Fire  ")][:fire]))
		cloak := []rune(lines[at("a Cloak")])
		plus := -1
		for x, r := range cloak {
			if r == '+' {
				plus = x
			}
		}
		if plus < fire || plus > fire+3 {
			t.Errorf("%d columns: the cloak's + at %d, Fire's label at %d:\n%s", w, plus, fire, snap)
		}
	}
}

// TestCompareScroll uses the game's 50 properties at 80 columns: the
// columns scroll sideways under the frozen names, as the cursor moves,
// by groups with v and Shift, and with the scroll bar; every label
// shows whole.
func TestCompareScroll(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	data := fakeCompare()
	data.Props = realProps()
	for i := range data.Items {
		data.Items[i].Text = strings.Repeat(".", 50)
		data.Items[i].Values = make([]int, 50)
	}
	s := newCompare(m, data)
	m.push(s)
	lines := drawCompare(s, 80, 24)
	if s.across >= 50 || s.across < 6 {
		t.Fatalf("%d columns in view at 80 columns", s.across)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "◀") {
		t.Error("no sideways scroll bar")
	}
	for _, want := range []string{"Acid", "Poison", "Disench"} {
		if !strings.Contains(strings.Join(lines, "\n"), want) && s.across >= 13 {
			t.Errorf("no %q", want)
		}
	}
	// Right past the last column in view scrolls.
	for i := 0; i < s.across; i++ {
		s.key(keyOf(driver.KeyArrowRight))
	}
	if s.left == 0 || s.col != s.across {
		t.Errorf("after %d steps right: left %d, column %d", s.across, s.left, s.col)
	}
	// v goes to the next group; Shift+Left back to this one's start.
	s.col, s.left = 0, 0
	s.key(runeKey('v'))
	if data.Props[s.col].Name != "Fear resistance" {
		t.Errorf("v went to %s", data.Props[s.col].Name)
	}
	lines = drawCompare(s, 80, 24)
	snap := strings.Join(lines, "\n")
	for _, want := range []string{"Abilities", "No fear", "No blind", "No conf", "No stun"} {
		if !strings.Contains(snap, want) {
			t.Errorf("abilities in view, but no %q:\n%s", want, snap)
		}
	}
	s.key(driver.KeyEvent{Type: driver.KeyArrowLeft, Shift: true})
	if s.col != 0 {
		t.Errorf("Shift+Left went to column %d", s.col)
	}
	// The scroll bar sets the first column; the cursor follows.
	s.scrollCols(49)
	if s.left != s.maxLeft() || s.left+s.visible(s.left, s.room) != 50 || s.col < s.left {
		t.Errorf("scrolled to the end: left %d, column %d", s.left, s.col)
	}
	lines = drawCompare(s, 80, 24)
	snap = strings.Join(lines, "\n")
	for _, want := range []string{"Stats", "STR", "CON"} {
		if !strings.Contains(snap, want) {
			t.Errorf("at the end, no %q:\n%s", want, snap)
		}
	}
}

// TestCompareLabels checks every label is at most eight letters, so
// two rows of them never overlap.
func TestCompareLabels(t *testing.T) {
	for _, p := range realProps() {
		if l := compareLabel(p); len([]rune(l)) > 2*compareColW-2 || l == "" {
			t.Errorf("%s's label %q", p.Name, l)
		}
	}
	if compareLabel(proto.CompareProp{Name: "Acid"}) != "Acid" {
		t.Error("Acid is not Acid")
	}
}

// TestCompareDescribe describes an item by its compare reference, and
// compares two with x.
func TestCompareDescribe(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	var sent sentLog
	m.sendHook = sent.hook
	s := newCompare(m, fakeCompare())
	m.push(s)
	s.sel = 1
	s.key(keyOf(driver.KeyEnter))
	args, _ := sent.last("RECALL")["args"].(map[string]any)
	if ref, _ := args["item"].(map[string]any); ref["compare"] != 1.0 {
		t.Fatalf("RECALL args %v", args)
	}
	title := "Boots of Speed"
	answerQuery(t, m, &sent, "RECALL", "", proto.EventData{Title: &title, Description: proto.PlainText(1, "Fast.")})
	if ts, ok := top(m).(*textScreen); !ok || ts.title != "Boots of Speed" {
		t.Fatalf("top %T", top(m))
	}
	top(m).key(keyOf(driver.KeyEsc))
	if top(m) != s {
		t.Fatalf("Escape left %T on top", top(m))
	}

	// x marks, x on another compares; Escape first clears a mark.
	s.key(runeKey('x'))
	s.key(keyOf(driver.KeyEsc))
	if s.mark != -1 || top(m) != s {
		t.Fatalf("Escape with a mark: mark %d, top %T", s.mark, top(m))
	}
	s.key(runeKey('x'))
	s.key(keyOf(driver.KeyArrowDown))
	s.key(runeKey('x'))
	a, b := "Boots of Speed", "a Ring of the Dog"
	answerQuery(t, m, &sent, "RECALL", "", proto.EventData{Title: &a, Description: proto.PlainText(1, "Fast.")})
	answerQuery(t, m, &sent, "RECALL", "", proto.EventData{Title: &b, Description: proto.PlainText(1, "Noisy.")})
	ts, ok := top(m).(*textScreen)
	if !ok || ts.title != "Object comparison" {
		t.Fatalf("top %T", top(m))
	}
	var text []string
	for _, l := range ts.text {
		for _, sp := range l {
			text = append(text, sp.Text)
		}
	}
	if got := strings.Join(text, "|"); got != "Boots of Speed|Fast.|A Ring of the Dog|Noisy." {
		t.Errorf("comparison text %s", got)
	}
}

// TestComparePlay opens the comparison from the knowledge screen in a
// game, describes the first item, and closes both.
func TestComparePlay(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, m := startPlayer(t, 80, 24, false, d.Args("-n"))
	p.createCharacter()
	p.page.Press("~")
	p.page.Expect(p.page.GetByID(knowledgeID)).ToBeVisible()
	p.page.Press("shift+tab")
	p.waitText("Compare what you wear")
	p.page.Press("enter")
	p.page.Expect(p.page.GetByID(compareID)).ToBeVisible()
	p.waitText("You, with your equipment")
	p.waitText("a Dagger")
	p.page.Press("enter")
	p.page.Expect(p.page.GetByID(recallID)).ToBeVisible()
	p.waitText("Combat info")
	p.page.Press("escape")
	p.gone(recallID)
	p.page.Press("c")
	p.waitText("in a store")
	p.page.Press("escape")
	p.gone(compareID)
	p.page.Press("escape")
	p.gone(knowledgeID)
	p.page.Press("ctrl+c")
	p.page.ExpectExit()
	m.Session().Wait()
}

package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

// answerQuery answers the query named name the model sent last, whose
// args have section, if given.
func answerQuery(t *testing.T, m *Model, sent *sentLog, name, section string, a proto.EventData) {
	t.Helper()
	for i := len(sent.msgs) - 1; i >= 0; i-- {
		v := sent.msgs[i]
		if v["cmd"] != name {
			continue
		}
		if args, _ := v["args"].(map[string]any); section != "" && (args == nil || args["section"] != section) {
			continue
		}
		id := int(v["id"].(float64))
		done, ok := m.pending[id]
		if !ok {
			continue
		}
		delete(m.pending, id)
		a.ID, a.Event = id, name
		m.answers[id] = a
		done(proto.EventData{Event: "COMMAND", ID: id, OK: true})
		return
	}
	t.Fatalf("no %s %s waits for its answer: %v", name, section, sent.msgs)
}

// top returns the screen on top of the stack.
func top(m *Model) screen {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

func labels(items []listItem) []string {
	var out []string
	for _, it := range items {
		l := it.Label
		if it.Header {
			l = "# " + l
		}
		out = append(out, l)
	}
	return out
}

// TestKnowledgeScreen opens the knowledge screen, reads a section into
// groups, moves past the headings, describes the selected entry, reads
// the stores into one tab, and opens the equipment comparison.
func TestKnowledgeScreen(t *testing.T) {
	m := New(Config{Width: 110, Height: 34})
	var sent sentLog
	m.sendHook = sent.hook
	m.openKnowledge(7, "")
	answerQuery(t, m, &sent, "KNOWLEDGE", "", proto.EventData{Sections: []proto.KnowledgeSection{
		{Name: "objects", Title: "Objects", Known: true},
		{Name: "runes", Title: "Runes"},
		{Name: "monsters", Title: "Monsters", Known: true},
		{Name: "store:0", Title: "General Store", Known: true},
		{Name: "store:1", Title: "Armoury", Known: true},
		{Name: "scores", Title: "Hall of fame", Known: true},
		{Name: "equipment", Title: "Equipment comparison", Known: true},
	}})
	k, ok := top(m).(*knowledgeScreen)
	if !ok {
		t.Fatalf("no knowledge screen: %T", top(m))
	}
	if got := strings.Join(k.tabs, ","); got != "Objects,Monsters,Stores,Hall of fame,Equipment comparison" {
		t.Errorf("tabs %s", got)
	}
	answerQuery(t, m, &sent, "KNOWLEDGE", "objects", proto.EventData{List: []proto.KnowledgeEntry{
		{Group: "Potion", ID: 10, Name: "Cure Light Wounds", Glyph: proto.Glyph{Char: "!", Color: 4}},
		{Group: "Potion", ID: 11, Name: "Blue Potion", Dim: true, Glyph: proto.Glyph{Char: "!", Color: 6}},
		{Group: "Scroll", ID: 20, Name: "Phase Door", Note: "@r1", Glyph: proto.Glyph{Char: "?", Color: 1}},
	}})
	if got := strings.Join(labels(k.items), ","); got != "# Potion,Cure Light Wounds,Blue Potion,# Scroll,Phase Door" {
		t.Fatalf("rows %s", got)
	}
	if k.selected() != 1 {
		t.Errorf("selected %d, want the first entry", k.selected())
	}

	// Drawing asks for the selected entry's description.
	term := testkit.NewTerminal(110, 34)
	env := &drawEnv{pal: newPalette(nil)}
	draw := func() {
		b := k.box(Box{0, 0, 110, 34})
		term.Draw(func(f *terminal.Frame) { k.draw(f, b, env) })
	}
	draw()
	title := "Potion of Cure Light Wounds"
	answerQuery(t, m, &sent, "RECALL", "objects", proto.EventData{Title: &title, Description: proto.PlainText(colorWhite, "It heals you a little.")})
	if d := k.items[1].Detail; len(d) == 0 || spansText(d[0]) != title {
		t.Errorf("detail %v", d)
	}
	if v := sent.last("RECALL")["args"].(map[string]any); v["id"] != float64(10) {
		t.Errorf("RECALL args %v", v)
	}
	draw()
	if n := len(m.pending); n != 0 {
		t.Errorf("%d queries pending after drawing again", n)
	}

	// The arrows skip the headings, and a search leaves them out.
	k.key(keyOf(driver.KeyArrowDown))
	k.key(keyOf(driver.KeyArrowDown))
	if k.selected() != 4 {
		t.Errorf("two down from the first: %d", k.selected())
	}
	k.key(keyOf(driver.KeyArrowUp))
	if k.selected() != 2 {
		t.Errorf("up past the heading: %d", k.selected())
	}
	k.key(keyOf(driver.KeyHome))
	if k.selected() != 1 {
		t.Errorf("Home: %d", k.selected())
	}
	k.key(runeKey('/'))
	for _, r := range "door" {
		k.key(runeKey(r))
	}
	if shown := k.shown(); len(shown) != 1 || k.selected() != 4 {
		t.Errorf("search: shown %v, selected %d", shown, k.selected())
	}
	k.key(keyOf(driver.KeyEsc))
	if top(m) != k || k.query != "" {
		t.Fatalf("Escape in the search: query %q, top %T", k.query, top(m))
	}

	// Monsters: t opens the Tile Editor, which needs a tile set.
	k.key(keyOf(driver.KeyTab))
	answerQuery(t, m, &sent, "KNOWLEDGE", "monsters", proto.EventData{List: []proto.KnowledgeEntry{
		{Group: "Canines", ID: 5, Name: "Jackal", Note: "3", Glyph: proto.Glyph{Char: "C", Color: 11}, Visual: &proto.VisualRef{Kind: "monster", ID: 5}},
	}})
	if k.items[1].Hint != "3" || k.items[1].Icon != 'C' {
		t.Errorf("jackal row %+v", k.items[1])
	}
	k.key(runeKey('t'))
	if !strings.Contains(m.notice, "Tile Editor needs a tile set") {
		t.Errorf("notice %q", m.notice)
	}

	// Stores: one tab, a heading per store with stock.
	k.key(keyOf(driver.KeyTab))
	answerQuery(t, m, &sent, "KNOWLEDGE", "store:1", proto.EventData{})
	answerQuery(t, m, &sent, "KNOWLEDGE", "store:0", proto.EventData{List: []proto.KnowledgeEntry{
		{ID: 0, Name: "a Flask of Oil", Note: "3 gold", Glyph: proto.Glyph{Char: "!", Color: 1}},
	}})
	if got := strings.Join(labels(k.items), ","); got != "# General Store,a Flask of Oil" {
		t.Errorf("stores %s", got)
	}

	// The equipment comparison opens the native one over the knowledge
	// screen.
	k.setTab(4)
	k.key(keyOf(driver.KeyEnter))
	answerQuery(t, m, &sent, "COMPARE", "", proto.EventData{Compare: fakeCompare()})
	if _, ok := top(m).(*compareScreen); !ok || m.stack[len(m.stack)-2] != k {
		t.Fatalf("Enter opened %T", top(m))
	}
	m.pop()

	// With its switch off, the classic one.
	m.settings.Classic = []string{compareName}
	k.key(keyOf(driver.KeyEnter))
	if r := sent.last(""); r == nil || r["id"] != float64(7) || r["value"] != nil && r["classic"] != "equipment" {
		t.Errorf("reply %v", r)
	}
	if v, _ := sent.last("")["classic"].(string); v != "equipment" {
		t.Errorf("reply %v", sent.last(""))
	}
	if top(m) == k {
		t.Error("the knowledge screen stayed open")
	}
}

// TestKnowledgeOpensOnView opens the screen on the tab the request
// names, as after the equipment comparison.
func TestKnowledgeOpensOnView(t *testing.T) {
	m := New(Config{Width: 110, Height: 34})
	var sent sentLog
	m.sendHook = sent.hook
	m.openKnowledge(3, "equipment")
	answerQuery(t, m, &sent, "KNOWLEDGE", "", proto.EventData{Sections: []proto.KnowledgeSection{
		{Name: "objects", Title: "Objects", Known: true},
		{Name: "equipment", Title: "Equipment comparison", Known: true},
	}})
	k := top(m).(*knowledgeScreen)
	if k.tab != 1 {
		t.Errorf("tab %d", k.tab)
	}
	k.key(keyOf(driver.KeyEsc))
	if r := sent.last(""); r == nil || r["id"] != float64(3) || r["classic"] != nil {
		t.Errorf("reply %v", r)
	}
	if len(m.stack) != 0 {
		t.Errorf("stack %v", m.stack)
	}
}

// TestPagerScrolls scrolls a pager with the arrows, the page keys, and
// the wheel, searches it, and changes tabs.
func TestPagerScrolls(t *testing.T) {
	var text proto.StyledText
	for i := 0; i < 100; i++ {
		text = append(text, []proto.Span{{Text: fmt.Sprintf("line %02d", i), Color: colorWhite}})
	}
	closed := false
	p := &pagerScreen{id: "test", title: "Test", text: text, tabs: []string{"One", "Two"}, searchable: true, close: func() { closed = true }}
	p.onTab = func(t int) { p.text = text[:10*(t+1)] }
	term := testkit.NewTerminal(80, 24)
	env := &drawEnv{pal: newPalette(nil)}
	var b Box
	draw := func() {
		b = p.box(Box{0, 0, 80, 24})
		term.Draw(func(f *terminal.Frame) { p.draw(f, b, env) })
	}
	draw()
	p.key(keyOf(driver.KeyArrowDown))
	draw()
	if p.offset != 1 {
		t.Errorf("down: %d", p.offset)
	}
	p.key(keyOf(driver.KeyPageDown))
	draw()
	if p.offset != 1+pageRows(p.height) {
		t.Errorf("Page Down: %d", p.offset)
	}
	p.key(keyOf(driver.KeyPageUp))
	p.key(keyOf(driver.KeyArrowUp))
	draw()
	if p.offset != 0 {
		t.Errorf("back up: %d", p.offset)
	}
	wheel(t, term, b.X+5, b.Y+6, 1)
	draw()
	if p.offset != wheelStep {
		t.Errorf("wheel: %d", p.offset)
	}
	p.key(keyOf(driver.KeyEnd))
	draw()
	if p.offset != 100-p.height {
		t.Errorf("End: %d, height %d", p.offset, p.height)
	}
	if !strings.Contains(term.Snapshot(), "line 99") {
		t.Errorf("End does not show the last line:\n%s", term.Snapshot())
	}

	p.key(runeKey('/'))
	for _, r := range "line 4" {
		if r == ' ' {
			p.key(keyOf(driver.KeySpace))
		} else {
			p.key(runeKey(r))
		}
	}
	if got := len(p.shownLines()); got != 10 {
		t.Errorf("search shows %d lines", got)
	}
	p.key(keyOf(driver.KeyEsc))
	if p.query != "" || closed {
		t.Errorf("Escape in the search: query %q, closed %v", p.query, closed)
	}

	p.key(keyOf(driver.KeyTab))
	if p.tab != 1 || len(p.text) != 20 || p.offset != 0 {
		t.Errorf("Tab: tab %d, %d lines, offset %d", p.tab, len(p.text), p.offset)
	}
	p.key(keyOf(driver.KeyEsc))
	if !closed {
		t.Error("Escape did not close")
	}
}

// fakeSheet is a CHARACTER answer.
func fakeSheet() *proto.CharacterSheet {
	return &proto.CharacterSheet{
		Name: "Tester",
		Panels: [][]proto.PanelLine{
			{{Label: "Name", Value: "Tester", Color: 14}, {Label: "Race", Value: "Human", Color: 14}},
			{{Label: "Age", Value: "18", Color: 14}},
			{{Label: "Level", Value: "1", Color: 13}, {}, {Label: "Gold", Value: "137", Color: 13}},
			{{Label: "Armor", Value: "[8,+2]", Color: 14}},
			{{Label: "Stealth", Value: "Bad", Color: 4}},
		},
		Stats:         []proto.SheetStat{{Name: "STR:", Self: "17", Race: 0, Class: 3, Best: "18/20"}, {Name: "Int:", Drained: true, Self: "10", Class: -2, Best: "8", Current: "7"}},
		History:       "You are one of several children of a Townsman.",
		AbilitiesHead: proto.StyledText{{{Text: "      |    ~(", Color: 1}}, {{Text: "      abcdefgimnop@", Color: 1}}},
		Abilities:     proto.StyledText{{{Text: "Resistances", Color: 14}}, {{Text: " Acid:.....", Color: 1}}},
		SlotColumn:    6,
		Equippy:       make([]*proto.Glyph, 12),
	}
}

// TestCharacterSheet opens the sheet, reads each tab, renames the
// character, and saves it to a file.
func TestCharacterSheet(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	m.openCharacter(9)
	answerQuery(t, m, &sent, "CHARACTER", "", proto.EventData{Character: fakeSheet()})
	p, ok := top(m).(*pagerScreen)
	if !ok || p.id != characterID {
		t.Fatalf("no sheet: %T", top(m))
	}
	var all []string
	for _, l := range p.text {
		all = append(all, spansText(l))
	}
	summary := strings.Join(all, "\n")
	for _, want := range []string{"Name", "Tester", "Age", "18/20", "+3", "Gold", "History", "Townsman"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
	if !strings.Contains(summary, "      7") {
		t.Errorf("a drained stat has no current value:\n%s", summary)
	}
	p.key(keyOf(driver.KeyTab))
	if s := spansText(p.text[0]); !strings.Contains(s, "Armor") || !strings.Contains(s, "Stealth") {
		t.Errorf("skills %q", s)
	}
	p.key(keyOf(driver.KeyTab))
	if len(p.text) != 2 || !strings.HasPrefix(spansText(p.text[1]), " Acid:   .  .  .") || spansText(p.text[0]) != "Resistances" || len(p.head) != 2 {
		t.Errorf("abilities %v, head %v", p.text, p.head)
	}

	// c renames.
	p.key(runeKey('c'))
	d, ok := top(m).(*textDialog)
	if !ok {
		t.Fatalf("no rename dialog: %T", top(m))
	}
	for _, r := range "Renamed" {
		d.key(runeKey(r))
	}
	d.key(keyOf(driver.KeyEnter))
	if c := sent.last("RENAME"); c == nil || c["args"].(map[string]any)["name"] != "Renamed" {
		t.Fatalf("RENAME %v", c)
	}
	answer(t, m, true, proto.EventData{})
	if p.title != "Renamed" || top(m) != p {
		t.Errorf("after the rename: title %q, top %T", p.title, top(m))
	}

	// f saves to a file named after the character.
	p.key(runeKey('f'))
	if c := sent.last("DUMP_CHARACTER"); c == nil || c["args"].(map[string]any)["file"] != "Renamed.txt" {
		t.Fatalf("DUMP_CHARACTER %v", c)
	}
	answer(t, m, true, proto.EventData{})
	if !strings.Contains(m.notice, "Renamed.txt") {
		t.Errorf("notice %q", m.notice)
	}

	p.key(keyOf(driver.KeyEsc))
	if r := sent.last(""); r == nil || r["id"] != float64(9) {
		t.Errorf("reply %v", r)
	}
}

// TestSafeName checks the file names character dumps get.
func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"Tester": "Tester", "Bilbo B.": "Bilbo_B_", "": "PLAYER", "Éowyn": "_owyn"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMessagesScreen shows the history at its end, and filters it by
// kind.
func TestMessagesScreen(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	m.openMessages(4)
	answerQuery(t, m, &sent, "MESSAGES", "", proto.EventData{Messages: []proto.MessageData{
		{Text: "You hit the jackal.", Kind: "HIT", Color: 1},
		{Text: "You miss the jackal.", Kind: "MISS", Color: 1, Count: 2},
		{Text: "You hit the jackal.", Kind: "HIT", Color: 1},
		{Text: "The jackal dies.", Kind: "KILL", Color: 4},
	}})
	p := top(m).(*pagerScreen)
	if len(p.text) != 4 || !p.atEnd || p.note != "All 4 messages" {
		t.Errorf("messages: %d lines, at end %v, note %q", len(p.text), p.atEnd, p.note)
	}
	if s := spansText(p.text[1]); s != "You miss the jackal. <2x>" {
		t.Errorf("a repeated message: %q", s)
	}
	p.key(runeKey('f'))
	if len(p.text) != 2 || p.note != "Only hit messages: 2 of 4" {
		t.Errorf("filtered: %d lines, note %q", len(p.text), p.note)
	}
	p.key(runeKey('F'))
	p.key(runeKey('F'))
	if p.note != "Only miss messages: 1 of 4" {
		t.Errorf("back twice: note %q", p.note)
	}
	p.key(keyOf(driver.KeyEsc))
	if r := sent.last(""); r == nil || r["id"] != float64(4) {
		t.Errorf("reply %v", r)
	}
}

// TestHelpScreen opens the index, follows a link by its key and by a
// click, and goes back.
func TestHelpScreen(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	m.openHelp(6, "")
	page := proto.StyledText{
		{{Text: "Please choose one of the following help files:", Color: 1}},
		nil,
		{{Text: "    (a) Available commands", Color: 1}},
		{{Text: "    (b) Available symbols", Color: 1}},
		nil,
		{{Text: "Browser/help commands:", Color: 1}},
		{{Text: "  #     go to line", Color: 1}},
		nil,
	}
	answerQuery(t, m, &sent, "HELP", "", proto.EventData{File: "index.txt", Page: page, Links: []proto.HelpLink{{Key: "a", File: "commands.txt"}, {Key: "b", File: "symbols.txt"}}})
	index := top(m).(*pagerScreen)
	if len(index.text) != 4 {
		t.Errorf("index text %v", index.text)
	}
	if len(index.keys) != 2 || index.keys[0].Label != "Available commands" {
		t.Errorf("keys %+v", index.keys)
	}
	index.key(runeKey('a'))
	if q := sent.last("HELP"); q["args"].(map[string]any)["file"] != "commands.txt" {
		t.Fatalf("HELP %v", q)
	}
	answerQuery(t, m, &sent, "HELP", "", proto.EventData{File: "commands.txt", Page: proto.PlainText(1, "Original Keyset Command Summary")})
	cmds := top(m).(*pagerScreen)
	if cmds == index || cmds.title != "Help: commands.txt" {
		t.Fatalf("top %q", cmds.title)
	}
	n := len(sent.msgs)
	cmds.key(keyOf(driver.KeyEsc))
	if top(m) != index || len(sent.msgs) != n {
		t.Errorf("Escape from a link: top %T, sent %v", top(m), sent.msgs[n:])
	}
	index.clickLine(3)
	if q := sent.last("HELP"); q["args"].(map[string]any)["file"] != "symbols.txt" {
		t.Errorf("a click on (b): %v", q)
	}
	answerQuery(t, m, &sent, "HELP", "", proto.EventData{File: "symbols.txt", Page: proto.PlainText(1, "Symbols")})
	m.pop()
	index.key(keyOf(driver.KeyEsc))
	if r := sent.last(""); r == nil || r["id"] != float64(6) || len(m.stack) != 0 {
		t.Errorf("reply %v, stack %v", r, m.stack)
	}
}

// TestHeadersSkipped checks that the selection never rests on a heading,
// with the keys or the wheel, and that a list may start with one.
func TestHeadersSkipped(t *testing.T) {
	items := []listItem{{Label: "A", Header: true}}
	for i := 0; i < 30; i++ {
		items = append(items, listItem{Label: fmt.Sprint(i)})
		if i%5 == 4 {
			items = append(items, listItem{Label: "H", Header: true})
		}
	}
	s := newListScreen("h", "H", items)
	if s.selected() != 1 {
		t.Fatalf("starts on %d", s.selected())
	}
	term := testkit.NewTerminal(60, 12)
	env := &drawEnv{pal: newPalette(nil)}
	b := s.box(Box{0, 0, 60, 12})
	for i := 0; i < 40; i++ {
		for _, k := range []driver.KeyType{driver.KeyArrowDown, driver.KeyPageDown, driver.KeyArrowUp} {
			s.key(keyOf(k))
			term.Draw(func(f *terminal.Frame) { s.draw(f, b, env) })
			if s.selected() < 0 {
				t.Fatalf("on a heading after %v: row %d", k, s.state.Selected)
			}
		}
		wheel(t, term, b.X+3, b.Y+3, 1)
		if s.selected() < 0 {
			t.Fatalf("on a heading after the wheel: row %d", s.state.Selected)
		}
	}
	s.clickItem(0)
	if s.selected() < 0 {
		t.Error("a click selected a heading")
	}
}

// TestKnowledgeToTileEditor opens the Tile Editor on an entry from the
// knowledge screen, and goes back to it.
func TestKnowledgeToTileEditor(t *testing.T) {
	m, ed, _ := testEditor(t, 1, 120, 40)
	var sent sentLog
	m.sendHook = sent.hook
	under := newListScreen("under", "Under", []listItem{{Label: "x"}})
	m.push(under)
	m.openTileEditorAt("terrain", 2)
	m.lastVisuals = ed.v
	answer(t, m, true, proto.EventData{})
	got, ok := top(m).(*tileEditor)
	if !ok {
		t.Fatalf("no Tile Editor: %T", top(m))
	}
	if tileKinds[got.tab].kind != "terrain" {
		t.Errorf("tab %s", got.tabs[got.tab])
	}
	if i := got.selected(); i < 0 || got.v.Entries[got.rows[i]].Name != "open floor" {
		t.Errorf("selected %d", i)
	}
	got.key(keyOf(driver.KeyEsc))
	if top(m) != under {
		t.Errorf("after Escape: %T", top(m))
	}
}

// TestSheetSlotTiles spreads the abilities table's slots out, leaving
// part names alone, centers the table, and puts each worn item's tile,
// on the screen's own background, over its slot's letter, and the
// character's over @; the heading stays put while the table scrolls.
func TestSheetSlotTiles(t *testing.T) {
	if got := slotLetters(12); got != "abcdefgimnop@" {
		t.Errorf("slot letters %q", got)
	}
	m := New(Config{Width: 100, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	m.sheet = &tiles.Sheet{}
	sheet := fakeSheet()
	sheet.Equippy[0] = &proto.Glyph{Char: "|", Tile: &proto.TileSpot{Row: 3, Col: 4}}
	sheet.Equippy[5] = &proto.Glyph{Char: "~", Tile: &proto.TileSpot{Row: 5, Col: 6}}
	sheet.Player = &proto.Glyph{Char: "@", Tile: &proto.TileSpot{Row: 7, Col: 8}}
	// With tiles, the game leaves the equipment's letters out.
	sheet.AbilitiesHead[0] = nil
	sheet.Abilities = nil
	sheet.Abilities = append(sheet.Abilities, []proto.Span{{Text: "Resistances", Color: 14}})
	for i := 0; i < 60; i++ {
		sheet.Abilities = append(sheet.Abilities, []proto.Span{{Text: fmt.Sprintf("Row%02d:", i), Color: 1}, {Text: "..+..........", Color: 4}})
	}
	m.openCharacter(2)
	answerQuery(t, m, &sent, "CHARACTER", "", proto.EventData{Character: sheet})
	p := top(m).(*pagerScreen)
	p.setTab(2)
	if len(p.head) != 1 || spansText(p.head[0]) != "         a  b  c  d  e  f  g  i  m  n  o  p  @ " {
		t.Fatalf("head %q", p.head)
	}
	if got := spansText(p.text[0]); got != "Resistances" {
		t.Errorf("a part's name %q", got)
	}
	if got := spansText(p.text[1]); got != "Row00:   .  .  +  .  .  .  .  .  .  .  .  .  . " {
		t.Errorf("first row %q", got)
	}
	if !p.center {
		t.Error("the table is not centered")
	}
	var bgs []color.RGBA
	sprite := func(row, col uint8, bg color.RGBA) image.Image {
		bgs = append(bgs, bg)
		return image.NewRGBA(image.Rect(0, 0, 16, 16))
	}
	term := testkit.NewTerminal(100, 30)
	env := &drawEnv{pal: newPalette(nil), sprite: sprite}
	var b Box
	draw := func() []terminal.ImageRegion {
		term.Draw(func(f *terminal.Frame) {
			b = p.box(Box{0, 0, 100, 30})
			p.draw(f, b, env)
		})
		return term.Frame().ImageRegionsSnapshot()
	}
	for _, scroll := range []bool{false, true} {
		if scroll {
			p.key(keyOf(driver.KeyPageDown))
		}
		regions := draw()
		if len(regions) != 3 {
			t.Fatalf("%d pictures, want 3", len(regions))
		}
		// Under the tabs, and centered: the table is 48 cells wide in 76.
		head := Box{b.X + 2 + (76-48)/2, b.Y + 1 + 2, 0, 0}
		want := map[[2]int]bool{}
		for _, slot := range []int{0, 5, 12} {
			want[[2]int{head.X + slotX(6, slot), head.Y}] = true
		}
		for _, r := range regions {
			if !want[[2]int{int(r.Area.X), int(r.Area.Y)}] || r.Area.Width != 2 || r.Area.Height != 1 || r.Transparent {
				t.Errorf("picture at %d,%d, %dx%d, see-through %v", r.Area.X, r.Area.Y, r.Area.Width, r.Area.Height, r.Transparent)
			}
		}
		// An empty slot is blank.
		if c := term.Frame().Buffer.Get(uint16(head.X+slotX(6, 1)), uint16(head.Y)); c == nil || c.Content != ' ' {
			t.Errorf("empty slot b shows %v", c)
		}
	}
	r, g, bl := screenBg.Bg.RGB()
	for _, bg := range bgs {
		if bg != (color.RGBA{r, g, bl, 255}) {
			t.Errorf("a tile on %v, not the screen's background", bg)
		}
	}
	if p.offset == 0 {
		t.Error("Page Down did not scroll")
	}
}

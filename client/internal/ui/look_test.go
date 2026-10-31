package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// sentLog records the commands and replies a model sends, as JSON.
type sentLog struct{ msgs []map[string]any }

func (l *sentLog) hook(msg proto.Message) {
	var raw []byte
	switch m := msg.(type) {
	case proto.Command:
		raw = m.JSON
	case proto.Reply:
		raw = m.JSON
	default:
		return
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err == nil {
		l.msgs = append(l.msgs, v)
	}
}

// last returns the last message whose "cmd" is name, or the last reply
// when name is "".
func (l *sentLog) last(name string) map[string]any {
	for i := len(l.msgs) - 1; i >= 0; i-- {
		v := l.msgs[i]
		if cmd, ok := v["cmd"].(string); ok && cmd == name || name == "" && v["cmd"] == nil {
			return v
		}
	}
	return nil
}

// fakeLook is a LOOK answer: the player near the top of the map, a jackal
// near the bottom, and a shop entrance beside the player.
func fakeLook() []proto.LookThing {
	feat := 3
	return []proto.LookThing{
		{X: 20, Y: 5, Cell: &[2]int{10, 2}, Kind: "player", Name: "a down staircase", Text: "You are on a down staircase, 0 N, 0 E.", Where: "0 N, 0 E", Glyph: proto.Glyph{Char: "@", Color: 1}, Feature: &feat},
		{X: 22, Y: 26, Cell: &[2]int{12, 23}, Kind: "monster", Name: "a jackal", Text: "You see a jackal (unhurt), 21 S, 2 E.", Where: "21 S, 2 E", Glyph: proto.Glyph{Char: "C", Color: 11}, Monster: 7, Midx: 4, Health: "unhurt", Targetable: true},
		{X: 25, Y: 5, Cell: &[2]int{15, 2}, Kind: "terrain", Name: "the entrance to the General Store", Text: "You see the entrance to the General Store, 0 N, 5 E.", Where: "0 N, 5 E", Glyph: proto.Glyph{Char: "1", Color: 1}, Feature: &feat},
	}
}

// TestLookScreen moves through the look list, checks that the marker
// follows the selection and the screen keeps out of its way, picks a
// grid on the map, targets the jackal, and travels.
func TestLookScreen(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	m.openLook(proto.RequestData{ID: 5, Prompt: "screen", Name: "look", Mode: "look", Pathfind: true})
	if q := sent.last("LOOK"); q == nil || q["args"].(map[string]any)["mode"] != "look" {
		t.Fatalf("no LOOK query: %v", sent.msgs)
	}
	m.nextID = int(sent.last("LOOK")["id"].(float64))
	answer(t, m, true, proto.EventData{Event: "LOOK", Things: fakeLook()})
	s, ok := m.stack[len(m.stack)-1].(*lookScreen)
	if !ok {
		t.Fatalf("no look screen: %T", m.stack)
	}
	main := m.layout.Main
	area := Box{0, 0, 100, 30}
	if m.marker == nil || m.marker.X != main.X+10 || m.marker.Y != main.Y+2 {
		t.Fatalf("marker %v on the player", m.marker)
	}
	if s.box(area).Overlaps(*m.marker) {
		t.Error("the screen covers the player's marker")
	}

	// Down selects the jackal, at the bottom; the screen moves to the top.
	s.key(keyOf(driver.KeyArrowDown))
	if m.marker == nil || m.marker.Y != main.Y+23 {
		t.Fatalf("marker %v on the jackal", m.marker)
	}
	if !s.top || s.box(area).Overlaps(*m.marker) {
		t.Errorf("the screen covers the jackal: top %v, box %v", s.top, s.box(area))
	}
	// Space steps on, and - back.
	s.key(runeKey(' '))
	if s.selected() != 2 {
		t.Errorf("Space selects %d", s.selected())
	}
	s.key(runeKey('-'))
	if s.selected() != 1 {
		t.Errorf("- selects %d", s.selected())
	}

	// A click on the shop's grid on the map selects it. The screen is at
	// the bottom again once the player is selected.
	s.selectItem(0)
	s.moved()
	term := testkit.NewTerminal(100, 30)
	env := &drawEnv{pal: newPalette(nil)}
	term.Draw(func(f *terminal.Frame) { s.draw(f, s.box(area), env) })
	s.click(main.X+15, main.Y+2)
	if s.selected() != 2 {
		t.Errorf("a click on the shop selects %d", s.selected())
	}
	// A click on a grid not in the list looks at it.
	s.click(main.X+11, main.Y+3)
	q := sent.last("LOOK")["args"].(map[string]any)
	if q["x"] != float64(21) || q["y"] != float64(6) {
		t.Errorf("a click on the map looks at %v", q)
	}

	// t targets the jackal and closes the screen.
	s.selectItem(1)
	s.key(runeKey('t'))
	if tg := sent.last("SET_TARGET"); tg == nil || tg["args"].(map[string]any)["monster"] != float64(4) {
		t.Errorf("t sent %v", tg)
	}
	if r := sent.last(""); r == nil || r["id"] != float64(5) {
		t.Errorf("t replied %v", r)
	}
	if len(m.stack) != 0 || m.marker != nil {
		t.Errorf("after t: %d screens, marker %v", len(m.stack), m.marker)
	}

	// g travels to the selected grid.
	m.openLook(proto.RequestData{ID: 6, Prompt: "screen", Name: "look", Mode: "look", Pathfind: true})
	m.nextID = int(sent.last("LOOK")["id"].(float64))
	answer(t, m, true, proto.EventData{Event: "LOOK", Things: fakeLook()})
	s = m.stack[len(m.stack)-1].(*lookScreen)
	s.selectItem(2)
	s.key(runeKey('g'))
	r := sent.last("")
	then, _ := r["then"].(map[string]any)
	if r["id"] != float64(6) || then["cmd"] != "PATHFIND" {
		t.Fatalf("g replied %v", r)
	}
	if p := then["args"].(map[string]any)["point"].(map[string]any); p["x"] != float64(25) || p["y"] != float64(5) {
		t.Errorf("g travels to %v", p)
	}
}

// TestTargetScreen opens target mode on the old target, and closes with
// Escape.
func TestTargetScreen(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	m.openLook(proto.RequestData{ID: 5, Prompt: "screen", Name: "target", Mode: "target", Target: &proto.TargetData{X: 22, Y: 26, Monster: 4}})
	if q := sent.last("LOOK"); q["args"].(map[string]any)["mode"] != "target" {
		t.Fatalf("LOOK for targets: %v", q)
	}
	m.nextID = int(sent.last("LOOK")["id"].(float64))
	answer(t, m, true, proto.EventData{Event: "LOOK", Things: fakeLook()[1:2]})
	s := m.stack[len(m.stack)-1].(*lookScreen)
	if s.selected() != 0 || s.title != "Target" {
		t.Errorf("target screen %q selects %d", s.title, s.selected())
	}
	s.key(keyOf(driver.KeyEsc))
	if r := sent.last(""); r["id"] != float64(5) || len(m.stack) != 0 {
		t.Errorf("Escape replied %v, %d screens", r, len(m.stack))
	}

	// Nothing to target: a short note.
	m.openLook(proto.RequestData{ID: 6, Prompt: "screen", Name: "target", Mode: "target"})
	m.nextID = int(sent.last("LOOK")["id"].(float64))
	answer(t, m, true, proto.EventData{Event: "LOOK"})
	s = m.stack[len(m.stack)-1].(*lookScreen)
	if !strings.Contains(s.note, "Nothing") || s.detail {
		t.Errorf("empty target screen: note %q, detail %v", s.note, s.detail)
	}
}

// TestAimScreen fires in a direction, at the selected target, and at
// the closest, and falls back to the direction dialog with no targets.
func TestAimScreen(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	things := fakeLook()[1:2]
	second := things[0]
	second.Midx, second.Name, second.Cell = 9, "a cave spider", &[2]int{30, 10}
	things = append(things, second)
	open := func(id int, look proto.EventData) *lookScreen {
		t.Helper()
		m.openAim(proto.RequestData{ID: id, Prompt: "aim"})
		m.nextID = int(sent.last("LOOK")["id"].(float64))
		answer(t, m, true, look)
		s, _ := m.stack[len(m.stack)-1].(*lookScreen)
		return s
	}

	// A digit fires that way.
	s := open(1, proto.EventData{Event: "LOOK", Things: things, Target: &proto.TargetData{Monster: 9}})
	if s == nil || s.id != aimID || s.selected() != 1 {
		t.Fatalf("aim screen %+v", s)
	}
	s.key(runeKey('6'))
	if r := sent.last(""); r["id"] != float64(1) || r["value"] != float64(6) {
		t.Errorf("6 replied %v", r)
	}

	// Enter fires at the selected target; ' at the closest.
	for _, k := range []driver.KeyEvent{keyOf(driver.KeyEnter), runeKey('\'')} {
		s = open(2, proto.EventData{Event: "LOOK", Things: things, Target: &proto.TargetData{Monster: 9}})
		s.key(k)
		want := float64(9)
		if k.Type == driver.KeyRune {
			want = 4
		}
		if tg := sent.last("SET_TARGET"); tg["args"].(map[string]any)["monster"] != want {
			t.Errorf("%v targets %v", k, tg)
		}
		if r := sent.last(""); r["value"] != float64(5) || len(m.stack) != 0 {
			t.Errorf("%v replied %v with %d screens", k, r, len(m.stack))
		}
	}

	// Nothing to target: the direction dialog.
	m.openAim(proto.RequestData{ID: 3, Prompt: "aim"})
	m.nextID = int(sent.last("LOOK")["id"].(float64))
	answer(t, m, true, proto.EventData{Event: "LOOK"})
	if _, ok := m.stack[len(m.stack)-1].(*directionDialog); !ok {
		t.Errorf("no direction dialog: %T", m.stack[len(m.stack)-1])
	}
}

// TestRecallFromMonsterList clicks a monster in the monster list pane
// and scrolls its description.
func TestRecallFromMonsterList(t *testing.T) {
	m := New(Config{Width: 160, Height: 48, Subwindows: true})
	var sent sentLog
	m.sendHook = sent.hook
	var pane *Pane
	for i, p := range m.layout.Panes {
		if p.Term == TermMonsters {
			pane = &m.layout.Panes[i]
		}
	}
	if pane == nil {
		t.Fatalf("no monster pane in %+v", m.layout)
	}
	b := pane.TermBox()
	if p, row, ok := m.paneAt(b.X+3, b.Y+1); !ok || p.Term != TermMonsters || row != 1 {
		t.Fatalf("paneAt gives %+v row %d", p, row)
	}
	m.openRecallAt(1)
	m.nextID = int(sent.last("MONSTER_LIST")["id"].(float64))
	race := 7
	answer(t, m, true, proto.EventData{Event: "MONSTER_LIST", Rows: []*int{nil, &race}})
	if q := sent.last("RECALL"); q["args"].(map[string]any)["monster"] != float64(7) {
		t.Fatalf("RECALL %v", q)
	}
	var long proto.StyledText
	for i := 0; i < 60; i++ {
		long = append(long, []proto.Span{{Text: "A line about jackals.", Color: colorWhite}})
	}
	title := "jackal"
	answer(t, m, true, proto.EventData{Event: "RECALL", Title: &title, Description: long})
	ts, ok := m.stack[len(m.stack)-1].(*textScreen)
	if !ok || ts.title != "Jackal" {
		t.Fatalf("no description: %T", m.stack[len(m.stack)-1])
	}
	term := testkit.NewTerminal(160, 48)
	env := &drawEnv{pal: newPalette(nil)}
	area := Box{0, 0, 160, 48}
	draw := func() { term.Draw(func(f *terminal.Frame) { ts.draw(f, ts.box(area), env) }) }
	draw()
	ts.key(keyOf(driver.KeyPageDown))
	ts.key(keyOf(driver.KeyArrowDown))
	draw()
	if ts.offset <= 1 {
		t.Errorf("Page Down and Down scroll to %d", ts.offset)
	}
	tb := ts.box(area)
	wheel(t, term, tb.X+5, tb.Y+3, -1)
	ts.key(keyOf(driver.KeyEsc))
	if len(m.stack) != 0 {
		t.Error("Escape did not close the description")
	}
}

// TestLookPlay opens look mode and target mode by their keys in a real
// game, reads the player's own grid, and closes both.
func TestLookPlay(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, _ := startPlayer(t, 110, 32, false, d.Args("-n"))
	p.createCharacter()

	p.open("l", lookID)
	p.waitText("You are on")
	p.waitText("entrance to the")
	p.page.Press("Escape")
	p.gone(lookID)

	p.open("*", lookID)
	p.waitText("Target")
	p.page.Press("Escape")
	p.gone(lookID)
	p.waitText("Target Aborted.")
}

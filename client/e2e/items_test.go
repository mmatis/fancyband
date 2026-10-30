package e2e

import (
	"os"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// answerOf sends a query and returns the event that answers it.
func (g *game) answerOf(id int, name string, args map[string]any) proto.EventData {
	g.t.Helper()
	from := len(g.events)
	if r := g.command(id, name, args); !r.OK {
		g.t.Fatalf("%s: %+v", name, r)
	}
	for _, e := range g.events[from:] {
		if e.Event == name && e.ID == id {
			return e
		}
	}
	g.t.Fatalf("no %s answer", name)
	return proto.EventData{}
}

// afterWaits checks ok, and again after each WAIT, until it passes. A
// query asked before the game has run a command sees the game as it was.
func (g *game) afterWaits(what string, ok func() bool) {
	g.t.Helper()
	for i := 0; i < 20; i++ {
		from := len(g.events)
		if ok() {
			return
		}
		g.waitFor("a WAIT", func(*grid.Grid) bool {
			for _, e := range g.events[from:] {
				if e.Event == "WAIT" {
					return true
				}
			}
			return false
		})
	}
	g.t.Fatalf("%s never happened", what)
}

// autoMore turns on auto_more, so no -more- stops the game.
func (g *game) autoMore(id int) {
	g.t.Helper()
	if r := g.command(id, "SET_OPTION", map[string]any{"name": "auto_more", "value": true}); !r.OK {
		g.t.Fatalf("auto_more: %+v", r)
	}
}

// inventory asks for the INVENTORY answer.
func (g *game) inventory(id int) proto.EventData {
	g.t.Helper()
	return g.answerOf(id, "INVENTORY", nil)
}

// packItem returns the pack item whose name contains name.
func packItem(t *testing.T, inv proto.EventData, name string) proto.Item {
	t.Helper()
	for _, it := range inv.Inventory {
		if strings.Contains(it.Name, name) {
			return it
		}
	}
	t.Fatalf("no %q in the pack: %+v", name, inv.Inventory)
	return proto.Item{}
}

// action returns the item's action named label.
func action(t *testing.T, it proto.Item, label string) proto.Action {
	t.Helper()
	for _, a := range it.Actions {
		if a.Label == label {
			return a
		}
	}
	t.Fatalf("%s has no %q action: %+v", it.Name, label, it.Actions)
	return proto.Action{}
}

// plain joins styled text into one string.
func plain(st proto.StyledText) string {
	var b strings.Builder
	for _, line := range st {
		for _, sp := range line {
			b.WriteString(sp.Text)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// prompts records the prompt requests a game sees and answers each with
// reply, or cancels it when reply is nil or returns nil.
type prompts struct {
	seen  []proto.RequestData
	reply func(proto.RequestData) map[string]any
}

func (p *prompts) answer(r proto.RequestData) map[string]any {
	p.seen = append(p.seen, r)
	if p.reply != nil {
		return p.reply(r)
	}
	return nil
}

// waitPrompt reads until the game has asked n prompts in all.
func (g *game) waitPrompt(p *prompts, n int) proto.RequestData {
	g.t.Helper()
	g.waitFor("a prompt", func(*grid.Grid) bool { return len(p.seen) >= n })
	return p.seen[n-1]
}

// TestInventoryAndRecall checks INVENTORY and RECALL for a new warrior,
// and that an object's description matches the classic examine screen.
func TestInventoryAndRecall(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events"})
	g.createCharacter()

	inv := g.inventory(1)
	if len(inv.Slots) < 10 || inv.Slots[0] != "weapon" {
		t.Errorf("slots %v", inv.Slots)
	}
	if inv.Burden <= 0 {
		t.Errorf("burden %d", inv.Burden)
	}
	potion := packItem(t, inv, "Berserk Strength")
	if potion.Char != "!" || potion.Tile != nil {
		t.Errorf("potion looks like %q %v", potion.Char, potion.Tile)
	}
	if a := action(t, potion, "Quaff"); a.Cmd != "QUAFF" || a.Key != "q" || !a.Valid {
		t.Errorf("quaff action %+v", a)
	}
	action(t, potion, "Drop")
	action(t, potion, "Throw")
	var dagger *proto.Item
	for i, it := range inv.Equipment {
		if strings.Contains(it.Name, "Dagger") {
			dagger = &inv.Equipment[i]
		}
	}
	if dagger == nil || dagger.Ref.Equip == nil || *dagger.Ref.Equip != 0 {
		t.Fatalf("no dagger in the weapon slot: %+v", inv.Equipment)
	}
	if a := action(t, *dagger, "Take off"); a.Cmd != "TAKEOFF" {
		t.Errorf("take off action %+v", a)
	}

	r := g.answerOf(2, "RECALL", map[string]any{"item": potion.Ref.Map()})
	if r.Title == nil || !strings.Contains(*r.Title, "Berserk Strength") {
		t.Fatalf("recall title %v", r.Title)
	}
	text := plain(r.Description)
	if !strings.Contains(text, "berserk") {
		t.Errorf("recall text %q", text)
	}
	if r := g.answerOf(3, "RECALL", map[string]any{"item": map[string]any{"inven": 40}}); r.Title != nil {
		t.Errorf("recall of nothing has title %q", *r.Title)
	}
	if r := g.answerOf(4, "RECALL", map[string]any{"monster": 1}); r.Title == nil || len(r.Description) == 0 {
		t.Errorf("monster recall %+v", r)
	}

	// The classic examine screen shows the same words.
	g.key('I')
	g.waitText("Examine which item?")
	g.key(uint32('a' + *potion.Ref.Inven))
	g.waitText("Berserk Strength")
	first := strings.Fields(text)
	want := strings.Join(first[:min(4, len(first))], " ")
	g.waitFor("the description "+want, func(gr *grid.Grid) bool {
		return strings.Contains(strings.Join(strings.Fields(gr.String()), " "), want)
	})
}

// TestInventoryScreen opens the inventory screen by its keys and drops
// and quaffs through its reply, and checks that an inscription such as
// !d still asks first.
func TestInventoryScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:inventory", "prompt:check", "prompt:quantity"})
	var then map[string]any
	a := &screenAnswers{reply: func(proto.RequestData) map[string]any {
		if then == nil {
			return map[string]any{}
		}
		return map[string]any{"then": then}
	}}
	checks := 0
	checkAnswer := false
	g.answer = func(r proto.RequestData) map[string]any {
		switch r.Prompt {
		case "check":
			checks++
			return map[string]any{"value": checkAnswer}
		case "quantity":
			return map[string]any{"value": 1}
		}
		return a.answer(r)
	}
	// A priest, who starts with two potions of Cure Serious Wounds.
	g.createCharacterAs("Priest")
	g.autoMore(99)

	for n, k := range []struct {
		key  uint32
		view string
	}{{'i', "inven"}, {'e', "equip"}, {'|', "quiver"}} {
		g.key(k.key)
		if r := g.waitScreen(a, n+1); r.Name != "inventory" || r.View != k.view {
			t.Fatalf("key %c: screen %+v", k.key, r)
		}
	}

	inv := g.inventory(1)
	cure := packItem(t, inv, "Cure Serious")
	if cure.Number != 2 {
		t.Fatalf("%d potions of Cure Serious Wounds", cure.Number)
	}
	// Drop one: the game asks how many.
	then = map[string]any{"id": 10, "cmd": "DROP", "args": map[string]any{"item": cure.Ref.Map()}}
	g.key('i')
	g.waitScreen(a, 4)
	g.waitFor("the DROP result", func(*grid.Grid) bool { _, ok := g.results[10]; return ok })
	if r := g.results[10]; !r.OK {
		t.Fatalf("DROP: %+v", r)
	}
	g.afterWaits("the potion dropped", func() bool {
		return packItem(t, g.inventory(len(g.results)+100), "Cure Serious").Number == 1
	})

	// Inscribe the potion !q; quaffing it asks, and no keeps it.
	potion := packItem(t, g.inventory(20), "Cure Serious")
	g.command(21, "INSCRIBE", map[string]any{"item": potion.Ref.Map(), "inscription": "!q"})
	g.afterWaits("the inscription", func() bool {
		return strings.Contains(packItem(t, g.inventory(len(g.results)+200), "Cure Serious").Name, "!q")
	})
	then = map[string]any{"id": 30, "cmd": "QUAFF", "args": map[string]any{"item": potion.Ref.Map()}}
	g.key('i')
	g.waitScreen(a, 5)
	g.waitFor("the QUAFF result", func(*grid.Grid) bool { _, ok := g.results[30]; return ok })
	if r := g.results[30]; r.OK || r.Error != "cancelled" || checks != 1 {
		t.Fatalf("QUAFF of a !q potion with no: %+v, %d checks", r, checks)
	}
	checkAnswer = true
	then = map[string]any{"id": 31, "cmd": "QUAFF", "args": map[string]any{"item": potion.Ref.Map()}}
	g.key('i')
	g.waitScreen(a, 6)
	g.waitFor("the QUAFF result", func(*grid.Grid) bool { _, ok := g.results[31]; return ok })
	if r := g.results[31]; !r.OK {
		t.Fatalf("QUAFF with yes: %+v", r)
	}
	g.afterWaits("the potion gone", func() bool {
		for _, it := range g.inventory(len(g.results) + 300).Inventory {
			if strings.Contains(it.Name, "Cure Serious") {
				return false
			}
		}
		return true
	})
}

// TestItemPrompt claims item prompts: quaff offers only potions, tags
// come from inscriptions, a keymap that answers the prompt still works,
// and with nothing to read the game says so without asking.
func TestItemPrompt(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "prompt:item", "prompt:direction"})
	p := &prompts{}
	g.answer = p.answer
	g.createCharacter()

	g.key('q')
	r := g.waitPrompt(p, 1)
	items, err := r.Items()
	if err != nil || r.Prompt != "item" || len(items) == 0 {
		t.Fatalf("quaff prompt %+v %v", r, err)
	}
	for _, it := range items {
		if !strings.Contains(it.Name, "Potion") {
			t.Errorf("quaff offers %q", it.Name)
		}
	}
	if !strings.Contains(r.Text, "Quaff") {
		t.Errorf("prompt text %q", r.Text)
	}

	// An @q1 inscription tags the potion with 1.
	potion := items[0]
	g.waitEvent("WAIT")
	if r := g.command(1, "INSCRIBE", map[string]any{"item": potion.Ref.Map(), "inscription": "@q1"}); !r.OK {
		t.Fatalf("INSCRIBE: %+v", r)
	}
	g.afterWaits("the inscription", func() bool {
		return strings.Contains(packItem(t, g.inventory(len(g.results)+50), "Berserk").Name, "@q1")
	})
	g.key('q')
	r = g.waitPrompt(p, 2)
	items, _ = r.Items()
	if len(items) == 0 || items[0].Tags != "1" {
		t.Errorf("tags %+v", items)
	}

	// A walk with no direction asks for one. The game takes it once it
	// waits for a command again.
	g.waitEvent("WAIT")
	if r := g.command(2, "WALK", nil); !r.OK {
		t.Fatalf("WALK: %+v", r)
	}
	if r := g.waitPrompt(p, 3); r.Prompt != "direction" {
		t.Errorf("walk asks %+v", r)
	}
}

// TestItemPromptKeymap checks that a keymap which answers the item
// prompt still works, since the classic prompt reads the keymap's keys,
// and that with nothing to quaff the game says so without asking.
func TestItemPromptKeymap(t *testing.T) {
	d := enginetest.NewDirs(t)
	// X quaffs the potion tagged 1.
	prf := "keymap-act:q1\nkeymap-input:0:X\n"
	if err := os.WriteFile(d.User+"/user.prf", []byte(prf), 0o644); err != nil {
		t.Fatal(err)
	}
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "prompt:item"})
	p := &prompts{}
	g.answer = p.answer
	g.createCharacter()
	g.autoMore(99)

	potion := packItem(t, g.inventory(1), "Berserk")
	if r := g.command(2, "INSCRIBE", map[string]any{"item": potion.Ref.Map(), "inscription": "@q1"}); !r.OK {
		t.Fatalf("INSCRIBE: %+v", r)
	}
	g.afterWaits("the inscription", func() bool {
		return strings.Contains(packItem(t, g.inventory(len(g.results)+50), "Berserk").Name, "@q1")
	})
	g.key('X')
	g.afterWaits("the potion gone", func() bool {
		for _, it := range g.inventory(len(g.results) + 100).Inventory {
			if strings.Contains(it.Name, "Berserk") {
				return false
			}
		}
		return true
	})
	if len(p.seen) != 0 {
		t.Errorf("the keymap's quaff asked %d prompts", len(p.seen))
	}

	g.key('q')
	g.waitText("You have no potions")
	if len(p.seen) != 0 {
		t.Errorf("quaff with no potions asked %+v", p.seen)
	}
}

// TestSpellPrompts makes a mage, studies a spell through the inventory
// screen's Study, then casts it from the spell prompt and browses the
// book, and checks SPELLS and RECALL for the spell.
func TestSpellPrompts(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:inventory", "prompt:item", "prompt:spell", "prompt:spell_from_book"})
	var then map[string]any
	var replies func(proto.RequestData) map[string]any
	p := &prompts{reply: func(r proto.RequestData) map[string]any {
		if r.Prompt == "screen" {
			return map[string]any{"then": then}
		}
		if replies != nil {
			return replies(r)
		}
		return nil
	}}
	g.answer = p.answer
	g.createCharacterAs("Mage")
	g.autoMore(99)

	inv := g.inventory(1)
	book := packItem(t, inv, "First Spells")
	if a := action(t, book, "Study"); a.Cmd != "STUDY" {
		t.Errorf("study action %+v", a)
	}
	if a := action(t, book, "Browse"); a.Cmd != "BROWSE_SPELL" {
		t.Errorf("browse action %+v", a)
	}

	sp := g.answerOf(2, "SPELLS", map[string]any{"item": book.Ref.Map()})
	if sp.Noun != "spell" || len(sp.Spells) == 0 || sp.Spells[0].Name != "Magic Missile" {
		t.Fatalf("SPELLS %+v", sp)
	}
	missile := sp.Spells[0]
	if missile.Usable || missile.Comment != "unknown" || missile.Fail <= 0 || missile.Fail > 95 {
		t.Errorf("an unlearned Magic Missile: %+v", missile)
	}
	rc := g.answerOf(3, "RECALL", map[string]any{"spell": missile.Value})
	if rc.Title == nil || *rc.Title != "Magic Missile" || len(rc.Description) == 0 {
		t.Errorf("spell recall %+v", rc)
	}

	// Study through the inventory screen: the game asks which spell, from
	// every book it could come from.
	then = map[string]any{"id": 10, "cmd": "STUDY", "args": map[string]any{"item": book.Ref.Map()}}
	replies = func(r proto.RequestData) map[string]any {
		if strings.HasPrefix(r.Prompt, "spell") && r.Verb == "study" {
			return map[string]any{"value": missile.Value}
		}
		return nil
	}
	g.key('i')
	study := g.waitPrompt(p, 2)
	if !strings.HasPrefix(study.Prompt, "spell") || study.Verb != "study" || study.Noun != "spell" {
		t.Fatalf("study prompt %+v", study)
	}
	g.afterWaits("Magic Missile learned", func() bool {
		s := g.answerOf(len(g.results)+100, "SPELLS", map[string]any{"item": book.Ref.Map()})
		return s.Spells[0].Usable
	})

	// Cast: the prompt offers the spell, with its fail rate.
	replies = nil
	n := len(p.seen)
	g.key('m')
	cast := g.waitPrompt(p, n+1)
	spells, err := cast.SpellChoices()
	if err != nil || (cast.Prompt != "spell" && cast.Prompt != "spell_from_book") || len(spells) == 0 {
		t.Fatalf("cast prompt %+v %v", cast, err)
	}
	if spells[0].Name != "Magic Missile" || !spells[0].Usable || spells[0].Comment != "untried" {
		t.Errorf("cast offers %+v", spells[0])
	}

	// Browse: the book first, then its spells, to look at.
	g.waitEvent("WAIT")
	n = len(p.seen)
	replies = func(r proto.RequestData) map[string]any {
		if r.Prompt == "item" {
			return map[string]any{"item": book.Ref.Map()}
		}
		return nil
	}
	g.key('b')
	g.waitPrompt(p, n+2)
	if b := p.seen[n+1]; b.Prompt != "spell_from_book" || b.Verb != "browse" {
		t.Errorf("browse asks %+v", b)
	}
}

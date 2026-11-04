package e2e

import (
	"strconv"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// lookThing returns the grid LOOK lists whose name contains name.
func lookThing(t *testing.T, look proto.EventData, name string) proto.LookThing {
	t.Helper()
	for _, th := range look.Things {
		if strings.Contains(strings.ToLower(th.Name), strings.ToLower(name)) {
			return th
		}
	}
	t.Fatalf("LOOK has no %q: %+v", name, look.Things)
	return proto.LookThing{}
}

// heldScreen waits until the game holds a screen request named name
// open, and returns it.
func (g *game) heldScreen(name string) proto.RequestData {
	g.t.Helper()
	var got proto.RequestData
	g.waitFor("the "+name+" screen", func(*grid.Grid) bool {
		for _, r := range g.held {
			if r.Prompt == "screen" && r.Name == name {
				got = r
				return true
			}
		}
		return false
	})
	g.held = nil
	return got
}

// TestLookAndTarget summons a shrieker mushroom patch, which never
// moves or attacks, beside a new mage; looks at it, the player, and the town's shops through LOOK
// and RECALL; opens look and target mode by their keys; targets the patch
// with SET_TARGET while the target screen is open; and casts Magic
// Missile at it through the aiming prompt.
func TestLookAndTarget(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:look", "screen:target", "prompt:aim", "prompt:spell", "prompt:spell_from_book"})
	var spell int
	var aims []proto.RequestData
	aimReply := map[string]any{"value": 5}
	g.answer = func(r proto.RequestData) map[string]any {
		switch r.Prompt {
		case "spell", "spell_from_book":
			return map[string]any{"value": spell}
		case "aim":
			aims = append(aims, r)
			return aimReply
		}
		return nil
	}
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" }
	g.createCharacterAs("Mage")
	g.autoMore(1)

	// Learn Magic Missile.
	inv := g.inventory(2)
	book := packItem(t, inv, "First Spells")
	sp := g.answerOf(3, "SPELLS", map[string]any{"item": book.Ref.Map()})
	spell = sp.Spells[0].Value
	if r := g.command(4, "STUDY", map[string]any{"item": book.Ref.Map()}); !r.OK {
		t.Fatalf("STUDY: %+v", r)
	}
	g.afterWaits("Magic Missile learned", func() bool {
		s := g.answerOf(len(g.results)+100, "SPELLS", map[string]any{"item": book.Ref.Map()})
		return s.Spells[0].Usable
	})

	g.debug('n', [2]string{"Summon which monster", "shrieker mushroom patch"})
	var look proto.EventData
	g.afterWaits("the patch in LOOK", func() bool {
		look = g.answerOf(len(g.results)+100, "LOOK", map[string]any{"mode": "look"})
		for _, th := range look.Things {
			if th.Kind == "monster" && strings.Contains(th.Name, "shrieker mushroom patch") {
				return true
			}
		}
		return false
	})

	// The player's own grid comes first, then the patch, beside it.
	if me := look.Things[0]; me.Kind != "player" || me.Where != "0 N, 0 E" || !strings.HasPrefix(me.Text, "You are ") || me.Cell == nil {
		t.Errorf("first LOOK grid %+v", me)
	}
	patch := lookThing(t, look, "shrieker mushroom patch")
	if patch.Monster <= 0 || patch.Midx <= 0 || !patch.Targetable || patch.Cell == nil || patch.Char == "" {
		t.Errorf("the patch: %+v", patch)
	}
	if !strings.HasPrefix(patch.Text, "You see a shrieker mushroom patch (") || !strings.HasPrefix(patch.Health, "un") {
		t.Errorf("the patch's sentence %q, health %q", patch.Text, patch.Health)
	}
	rc := g.answerOf(10, "RECALL", map[string]any{"monster": patch.Monster})
	if rc.Title == nil || !strings.EqualFold(*rc.Title, "shrieker mushroom patch") || len(rc.Description) == 0 {
		t.Errorf("the patch's recall %+v", rc)
	}
	// The town's shop entrances are interesting terrain, with their own
	// descriptions.
	for _, th := range look.Things {
		if th.Kind != "terrain" {
			continue
		}
		if th.Feature == nil {
			t.Errorf("terrain without a feature: %+v", th)
			continue
		}
		rc := g.answerOf(11, "RECALL", map[string]any{"feature": *th.Feature})
		if rc.Title == nil || len(rc.Description) == 0 {
			t.Errorf("recall of %s: %+v", th.Name, rc)
		}
		break
	}
	// Target mode lists only what can be targeted.
	targets := g.answerOf(12, "LOOK", map[string]any{"mode": "target"})
	for _, th := range targets.Things {
		if th.Kind != "monster" || !th.Targetable {
			t.Errorf("target mode lists %+v", th)
		}
	}
	lookThing(t, targets, "shrieker mushroom patch")
	// A picked grid comes first, even when look mode would pass it by.
	listed := map[[2]int]bool{}
	for _, th := range look.Things {
		listed[[2]int{th.X, th.Y}] = true
	}
	me := look.Things[0]
	px, py := -1, -1
	for d := 0; d < 9 && px < 0; d++ {
		x, y := me.X+d%3-1, me.Y+d/3-1
		if x >= 1 && y >= 1 && !listed[[2]int{x, y}] {
			px, py = x, y
		}
	}
	if px < 0 {
		t.Fatal("every grid beside the player is in LOOK")
	}
	at := g.answerOf(13, "LOOK", map[string]any{"mode": "look", "x": px, "y": py})
	if len(at.Things) == 0 || at.Things[0].X != px || at.Things[0].Y != py || !at.Things[0].Free {
		t.Errorf("LOOK at %d, %d: %+v", px, py, at.Things[0])
	}

	// Without a target, 5 is refused, so the prompt cancels and no mana
	// is spent.
	if res := g.command(30, "SET_TARGET", map[string]any{"clear": true}); !res.OK {
		t.Fatalf("SET_TARGET clear: %+v", res)
	}
	n, from := len(aims), len(g.events)
	if res := g.command(31, "CAST", map[string]any{"spell": spell}); !res.OK {
		t.Fatalf("CAST: %+v", res)
	}
	g.waitFor("the aiming prompt, then a WAIT", func(*grid.Grid) bool {
		if len(aims) == n {
			return false
		}
		for _, e := range g.events[from:] {
			if e.Event == "WAIT" {
				return true
			}
		}
		return false
	})

	// Look mode goes to the client, and closes with its REPLY.
	g.key('l')
	r := g.heldScreen("look")
	if r.Mode != "look" || !r.Pathfind {
		t.Errorf("look request %+v", r)
	}
	g.send(proto.NewReply(r.ID, map[string]any{}))
	g.waitEvent("WAIT")

	// Target mode: SET_TARGET works while it is open.
	g.key('*')
	r = g.heldScreen("target")
	if r.Mode != "target" {
		t.Errorf("target request %+v", r)
	}
	if res := g.command(20, "SET_TARGET", map[string]any{"monster": patch.Midx}); !res.OK {
		t.Fatalf("SET_TARGET: %+v", res)
	}
	g.send(proto.NewReply(r.ID, map[string]any{}))
	g.waitText("Target Selected.")
	if tg := g.answerOf(21, "LOOK", map[string]any{"mode": "target"}).Target; tg == nil || tg.Monster != patch.Midx {
		t.Errorf("target after SET_TARGET %+v", tg)
	}
	// A grid with no monster cannot be targeted as one.
	if res := g.command(22, "SET_TARGET", map[string]any{"monster": 0}); res.OK {
		t.Errorf("SET_TARGET of no monster: %+v", res)
	}

	// The next target screen starts from the old target.
	g.key('*')
	r = g.heldScreen("target")
	if r.Target == nil || r.Target.Monster != patch.Midx {
		t.Errorf("second target request %+v", r)
	}
	if res := g.command(23, "SET_TARGET", map[string]any{"monster": patch.Midx}); !res.OK {
		t.Fatalf("SET_TARGET again: %+v", res)
	}
	g.send(proto.NewReply(r.ID, map[string]any{}))
	g.waitText("Target Selected.")

	// A new mage has two mana and fails a fifth of the time, and with
	// too little mana the game asks "Attempt it anyway?" on the screen,
	// which would stall the test. So raise the mage's level, for a lower
	// failure rate and more mana, and fill the mana, which a new level
	// does not. This happens now, while the game is idle: keys sent
	// while a cast plays out can be dropped.
	g.debug('x', [2]string{"Gain how much experience", "9999"})
	g.waitEvent("WAIT")
	g.cureAll()
	g.until("full mana", func(gr *grid.Grid) bool {
		cur, max := spOnScreen(gr)
		return max >= 10 && cur == max
	})

	// Cast at the target: the aiming prompt goes to the client, which
	// answers 5, for the target, until the patch is hurt or dead.
	before := len(aims)
	g.afterWaits("the patch hurt or killed", func() bool {
		l := g.answerOf(len(g.results)+100, "LOOK", map[string]any{"mode": "look"})
		for _, th := range l.Things {
			if th.Midx == patch.Midx {
				if !strings.HasPrefix(th.Health, "un") {
					return true
				}
				n := len(g.results) + 100
				// The game may still be busy with the last cast.
				if res := g.command(n, "CAST", map[string]any{"spell": spell}); !res.OK && res.Error != "busy" {
					t.Fatalf("CAST: %+v", res)
				}
				return false
			}
		}
		return true
	})
	if len(aims) == before {
		t.Fatal("no aiming prompt")
	}

}

// cureAll runs the debug command "Cure everything" (Ctrl+A, a), which
// also restores mana, and waits for the game to take the next command.
// The test must already have used a debug command, so the game does not
// ask first, and the game must be idle, or it may drop the keys.
func (g *game) cureAll() {
	g.t.Helper()
	g.key(proto.Ctrl('a'))
	g.until("the debug prompt", func(gr *grid.Grid) bool { return gr.Contains("Debug Command:") })
	from := len(g.events)
	g.key('a')
	g.waitFor("a WAIT after the cure", func(*grid.Grid) bool {
		for _, e := range g.events[from:] {
			if e.Event == "WAIT" {
				return true
			}
		}
		return false
	})
}

// spOnScreen reads the sidebar's mana line, "SP    1/   2", or returns
// -1, -1.
func spOnScreen(gr *grid.Grid) (cur, max int) {
	for y := 0; y < gr.Height; y++ {
		line := strings.TrimSpace(gr.Line(y))
		if rest, ok := strings.CutPrefix(line, "SP "); ok {
			a, b, ok := strings.Cut(rest, "/")
			if !ok {
				continue
			}
			f := strings.Fields(b)
			if len(f) == 0 {
				continue
			}
			c, err1 := strconv.Atoi(strings.TrimSpace(a))
			m, err2 := strconv.Atoi(f[0])
			if err1 == nil && err2 == nil {
				return c, m
			}
		}
	}
	return -1, -1
}

// TestMonsterList checks that MONSTER_LIST names the monster on each row
// of the monster list subwindow.
func TestMonsterList(t *testing.T) {
	d := enginetest.NewDirs(t)
	terms := []proto.TermSize{{Index: 0, Width: 80, Height: 24}, {Index: 3, Width: 40, Height: 12}}
	g := startTerms(t, dargs(d, "-n"), nil, []string{"events", "waits"}, terms)
	g.createCharacter()
	g.autoMore(1)
	g.debug('n', [2]string{"Summon which monster", "shrieker mushroom patch"})
	var ml proto.EventData
	g.afterWaits("the patch in the monster list", func() bool {
		ml = g.answerOf(len(g.results)+100, "MONSTER_LIST", nil)
		for _, r := range ml.Rows {
			if r != nil {
				rc := g.answerOf(len(g.results)+100, "RECALL", map[string]any{"monster": *r})
				if rc.Title != nil && strings.EqualFold(*rc.Title, "shrieker mushroom patch") {
					return true
				}
			}
		}
		return false
	})
	if len(ml.Rows) == 0 || ml.Rows[0] != nil {
		t.Errorf("the first row is a heading: %v", ml.Rows)
	}
}

package e2e

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// screenAnswers records the screen requests a game sees and answers each
// with reply, or closes it when reply is nil.
type screenAnswers struct {
	seen  []proto.RequestData
	reply func(proto.RequestData) map[string]any
}

func (a *screenAnswers) answer(r proto.RequestData) map[string]any {
	if r.Prompt != "screen" {
		return nil
	}
	a.seen = append(a.seen, r)
	if a.reply != nil {
		return a.reply(r)
	}
	return map[string]any{}
}

// waitScreen reads until the game has asked for n screens in all.
func (g *game) waitScreen(a *screenAnswers, n int) proto.RequestData {
	g.t.Helper()
	g.waitFor("a screen request", func(*grid.Grid) bool { return len(a.seen) >= n })
	return a.seen[n-1]
}

// waitEvent reads until an event named name arrives, and returns it.
func (g *game) waitEvent(name string) proto.EventData {
	g.t.Helper()
	from := len(g.events)
	var got proto.EventData
	g.waitFor("event "+name, func(*grid.Grid) bool {
		for _, e := range g.events[from:] {
			if e.Event == name {
				got = e
				return true
			}
		}
		return false
	})
	return got
}

// command sends a structured command and waits for its result.
func (g *game) command(id int, name string, args map[string]any) proto.EventData {
	g.t.Helper()
	g.send(proto.NewCommand(id, name, args))
	var got proto.EventData
	g.waitFor(name+" result", func(*grid.Grid) bool {
		r, ok := g.results[id]
		got = r
		return ok
	})
	return got
}

// TestScreenHandover claims the map screen and opens it three ways: by
// its key, by a keymap made in game, and from the Enter command menu.
func TestScreenHandover(t *testing.T) {
	d := enginetest.NewDirs(t)
	// A keymap from X to M, which the game loads from user.prf.
	prf := "keymap-act:M\nkeymap-input:0:X\n"
	if err := os.WriteFile(d.User+"/user.prf", []byte(prf), 0o644); err != nil {
		t.Fatal(err)
	}
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:map"})
	a := &screenAnswers{}
	g.answer = a.answer
	g.createCharacter()

	g.key('M')
	if r := g.waitScreen(a, 1); r.Name != "map" {
		t.Fatalf("screen request %+v, want map", r)
	}

	g.key('X')
	g.waitScreen(a, 2)

	// The Enter menu: Information, the fourth entry, then Full dungeon
	// map, the fifth.
	g.key(proto.KeyEnter)
	g.waitText("Information")
	for i := 0; i < 3; i++ {
		g.key(proto.KeyArrowDown)
	}
	g.key(proto.KeyEnter)
	g.waitText("Full dungeon map")
	for i := 0; i < 4; i++ {
		g.key(proto.KeyArrowDown)
	}
	g.key(proto.KeyEnter)
	g.waitScreen(a, 3)
}

// TestScreenClassicAndThen checks the two other replies: classic shows
// the text UI's own screen, and then runs a command.
func TestScreenClassicAndThen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:map"})
	a := &screenAnswers{reply: func(proto.RequestData) map[string]any {
		return map[string]any{"classic": true}
	}}
	g.answer = a.answer
	g.createCharacter()

	g.key('M')
	g.waitScreen(a, 1)
	// The classic map waits for a key with the whole level on screen.
	g.waitFor("the classic map", func(gr *grid.Grid) bool { return !gr.Contains("LEVEL") })
	g.key(proto.KeyEscape)
	g.waitFor("the town", inTown)

	a.reply = func(proto.RequestData) map[string]any {
		return map[string]any{"then": map[string]any{"id": 42, "cmd": "HOLD"}}
	}
	g.key('M')
	g.waitScreen(a, 2)
	g.waitFor("the HOLD result", func(*grid.Grid) bool { _, ok := g.results[42]; return ok })
	if r := g.results[42]; !r.OK {
		t.Errorf("HOLD after the screen: %+v", r)
	}
}

// TestWaits checks WAIT in screen mode: one at the splash screen, then
// one each time the game waits for a command with nothing open.
func TestWaits(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits"})
	if w := g.waitEvent("WAIT"); w.Context != "splash" {
		t.Fatalf("first WAIT %+v, want the splash screen", w)
	}
	g.createCharacter()
	if w := g.waitEvent("WAIT"); w.Context != "game" {
		t.Fatalf("WAIT %+v in the town, want game", w)
	}

	// No WAIT while the C inventory is open, and one after it closes.
	from := len(g.events)
	g.key('i')
	g.waitText("(Inven:")
	g.measure(time.Now(), 200*time.Millisecond)
	for _, e := range g.events[from:] {
		if e.Event == "WAIT" {
			t.Fatalf("WAIT while the inventory was open")
		}
	}
	g.key(proto.KeyEscape)
	g.waitEvent("WAIT")
}

// TestSaveFilesAndSave lists saves before the game starts, saves with
// SAVE, and finds the character listed as current.
func TestSaveFilesAndSave(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits"})
	g.waitEvent("WAIT")
	if r := g.command(1, "SAVEFILES", nil); !r.OK {
		t.Fatalf("SAVEFILES at the splash screen: %+v", r)
	}
	saves := g.lastEvent("SAVEFILES")
	if len(saves.Saves) != 0 {
		t.Errorf("saves in an empty folder: %+v", saves.Saves)
	}

	g.createCharacter()
	g.waitEvent("WAIT")
	if r := g.command(2, "SAVE", nil); !r.OK {
		t.Fatalf("SAVE: %+v", r)
	}
	requireSavefile(t, d)
	g.command(3, "SAVEFILES", nil)
	saves = g.lastEvent("SAVEFILES")
	if len(saves.Saves) != 1 || saves.Saves[0].Name != "tester" || !saves.Saves[0].Current || saves.Saves[0].Dead {
		t.Fatalf("saves after SAVE: %+v", saves.Saves)
	}
	if !strings.Contains(saves.Saves[0].Description, "Tester") {
		t.Errorf("description %q does not name the character", saves.Saves[0].Description)
	}

	// A snapshot with only some parts.
	g.command(4, "SNAPSHOT", map[string]any{"parts": []string{"player"}})
	if g.snap.Player == nil || g.snap.Map != nil || g.snap.Inventory != nil {
		t.Errorf("snapshot of the player only: player %v, map %v, inventory %v",
			g.snap.Player != nil, g.snap.Map != nil, g.snap.Inventory)
	}
}

// lastEvent returns the last event named name.
func (g *game) lastEvent(name string) proto.EventData {
	g.t.Helper()
	for i := len(g.events) - 1; i >= 0; i-- {
		if g.events[i].Event == name {
			return g.events[i]
		}
	}
	g.t.Fatalf("no %s event", name)
	return proto.EventData{}
}

// TestDeathScreen retires a character with the death screen claimed: the
// engine hands it over, then saves the death and exits.
func TestDeathScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:death"})
	a := &screenAnswers{}
	g.answer = a.answer
	g.createCharacter()

	g.key('Q')
	g.waitText("Do you really want to retire?")
	g.key('y')
	g.waitText("'@' sign")
	g.key('@')
	// Clear "-more-" prompts, such as the note that a retired character
	// is not scored, until the death screen is handed over.
	for len(a.seen) == 0 {
		g.waitFor("-more- or the death screen", func(gr *grid.Grid) bool {
			return len(a.seen) > 0 || gr.Contains("-more-")
		})
		if len(a.seen) == 0 {
			g.key(proto.KeyEnter)
		}
	}
	if r := g.waitScreen(a, 1); r.Name != "death" {
		t.Fatalf("screen request %+v, want death", r)
	}
	if reason := g.waitBye(); reason != "exit" {
		t.Errorf("BYE %q after the death screen, want exit", reason)
	}
	g.waitExit()

	// The save list, asked at the next start's splash screen, shows the
	// character as dead.
	g = startWith(t, dargs(d), []string{"events", "waits"})
	g.waitEvent("WAIT")
	g.command(1, "SAVEFILES", nil)
	if saves := g.lastEvent("SAVEFILES").Saves; len(saves) != 1 || !saves[0].Dead {
		t.Fatalf("saves after the death: %+v", saves)
	}
}

// TestBorgSwitch starts the borg with BORG, lets it play, stops it, and
// checks that the player's options came back.
func TestBorgSwitch(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits"})
	g.createCharacter()
	g.waitEvent("WAIT")

	if r := g.command(1, "BORG", map[string]any{"on": true}); !r.OK {
		t.Fatalf("BORG on: %+v", r)
	}
	if e := g.waitEvent("BORG"); !e.On {
		t.Fatalf("BORG event %+v, want on", e)
	}
	// Let it play a little.
	deadline := time.Now().Add(2 * time.Second)
	g.waitFor("two seconds of borg play", func(*grid.Grid) bool { return time.Now().After(deadline) })

	if r := g.command(2, "BORG", map[string]any{"on": false}); !r.OK {
		t.Fatalf("BORG off: %+v", r)
	}
	if e := g.waitEvent("BORG"); e.On {
		t.Fatalf("BORG event %+v, want off", e)
	}
	g.waitEvent("WAIT")

	// The borg turns pickup_always on; it must be off again. The first
	// key after the borg stops must reach the game.
	g.key('=')
	g.waitText("User interface options")
	g.key('a')
	g.waitFor("the interface options", func(gr *grid.Grid) bool {
		for y := 0; y < gr.Height; y++ {
			if line := gr.Line(y); strings.Contains(line, "pickup_always") {
				return strings.Contains(line, ": no")
			}
		}
		return false
	})
	if !slices.ContainsFunc(g.events, func(e proto.EventData) bool { return e.Event == "BORG" && e.On }) {
		t.Error("no BORG on event")
	}
}

package e2e

import (
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// TestTomb retires a character with the death screen claimed and reads
// TOMB while the death screen is open: it has what the game's own
// tombstone shows, marked as a retirement.
func TestTomb(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:death"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" }
	g.createCharacter()
	// No -more- waits before the death screen.
	g.autoMore(3)

	if tomb := g.answerOf(1, "TOMB", nil).Tomb; tomb == nil || tomb.Dead || tomb.Retired {
		t.Fatalf("TOMB of a living character: %+v", tomb)
	}

	g.key('Q')
	g.waitText("Do you really want to retire?")
	g.key('y')
	g.waitText("'@' sign")
	g.key('@')
	r := g.heldScreen("death")

	tomb := g.answerOf(2, "TOMB", nil).Tomb
	if tomb == nil {
		t.Fatal("TOMB gave no tombstone")
	}
	if tomb.Name != "Tester" || tomb.Class != "Warrior" || tomb.Title == "" || tomb.Level != 1 {
		t.Errorf("TOMB %+v, want Tester, a level 1 Warrior with a title", tomb)
	}
	if !tomb.Dead || !tomb.Retired || tomb.Winner || tomb.DiedFrom != "Retiring" {
		t.Errorf("TOMB %+v, want a retired, not winning, character", tomb)
	}
	if tomb.Depth != 0 || tomb.Gold <= 0 || len(tomb.Date) != 24 {
		t.Errorf("TOMB %+v, want depth 0, some gold, and a ctime date", tomb)
	}

	g.send(proto.NewReply(r.ID, map[string]any{}))
	if reason := g.waitBye(); reason != "exit" {
		t.Errorf("BYE %q after the death screen, want exit", reason)
	}
}

// commandIndex finds a command by its list's name and its own, and
// returns both indexes and the entry.
func commandIndex(t *testing.T, menu *proto.CommandMenu, list, name string) (int, int, proto.CommandEntry) {
	t.Helper()
	for li, l := range menu.Lists {
		if l.Name != list {
			continue
		}
		for ci, c := range l.Commands {
			if c.Name == name {
				return li, ci, c
			}
		}
	}
	t.Fatalf("no command %q in list %q", name, list)
	return 0, 0, proto.CommandEntry{}
}

// TestCommandMenu claims the command menu and checks COMMANDS in both
// keysets, and that a command chosen in the native menu runs, as one
// chosen in the game's own menu does: Full dungeon map opens the map
// screen, also claimed. It also checks a reply that chooses nothing, one
// that chooses a submenu's entry, and the classic menu.
func TestCommandMenu(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:commands", "screen:map"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" }
	g.createCharacter()
	g.autoMore(1)
	id := 10

	for _, keyset := range []string{"original", "roguelike"} {
		if keyset == "roguelike" {
			id++
			if r := g.command(id, "SET_OPTION", map[string]any{"name": "rogue_like_commands", "value": true}); !r.OK {
				t.Fatalf("SET_OPTION rogue_like_commands: %+v", r)
			}
		}
		g.key(proto.KeyEnter)
		r := g.heldScreen("commands")

		id++
		menu := g.answerOf(id, "COMMANDS", nil).Commands
		if menu == nil || menu.Keyset != keyset {
			t.Fatalf("COMMANDS in the %s keyset: %+v", keyset, menu)
		}
		var groups []string
		for _, l := range menu.Lists {
			if l.Level == 0 {
				groups = append(groups, l.Name)
			}
		}
		if len(groups) < 5 || groups[0] != "Items" || groups[3] != "Information" {
			t.Errorf("groups %q, want Items first and Information fourth", groups)
		}
		// Look around is l in the original keys and x in the roguelike.
		_, _, look := commandIndex(t, menu, "Action commands", "Look around")
		if want := map[string]string{"original": "l", "roguelike": "x"}[keyset]; look.Key != want {
			t.Errorf("Look around's key is %q in the %s keyset, want %q", look.Key, keyset, want)
		}
		// Debug mode commands opens the Debug list, whose Items opens
		// DbgObj.
		_, _, dbg := commandIndex(t, menu, "Hidden", "Debug mode commands")
		if dbg.Submenu == nil || menu.Lists[*dbg.Submenu].Name != "Debug" || menu.Lists[*dbg.Submenu].Level != 1 {
			t.Fatalf("Debug mode commands opens %v", dbg.Submenu)
		}
		_, _, objs := commandIndex(t, menu, "Debug", "Items")
		if objs.Submenu == nil || menu.Lists[*objs.Submenu].Name != "DbgObj" {
			t.Fatalf("Debug's Items opens %v", objs.Submenu)
		}

		li, ci, _ := commandIndex(t, menu, "Information", "Full dungeon map")
		g.send(proto.NewReply(r.ID, map[string]any{"list": li, "command": ci}))
		m := g.heldScreen("map")
		g.send(proto.NewReply(m.ID, map[string]any{}))
		g.afterWaits("back at the prompt", func() bool { return g.lastEvent("WAIT").Context == "game" })
	}

	// A reply that chooses nothing, and one that names a submenu's
	// placeholder, run nothing; the game waits for a command again.
	for _, reply := range []map[string]any{{}, {"list": 5, "command": 0}} {
		from := len(g.events)
		g.key(proto.KeyEnter)
		r := g.heldScreen("commands")
		if reply["list"] != nil {
			id++
			menu := g.answerOf(id, "COMMANDS", nil).Commands
			li, ci, _ := commandIndex(t, menu, "Hidden", "Debug mode commands")
			reply = map[string]any{"list": li, "command": ci}
		}
		g.send(proto.NewReply(r.ID, reply))
		g.waitFor("a WAIT", func(*grid.Grid) bool {
			for _, e := range g.events[from:] {
				if e.Event == "WAIT" && e.Context == "game" {
					return true
				}
			}
			return false
		})
		if len(g.held) > 0 {
			t.Fatalf("reply %v opened %+v", reply, g.held)
		}
	}

	// Classic: the game's own menu.
	g.key(proto.KeyEnter)
	r := g.heldScreen("commands")
	g.send(proto.NewReply(r.ID, map[string]any{"classic": true}))
	g.until("the game's own menu", func(gr *grid.Grid) bool { return gr.Contains("Action commands") })
	g.key(proto.KeyEscape)
}

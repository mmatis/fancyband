package e2e

import (
	"errors"
	"os"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// TestDisconnectInScreens drops the connection while each native screen
// or prompt is open: the engine has sent its REQUEST and waits for the
// client's REPLY. Each time the engine must exit cleanly with a save that
// loads, as it does at a classic screen.
func TestDisconnectInScreens(t *testing.T) {
	// firstItem answers an item prompt with the first item it offers.
	firstItem := func(r proto.RequestData) map[string]any {
		if r.Prompt != "item" {
			return nil
		}
		items, err := r.Items()
		if err != nil || len(items) == 0 {
			return nil
		}
		return map[string]any{"item": items[0].Ref.Map()}
	}
	cases := []struct {
		name string
		// caps are claimed besides "events" and "waits".
		caps []string
		// class is the new character's class.
		class string
		// answer answers the requests that come before the one held.
		answer func(proto.RequestData) map[string]any
		// hold picks the request to leave open.
		prompt, screen string
		// open opens it.
		open func(g *game)
	}{
		{name: "options", caps: []string{"screen:options"}, screen: "options", open: keys('=')},
		{name: "commands", caps: []string{"screen:commands"}, screen: "commands", open: keys(proto.KeyEnter)},
		{name: "inventory", caps: []string{"screen:inventory"}, screen: "inventory", open: keys('i')},
		{name: "knowledge", caps: []string{"screen:knowledge"}, screen: "knowledge", open: keys('~')},
		{name: "character", caps: []string{"screen:character"}, screen: "character", open: keys('C')},
		{name: "messages", caps: []string{"screen:messages"}, screen: "messages", open: keys(proto.Ctrl('p'))},
		{name: "help", caps: []string{"screen:help"}, screen: "help", open: keys('?')},
		{name: "map", caps: []string{"screen:map"}, screen: "map", open: keys('M')},
		{name: "look", caps: []string{"screen:look"}, screen: "look", open: keys('l')},
		{name: "target", caps: []string{"screen:target"}, screen: "target", open: keys('*')},
		{name: "item", caps: []string{"prompt:item"}, prompt: "item", open: keys('q')},
		{name: "check", caps: []string{"prompt:check"}, prompt: "check", open: keys('Q')},
		{name: "direction", caps: []string{"prompt:direction"}, prompt: "direction", open: func(g *game) {
			g.waitEvent("WAIT")
			g.send(proto.NewCommand(1, "WALK", nil))
		}},
		// Throwing asks for the item, then where to aim.
		{name: "aim", caps: []string{"prompt:item", "prompt:aim"}, answer: firstItem, prompt: "aim", open: keys('v')},
		// A priest starts with two potions, so dropping them asks how many.
		{name: "quantity", caps: []string{"prompt:quantity"}, class: "Priest", prompt: "quantity", open: func(g *game) {
			cure := packItem(g.t, g.inventory(1), "Cure Serious")
			g.send(proto.NewCommand(2, "DROP", map[string]any{"item": cure.Ref.Map()}))
		}},
		// Browsing asks for the book, then shows its spells.
		{name: "spell", caps: []string{"prompt:item", "prompt:spell", "prompt:spell_from_book"}, class: "Mage",
			answer: firstItem, prompt: "spell_from_book", open: keys('b')},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := enginetest.NewDirs(t)
			g := startWith(t, dargs(d, "-n"), append([]string{"events", "waits"}, c.caps...))
			g.answer = c.answer
			g.hold = func(r proto.RequestData) bool {
				if c.screen != "" {
					return r.Prompt == "screen" && r.Name == c.screen
				}
				return r.Prompt == c.prompt
			}
			class := c.class
			if class == "" {
				class = "Warrior"
			}
			g.createCharacterAs(class)
			c.open(g)
			g.until("the held request", func(*grid.Grid) bool { return len(g.held) > 0 })
			g.s.Close()
			g.waitExit()
			requireCleanSave(t, d)
		})
	}
}

// keys returns an opener that types ks.
func keys(ks ...uint32) func(g *game) {
	return func(g *game) {
		for _, k := range ks {
			g.key(k)
		}
	}
}

// TestDisconnectInStoreScreen walks into the General Store with the
// native store screen claimed and drops the connection there.
func TestDisconnectInStoreScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "snapshots", "screen:store", "prompt:check"})
	g.createCharacter()
	g.autoMore(1)
	// Townspeople can kill a new character on the walk; with this on,
	// the game asks before death, and the answer is no.
	if r := g.command(2, "SET_OPTION", map[string]any{"name": "cheat_live", "value": true}); !r.OK {
		t.Fatalf("cheat_live: %+v", r)
	}
	id := 2
	g.walkIntoStore("General Store", &id)
	g.s.Close()
	g.waitExit()
	requireCleanSave(t, d)
}

// TestDisconnectInBirthScreen drops the connection while the native
// character creation screen is open. As in the classic one, the engine
// exits without saving.
func TestDisconnectInBirthScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:birth"})
	birth := g.holdScreen("birth")
	g.waitText("[Press any key to continue]")
	g.key('x')
	birth(1)
	g.s.Close()
	g.waitExit()
	requireNoSave(t, d)
}

// TestDisconnectAtSplash drops the connection while the engine waits at
// the splash screen, where the native splash screen draws over it.
func TestDisconnectAtSplash(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits"})
	g.waitText("[Press any key to continue]")
	g.s.Close()
	g.waitExit()
	requireNoSave(t, d)
}

// TestDisconnectInDeathScreen retires a character and drops the
// connection while the native death screen is open. The engine exits
// cleanly, with no panic save.
func TestDisconnectInDeathScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:death"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" }
	g.createCharacter()
	g.autoMore(3)
	g.key('Q')
	g.waitText("Do you really want to retire?")
	g.key('y')
	g.waitText("'@' sign")
	g.key('@')
	g.heldScreen("death")
	g.s.Close()
	g.waitExit()
	if panics, _ := os.ReadDir(d.Panic); len(panics) > 0 {
		t.Errorf("the engine left a panic save: %s", panics[0].Name())
	}
}

// requireNoSave checks that the engine left neither a save nor a panic
// save.
func requireNoSave(t *testing.T, d enginetest.Dirs) {
	t.Helper()
	if _, err := os.Stat(d.Savefile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("savefile: %v", err)
	}
	if panics, _ := os.ReadDir(d.Panic); len(panics) > 0 {
		t.Errorf("the engine left a panic save: %s", panics[0].Name())
	}
}

// TestClassicStoreWithNativePrompts walks into a shop with the classic
// store screen and the native prompts claimed: examining an item in the
// classic store asks through the native item prompt, cancelling it returns
// to the store, and leaving the store returns to the game. It examines
// because the shop it reaches may want nothing the character has to sell,
// and then selling asks nothing.
func TestClassicStoreWithNativePrompts(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "snapshots", "prompt:item"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "item" }
	g.createCharacter()
	g.autoMore(1)
	// Townspeople can kill a new character on the walk; with this on, the
	// game asks before death, and the answer is no. One in the way stops a
	// walk, so they go first.
	if r := g.command(1, "SET_OPTION", map[string]any{"name": "cheat_live", "value": true}); !r.OK {
		t.Fatalf("cheat_live: %+v", r)
	}
	g.debug('z', [2]string{"Zap within what distance", "100"})
	g.waitEvent("WAIT")

	entered := func() bool {
		for _, e := range g.events {
			if e.Event == "ENTER_STORE" {
				return true
			}
		}
		return false
	}
	// Townspeople who come into view stop a walk, so it tries the shops in
	// turn, a few times over.
	id := 1
	m := g.snap.Map
	for pass := 0; pass < 10 && !entered(); pass++ {
		for y := 0; y < m.Height && !entered(); y++ {
			for x := 0; x < m.Width && !entered(); x++ {
				if m.At(x, y) != 's' {
					continue
				}
				from := len(g.events)
				id++
				g.command(id, "PATHFIND", map[string]any{"point": map[string]any{"x": x, "y": y}})
				g.until("a wait or a shop after the walk", func(*grid.Grid) bool {
					for _, e := range g.events[from:] {
						if e.Event == "WAIT" || e.Event == "ENTER_STORE" {
							return true
						}
					}
					return false
				})
			}
		}
	}
	if !entered() {
		t.Fatal("could not reach any shop")
	}

	g.key('I')
	g.until("the native item prompt", func(*grid.Grid) bool { return len(g.held) > 0 })
	g.send(proto.NewReply(g.held[0].ID, map[string]any{}))
	g.key(proto.KeyEscape)
	g.waitEvent("LEAVE_STORE")
	g.s.Close()
	g.waitExit()
	requireCleanSave(t, d)
}

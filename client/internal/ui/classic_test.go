package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/settings"
)

// TestClassicCaps checks each native screen's switch: turned off, the
// client stops claiming that screen's capabilities and no other's.
func TestClassicCaps(t *testing.T) {
	all := New(Config{Width: 80, Height: 24}).Caps()
	for _, ns := range nativeScreens {
		m := New(Config{Width: 80, Height: 24})
		m.settings.Classic = []string{ns.name}
		caps := m.Caps()
		for _, c := range all {
			if want := !slices.Contains(ns.caps, c); slices.Contains(caps, c) != want {
				t.Errorf("%s off: claims %q is %v, want %v", ns.name, c, !want, want)
			}
		}
	}
}

// TestClassicScreens turns every native screen off and plays through the
// client: character creation, the splash screen, each screen a key opens,
// the prompts, and the death screen are the game's classic ones.
func TestClassicScreens(t *testing.T) {
	d := enginetest.NewDirs(t)
	sp := filepath.Join(d.User, settings.FileName)
	var s settings.Settings
	for _, ns := range nativeScreens {
		s.Classic = append(s.Classic, ns.name)
	}
	if err := s.Save(sp); err != nil {
		t.Fatal(err)
	}
	p, m := startPlayerWith(t, 80, 24, false, d.Args("-n"), sp)
	for _, ns := range nativeScreens {
		for _, c := range ns.caps {
			if slices.Contains(m.Caps(), c) {
				t.Fatalf("with every native screen off the client claims %q", c)
			}
		}
	}

	// The classic splash screen, under the start menu.
	p.waitText("[Press any key to continue]")
	p.choose(startMenuID, "New character")

	// Classic character creation.
	p.waitText("a) Human")
	p.page.Press("enter")
	p.waitText("Warrior")
	p.page.Press("enter")
	p.waitText("Point-based")
	p.page.Press("enter")
	p.waitText("'Enter' to accept")
	p.page.Press("enter")
	p.waitText("Enter a name for your character")
	p.page.Type("Tester")
	p.page.Press("enter")
	p.waitText("Accept character history? [y/n]")
	p.page.Press("y")
	p.waitText("any other key to continue")
	p.page.Press("enter")
	p.waitFor("the town", inTownScreen)

	topLine := func(s string) string { return strings.SplitN(s, "\n", 2)[0] }
	for _, c := range []struct {
		key, what string
		shows     func(screen string) bool
		close     string
	}{
		{"i", "the classic inventory", has("(Inven:"), "escape"},
		{"e", "the classic equipment", has("(Equip:"), "escape"},
		{"C", "the classic character sheet", has("Self  RB  CB  EB"), "escape"},
		{"~", "the classic knowledge menu", has("Display current knowledge"), "escape"},
		{"ctrl+p", "the classic message history", has("Message recall"), "escape"},
		{"?", "the classic help", has("Help file 'index.txt'"), "escape"},
		{"M", "the classic level map", func(s string) bool { return strings.HasPrefix(topLine(s), "+---") }, "escape"},
		{"=", "the classic options menu", has("Options Menu"), "escape"},
		{"enter", "the classic command menu", has("Action commands"), "escape"},
		{"l", "classic look mode", func(s string) bool { return strings.Contains(topLine(s), ", 0 N, 0 E") }, "escape"},
		{"q", "the classic item prompt", has("Quaff which potion?"), "escape"},
		{"Q", "the classic yes-or-no prompt", has("Do you really want to retire?"), "n"},
		{";", "the classic direction prompt", has("Direction or <click>"), "escape"},
	} {
		p.page.Press(c.key)
		p.waitFor(c.what, c.shows)
		p.page.Press(c.close)
		p.waitFor("the town after "+c.what, func(s string) bool { return !c.shows(s) && inTownScreen(s) })
		p.waitFor("the game to wait", func(string) bool { return m.waiting })
	}

	// Retiring ends at the classic death screen.
	p.page.Press("Q")
	p.waitText("Do you really want to retire?")
	p.page.Press("y")
	p.waitText("'@' sign")
	p.page.Press("@")
	// Messages wait with -more- before it. Each is answered once, when
	// the screen has changed since the last answer, so no extra key
	// reaches the death screen's menu.
	answered := ""
	p.waitFor("the classic death screen", func(s string) bool {
		if strings.Contains(s, "-more-") && s != answered {
			answered = s
			p.page.Press("enter")
		}
		return strings.Contains(s, "View scores")
	})
}

// classicStep presses key, waits for a classic screen that native, the
// id of the native screen it replaces, must not cover, and closes it with
// closeKey.
func (p *player) classicStep(m *Model, key, what, native string, shows func(string) bool, closeKey string) {
	p.t.Helper()
	p.page.Press(key)
	p.waitFor(what, shows)
	if native != "" && p.page.GetByID(native).Count() > 0 {
		p.t.Fatalf("%s: the native %s opened:\n%s", what, native, p.page.Screen())
	}
	p.page.Press(closeKey)
	p.waitFor("the town after "+what, func(s string) bool { return !shows(s) && inTownScreen(s) })
	p.waitFor("the game to wait", func(string) bool { return m.waiting })
}

// birthDown is birth, with the class down rows below Warrior.
func (p *player) birthDown(name string, down int) {
	p.t.Helper()
	p.page.Expect(p.page.GetByID(birthID)).ToBeVisible()
	p.page.Expect(p.page.GetByID(birthRacesID)).ToBeVisible()
	p.page.Press("home")
	p.page.Press("enter")
	p.page.Expect(p.page.GetByID(birthClassesID)).ToBeVisible()
	p.page.Press("home")
	for i := 0; i < down; i++ {
		p.page.Press("down")
	}
	p.page.Press("enter")
	p.waitText("Points left")
	p.page.Press("enter")
	p.waitText("What is your character's name?")
	p.page.Type(name)
	p.page.Press("enter")
	p.waitText("Your character's history:")
	p.page.Press("enter")
	p.waitText("Start over")
	p.page.Press("enter")
	p.waitFor("the town", inTownScreen)
}

// TestClassicEach turns off one native screen at a time, so the classic
// screen runs beside every other native one, and checks that the classic
// one opens and hands back to a game that still takes keys. The store is
// in the e2e package's TestClassicStoreWithNativePrompts, which can walk
// to a shop.
func TestClassicEach(t *testing.T) {
	cases := []struct {
		name string
		// down picks the class, this many rows below Warrior.
		down int
		// skipBirth means run makes the character itself.
		skipBirth bool
		run       func(p *player, m *Model)
	}{
		{name: splashName, skipBirth: true, run: func(p *player, m *Model) {
			p.waitText("[Press any key to continue]")
			p.createCharacter()
		}},
		{name: "options", run: func(p *player, m *Model) {
			p.classicStep(m, "=", "the classic options menu", optionsID, has("Options Menu"), "escape")
		}},
		{name: "death", run: func(p *player, m *Model) {
			p.page.Press("Q")
			p.page.Expect(p.page.GetByID(checkID)).ToBeVisible()
			p.page.Press("y")
			p.waitText("'@' sign")
			p.page.Press("@")
			answered := ""
			p.waitFor("the classic death screen", func(s string) bool {
				if strings.Contains(s, "-more-") && s != answered {
					answered = s
					p.page.Press("enter")
				}
				return strings.Contains(s, "View scores")
			})
			if p.page.GetByID(deathMenuID).Count() > 0 {
				t.Errorf("the native death menu opened:\n%s", p.page.Screen())
			}
		}},
		{name: "commands", run: func(p *player, m *Model) {
			p.classicStep(m, "enter", "the classic command menu", commandMenuID, has("Action commands"), "escape")
		}},
		{name: "inventory", run: func(p *player, m *Model) {
			p.classicStep(m, "i", "the classic inventory", inventoryID, has("(Inven:"), "escape")
			p.classicStep(m, "e", "the classic equipment", inventoryID, has("(Equip:"), "escape")
		}},
		{name: "birth", skipBirth: true, run: func(p *player, m *Model) {
			p.choose(startMenuID, "New character")
			p.waitText("a) Human")
			p.page.Press("enter")
			p.waitText("Warrior")
			p.page.Press("enter")
			p.waitText("Point-based")
			p.page.Press("enter")
			p.waitText("'Enter' to accept")
			p.page.Press("enter")
			p.waitText("Enter a name for your character")
			p.page.Type("Tester")
			p.page.Press("enter")
			p.waitText("Accept character history? [y/n]")
			p.page.Press("y")
			p.waitText("any other key to continue")
			p.page.Press("enter")
			p.waitFor("the town", inTownScreen)
		}},
		{name: "choices", down: 1, run: func(p *player, m *Model) {
			p.classicStep(m, "E", "the classic item prompt", itemPromptID, has("Eat which food?"), "escape")
			// Browsing asks for the book, then lists its spells.
			p.page.Press("b")
			p.waitText("Browse which book?")
			p.page.Press("enter")
			p.waitText("Magic Missile")
			if p.page.GetByID(spellsID).Count() > 0 {
				t.Errorf("the native spell list opened:\n%s", p.page.Screen())
			}
			p.page.Press("escape")
			p.waitFor("the town after browsing", func(s string) bool {
				if strings.Contains(s, "Browse which book?") || strings.Contains(s, "Magic Missile") {
					p.page.Press("escape")
					return false
				}
				return inTownScreen(s)
			})
			p.waitFor("the game to wait", func(string) bool { return m.waiting })
			// A native aiming prompt follows the classic item prompt: the
			// aiming screen, or with nothing to aim at, the direction dialog.
			p.page.Press("v")
			p.waitText("Throw which item?")
			p.page.Press("enter")
			p.waitFor("the native aiming prompt", func(string) bool {
				return p.page.GetByID(aimID).Count()+p.page.GetByID(directionID).Count() > 0
			})
			p.page.Press("escape")
			p.gone(aimID)
			p.gone(directionID)
		}},
		{name: "dialogs", down: 3, run: func(p *player, m *Model) {
			p.classicStep(m, "Q", "the classic yes-or-no prompt", checkID, has("Do you really want to retire?"), "n")
			p.classicStep(m, ";", "the classic direction prompt", directionID, has("Direction or <click>"), "escape")
			// A classic quantity prompt follows a native item prompt.
			p.page.Press("d")
			p.page.Expect(p.page.GetByID(itemPromptID)).ToBeVisible()
			// A priest starts with two of these.
			p.waitText("Cure Serious")
			p.page.Press(letterOf(t, p.page.Screen(), "Cure Serious"))
			p.waitText("Quantity")
			if p.page.GetByID(quantityID).Count() > 0 {
				t.Errorf("the native quantity dialog opened:\n%s", p.page.Screen())
			}
			p.page.Press("escape")
			p.gone(itemPromptID)
		}},
		{name: "look", run: func(p *player, m *Model) {
			top := func(s string) string { return strings.SplitN(s, "\n", 2)[0] }
			p.classicStep(m, "l", "classic look mode", lookID, func(s string) bool { return strings.Contains(top(s), ", 0 N, 0 E") }, "escape")
			// A classic aiming prompt follows a native item prompt.
			p.page.Press("v")
			p.page.Expect(p.page.GetByID(itemPromptID)).ToBeVisible()
			p.page.Press("enter")
			p.waitText("to target")
			if p.page.GetByID(aimID).Count() > 0 {
				t.Errorf("the native aiming screen opened:\n%s", p.page.Screen())
			}
			p.page.Press("escape")
		}},
		{name: "knowledge", run: func(p *player, m *Model) {
			p.classicStep(m, "~", "the classic knowledge menu", knowledgeID, has("Display current knowledge"), "escape")
		}},
		{name: compareName, run: func(p *player, m *Model) {
			p.open("~", knowledgeID)
			p.waitText("Equipment comparison")
			// Tab to the comparison's own tab, the last one.
			for i := 0; i < 12 && !strings.Contains(p.page.Screen(), "The game's own screen compares"); i++ {
				p.page.Press("tab")
				time.Sleep(200 * time.Millisecond)
			}
			p.waitText("The game's own screen compares")
			p.page.Press("enter")
			p.waitText("ESC to exit")
			if p.page.GetByID(compareID).Count() > 0 {
				t.Errorf("the native comparison opened:\n%s", p.page.Screen())
			}
			p.page.Press("escape")
			p.page.Expect(p.page.GetByID(knowledgeID)).ToBeVisible()
			p.page.Press("escape")
			p.gone(knowledgeID)
		}},
		{name: "character", run: func(p *player, m *Model) {
			p.classicStep(m, "C", "the classic character sheet", characterID, has("Self  RB  CB  EB"), "escape")
		}},
		{name: "messages", run: func(p *player, m *Model) {
			p.classicStep(m, "ctrl+p", "the classic message history", messagesID, has("Message recall"), "escape")
		}},
		{name: "help", run: func(p *player, m *Model) {
			p.classicStep(m, "?", "the classic help", helpID, has("Help file 'index.txt'"), "escape")
		}},
		{name: "map", run: func(p *player, m *Model) {
			p.classicStep(m, "M", "the classic level map", levelMapID, has("Hit any key to continue"), "escape")
		}},
		{name: "lists", run: func(p *player, m *Model) {
			p.classicStep(m, "[", "the classic monster list", visibleID, has("You can see"), "escape")
		}},
	}
	seen := map[string]bool{"store": true}
	for _, c := range cases {
		seen[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			d := enginetest.NewDirs(t)
			sp := filepath.Join(d.User, settings.FileName)
			s := settings.Settings{Classic: []string{c.name}}
			if err := s.Save(sp); err != nil {
				t.Fatal(err)
			}
			p, m := startPlayerWith(t, 110, 32, false, d.Args("-n"), sp)
			switch {
			case c.skipBirth:
			case c.down > 0:
				p.choose(startMenuID, "New character")
				p.birthDown("Tester", c.down)
			default:
				p.createCharacter()
			}
			if !c.skipBirth {
				p.waitFor("the game to wait", func(string) bool { return m.waiting })
			}
			c.run(p, m)
			if c.name == "death" {
				return
			}
			// The game still takes keys, and another native screen opens.
			p.waitFor("the town at the end", inTownScreen)
			p.waitFor("the game to wait", func(string) bool { return m.waiting })
			key, id := "i", inventoryID
			if c.name == "inventory" {
				key, id = "C", characterID
			}
			p.open(key, id)
			p.page.Press("escape")
			p.gone(id)
		})
	}
	for _, ns := range nativeScreens {
		if !seen[ns.name] {
			t.Errorf("no case for the native screen %q", ns.name)
		}
	}
}

// has returns a check for screens that contain text.
func has(text string) func(string) bool {
	return func(s string) bool { return strings.Contains(s, text) }
}

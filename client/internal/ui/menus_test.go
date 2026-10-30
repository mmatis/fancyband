package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/settings"
)

// openMenu opens the game menu with Escape. It waits out the guard that
// keeps an Escape that ends a run from opening the menu.
func (p *player) openMenu() {
	p.t.Helper()
	time.Sleep(menuGuard)
	p.page.Press("escape")
	p.page.Expect(p.page.GetByID(gameMenuID)).ToBeVisible()
}

// choose selects the entry labelled label in the list with id, then
// presses Enter.
func (p *player) choose(id, label string) {
	p.t.Helper()
	list := p.page.GetByID(id)
	p.page.Expect(list).ToBeVisible()
	prev := ""
	for i := 0; i < 12; i++ {
		// The program draws on its own goroutine; wait for the move.
		var v string
		deadline := time.Now().Add(2 * time.Second)
		for {
			if list.Count() == 1 {
				v = list.Node().Value
			}
			if v != prev || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if strings.TrimSpace(v) == label {
			p.page.Press("enter")
			return
		}
		prev = v
		p.page.Press("down")
	}
	p.t.Fatalf("no %q in %s:\n%s", label, id, p.page.Screen())
}

// inTownAs waits for the town, then checks the character's name on the
// character sheet, since the sidebar has no room for it at 80 columns.
func (p *player) inTownAs(name string) {
	p.t.Helper()
	p.waitFor("the town", inTownScreen)
	p.page.Press("C")
	p.waitText("Name")
	if s := p.page.Screen(); !strings.Contains(s, name) {
		p.t.Fatalf("playing the wrong character, want %s:\n%s", name, s)
	}
	p.page.Press("escape")
	p.waitFor("the town", inTownScreen)
}

// withoutName drops -u from the engine options, as when the player gives
// none.
func withoutName(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-u") {
			out = append(out, a)
		}
	}
	return out
}

// TestMenusSwitchCharacters plays the Phase 1 exit test through the
// menus: make two characters, switch between them, save, and quit, and
// see Continue load the last one played.
func TestMenusSwitchCharacters(t *testing.T) {
	d := enginetest.NewDirs(t)
	sp := filepath.Join(d.User, settings.FileName)

	// The first character, made with -n, which selects New character in
	// the start menu; saved from the game menu, and quit from it.
	p, m := startPlayerWith(t, 80, 24, false, d.Args("-n"), sp)
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()
	p.page.Expect(p.page.GetByID(startMenuID)).ToHaveValue(" New character")
	p.page.Press("enter")
	p.birth("Tester")
	// The mouse: one click on Save saves.
	p.openMenu()
	p.page.GetByRole("list-item", " Save").Click()
	p.waitText("Game saved.")
	if _, err := os.Stat(filepath.Join(d.Save, "Tester")); err != nil {
		t.Fatalf("no savefile after Save: %v", err)
	}
	p.openMenu()
	p.choose(gameMenuID, "Save & Quit")
	p.page.ExpectExit()
	if m.ByeReason() != "quit" {
		t.Errorf("BYE %q after Quit, want quit", m.ByeReason())
	}
	m.Session().Wait()

	// The start menu offers Continue for Tester; make a second character
	// instead, in a savefile of its own.
	p, m = startPlayerWith(t, 80, 24, false, d.Args(), sp)
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()
	p.waitText("Tester, L1 Human Warrior")
	p.choose(startMenuID, "New character")
	p.birth("Second")
	p.inTownAs("Second")

	// Load Tester from the game menu, which saves Second in a savefile
	// of its own; then load Second again.
	p.openMenu()
	p.choose(gameMenuID, "Load a character")
	p.choose(saveListID, "Tester")
	p.inTownAs("Tester")
	if _, err := os.Stat(filepath.Join(d.Save, "Second")); err != nil {
		t.Fatalf("the new character has no savefile of its own: %v", err)
	}
	// The mouse again: select Second, then press Load.
	p.openMenu()
	p.choose(gameMenuID, "Load a character")
	p.page.GetByRole("list-item", " Second").Click()
	p.page.Expect(p.page.GetByID(saveListID)).ToHaveValue(" Second")
	p.page.GetByRole("button", "Load").Click()
	p.inTownAs("Second")
	p.openMenu()
	p.choose(gameMenuID, "Save & Quit")
	p.page.ExpectExit()
	m.Session().Wait()

	s, err := settings.Load(sp)
	if err != nil || s.LastCharacter != "Second" {
		t.Fatalf("settings %+v, %v; want Second as the last character", s, err)
	}

	// With no -u, Continue loads the last character played.
	p, m = startPlayerWith(t, 80, 24, false, withoutName(d.Args()), sp)
	p.waitText("Second, L1 Human Warrior")
	p.choose(startMenuID, "Continue")
	p.inTownAs("Second")
	p.page.Press("ctrl+c")
	p.page.ExpectExit()
	m.Session().Wait()
}

// TestDeathMenu retires a character and makes a new one from the death
// menu.
func TestDeathMenu(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, m := startPlayer(t, 80, 24, false, d.Args("-n"))
	p.createCharacter()
	p.page.Press("Q")
	p.page.Expect(p.page.GetByID(checkID)).ToBeVisible()
	p.waitText("Do you really want to retire?")
	p.page.Press("y")
	p.waitText("'@' sign")
	p.page.Press("@")
	deadline := time.Now().Add(playWait)
	for p.page.GetByID(deathMenuID).Count() == 0 {
		if strings.Contains(p.page.Screen(), "-more-") {
			p.page.Press("enter")
		}
		if time.Now().After(deadline) {
			t.Fatalf("no death menu:\n%s", p.page.Screen())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Look at the items, and go back to the menu.
	p.choose(deathMenuID, "Look at the items")
	p.page.Expect(p.page.GetByID(inventoryID)).ToBeVisible()
	p.waitText("Ration")
	p.page.Press("escape")
	p.gone(inventoryID)
	p.choose(deathMenuID, "New character")
	p.birth("Phoenix")
	p.inTownAs("Phoenix")
	p.page.Press("ctrl+c")
	p.page.ExpectExit()
	m.Session().Wait()
	if _, err := os.Stat(filepath.Join(d.Save, "Phoenix")); err != nil {
		t.Errorf("no savefile for the new character: %v", err)
	}
}

// TestBorgMenu turns the borg on from the game menu, through its
// warning, lets it play, and stops it with Escape.
func TestBorgMenu(t *testing.T) {
	d := enginetest.NewDirs(t)
	sp := filepath.Join(d.User, settings.FileName)
	p, m := startPlayerWith(t, 80, 24, false, d.Args("-n"), sp)
	p.createCharacter()
	p.openMenu()
	p.choose(gameMenuID, "Borg")
	p.waitText("high score list")
	// The warning starts on Cancel.
	p.choose(borgAskID, "Turn on the borg")
	p.waitFor("the borg to start", func(string) bool { return m.borgOn })
	time.Sleep(time.Second)

	p.page.Press("escape")
	p.page.Expect(p.page.GetByID(gameMenuID)).ToBeVisible()
	p.waitFor("the borg to stop", func(string) bool { return !m.borgOn })
	// The menu opened while the borg was stopping; open it again to see
	// the switch off.
	p.choose(gameMenuID, "Resume")
	p.openMenu()
	p.waitText("Off")
	p.choose(gameMenuID, "Resume")
	if _, err := os.Stat(filepath.Join(d.User, "borg.txt")); err != nil {
		t.Errorf("no borg.txt in the user folder: %v", err)
	}
	if s, _ := settings.Load(sp); !s.Warned("Tester") {
		t.Errorf("settings %+v do not record the warning", s)
	}

	// The second time, there is no warning.
	p.openMenu()
	p.choose(gameMenuID, "Borg")
	p.waitFor("the borg to start again", func(string) bool { return m.borgOn })
	p.page.Press("f10")
	p.waitFor("the borg to stop again", func(string) bool { return !m.borgOn })
	p.choose(gameMenuID, "Save & Quit")
	p.page.ExpectExit()
	m.Session().Wait()
}

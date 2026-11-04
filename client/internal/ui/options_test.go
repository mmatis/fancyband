package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/settings"
)

// chooseRow selects the options row whose text starts with prefix.
func (p *player) chooseRow(prefix string) {
	p.t.Helper()
	p.chooseRowIn(optionsID, prefix)
}

// chooseRowIn selects the row of the list id whose text, without the
// spaces around it, starts with prefix.
func (p *player) chooseRowIn(id, prefix string) {
	p.t.Helper()
	list := p.page.GetByID(id)
	p.page.Expect(list).ToBeVisible()
	prev := ""
	for i := 0; i < 60; i++ {
		var v string
		deadline := time.Now().Add(5 * time.Second)
		for {
			if list.Count() == 1 {
				v = list.Node().Value
			}
			if v != prev || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if strings.HasPrefix(strings.TrimSpace(v), prefix) {
			if p.stillOn(list, v) {
				return
			}
			prev = v
			continue
		}
		prev = v
		p.page.Press("down")
	}
	p.t.Fatalf("no row %q:\n%s", prefix, p.page.Screen())
}

// TestOptionsScreen opens the native options screen with '=' and changes
// an interface option, a cheat option behind its warning, and the tile
// set; a birth option stays as it is.
func TestOptionsScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	sp := filepath.Join(d.User, settings.FileName)
	p, m := startPlayerWith(t, 100, 30, false, d.Args("-n"), sp)
	p.createCharacter()

	p.page.Press("=")
	p.waitText("Interface")
	p.chooseRow("[ ] solid_walls")
	p.page.Press("space")
	p.waitText("[x] solid_walls")
	p.page.Press("escape")
	p.page.Expect(p.page.GetByID(optionsID)).Not().ToBeVisible()

	// The engine kept it: the screen opens again with it on.
	p.page.Press("=")
	p.waitText("[x] solid_walls")

	// Birth options cannot change after birth.
	p.page.Press("3")
	p.chooseRow("[ ] birth_randarts")
	p.page.Press("space")
	p.waitText("set only while making a character")
	if strings.Contains(p.page.Screen(), "[x] birth_randarts") {
		t.Error("a birth option changed after birth")
	}

	// A cheat option asks first.
	p.page.Press("4")
	p.chooseRow("[ ] cheat_hear")
	p.page.Press("space")
	p.page.Expect(p.page.GetByID("cheat-warning")).ToBeVisible()
	p.choose("cheat-warning", "Turn it on")
	p.waitText("[x] cheat_hear")

	// The Display tab switches tile sets while the game runs.
	p.page.Press("5")
	p.chooseRow("Tile set")
	p.page.Press("right")
	p.waitFor("tiles", func(string) bool { return m.sheet != nil })
	if s, _ := settings.Load(sp); s.TileSet == nil || *s.TileSet != 1 {
		t.Errorf("settings %+v do not record tile set 1", s)
	}
	p.page.Press("left")
	p.waitFor("text again", func(string) bool { return m.sheet == nil })
	p.page.Press("escape")
	p.waitFor("the town", inTownScreen)

	// More options opens the game's own menu, from '=' and from the game
	// menu.
	p.page.Press("=")
	p.chooseRow("» More options")
	p.page.Press("enter")
	p.waitText("User interface options")
	p.page.Press("escape")
	p.waitFor("the town", inTownScreen)
	p.openMenu()
	p.choose(gameMenuID, "Options")
	p.chooseRow("» More options")
	p.page.Press("enter")
	p.waitText("User interface options")
	p.page.Press("escape")
	p.waitFor("the town", inTownScreen)
}

// TestOptionsFromStartMenu opens the options before a character loads:
// the Display tab, with the game's options shown but not editable.
func TestOptionsFromStartMenu(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, _ := startPlayer(t, 100, 30, false, d.Args())
	p.choose(startMenuID, "Options")
	p.waitText("Tile set")
	p.page.Press("1")
	p.chooseRow("[ ] solid_walls")
	p.waitText("the default for new")
	p.page.Press("space")
	if strings.Contains(p.page.Screen(), "[x] solid_walls") {
		t.Error("an option changed before a character loaded")
	}
	p.page.Press("escape")
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()
}

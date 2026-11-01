package ui

import (
	"testing"

	"github.com/thebanri/limoni/core/driver"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// fakeCommands is a COMMANDS answer: two groups, then a submenu that
// Hidden's second entry opens.
func fakeCommands() *proto.CommandMenu {
	sub := 2
	return &proto.CommandMenu{Keyset: "original", Lists: []proto.CommandList{
		{Name: "Items", Commands: []proto.CommandEntry{{Name: "Inscribe an object", Key: "{"}, {Name: "Eat some food", Key: "E"}}},
		{Name: "Hidden", Commands: []proto.CommandEntry{{Name: "Walk", Key: ";"}, {Name: "Debug mode commands", Key: "^a", Submenu: &sub}}},
		{Name: "Debug", Level: 1, Commands: []proto.CommandEntry{{Name: "Wizard light level", Key: "w"}}},
	}}
}

func openFakeCommands(t *testing.T) (*Model, *sentLog) {
	t.Helper()
	m := New(Config{Width: 80, Height: 24})
	var sent sentLog
	m.sendHook = sent.hook
	m.handleRequest(proto.Request{JSON: []byte(`{"id": 7, "prompt": "screen", "name": "commands"}`)})
	answerQuery(t, m, &sent, "COMMANDS", "", proto.EventData{Commands: fakeCommands()})
	return m, &sent
}

// TestCommandMenuGroups opens the groups, leaving out the submenus, and
// runs a command from the second group.
func TestCommandMenuGroups(t *testing.T) {
	m, sent := openFakeCommands(t)
	groups := top(m).(*listScreen)
	if groups.id != commandMenuID || len(groups.items) != 2 || groups.items[1].Label != "Hidden" {
		t.Fatalf("groups %s %+v", groups.id, groups.items)
	}
	groups.key(keyOf(driver.KeyArrowDown))
	groups.key(keyOf(driver.KeyEnter))
	list := top(m).(*listScreen)
	if list.id != commandListID(1) || list.title != "Hidden" || list.items[0].Hint != ";" || list.items[1].Label != "Debug mode commands ›" {
		t.Fatalf("Hidden's list %s %q %+v", list.id, list.title, list.items)
	}
	list.key(keyOf(driver.KeyEnter))
	r := sent.last("")
	if len(m.stack) != 0 || r == nil || r["id"] != 7.0 || r["list"] != 1.0 || r["command"] != 0.0 {
		t.Errorf("Walk sent %v and left %d screens", r, len(m.stack))
	}
}

// TestCommandMenuSubmenu goes into a submenu and back out with Escape,
// then runs a command from the submenu.
func TestCommandMenuSubmenu(t *testing.T) {
	m, sent := openFakeCommands(t)
	top(m).key(keyOf(driver.KeyArrowDown))
	top(m).key(keyOf(driver.KeyEnter))
	top(m).key(keyOf(driver.KeyArrowDown))
	top(m).key(keyOf(driver.KeyEnter))
	sub := top(m).(*listScreen)
	if sub.id != commandListID(2) || sub.title != "Debug mode commands" {
		t.Fatalf("submenu %s %q", sub.id, sub.title)
	}
	// Each list sits below and right of the last.
	area := Box{0, 0, 80, 24}
	if b, a := sub.box(area), m.stack[1].box(area); b.X <= a.X || b.Y <= a.Y {
		t.Errorf("submenu at %+v, its parent at %+v", b, a)
	}
	sub.key(keyOf(driver.KeyEsc))
	if len(m.stack) != 2 {
		t.Fatalf("Escape left %d screens, want 2", len(m.stack))
	}
	top(m).key(keyOf(driver.KeyEnter))
	top(m).key(keyOf(driver.KeyEnter))
	if r := sent.last(""); r["list"] != 2.0 || r["command"] != 0.0 {
		t.Errorf("Wizard light level sent %v", r)
	}
}

// TestCommandMenuCancel closes the menu with Escape, which chooses
// nothing, and falls back to the game's menu without a COMMANDS answer.
func TestCommandMenuCancel(t *testing.T) {
	m, sent := openFakeCommands(t)
	top(m).key(keyOf(driver.KeyEsc))
	if r := sent.last(""); len(m.stack) != 0 || r["id"] != 7.0 || r["list"] != nil {
		t.Errorf("Escape sent %v and left %d screens", r, len(m.stack))
	}

	m = New(Config{Width: 80, Height: 24})
	var s2 sentLog
	m.sendHook = s2.hook
	m.openCommandMenu(8)
	id := int(s2.last("COMMANDS")["id"].(float64))
	m.pending[id](proto.EventData{Event: "COMMAND", ID: id, Error: "unknown command"})
	if r := s2.last(""); r["classic"] != true || len(m.stack) != 0 {
		t.Errorf("without COMMANDS sent %v", r)
	}
}

// TestCommandMenuPlay presses Enter in a game and runs Full dungeon map
// from the native menu's Information group: the native map opens, as it
// does by its key. Escape in a group goes back to the groups, and Escape
// there closes the menu.
func TestCommandMenuPlay(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, m := startPlayer(t, 80, 24, false, d.Args("-n"))
	p.createCharacter()
	p.page.Press("enter")
	p.choose(commandMenuID, "Information")
	p.page.Expect(p.page.GetByID(commandListID(1))).ToBeVisible()
	p.page.Press("escape")
	p.gone(commandListID(1))
	p.choose(commandMenuID, "Information")
	p.choose(commandListID(1), "Full dungeon map")
	p.page.Expect(p.page.GetByID(levelMapID)).ToBeVisible()
	p.page.Press("escape")
	p.gone(levelMapID)
	p.page.Press("enter")
	p.page.Expect(p.page.GetByID(commandMenuID)).ToBeVisible()
	p.page.Press("escape")
	p.gone(commandMenuID)
	p.waitFor("the town", inTownScreen)
	p.page.Press("ctrl+c")
	p.page.ExpectExit()
	m.Session().Wait()
}

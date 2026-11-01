package ui

import (
	"strings"
	"testing"

	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/proto"
)

func fakeTomb() proto.TombData {
	return proto.TombData{Name: "Grimble", Title: "Rookie", Class: "Warrior", Level: 3, Exp: 42, Gold: 134,
		Depth: 2, DiedFrom: "Grip, Farmer Maggot's Dog", Dead: true, Date: "Sat Oct 31 17:24:41 2026"}
}

// openFakeDeath opens the death menu with tomb as TOMB's answer.
func openFakeDeath(t *testing.T, tomb proto.TombData) (*Model, *sentLog, string) {
	t.Helper()
	m := New(Config{Width: 80, Height: 24})
	var sent sentLog
	m.sendHook = sent.hook
	m.saveName = "Grimble"
	m.openDeathMenu(5)
	answerQuery(t, m, &sent, "TOMB", "", proto.EventData{Tomb: &tomb})
	term := testkit.NewTerminal(80, 24)
	drawStack(m, term)
	return m, &sent, term.Snapshot()
}

// TestTombstone checks the stone's engraving for a death and for a
// retirement, and that the menu sits beside it without "Look back".
func TestTombstone(t *testing.T) {
	tomb := fakeTomb()
	m, _, snap := openFakeDeath(t, tomb)
	for _, want := range []string{"R I P", "Grimble", "Rookie", "Warrior", "Level: 3", "Exp: 42", "AU: 134",
		"Killed on Level 2", "by Grip, Farmer Maggot's Dog.", "on Sat Oct 31 17:24:41 2026", "You have died"} {
		if !strings.Contains(snap, want) {
			t.Errorf("no %q on the death screen:\n%s", want, snap)
		}
	}
	if strings.Contains(snap, "Look back") {
		t.Errorf("the death menu still offers Look back:\n%s", snap)
	}
	// The stone and the menu sit side by side, and fit.
	stone := m.stack[0].(*tombScreen)
	menu := m.stack[1].(*listScreen)
	area := Box{0, 0, 80, 24}
	pic, _ := stone.placement(area, menu.box(area).W, 0)
	mb := menu.box(area)
	if pic.X+pic.W > mb.X || mb.X+mb.W > 80 || pic.Y+pic.H > 24 {
		t.Errorf("stone %+v and menu %+v overlap or do not fit", pic, mb)
	}

	tomb.Retired, tomb.DiedFrom = true, "Retiring"
	_, _, snap = openFakeDeath(t, tomb)
	if !strings.Contains(snap, "Retired on Level 2") || strings.Contains(snap, "Killed") || strings.Contains(snap, "Retiring") {
		t.Errorf("retirement's stone:\n%s", snap)
	}
	if !strings.Contains(snap, "You have retired") {
		t.Errorf("retirement's menu title:\n%s", snap)
	}
}

// TestCrown checks that a winner gets the crown and not the stone.
func TestCrown(t *testing.T) {
	tomb := fakeTomb()
	tomb.Winner, tomb.Title, tomb.Level = true, "Magnificent", 50
	_, _, snap := openFakeDeath(t, tomb)
	for _, want := range []string{"All Hail the Mighty Champion!", "Grimble", "the Magnificent", "Veni, Vidi, Vici!", "You have won", "▀"} {
		if !strings.Contains(snap, want) {
			t.Errorf("no %q on the winner's screen:\n%s", want, snap)
		}
	}
	if strings.Contains(snap, "R I P") {
		t.Errorf("a winner got a tombstone:\n%s", snap)
	}
	for i, row := range crownArt {
		if len(row) != len(crownArt[0]) {
			t.Errorf("crown row %d is %d wide", i, len(row))
		}
	}
}

// TestDeathMenuChoices checks the death menu's entries, without Look
// back, and that Quit closes the death screen; without a TOMB answer the
// menu still opens.
func TestDeathMenuChoices(t *testing.T) {
	m, sent, _ := openFakeDeath(t, fakeTomb())
	menu := top(m).(*listScreen)
	var labels []string
	for _, it := range menu.items {
		labels = append(labels, it.Label)
	}
	want := "New character like Grimble|New character|Load a character|Look at the items|Character sheet|Messages|Knowledge|Hall of fame|Quit"
	if got := strings.Join(labels, "|"); got != want {
		t.Errorf("death menu %s, want %s", got, want)
	}
	menu.choose(len(labels) - 1)
	if r := sent.last(""); r == nil || int(r["id"].(float64)) != 5 || len(m.stack) != 0 {
		t.Errorf("Quit sent %v and left %d screens", r, len(m.stack))
	}

	// TOMB refused: the menu alone.
	m = New(Config{Width: 80, Height: 24})
	var s2 sentLog
	m.sendHook = s2.hook
	m.openDeathMenu(6)
	id := int(s2.last("TOMB")["id"].(float64))
	m.pending[id](proto.EventData{Event: "COMMAND", ID: id, Error: "unknown command"})
	if len(m.stack) != 1 || top(m).(*listScreen).id != deathMenuID {
		t.Errorf("without TOMB: %d screens", len(m.stack))
	}
}

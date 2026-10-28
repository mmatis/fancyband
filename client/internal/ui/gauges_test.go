package ui

import (
	"testing"

	"fancyband/internal/proto"
)

func TestGaugesFollowEvents(t *testing.T) {
	m := New(Config{Width: 120, Height: 36, Subwindows: true})
	if m.gauges.known {
		t.Fatal("gauges known before any event")
	}
	if !m.handleEvent(proto.Event{JSON: []byte(`{"event":"HP","hp":7,"mhp":20}`)}) {
		t.Error("an HP event did not ask for a redraw")
	}
	m.handleEvent(proto.Event{JSON: []byte(`{"event":"MANA","sp":3,"msp":5}`)})
	if g := m.gauges; !g.known || g.hp != 7 || g.mhp != 20 || g.sp != 3 || g.msp != 5 || !g.hasMagic {
		t.Errorf("gauges = %+v", g)
	}
	if m.handleEvent(proto.Event{JSON: []byte(`{"event":"GOLD","gold":5}`)}) {
		t.Error("a GOLD event asked for a redraw")
	}
	// A warrior has no mana, so the mana gauge hides again.
	m.handleEvent(proto.Event{JSON: []byte(`{"event":"MANA","sp":0,"msp":0}`)})
	if m.gauges.hasMagic {
		t.Error("the mana gauge shows with no mana")
	}
}

func TestHPStyle(t *testing.T) {
	healthy, hurt, danger := hpStyle(20, 20), hpStyle(10, 20), hpStyle(2, 20)
	if healthy == hurt || hurt == danger || healthy == danger {
		t.Error("the HP colors do not change with damage")
	}
}

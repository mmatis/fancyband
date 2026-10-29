package e2e

import (
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// waitStatus reads until a snapshot's status part passes ok.
func (g *game) waitStatus(what string, ok func(*proto.StatusData) bool) *proto.StatusData {
	g.t.Helper()
	if g.snap.Status != nil && ok(g.snap.Status) {
		return g.snap.Status
	}
	var got *proto.StatusData
	g.waitFor(what, func(*grid.Grid) bool {
		if g.snap.Status != nil && ok(g.snap.Status) {
			got = g.snap.Status
			return true
		}
		return false
	})
	return got
}

// until is waitFor, but returns at once if the screen already passes.
func (g *game) until(what string, ok func(*grid.Grid) bool) {
	g.t.Helper()
	if g.screen() == nil || !ok(g.screen()) {
		g.waitFor(what, ok)
	}
}

// effect runs one of the game's effects on the character through the
// debug command "Perform an effect" (Ctrl+A, E), which asks for the
// effect, its dice, and its subtype, then four numbers.
func (g *game) effect(name, dice, subtype string) {
	g.t.Helper()
	g.key(proto.Ctrl('a'))
	g.until("the debug prompt", func(gr *grid.Grid) bool { return gr.Contains("Debug Command:") })
	g.key('E')
	// The first time, the game warns about debug commands and asks. The
	// test turns on auto_more, since the game drops keys typed ahead of
	// a -more-.
	g.until("the effect prompt", func(gr *grid.Grid) bool {
		top := gr.Line(0)
		return strings.Contains(top, "Do which effect") || strings.Contains(top, "[y/n]")
	})
	if strings.Contains(g.screen().Line(0), "[y/n]") {
		g.key('y')
	}
	prompts := []struct{ prompt, answer string }{
		{"Do which effect", name},
		{"Enter damage dice", dice},
		{"effect subtype", subtype},
		{"second parameter", ""},
		{"third parameter", ""},
		{"y parameter", ""},
		{"x parameter", ""},
	}
	for _, p := range prompts {
		g.until(p.prompt, func(gr *grid.Grid) bool { return strings.Contains(gr.Line(0), p.prompt) })
		g.typeText(p.answer)
		g.key(proto.KeyEnter)
	}
}

// TestStatus checks the status part as the character goes hungry, is
// poisoned, hurt, slowed, and hasted, and that PANELS takes the sidebar and the status
// line off the screen.
func TestStatus(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "status"})
	g.createCharacter()
	g.waitEvent("WAIT")
	if r := g.command(100, "SET_OPTION", map[string]any{"name": "auto_more", "value": true}); !r.OK {
		t.Fatalf("SET_OPTION auto_more: %+v", r)
	}
	st := g.waitStatus("a status", func(*proto.StatusData) bool { return true })
	if st.Name != "Tester" || st.Race != "Human" || st.Class != "Warrior" || st.Level != 1 {
		t.Errorf("status names the character %q %q %q level %d", st.Name, st.Race, st.Class, st.Level)
	}
	if st.HP != st.MHP || st.HP <= 0 || st.Magic || st.DepthText != "Town" || len(st.Stats) != 5 {
		t.Errorf("a new warrior's status: %+v", st)
	}
	if st.NextExp <= 0 || st.Title == "" || !st.Has("Light") {
		t.Errorf("status has next_exp %d, title %q, line %+v", st.NextExp, st.Title, st.Line)
	}
	if len(st.Equippy) == 0 || st.Equippy[0] == nil {
		t.Errorf("a warrior starts with a weapon, but equippy is %+v", st.Equippy)
	}

	g.effect("NOURISH", "10", "2") // set the food level low
	g.waitStatus("hungry", func(s *proto.StatusData) bool { return s.Has("Hungry") })

	g.effect("TIMED_INC", "20", "POISONED")
	g.waitStatus("poisoned", func(s *proto.StatusData) bool { return s.Has("Poisoned") })

	before := g.snap.Status.HP
	g.effect("DAMAGE", "5", "0")
	g.waitStatus("hurt", func(s *proto.StatusData) bool { return s.HP <= before-5 && s.HP < s.MHP })

	g.effect("TIMED_INC", "20", "SLOW")
	g.waitStatus("slowed", func(s *proto.StatusData) bool {
		return s.Speed < 0 && strings.HasPrefix(s.SpeedText, "Slow") && s.Has("Slow")
	})
	g.effect("CURE", "0", "SLOW")
	g.effect("TIMED_INC", "20", "FAST")
	g.waitStatus("hasted", func(s *proto.StatusData) bool {
		return s.Speed > 0 && strings.HasPrefix(s.SpeedText, "Fast") && s.Has("Haste")
	})

	// The game draws its sidebar, with the level, and its status line,
	// on the bottom row; PANELS takes both away and gives the map the room.
	bottom := func(gr *grid.Grid) string { return gr.Line(gr.Height - 1) }
	if !g.screen().Contains("LEVEL") || !strings.Contains(bottom(g.screen()), "Light") {
		t.Fatalf("no sidebar or status line on the classic screen:\n%s", g.screen())
	}
	if r := g.command(1, "PANELS", map[string]any{"sidebar": false, "status_line": false}); !r.OK {
		t.Fatalf("PANELS: %+v", r)
	}
	g.until("no sidebar", func(gr *grid.Grid) bool {
		return !gr.Contains("LEVEL") && !strings.Contains(bottom(gr), "Light")
	})
	if n := g.options(2).Numbers["sidebar_mode"]; n != 0 {
		t.Errorf("PANELS changed the saved sidebar mode to %d", n)
	}
	if r := g.command(3, "PANELS", map[string]any{"sidebar": true, "status_line": true}); !r.OK {
		t.Fatalf("PANELS: %+v", r)
	}
	g.until("the sidebar back", func(gr *grid.Grid) bool {
		return gr.Contains("LEVEL") && strings.Contains(bottom(gr), "Light")
	})
}

// TestStatusTiles checks that with tiles the status part gives each
// piece of gear its tile.
func TestStatusTiles(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "status", "tiles:2"})
	g.createCharacter()
	st := g.waitStatus("a status", func(*proto.StatusData) bool { return true })
	worn := 0
	for i, e := range st.Equippy {
		if e == nil {
			continue
		}
		worn++
		if e.Tile == nil || (e.Tile.Row == 0 && e.Tile.Col == 0) {
			t.Errorf("slot %d has no tile: %+v", i, e)
		}
	}
	if worn == 0 {
		t.Error("a new warrior wears nothing")
	}
}

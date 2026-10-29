package e2e

import (
	"fmt"
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

// debug runs a debug command (Ctrl+A, then key) and answers its
// prompts in order, each a prompt and the text to type for it. The first
// time, the game warns about debug commands and asks; the tests turn on
// auto_more, since the game drops keys typed ahead of a -more-.
func (g *game) debug(key rune, prompts ...[2]string) {
	g.t.Helper()
	g.key(proto.Ctrl('a'))
	g.until("the debug prompt", func(gr *grid.Grid) bool { return gr.Contains("Debug Command:") })
	g.key(uint32(key))
	first := ""
	if len(prompts) > 0 {
		first = prompts[0][0]
	}
	g.until("the command's first prompt", func(gr *grid.Grid) bool {
		top := gr.Line(0)
		return (first != "" && strings.Contains(top, first)) || strings.Contains(top, "[y/n]")
	})
	if strings.Contains(g.screen().Line(0), "[y/n]") && !strings.Contains(g.screen().Line(0), first) {
		g.key('y')
	}
	for _, p := range prompts {
		g.until(p[0], func(gr *grid.Grid) bool { return strings.Contains(gr.Line(0), p[0]) })
		g.typeText(p[1])
		g.key(proto.KeyEnter)
	}
}

// effect runs one of the game's effects on the character through the
// debug command "Perform an effect" (Ctrl+A, E), which asks for the
// effect, its dice, and its subtype, then four numbers.
func (g *game) effect(name, dice, subtype string) {
	g.t.Helper()
	g.debug('E',
		[2]string{"Do which effect", name},
		[2]string{"Enter damage dice", dice},
		[2]string{"effect subtype", subtype},
		[2]string{"second parameter", ""},
		[2]string{"third parameter", ""},
		[2]string{"y parameter", ""},
		[2]string{"x parameter", ""})
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

// TestMoves walks the character and checks the MOVES event: a path of
// one step, ending where the @ now stands. Townspeople move too, so the
// hero's path is the one that starts where the @ stood.
func TestMoves(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "moves"})
	g.createCharacter()
	g.waitEvent("WAIT")
	var move proto.EventData
	var path [][2]int
	for _, k := range []uint32{'4', '6', '8', '2', '1', '3', '7', '9'} {
		x, y := heroOnScreen(g.screen())
		from := len(g.events)
		g.key(k)
		g.waitEvent("WAIT")
		for _, e := range g.events[from:] {
			if e.Event != "MOVES" {
				continue
			}
			for _, m := range e.Moves {
				if len(m.Path) > 0 && m.Path[0] == [2]int{x, y} {
					move, path = e, m.Path
				}
			}
		}
		if path != nil {
			break
		}
	}
	if path == nil {
		t.Fatal("no MOVES path for the @ after walking every way")
	}
	if move.Busy || len(path) != 2 {
		t.Fatalf("MOVES %+v, want one step for the @, not busy", move)
	}
	x, y := heroOnScreen(g.screen())
	if path[1] != [2]int{x, y} {
		t.Errorf("the path ends at %v, but the @ is at %d,%d", path[1], x, y)
	}
	if dx, dy := path[1][0]-path[0][0], path[1][1]-path[0][1]; max(dx, -dx) > 1 || max(dy, -dy) > 1 {
		t.Errorf("the step %v is not to a neighboring cell", path)
	}
}

// TestScroll goes down to a level wider than the map and moves the map
// with the locate command (L), and checks the SCROLL event that comes
// before the game redraws it.
func TestScroll(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "scrolls"})
	g.createCharacter()
	g.waitEvent("WAIT")
	if r := g.command(1, "SET_OPTION", map[string]any{"name": "auto_more", "value": true}); !r.OK {
		t.Fatalf("SET_OPTION auto_more: %+v", r)
	}
	g.debug('j', [2]string{"Jump to level", "5"})
	g.until("the cave profile question", func(gr *grid.Grid) bool { return gr.Contains("Choose cave profile?") })
	g.key('n')
	g.until("level 5", func(gr *grid.Grid) bool { return gr.Contains("250' (L5)") })
	g.waitEvent("WAIT")
	for _, e := range g.events {
		if e.Event == "SCROLL" {
			t.Errorf("the new level scrolled from the town: %+v", e)
		}
	}

	// Look at the map to one side: it scrolls by half its width. At the
	// level's edge the map does not move, so look left unless the map is
	// in the leftmost sector, column 0.
	g.key('L')
	g.until("the locate prompt", func(gr *grid.Grid) bool { return gr.Contains("Map sector") })
	var row, col int
	if _, err := fmt.Sscanf(g.screen().Line(0), "Map sector [%d,%d]", &row, &col); err != nil {
		t.Fatalf("reading the locate prompt: %v\n%s", err, g.screen())
	}
	dir := uint32('4')
	if col == 0 {
		dir = '6'
	}
	from := len(g.events)
	var sc proto.EventData
	g.key(dir)
	g.waitFor("the map to move", func(*grid.Grid) bool {
		for _, e := range g.events[from:] {
			if e.Event == "SCROLL" {
				sc = e
				return true
			}
		}
		return false
	})
	g.key(proto.KeyEscape)
	if sc.Busy || sc.Map == nil || sc.Shift[1] != 0 || sc.Shift[0] == 0 {
		t.Fatalf("SCROLL %+v, want a sideways shift and the map's cells", sc)
	}
	if m := sc.Map; m.X != 13 || m.Y != 1 || m.W != 66 || m.H != 22 {
		t.Errorf("map %+v, want the 66x22 cells right of the sidebar", *m)
	}
	// Half the map's width, or less where the level's edge stops it
	if sc.Shift[0] < -33 || sc.Shift[0] > 33 {
		t.Errorf("shift %v, want at most half the map's width", sc.Shift)
	}
}

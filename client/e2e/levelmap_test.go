package e2e

import (
	"testing"
	"unicode"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// levelChar returns the letter MAP gives grid x, y.
func levelChar(lv *proto.LevelMap, x, y int) rune {
	r := []rune(lv.Rows[y].Text)
	if x >= len(r) {
		return 0
	}
	return r[x]
}

// travelReply answers the map screen with a PATHFIND to x, y.
func travelReply(id, cmdID, x, y int) proto.Message {
	return proto.NewReply(id, map[string]any{"then": map[string]any{
		"id": cmdID, "cmd": "PATHFIND",
		"args": map[string]any{"point": map[string]any{"x": x, "y": y}},
	}})
}

// TestLevelMap reads the town with MAP and compares it with the
// snapshot's map and the game's own map view; opens the map screen by
// its key, travels from it to the down staircase, and checks what it
// refuses; then reads a whole dungeon level with tiles.
func TestLevelMap(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:map"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" }
	g.createCharacter()
	g.autoMore(1)

	lv := g.answerOf(2, "MAP", nil).Level
	if r := g.command(3, "SNAPSHOT", nil); !r.OK {
		t.Fatalf("SNAPSHOT: %+v", r)
	}
	snap := g.snap
	if lv == nil || snap.Map == nil || snap.Player == nil {
		t.Fatalf("MAP %+v, snapshot map %v", lv, snap.Map)
	}
	if lv.Width != snap.Map.Width || lv.Height != snap.Map.Height || len(lv.Rows) != lv.Height || lv.Depth != 0 {
		t.Fatalf("MAP is %dx%d at depth %d with %d rows; the snapshot's map is %dx%d",
			lv.Width, lv.Height, lv.Depth, len(lv.Rows), snap.Map.Width, snap.Map.Height)
	}
	if lv.Player.X != snap.Player.X || lv.Player.Y != snap.Player.Y || levelChar(lv, lv.Player.X, lv.Player.Y) != '@' {
		t.Errorf("player at %v showing %q; the snapshot has %d, %d", lv.Player, levelChar(lv, lv.Player.X, lv.Player.Y), snap.Player.X, snap.Player.Y)
	}
	busy := map[[2]int]bool{{lv.Player.X, lv.Player.Y}: true}
	for _, mon := range snap.Monsters {
		busy[[2]int{mon.X, mon.Y}] = true
	}
	marks := map[[2]int]string{}
	for _, mk := range lv.Marks {
		marks[[2]int{mk.X, mk.Y}] = mk.Kind
		if mk.Kind == "shop" && mk.Name == "" {
			t.Errorf("unnamed shop %+v", mk)
		}
	}
	for y := 0; y < lv.Height; y++ {
		if n := len([]rune(lv.Rows[y].Text)); n != lv.Width || len(lv.Rows[y].Colors) != lv.Width {
			t.Fatalf("row %d has %d letters and %d colors", y, n, len(lv.Rows[y].Colors))
		}
		for x := 0; x < lv.Width; x++ {
			at := [2]int{x, y}
			s, c := snap.Map.At(x, y), levelChar(lv, x, y)
			switch {
			case s == ' ' && c != ' ' && !busy[at]:
				t.Errorf("grid %d, %d is unknown but MAP shows %q", x, y, c)
			case s == '>' && marks[at] != "down", s == '<' && marks[at] != "up", s == 's' && marks[at] != "shop":
				t.Errorf("grid %d, %d is %q but marked %q", x, y, s, marks[at])
			case s == 's' && !busy[at] && !unicode.IsDigit(c):
				t.Errorf("shop entrance at %d, %d shows %q", x, y, c)
			case s == '>' && !busy[at] && c != '>':
				t.Errorf("down staircase at %d, %d shows %q", x, y, c)
			}
		}
	}
	for at, kind := range marks {
		if want := map[string]byte{"up": '<', "down": '>', "shop": 's'}[kind]; snap.Map.At(at[0], at[1]) != want {
			t.Errorf("mark %s at %v is %q in the snapshot", kind, at, snap.Map.At(at[0], at[1]))
		}
	}
	var stairs *proto.LevelMark
	for i, mk := range lv.Marks {
		if mk.Kind == "down" {
			stairs = &lv.Marks[i]
		}
	}
	if stairs == nil {
		t.Fatal("MAP marks no down staircase in the town")
	}

	// The game's own map view shows the same letters, when the level
	// fits the screen at one grid a cell.
	g.key('M')
	r := g.heldScreen("map")
	g.send(proto.NewReply(r.ID, map[string]any{"classic": true}))
	g.until("the classic map", func(gr *grid.Grid) bool { return gr.Contains("Hit any key to continue") })
	if gr := g.screen(); lv.Width <= gr.Width-2 && lv.Height <= gr.Height-2 {
		wrong := 0
		for y := 0; y < lv.Height; y++ {
			for x := 0; x < lv.Width; x++ {
				if got, want := gr.Cell(x+1, y+1).Rune, levelChar(lv, x, y); got != want && wrong < 10 {
					wrong++
					t.Errorf("classic map shows %q at %d, %d; MAP has %q", got, x, y, want)
				}
			}
		}
	} else {
		t.Logf("the town, %dx%d, does not fit the classic map at one grid a cell", lv.Width, lv.Height)
	}
	g.key(' ')
	g.waitEvent("WAIT")

	// The map screen refuses to travel to the player's own grid, and any
	// command but PATHFIND.
	g.key('M')
	r = g.heldScreen("map")
	g.send(travelReply(r.ID, 10, lv.Player.X, lv.Player.Y))
	g.waitFor("the refusal", func(*grid.Grid) bool { _, ok := g.results[10]; return ok })
	if res := g.results[10]; res.OK || res.Error != "you are already there" {
		t.Errorf("travel to the player's grid: %+v", res)
	}
	g.waitEvent("WAIT")
	g.key('M')
	r = g.heldScreen("map")
	g.send(proto.NewReply(r.ID, map[string]any{"then": map[string]any{"id": 11, "cmd": "HOLD"}}))
	g.waitFor("the refusal", func(*grid.Grid) bool { _, ok := g.results[11]; return ok })
	if res := g.results[11]; res.OK || res.Error != "only PATHFIND may follow this screen" {
		t.Errorf("HOLD from the map: %+v", res)
	}
	g.waitEvent("WAIT")

	// Travel to a floor grid a few steps away, then back to the down
	// staircase, where a new character starts. A townsperson in the way
	// stops the walk, as it would any run, so they go first.
	g.debug('z', [2]string{"Zap within what distance", "100"})
	g.waitEvent("WAIT")
	// pickFloor returns a floor grid 4 to 10 steps from the player with no
	// monster on it now.
	pickFloor := func() [2]int {
		taken := map[[2]int]bool{}
		for _, mon := range g.snap.Monsters {
			taken[[2]int{mon.X, mon.Y}] = true
		}
		for y := 0; y < lv.Height; y++ {
			for x := 0; x < lv.Width; x++ {
				dx, dy := x-lv.Player.X, y-lv.Player.Y
				if n := max(dx, -dx, dy, -dy); n >= 4 && n <= 10 && levelChar(lv, x, y) == '.' && !taken[[2]int{x, y}] {
					return [2]int{x, y}
				}
			}
		}
		t.Fatal("no free floor grid near the player")
		return [2]int{}
	}
	cmdID := 20
	// travel walks from the map to the grid pick gives, asking again
	// before each try. As when running, a monster coming into view stops
	// the walk, so it may take a few tries.
	travel := func(pick func() [2]int) {
		t.Helper()
		var to [2]int
		at := func() bool {
			g.command(len(g.results)+100, "SNAPSHOT", nil)
			to = pick()
			return g.snap.Player.X == to[0] && g.snap.Player.Y == to[1]
		}
		for try := 0; !at(); try++ {
			if try == 10 {
				t.Fatalf("never reached %v; at %d, %d", to, g.snap.Player.X, g.snap.Player.Y)
			}
			g.key('M')
			r := g.heldScreen("map")
			cmdID++
			id := cmdID
			g.send(travelReply(r.ID, id, to[0], to[1]))
			g.waitFor("the travel command", func(*grid.Grid) bool { _, ok := g.results[id]; return ok })
			if res := g.results[id]; !res.OK {
				t.Fatalf("travel to %v: %+v", to, res)
			}
			g.waitEvent("WAIT")
		}
	}
	floor := pickFloor()
	travel(func() [2]int {
		if g.snap.Player.X == floor[0] && g.snap.Player.Y == floor[1] {
			return floor
		}
		for _, mon := range g.snap.Monsters {
			if mon.X == floor[0] && mon.Y == floor[1] {
				floor = pickFloor()
			}
		}
		return floor
	})
	travel(func() [2]int { return [2]int{stairs.X, stairs.Y} })

	// A whole dungeon level with tiles: four numbers a grid.
	if res := g.command(40, "GRAPHICS", map[string]any{"set": 2, "scale": 1}); !res.OK {
		t.Fatalf("GRAPHICS: %+v", res)
	}
	g.debug('j', [2]string{"Jump to level", "5"})
	g.until("the cave profile question", func(gr *grid.Grid) bool { return gr.Contains("Choose cave profile?") })
	g.key('n')
	g.waitEvent("WAIT")
	g.afterWaits("level 5", func() bool {
		return g.answerOf(len(g.results)+100, "MAP", nil).Level.Depth == 5
	})
	deep := g.answerOf(41, "MAP", nil).Level
	if deep.Width < 100 || len(deep.Rows) != deep.Height {
		t.Errorf("level 5 is %dx%d with %d rows", deep.Width, deep.Height, len(deep.Rows))
	}
	for y, row := range deep.Rows {
		if len(row.Tiles) != 4*deep.Width || row.Text != "" {
			t.Fatalf("row %d has %d tile numbers and text %q", y, len(row.Tiles), row.Text)
		}
	}
	p := deep.Rows[deep.Player.Y].Tiles[4*deep.Player.X:]
	if p[0] == p[2] && p[1] == p[3] {
		t.Errorf("the player's grid shows only its terrain: %v", p[:4])
	}
	if res := g.command(42, "GRAPHICS", map[string]any{"set": 0}); !res.OK {
		t.Fatalf("GRAPHICS 0: %+v", res)
	}

	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()
}

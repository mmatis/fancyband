package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// visuals asks for VISUALS and returns the answer.
func (g *game) visuals(id int) proto.EventData {
	g.t.Helper()
	if r := g.command(id, "VISUALS", nil); !r.OK {
		g.t.Fatalf("VISUALS: %+v", r)
	}
	return g.lastEvent("VISUALS")
}

// entry returns the entry of a kind with a name.
func entry(t *testing.T, v proto.EventData, kind, name string) proto.VisualData {
	t.Helper()
	for _, e := range v.Entries {
		if e.Kind == kind && e.Name == name {
			return e
		}
	}
	t.Fatalf("VISUALS has no %s %q", kind, name)
	return proto.VisualData{}
}

// showing counts the map's cells whose foreground is tile.
func showing(g *grid.Grid, tile [2]int) int {
	n := 0
	g.Tiles(func(_, _ int, c proto.PictCell) {
		if int(c.FgRow) == tile[0] && int(c.FgCol) == tile[1] {
			n++
		}
	})
	return n
}

// TestVisuals gives the town's floor a tile nothing uses, sees the map
// change, saves it, changes the fire bolt without saving, and reloads: the
// floor keeps its new tile, the bolt goes back. A new game reads the saved
// change again.
func TestVisuals(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "tiles:2"})
	g.createCharacter()
	g.waitEvent("WAIT")

	v := g.visuals(1)
	if v.VisualSet != "adam-bolt" || len(v.Lights) != 4 || len(v.Directions) != 5 {
		t.Fatalf("VISUALS set %q, lights %q, directions %q", v.VisualSet, v.Lights, v.Directions)
	}
	kinds := map[string]int{}
	for _, e := range v.Entries {
		kinds[e.Kind]++
	}
	for _, k := range []string{"monster", "object", "flavor", "terrain", "trap", "effect"} {
		if kinds[k] == 0 {
			t.Errorf("VISUALS lists no %s", k)
		}
	}
	dog := entry(t, v, "monster", "scruffy little dog")
	floor := entry(t, v, "terrain", "open floor")
	fire := entry(t, v, "effect", "fire")
	if len(dog.Tiles) != 1 || dog.Tiles[0] == nil || len(floor.Tiles) != 4 || len(fire.Tiles) != 5 {
		t.Fatalf("dog %+v, floor %+v, fire %+v", dog, floor, fire)
	}
	// A tile nothing uses, so no cell shows it yet.
	used := map[[2]int]bool{}
	for _, e := range v.Entries {
		for _, u := range e.Tiles {
			if u != nil {
				used[*u] = true
			}
		}
	}
	tile := *dog.Tiles[0]
	for used[tile] {
		tile[1]++
	}
	if n := showing(g.screen(), tile); n != 0 {
		t.Fatalf("%d cells show the unused tile %v before any change", n, tile)
	}

	if r := g.command(2, "SET_VISUAL", map[string]any{"kind": "terrain", "id": floor.ID, "tile": tile}); !r.OK {
		t.Fatalf("SET_VISUAL: %+v", r)
	}
	floorsAre := func(g *game) {
		t.Helper()
		// The redraw may have come with the command's result.
		shown := func(s *grid.Grid) bool { return showing(s, tile) > 10 }
		if !shown(g.screen()) {
			g.waitFor("floors with the new tile", shown)
		}
	}
	floorsAre(g)
	floor = entry(t, g.visuals(3), "terrain", "open floor")
	if floor.Own == nil || *floor.Tiles[2] != tile || *floor.Own[2] == tile {
		t.Errorf("after SET_VISUAL the floor is %+v", floor)
	}
	if r := g.command(4, "SAVE_VISUALS", nil); !r.OK {
		t.Fatalf("SAVE_VISUALS: %+v", r)
	}
	saved, err := os.ReadFile(filepath.Join(d.User, "tiles", "adam-bolt.prf"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(saved), "feat:FLOOR:"); got != 4 {
		t.Errorf("the saved file has %d floor lines, want 4:\n%s", got, saved)
	}

	// An unsaved change to one bolt direction, then a reload.
	if r := g.command(5, "SET_VISUAL", map[string]any{"kind": "effect", "id": fire.ID, "index": 1, "tile": tile}); !r.OK {
		t.Fatalf("SET_VISUAL on the fire bolt: %+v", r)
	}
	changed := entry(t, g.visuals(6), "effect", "fire")
	if *changed.Tiles[1] != tile || *changed.Tiles[2] != *fire.Tiles[2] {
		t.Errorf("the fire bolt after one change: %+v", changed)
	}
	if r := g.command(7, "RELOAD_VISUALS", nil); !r.OK {
		t.Fatalf("RELOAD_VISUALS: %+v", r)
	}
	after := g.visuals(8)
	if got := entry(t, after, "effect", "fire"); *got.Tiles[1] != *fire.Tiles[1] || got.Own != nil {
		t.Errorf("the fire bolt after RELOAD_VISUALS: %+v", got)
	}
	if got := entry(t, after, "terrain", "open floor"); *got.Tiles[2] != tile {
		t.Errorf("the saved floor after RELOAD_VISUALS: %+v", got)
	}

	// Reset gives the floor back the set's own tile.
	if r := g.command(9, "SET_VISUAL", map[string]any{"kind": "terrain", "id": floor.ID, "reset": true}); !r.OK {
		t.Fatalf("SET_VISUAL reset: %+v", r)
	}
	if got := entry(t, g.visuals(10), "terrain", "open floor"); got.Own != nil || *got.Tiles[2] != *floor.Own[2] {
		t.Errorf("the floor after reset: %+v", got)
	}
	for _, bad := range []map[string]any{
		{"kind": "monster", "id": -1, "tile": tile},
		{"kind": "dragon", "id": 1, "tile": tile},
		{"kind": "effect", "id": fire.ID, "index": 5, "tile": tile},
		{"kind": "monster", "id": dog.ID, "tile": "dog"},
	} {
		if r := g.command(11, "SET_VISUAL", bad); r.OK {
			t.Errorf("SET_VISUAL took %v", bad)
		}
	}
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()

	// The saved file is read again in a new game.
	// Without the save, the new game makes a new character.
	if err := os.Remove(d.Savefile()); err != nil {
		t.Fatal(err)
	}
	g = startWith(t, dargs(d, "-n"), []string{"events", "waits", "tiles:2"})
	g.createCharacter()
	g.waitEvent("WAIT")
	floorsAre(g)
}

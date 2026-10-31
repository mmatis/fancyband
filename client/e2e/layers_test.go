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

// setAt sends SET_VISUAL in a layer and fails the test if it is refused.
func (g *game) setAt(id int, layer string, args map[string]any) {
	g.t.Helper()
	args["layer"] = layer
	if r := g.command(id, "SET_VISUAL", args); !r.OK {
		g.t.Fatalf("SET_VISUAL %v: %+v", args, r)
	}
}

// saveAt sends SAVE_VISUALS for a layer and fails the test if it is
// refused.
func (g *game) saveAt(id int, layer string) {
	g.t.Helper()
	if r := g.command(id, "SAVE_VISUALS", map[string]any{"layer": layer}); !r.OK {
		g.t.Fatalf("SAVE_VISUALS %s: %+v", layer, r)
	}
}

// TestVisualLayers gives the open floor a different tile in each layer:
// the defaults and the global layer on the start menu, before a
// character loads, and the character's layer in the game. Each layer's
// VISUALS shows the layers up to it, the map shows the topmost, and a
// reset falls back to the layer below. A fresh user folder sees the
// defaults, and the defaults change only in developer mode.
func TestVisualLayers(t *testing.T) {
	d := enginetest.NewDirs(t)
	defaults := filepath.Join(t.TempDir(), "fancyband")
	module := []string{"--new-save", "--defaults-dir", defaults}
	g := startModule(t, dargs(d, "-n"), module, []string{"events", "waits", "tiles:2"})
	if w := g.waitEvent("WAIT"); w.Context != "splash" {
		t.Fatalf("first WAIT %+v, want the splash screen", w)
	}

	// On the start menu the set's own tiles are there, and there is no
	// character's layer yet.
	v := g.visualsAt(1, "global")
	floor := entry(t, v, "terrain", "open floor")
	if floor.Tiles[2] == nil || floor.Own != nil || floor.From != "" {
		t.Fatalf("the floor on the start menu: %+v", floor)
	}
	if v.VisualFiles["defaults"] != filepath.Join(defaults, "adam-bolt.prf") || v.VisualFiles["global"] != filepath.Join(d.User, "tiles", "adam-bolt.prf") || v.VisualFiles["character"] != "" {
		t.Errorf("files on the start menu: %v", v.VisualFiles)
	}
	if r := g.command(2, "VISUALS", map[string]any{"layer": "character"}); r.OK {
		t.Error("VISUALS gave the character's layer before a character")
	}
	own := *floor.Tiles[2]
	a, b, c := [2]int{40, 1}, [2]int{40, 2}, [2]int{40, 3}
	g.setAt(3, "defaults", map[string]any{"kind": "terrain", "id": floor.ID, "tile": a})
	g.setAt(4, "global", map[string]any{"kind": "terrain", "id": floor.ID, "tile": b})
	if r := g.command(5, "SET_SYMBOL", map[string]any{"kind": "terrain", "id": floor.ID, "char": ",", "color": 2, "layer": "global"}); !r.OK {
		t.Fatalf("SET_SYMBOL: %+v", r)
	}
	g.saveAt(6, "defaults")
	g.saveAt(7, "global")
	saved, err := os.ReadFile(filepath.Join(defaults, "adam-bolt.prf"))
	if err != nil || strings.Count(string(saved), "feat:FLOOR:") != 4 || !strings.Contains(string(saved), "0xA8:0x81") {
		t.Errorf("the defaults file (%v):\n%s", err, saved)
	}

	// The defaults' view knows nothing of the global layer above it,
	// except that it changes the floor.
	floor = entry(t, g.visualsAt(8, "defaults"), "terrain", "open floor")
	if *floor.Tiles[2] != a || floor.Own == nil || *floor.Own[2] != own || floor.Above != "global" {
		t.Errorf("the floor in the defaults: %+v", floor)
	}

	g.createCharacter()
	g.waitEvent("WAIT")
	shows := func(tile [2]int) {
		t.Helper()
		shown := func(s *grid.Grid) bool { return showing(s, tile) > 10 }
		if !shown(g.screen()) {
			g.waitFor("floors with the tile", shown)
		}
	}
	shows(b)

	// The character's layer starts with the global tile, and the
	// global layer's letter.
	floor = entry(t, g.visualsAt(10, "character"), "terrain", "open floor")
	if *floor.Tiles[2] != b || floor.Own != nil || floor.From != "global" || floor.Char != "," || floor.SymbolFrom != "global" {
		t.Errorf("the floor in the character's layer at first: %+v", floor)
	}
	g.setAt(11, "character", map[string]any{"kind": "terrain", "id": floor.ID, "tile": c})
	shows(c)
	floor = entry(t, g.visualsAt(12, "character"), "terrain", "open floor")
	if *floor.Tiles[2] != c || floor.Own == nil || *floor.Own[2] != b || floor.From != "" {
		t.Errorf("the floor in the character's layer: %+v", floor)
	}
	floor = entry(t, g.visualsAt(13, "global"), "terrain", "open floor")
	if *floor.Tiles[2] != b || floor.Above != "character" || floor.Own == nil || *floor.Own[2] != a {
		t.Errorf("the floor in the global layer: %+v", floor)
	}
	g.saveAt(14, "character")
	g.command(15, "SAVEFILES", nil)
	save := g.lastEvent("SAVEFILES").Current
	mine := filepath.Join(d.User, "characters", save, "tiles", "adam-bolt.prf")
	if saved, err := os.ReadFile(mine); err != nil || strings.Count(string(saved), "feat:FLOOR:") != 4 {
		t.Errorf("the character's file (%v):\n%s", err, saved)
	}

	// A reset in the character's layer falls back to the global tile.
	g.setAt(16, "character", map[string]any{"kind": "terrain", "id": floor.ID, "reset": true})
	shows(b)
	floor = entry(t, g.visualsAt(17, "character"), "terrain", "open floor")
	if *floor.Tiles[2] != b || floor.Own != nil || floor.From != "global" {
		t.Errorf("the floor after a reset: %+v", floor)
	}
	// Saving the empty layer removes its file; the defaults cannot be
	// saved to an unknown layer.
	g.saveAt(18, "character")
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Errorf("the character's file is still there: %v", err)
	}
	if r := g.command(19, "SAVE_VISUALS", map[string]any{"layer": "nobody"}); r.OK {
		t.Error("SAVE_VISUALS took an unknown layer")
	}
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()

	// A new player's folder sees the defaults; without developer mode
	// they cannot change.
	d2 := enginetest.NewDirs(t)
	g = startModule(t, dargs(d2, "-n"), []string{"--defaults-dir", defaults}, []string{"events", "waits", "tiles:2"})
	g.waitEvent("WAIT")
	floor = entry(t, g.visualsAt(20, "global"), "terrain", "open floor")
	if *floor.Tiles[2] != a || floor.From != "defaults" || floor.Char == "," {
		t.Errorf("the floor for a new player: %+v", floor)
	}
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()

	g = startWith(t, dargs(d2, "-n"), []string{"events", "waits", "tiles:2"})
	g.waitEvent("WAIT")
	if r := g.command(21, "SAVE_VISUALS", map[string]any{"layer": "defaults"}); r.OK {
		t.Error("SAVE_VISUALS wrote the defaults outside developer mode")
	}
}

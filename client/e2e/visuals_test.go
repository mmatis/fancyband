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
	return g.visualsAt(id, "")
}

// visualsAt asks for VISUALS as the layers up to layer give the tiles,
// or the global layer for "".
func (g *game) visualsAt(id int, layer string) proto.EventData {
	g.t.Helper()
	var args map[string]any
	if layer != "" {
		args = map[string]any{"layer": layer}
	}
	if r := g.command(id, "VISUALS", args); !r.OK {
		g.t.Fatalf("VISUALS %s: %+v", layer, r)
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
	// The dog's tile may be a virtual one, at the end of a full row, so
	// the search wraps to the next row past column 127, the engine's last.
	tile := *dog.Tiles[0]
	for used[tile] {
		if tile[1]++; tile[1] > 127 {
			tile = [2]int{tile[0] + 1, 0}
		}
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

// symbolOnScreen reports whether the map shows r, past the sidebar.
func symbolOnScreen(gr *grid.Grid, r rune) bool {
	for y := 1; y < gr.Height-1; y++ {
		line := []rune(gr.Line(y))
		for x := 13; x < len(line); x++ {
			if line[x] == r {
				return true
			}
		}
	}
	return false
}

// TestSymbols gives the player's own symbol a red & while the map uses
// tiles, saves it, and sees the & when the map draws letters. An unsaved
// change goes on a reload, and an unsaved reset leaves the & for a new
// game. An effect takes only a color.
func TestSymbols(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "tiles:2"})
	g.createCharacter()
	g.waitEvent("WAIT")

	v := g.visuals(1)
	if len(v.SymbolColors) < 16 || v.SymbolColors[4] != "Red" {
		t.Fatalf("VISUALS names the colors %q", v.SymbolColors)
	}
	me := entry(t, v, "player", "Default for every race and class")
	if me.Char != "@" || me.OwnSymbol != nil {
		t.Fatalf("the player is %+v before any change", me)
	}
	if r := g.command(2, "SET_SYMBOL", map[string]any{"kind": "player", "id": me.ID, "char": "&", "color": 4}); !r.OK {
		t.Fatalf("SET_SYMBOL: %+v", r)
	}
	me = entry(t, g.visuals(3), "player", "Default for every race and class")
	if me.Char != "&" || me.Color != 4 || me.OwnSymbol == nil || me.OwnSymbol.Char != "@" {
		t.Errorf("after SET_SYMBOL the player is %+v", me)
	}
	fire := entry(t, v, "effect", "fire")
	if r := g.command(4, "SET_SYMBOL", map[string]any{"kind": "effect", "id": fire.ID, "color": 5}); !r.OK {
		t.Fatalf("SET_SYMBOL on fire: %+v", r)
	}
	if got := entry(t, g.visuals(5), "effect", "fire"); got.Color != 5 || got.Char != "*" {
		t.Errorf("fire after SET_SYMBOL is %+v, want a green *", got)
	}
	for i, bad := range []map[string]any{
		{"kind": "player", "id": me.ID, "char": "&", "color": 99},
		{"kind": "player", "id": me.ID, "char": "ab", "color": 4},
		{"kind": "player", "id": me.ID, "char": "", "color": 4},
		{"kind": "player", "id": me.ID, "char": "\x07", "color": 4},
		{"kind": "player", "id": me.ID, "color": 4},
	} {
		if r := g.command(10+i, "SET_SYMBOL", bad); r.OK {
			t.Errorf("SET_SYMBOL took %v", bad)
		}
	}
	if r := g.command(20, "SAVE_VISUALS", nil); !r.OK {
		t.Fatalf("SAVE_VISUALS: %+v", r)
	}
	saved, err := os.ReadFile(filepath.Join(d.User, "symbols.prf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "monster:<player>:0x04:0x26") || !strings.Contains(string(saved), "GF:FIRE:static:0x05:") {
		t.Errorf("symbols.prf lacks the player or fire:\n%s", saved)
	}

	// Letters: the map shows the &.
	// The redraw may have come with a command's result.
	shows := func(r rune) {
		t.Helper()
		if ok := func(s *grid.Grid) bool { return symbolOnScreen(s, r) }; !ok(g.screen()) {
			g.waitFor("the player as "+string(r), ok)
		}
	}
	if r := g.command(21, "GRAPHICS", map[string]any{"set": 0}); !r.OK {
		t.Fatalf("GRAPHICS 0: %+v", r)
	}
	shows('&')

	// An unsaved change, then a reload, which puts back the saved one.
	if r := g.command(22, "SET_SYMBOL", map[string]any{"kind": "player", "id": me.ID, "char": "%", "color": 4}); !r.OK {
		t.Fatalf("SET_SYMBOL in text mode: %+v", r)
	}
	shows('%')
	if r := g.command(23, "RELOAD_VISUALS", nil); !r.OK {
		t.Fatalf("RELOAD_VISUALS with letters: %+v", r)
	}
	shows('&')
	if r := g.command(24, "SET_SYMBOL", map[string]any{"kind": "player", "id": me.ID, "reset": true}); !r.OK {
		t.Fatalf("SET_SYMBOL reset: %+v", r)
	}
	shows('@')
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()

	// A new game draws letters and reads symbols.prf, unchanged by the
	// reset, which was not saved.
	g = startWith(t, dargs(d), []string{"events", "waits"})
	g.waitText("[Press any key to continue]")
	g.key('x')
	// inTown looks for an @, so this looks for the town's name.
	g.waitFor("the player as & in a new game", func(s *grid.Grid) bool { return s.Contains("Town") && symbolOnScreen(s, '&') })
}

// playerTile returns the tile LOOK gives the player's own grid.
func (g *game) playerTile(id int) [2]int {
	g.t.Helper()
	l := g.answerOf(id, "LOOK", map[string]any{"mode": "look"})
	if len(l.Things) == 0 || l.Things[0].Kind != "player" || l.Things[0].Tile == nil {
		g.t.Fatalf("LOOK gives no tile for the player: %+v", l.Things)
	}
	return [2]int{int(l.Things[0].Tile.Row), int(l.Things[0].Tile.Col)}
}

// TestPlayerTiles lists the player's tiles: <player>, and one for each
// race and class, with Adam Bolt's own tile for a Human Warrior, a
// Half-Troll Warrior's, and none for a Druid. A Human Warrior's tile then
// changes, falls back to <player>'s when it has none, and survives a save
// and a reload.
func TestPlayerTiles(t *testing.T) {
	d := enginetest.NewDirs(t)
	// Fancyband's shipped defaults give a Druid a tile, and this test is
	// about the set's own tiles, so it reads an empty defaults folder.
	noDefaults := []string{"--defaults-dir", t.TempDir()}
	g := startModule(t, dargs(d, "-n"), noDefaults, []string{"events", "waits", "tiles:2"})
	g.createCharacter()
	g.waitEvent("WAIT")

	v := g.visuals(1)
	players := 0
	for _, e := range v.Entries {
		if e.Kind == "player" {
			players++
		}
		if e.Kind == "monster" && e.ID == 0 {
			t.Errorf("<player> is still a monster: %+v", e)
		}
	}
	// The character alone is in their own layer only.
	if players != 1+11*9 {
		t.Errorf("%d player entries, want the default and 99 races and classes", players)
	}
	def := entry(t, v, "player", "Default for every race and class")
	hw := entry(t, v, "player", "Human Warrior")
	tw := entry(t, v, "player", "Half-Troll Warrior")
	hd := entry(t, v, "player", "Human Druid")
	if v.PlayerEntry != hw.ID {
		t.Errorf("the character's entry is %d, want the Human Warrior's, %d", v.PlayerEntry, hw.ID)
	}
	if def.Group != "" || hw.Group != "Warrior" || hd.Group != "Druid" || hw.Char != "@" {
		t.Errorf("groups: <player> %q, Human Warrior %q, Human Druid %q; letter %q", def.Group, hw.Group, hd.Group, hw.Char)
	}
	if *def.Tiles[0] != [2]int{18, 0} || *hw.Tiles[0] != [2]int{18, 0} || *tw.Tiles[0] != [2]int{18, 7} || hd.Tiles[0] != nil {
		t.Errorf("own tiles: <player> %v, Human Warrior %v, Half-Troll Warrior %v, Human Druid %v",
			def.Tiles[0], hw.Tiles[0], tw.Tiles[0], hd.Tiles[0])
	}
	if got := g.playerTile(2); got != [2]int{18, 0} {
		t.Errorf("a Human Warrior shows %v", got)
	}
	if r := g.command(3, "SET_VISUAL", map[string]any{"kind": "monster", "id": 0, "tile": []int{19, 0}}); r.OK {
		t.Error("SET_VISUAL took monster 0")
	}

	set := func(id int, e proto.VisualData, tile any) {
		t.Helper()
		if r := g.command(id, "SET_VISUAL", map[string]any{"kind": "player", "id": e.ID, "tile": tile}); !r.OK {
			t.Fatalf("SET_VISUAL %s %v: %+v", e.Name, tile, r)
		}
	}
	// The race and class's own tile wins over <player>'s.
	set(4, hw, []int{18, 3})
	set(5, def, []int{19, 0})
	if got := g.playerTile(6); got != [2]int{18, 3} {
		t.Errorf("after changing the Human Warrior: %v", got)
	}
	// With none of its own, it takes <player>'s.
	set(7, hw, nil)
	if got := g.playerTile(8); got != [2]int{19, 0} {
		t.Errorf("with no tile of its own: %v, want <player>'s", got)
	}
	set(9, hd, []int{24, 0})

	if r := g.command(10, "SAVE_VISUALS", nil); !r.OK {
		t.Fatalf("SAVE_VISUALS: %+v", r)
	}
	saved, err := os.ReadFile(filepath.Join(d.User, "tiles", "adam-bolt.prf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"monster:<player>:0x93:0x80\n",
		"?:[AND [EQU $CLASS Warrior] [EQU $RACE Human] ]\nmonster:<player>:0x00:0x00\n?:1\n",
		"?:[AND [EQU $CLASS Druid] [EQU $RACE Human] ]\nmonster:<player>:0x98:0x80\n?:1\n",
	} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("the saved file lacks %q:\n%s", want, saved)
		}
	}

	// A reload reads them back.
	set(11, hw, []int{18, 5})
	if r := g.command(12, "RELOAD_VISUALS", nil); !r.OK {
		t.Fatalf("RELOAD_VISUALS: %+v", r)
	}
	v = g.visuals(13)
	hw, hd, def = entry(t, v, "player", "Human Warrior"), entry(t, v, "player", "Human Druid"), entry(t, v, "player", "Default for every race and class")
	if hw.Tiles[0] != nil || hw.Own == nil || *hd.Tiles[0] != [2]int{24, 0} || *def.Tiles[0] != [2]int{19, 0} {
		t.Errorf("after a reload: Human Warrior %v (own %v), Human Druid %v, <player> %v", hw.Tiles[0], hw.Own, hd.Tiles[0], def.Tiles[0])
	}
	if got := g.playerTile(14); got != [2]int{19, 0} {
		t.Errorf("after a reload the player shows %v", got)
	}

	// The letter is <player>'s for every race and class.
	if r := g.command(15, "SET_SYMBOL", map[string]any{"kind": "player", "id": tw.ID, "char": "&", "color": 4}); !r.OK {
		t.Fatalf("SET_SYMBOL: %+v", r)
	}
	v = g.visuals(16)
	if a, b := entry(t, v, "player", "Default for every race and class"), entry(t, v, "player", "Elf Mage"); a.Char != "&" || b.Char != "&" || b.Color != 4 {
		t.Errorf("letters: <player> %q, Elf Mage %q in %d", a.Char, b.Char, b.Color)
	}

	// The character alone, in their own layer: a tile of their own wins
	// over their race and class's, which stays as it was.
	set(17, hw, []int{18, 5})
	var me proto.VisualData
	for _, e := range g.visualsAt(18, "character").Entries {
		if e.Kind == "player" && e.You {
			me = e
		}
	}
	if me.Name != "Tester" || len(me.Tiles) != 1 || me.Tiles[0] != nil {
		t.Fatalf("the character's own entry: %+v", me)
	}
	if r := g.command(19, "SET_VISUAL", map[string]any{"kind": "player", "id": me.ID, "tile": []int{20, 0}}); r.OK {
		t.Error("SET_VISUAL gave the character's own tile in the global layer")
	}
	if r := g.command(40, "SET_VISUAL", map[string]any{"kind": "player", "id": me.ID, "tile": []int{20, 0}, "layer": "character"}); !r.OK {
		t.Fatalf("SET_VISUAL the character's own tile: %+v", r)
	}
	if got := g.playerTile(20); got != [2]int{20, 0} {
		t.Errorf("with a tile of their own the character shows %v", got)
	}
	if hw := entry(t, g.visuals(21), "player", "Human Warrior"); *hw.Tiles[0] != [2]int{18, 5} {
		t.Errorf("the Human Warrior's tile became %v", hw.Tiles[0])
	}
	if r := g.command(22, "SAVE_VISUALS", map[string]any{"layer": "character"}); !r.OK {
		t.Fatalf("SAVE_VISUALS: %+v", r)
	}
	if r := g.command(24, "SAVE_VISUALS", nil); !r.OK {
		t.Fatalf("SAVE_VISUALS: %+v", r)
	}
	g.command(23, "SAVEFILES", nil)
	save := g.lastEvent("SAVEFILES").Current
	mine := "?:[EQU $SAVEFILE " + save + "]\nmonster:<player>:0x94:0x80\n?:1\n"
	saved, _ = os.ReadFile(filepath.Join(d.User, "characters", save, "tiles", "adam-bolt.prf"))
	if !strings.Contains(string(saved), mine) {
		t.Errorf("the character's saved file lacks %q:\n%s", mine, saved)
	}
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()

	// Another Human Warrior has the race and class's tile, not Tester's,
	// and saving leaves Tester's alone.
	g = startModule(t, dargs(d, "-n"), append([]string{"--new-save"}, noDefaults...), []string{"events", "waits", "tiles:2"})
	g.startBirthAs("Other", "Warrior")
	g.waitText("Accept character history? [y/n]")
	g.key('y')
	g.waitText("any other key to continue")
	g.key(proto.KeyEnter)
	g.waitFor("the town", inTown)
	g.waitEvent("WAIT")
	if got := g.playerTile(30); got != [2]int{18, 5} {
		t.Errorf("another Human Warrior shows %v, want the race and class's [18 5]", got)
	}
	set(31, entry(t, g.visuals(32), "player", "Human Druid"), []int{24, 1})
	if r := g.command(33, "SAVE_VISUALS", nil); !r.OK {
		t.Fatalf("SAVE_VISUALS: %+v", r)
	}
	if r := g.command(34, "SAVE_VISUALS", map[string]any{"layer": "character"}); !r.OK {
		t.Fatalf("SAVE_VISUALS: %+v", r)
	}
	saved, _ = os.ReadFile(filepath.Join(d.User, "tiles", "adam-bolt.prf"))
	if strings.Contains(string(saved), "SAVEFILE") {
		t.Errorf("the global file names a character:\n%s", saved)
	}
	saved, _ = os.ReadFile(filepath.Join(d.User, "characters", save, "tiles", "adam-bolt.prf"))
	if !strings.Contains(string(saved), mine) {
		t.Errorf("saving for another character changed Tester's file:\n%s", saved)
	}
}

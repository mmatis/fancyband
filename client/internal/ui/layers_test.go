package ui

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
	"fancyband/internal/settings"
	"fancyband/internal/tiles"
)

// withTiles writes a settings file that draws the map with Adam Bolt's
// tiles, and returns its path.
func withTiles(t *testing.T, d enginetest.Dirs) string {
	t.Helper()
	sp := filepath.Join(d.User, settings.FileName)
	set := 2
	if err := (settings.Settings{TileSet: &set}).Save(sp); err != nil {
		t.Fatal(err)
	}
	return sp
}

// filterDog searches the open editor for the scruffy little dog, gives
// it a filter with the hue turned, and waits for its virtual tile, which
// it returns.
func (p *player) filterDog(m *Model, ed *tileEditor) [2]uint8 {
	p.t.Helper()
	p.page.Press("tab") // Monsters
	p.page.Press("/")
	p.page.Type("scruffy little dog")
	p.page.Press("enter")
	p.page.Press("f")
	p.waitText("Filter: scruffy little dog")
	dog := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Name == "scruffy little dog" })
	p.page.Press("right")
	var vt [2]uint8
	p.waitFor("the dog's virtual tile", func(string) bool {
		t := ed.v.Entries[dog].Tiles[0]
		if t == nil || t[0] < rowsOf(m.sheet) {
			return false
		}
		vt = [2]uint8{uint8(t[0]), uint8(t[1])}
		return m.sheet.Edits().Virtual[vt].Layer == ed.layer
	})
	p.page.Press("escape")
	return vt
}

// TestTileEditorLayers uses the Tile Editor at each layer: the defaults,
// in developer mode, and the global layer, both from the start menu
// before a character loads, then the character's from the game. Each
// saves its own files, a filter on another layer's tile gets a virtual
// tile of the layer's own, and the rows say where a tile comes from.
func TestTileEditorLayers(t *testing.T) {
	d := enginetest.NewDirs(t)
	defaults := filepath.Join(t.TempDir(), "fancyband")
	copied := filepath.Join(t.TempDir(), "lib", "tiles", "fancyband")
	p, m := startPlayerWith(t, 160, 50, false, d.Args("-n"), withTiles(t, d),
		func(c *Config) { c.DefaultsDir, c.DefaultsCopy = defaults, copied })
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()
	p.waitFor("Adam Bolt's tiles", func(string) bool { return m.sheet != nil })

	// The defaults: the dog gets the tile right of its own.
	p.choose(startMenuID, "Tile Editor (Defaults)")
	p.waitText("Tile Editor (Defaults): adam-bolt")
	ed := m.stack[len(m.stack)-1].(*tileEditor)
	p.page.Press("tab") // Monsters
	p.page.Press("/")
	p.page.Type("scruffy little dog")
	p.page.Press("enter")
	p.page.Press("enter")
	p.waitText("Pick a tile: scruffy little dog")
	p.page.Press("right")
	p.page.Press("enter")
	p.waitText("changed")
	p.page.Press("s")
	p.waitText("Saved the defaults")
	if saved, err := os.ReadFile(filepath.Join(defaults, "adam-bolt.prf")); err != nil || !strings.Contains(string(saved), "monster:scruffy little dog:") {
		t.Errorf("the defaults' file (%v):\n%s", err, saved)
	}
	// A game without -dev reads the engine's copy, so it changes as well.
	if a, err := os.ReadFile(filepath.Join(defaults, "adam-bolt.prf")); err != nil {
		t.Error(err)
	} else if b, err := os.ReadFile(filepath.Join(copied, "adam-bolt.prf")); err != nil || string(a) != string(b) {
		t.Errorf("the engine's copy of the defaults (%v):\n%s", err, b)
	}
	dog := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Name == "scruffy little dog" })
	shipped := *ed.v.Entries[dog].Tiles[0]
	p.page.Press("escape") // clears the search
	p.page.Press("escape")
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()

	// The global layer: the dog's tile comes from the defaults, and a
	// filter makes a virtual tile in the global layer's columns.
	p.choose(startMenuID, "Tile Editor (Global)")
	p.waitText("Tile Editor (Global): adam-bolt")
	ed = m.stack[len(m.stack)-1].(*tileEditor)
	if e := ed.v.Entries[dog]; e.From != tiles.LayerDefaults || *e.Tiles[0] != shipped {
		t.Errorf("the dog in the global layer: %+v, want the defaults' %v", e, shipped)
	}
	global := p.filterDog(m, ed)
	if global[1] > 42 || m.sheet.Edits().Virtual[global].Base != [2]uint8{uint8(shipped[0]), uint8(shipped[1])} {
		t.Errorf("the global virtual tile %v: %+v", global, m.sheet.Edits().Virtual[global])
	}
	p.page.Press("s")
	p.waitText("Saved your tiles and symbols for every character")
	globalJSON := filepath.Join(d.User, "tiles", "adam-bolt.json")
	if e, err := tiles.LoadEdits(globalJSON); err != nil || len(e.Virtual) != 1 {
		t.Errorf("the global filters (%v): %+v", err, e.Virtual)
	}
	p.page.Press("escape") // clears the search
	p.page.Press("escape")
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()

	// The character's layer, from F2 in the game: the dog's filtered tile
	// comes from the global layer, and changing its filter gives the
	// character a virtual tile of their own, leaving the global one.
	p.choose(startMenuID, "New character")
	p.birthWith("Tester", inTownTiles)
	p.openMenu()
	p.waitText("Tile Editor (Global)")
	p.page.Press("escape")
	p.waitFor("the prompt", func(string) bool { return m.waiting })
	p.page.Press("f2")
	p.waitText("Tile Editor: adam-bolt")
	ed = m.stack[len(m.stack)-1].(*tileEditor)
	if e := ed.v.Entries[dog]; e.From != tiles.LayerGlobal || *e.Tiles[0] != [2]int{int(global[0]), int(global[1])} {
		t.Errorf("the dog in the character's layer: %+v, want the global %v", e, global)
	}
	mine := p.filterDog(m, ed)
	if mine[1] < 43 || mine[1] > 85 || m.sheet.Edits().Virtual[mine].Base != [2]uint8{uint8(shipped[0]), uint8(shipped[1])} {
		t.Errorf("the character's virtual tile %v: %+v", mine, m.sheet.Edits().Virtual[mine])
	}
	if v, ok := m.sheet.Edits().Virtual[global]; !ok || v.Layer != tiles.LayerGlobal {
		t.Errorf("the global virtual tile became %+v, %v", v, ok)
	}
	p.page.Press("s")
	p.waitText("Saved this character's tiles")
	if e, err := tiles.LoadEdits(tiles.EditsPath(ed.v.File)); err != nil || len(e.Virtual) != 1 {
		t.Errorf("the character's filters (%v): %+v", err, e.Virtual)
	} else if _, ok := e.Virtual[mine]; !ok {
		t.Errorf("the character's filters lack %v: %+v", mine, e.Virtual)
	}
	if e, err := tiles.LoadEdits(globalJSON); err != nil || len(e.Virtual) != 1 {
		t.Errorf("the global filters after the character's save (%v): %+v", err, e.Virtual)
	}
}

// TestTileEditorMenus checks the Tile Editor's menu entries outside
// developer mode: the start menu has the global editor and not the
// defaults', and the game menu only the character's.
func TestTileEditorMenus(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, m := startPlayerWith(t, 100, 30, false, d.Args("-n"), withTiles(t, d))
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()
	p.waitText("Tile Editor (Global)")
	if strings.Contains(p.page.Screen(), "Defaults") {
		t.Errorf("the start menu offers the defaults outside developer mode:\n%s", p.page.Screen())
	}
	p.choose(startMenuID, "New character")
	p.birthWith("Tester", inTownTiles)
	p.openMenu()
	p.waitText("Tile Editor")
	if strings.Contains(p.page.Screen(), "Tile Editor (Global)") {
		t.Errorf("the game menu offers the global editor outside developer mode:\n%s", p.page.Screen())
	}
	_ = m
}

// TestShippedFiltersWithoutDev starts the client without -dev, as a player
// does. The defaults' filters must reach the tile sheet, though the client
// runs in another folder from the engine, whose lib/ path is relative.
func TestShippedFiltersWithoutDev(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, m := startPlayerWith(t, 160, 50, false, d.Args("-n"), withTiles(t, d),
		func(c *Config) { c.Start = engineStarter(enginetest.Binary(t), io.Discard) })
	p.page.Expect(p.page.GetByID(startMenuID)).ToBeVisible()
	p.waitFor("Adam Bolt's tiles", func(string) bool { return m.sheet != nil })
	p.choose(startMenuID, "New character")
	p.birthWith("Tester", inTownTiles)
	p.waitFor("the files of changes", func(string) bool { return m.visualsFiles["defaults"] != "" })
	f := m.visualsFiles["defaults"]
	if !filepath.IsAbs(f) {
		t.Errorf("the defaults' file is %q, which only the engine's folder can find", f)
	}
	if _, err := os.Stat(tiles.EditsPath(f)); os.IsNotExist(err) {
		t.Skip("no shipped filters for Adam Bolt's tiles")
	}
	p.waitFor("the shipped filters", func(string) bool {
		for _, v := range m.sheet.Edits().Virtual {
			if v.Layer == tiles.LayerDefaults {
				return true
			}
		}
		return false
	})
}

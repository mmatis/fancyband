package ui

import (
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

// TestTileFilters gives the scruffy little dog a filter, which moves it
// to a virtual tile while the other monsters on its tile keep theirs;
// gives lava a slow animation, which moves its tile in each light to a
// virtual one; saves both; and leaves a third change unsaved, which
// leaving without saving drops.
func TestTileFilters(t *testing.T) {
	d := enginetest.NewDirs(t)
	sp := filepath.Join(d.User, settings.FileName)
	p, m := startPlayerWith(t, 120, 40, false, d.Args("-n"), sp)
	p.createCharacter()
	p.page.Press("=")
	p.waitText("Interface")
	p.page.Press("5")
	p.chooseRow("Tile set")
	p.page.Press("right")
	p.page.Press("right")
	p.waitFor("Adam Bolt's tiles", func(string) bool { return m.sheet != nil && m.terms.TileSet != nil && m.terms.TileSet.ID == 2 })
	p.waitFor("the filters' file", func(string) bool { return m.editsPath != "" })
	if want := filepath.Join(d.User, "tiles", "adam-bolt.json"); m.editsPath != want {
		t.Errorf("the filters go to %s, want %s", m.editsPath, want)
	}
	p.page.Press("escape")
	p.waitFor("the prompt", func(string) bool { return m.waiting })

	p.page.Press("f2")
	p.waitText("Tile Editor: adam-bolt")
	p.page.Press("/")
	p.page.Type("scruffy little dog")
	p.page.Press("enter")
	p.page.Press("f")
	p.waitText("Filter: scruffy little dog")
	ed := m.stack[0].(*tileEditor)
	dog := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Name == "scruffy little dog" })
	own := *ed.v.Entries[dog].Tiles[0]
	// Hue is the first row: turn it 120 degrees.
	for i := 0; i < 8; i++ {
		p.page.Press("right")
	}
	p.waitText("+120°")
	p.waitFor("the dog's virtual tile", func(string) bool {
		return ed.v.Entries[dog].Tiles[0][0] >= rowsOf(m.sheet)
	})
	vt := *ed.v.Entries[dog].Tiles[0]
	v := m.sheet.Edits().Virtual[[2]uint8{uint8(vt[0]), uint8(vt[1])}]
	if v.Base != [2]uint8{uint8(own[0]), uint8(own[1])} || v.Filter.Hue != 120 {
		t.Errorf("the dog's virtual tile shows %v through %+v, want its own %v turned 120°", v.Base, v.Filter.Look, own)
	}
	for _, e := range ed.v.Entries {
		if e.Name != "scruffy little dog" && len(e.Tiles) > 0 && e.Tiles[0] != nil && *e.Tiles[0] == vt {
			t.Errorf("%s shares the dog's virtual tile", e.Name)
		}
	}
	pc := proto.PictCell{FgRow: uint8(vt[0]), FgCol: uint8(vt[1]), BgRow: uint8(own[0]), BgCol: uint8(own[1])}
	if m.sheet.Picture(pc) == nil {
		t.Error("no picture for the virtual tile")
	}
	p.page.Press("escape")
	p.waitText("filter")

	// Lava, from the Terrain tab: an animation with a 5.1 s period.
	p.page.Press("escape") // clears the search
	p.page.Press("tab")
	p.page.Press("tab")
	p.page.Press("tab")
	p.page.Press("/")
	p.page.Type("lava")
	p.page.Press("enter")
	p.page.Press("f")
	p.waitText("Filter: lava")
	p.chooseRowIn(filterID, "[ ] Animation")
	p.page.Press("enter")
	p.chooseRowIn(filterID, "Period")
	for i := 0; i < 36; i++ {
		p.page.Press("right")
	}
	p.waitText("5.1 s")
	p.page.Press("escape")
	p.page.Press("s")
	p.waitText("Saved your tiles")

	saved, err := tiles.LoadEdits(filepath.Join(d.User, "tiles", "adam-bolt.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Virtual[[2]uint8{uint8(vt[0]), uint8(vt[1])}]; got.Filter.Hue != 120 {
		t.Errorf("the saved dog filter = %+v", got)
	}
	lava := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Kind == "terrain" && e.Name == "lava" })
	for i, lt := range ed.v.Entries[lava].Tiles {
		v, ok := saved.Virtual[[2]uint8{uint8(lt[0]), uint8(lt[1])}]
		if !ok || v.Filter.Anim == nil || v.Filter.Anim.Period != 5.1 {
			t.Errorf("lava's tile %d, %v, saved as %+v, want a virtual tile with a 5.1 s animation", i, *lt, v)
		}
	}
	prf, _ := os.ReadFile(filepath.Join(d.User, "tiles", "adam-bolt.prf"))
	if !strings.Contains(string(prf), "monster:scruffy little dog:") {
		t.Errorf("the engine's file lacks the dog's virtual tile:\n%s", prf)
	}

	// Remove the dog's filter without saving: leaving puts it back.
	p.page.Press("escape") // clears the search
	p.page.Press("shift+tab")
	p.page.Press("shift+tab")
	p.page.Press("shift+tab")
	p.page.Press("/")
	p.page.Type("scruffy little dog")
	p.page.Press("enter")
	p.waitText("filter")
	p.page.Press("f")
	p.waitText("Filter: scruffy little dog")
	p.chooseRowIn(filterID, "» Remove the filter")
	p.page.Press("enter")
	p.waitFor("the dog's own tile", func(string) bool { return *ed.v.Entries[dog].Tiles[0] == own })
	p.page.Press("escape")
	p.page.Press("escape")
	p.waitText("Unsaved tiles")
	p.page.Press("down")
	p.page.Press("enter")
	p.waitFor("the saved filter back", func(string) bool {
		_, ok := m.sheet.Edits().Virtual[[2]uint8{uint8(vt[0]), uint8(vt[1])}]
		return ok
	})
}

func rowsOf(s *tiles.Sheet) int {
	_, rows := s.Size()
	return rows
}

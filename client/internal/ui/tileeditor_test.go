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
)

// TestTileEditor opens the Tile Editor with F2, gives the scruffy little
// dog the tile right of its own, undoes that and gives it again, and saves; then gives the fire bolt four
// new tiles and leaves without saving, which puts back the saved ones. It
// opens the editor again from the game menu.
func TestTileEditor(t *testing.T) {
	d := enginetest.NewDirs(t)
	sp := filepath.Join(d.User, settings.FileName)
	p, m := startPlayerWith(t, 120, 40, false, d.Args("-n"), sp)
	p.createCharacter()
	// Adam Bolt's tiles, from the Display tab.
	p.page.Press("=")
	p.waitText("Interface")
	p.page.Press("5")
	p.chooseRow("Tile set")
	p.page.Press("right")
	p.page.Press("right")
	p.waitFor("Adam Bolt's tiles", func(string) bool { return m.sheet != nil && m.terms.TileSet != nil && m.terms.TileSet.ID == 2 })
	p.page.Press("escape")
	p.waitFor("the prompt", func(string) bool { return m.waiting })

	p.page.Press("f2")
	p.waitText("Tile Editor: adam-bolt")
	p.waitText("Monsters")
	// The first row is selected, so Enter opens the sheet at once.
	p.page.Press("enter")
	p.waitText("Pick a tile: <player>")
	p.page.Press("escape")
	p.waitText("Tile Editor: adam-bolt")
	p.page.Press("/")
	p.page.Type("scruffy little dog")
	p.page.Press("enter")
	p.page.Press("enter")
	p.waitText("Pick a tile: scruffy little dog")
	ed := m.stack[0].(*tileEditor)
	dog := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Name == "scruffy little dog" })
	own := *ed.v.Entries[dog].Tiles[0]
	p.page.Press("right")
	p.page.Press("enter")
	p.waitText("changed")
	if got := *ed.v.Entries[dog].Tiles[0]; got != [2]int{own[0], own[1] + 1} {
		t.Errorf("the dog's tile is %v, want the one right of %v", got, own)
	}
	// u puts back the set's own tile; then the dog gets the new one again.
	p.page.Press("u")
	p.waitFor("the dog's own tile", func(string) bool {
		e := ed.v.Entries[dog]
		return e.Own == nil && *e.Tiles[0] == own
	})
	p.page.Press("enter")
	p.waitText("Pick a tile: scruffy little dog")
	p.page.Press("right")
	p.page.Press("enter")
	p.waitText("changed")
	p.page.Press("s")
	p.waitText("Saved your tiles")
	saved, err := os.ReadFile(filepath.Join(d.User, "tiles", "adam-bolt.prf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "monster:scruffy little dog:") {
		t.Errorf("the saved file lacks the dog:\n%s", saved)
	}

	// The fire bolt, from the Effects tab.
	p.page.Press("escape") // clears the search
	p.page.Press("shift+tab")
	p.waitText("lightning")
	p.page.Press("/")
	p.page.Type("fire")
	p.page.Press("enter")
	p.page.Press("enter")
	p.waitText("Pick a tile: fire")
	p.waitText("f all four bolts from here")
	fire := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Kind == "effect" && e.Name == "fire" })
	before := tileList(ed.v.Entries[fire].Tiles)
	p.page.Press("down")
	p.page.Press("f")
	p.waitText("changed")
	after := tileList(ed.v.Entries[fire].Tiles)
	if after[0] != before[0] || after[1] == before[1] {
		t.Errorf("the fire bolt went from %v to %v, want new bolts and the same still tile", before, after)
	}
	// Adam Bolt's bolts go across, down, then the diagonals: 0°, 90°,
	// 45°, 135°.
	if c := after[1][1]; after[3][1] != c+1 || after[2][1] != c+2 || after[4][1] != c+3 {
		t.Errorf("the new bolts %v are not in the set's order", after[1:])
	}

	p.page.Press("escape")
	p.page.Press("escape")
	p.waitText("Unsaved tiles")
	p.page.Press("down")
	p.page.Press("enter")
	p.page.Expect(p.page.GetByID(tileEditorID)).Not().ToBeVisible()

	// The engine put back the saved tiles: the dog keeps its change, and
	// the fire bolt has the set's own.
	p.waitFor("the prompt", func(string) bool { return m.waiting })
	p.page.Press("f10")
	p.choose(gameMenuID, "Tile Editor")
	p.waitText("Tile Editor: adam-bolt")
	ed = m.stack[1].(*tileEditor)
	if got := tileList(ed.v.Entries[fire].Tiles); !slices.Equal(got, before) {
		t.Errorf("after leaving without saving, the fire bolt has %v, want %v", got, before)
	}
	if got := *ed.v.Entries[dog].Tiles[0]; got == own {
		t.Error("after leaving without saving, the dog lost its saved tile")
	}
}

// TestBoltOffsets reads the order of the bolt tiles from the effects'
// most common ones.
func TestBoltOffsets(t *testing.T) {
	tile := func(r, c int) *[2]int { return &[2]int{r, c} }
	effect := func(still *[2]int, bolts ...*[2]int) proto.VisualData {
		return proto.VisualData{Kind: "effect", Tiles: append([]*[2]int{still}, bolts...)}
	}
	ed := &tileEditor{v: proto.EventData{Directions: []string{"static", "0", "45", "90", "135"}}}
	shared := []*[2]int{tile(15, 4), tile(15, 6), tile(15, 5), tile(15, 7)}
	ed.v.Entries = []proto.VisualData{
		effect(tile(17, 9), shared...),
		effect(tile(17, 9), shared...),
		effect(tile(17, 23), tile(16, 28), tile(16, 30), tile(16, 29), tile(16, 31)),
	}
	if got := ed.boltOffsets(); !slices.Equal(got, []int{0, 2, 1, 3}) {
		t.Errorf("offsets %v, want 0 2 1 3", got)
	}
	// Bolts on two rows fall back to the order VISUALS lists them.
	ed.v.Entries = []proto.VisualData{effect(nil, tile(1, 1), tile(2, 1), tile(1, 2), tile(1, 3))}
	if got := ed.boltOffsets(); !slices.Equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("offsets %v, want 0 1 2 3", got)
	}
}

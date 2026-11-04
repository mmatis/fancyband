package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
	"fancyband/internal/settings"
	"fancyband/internal/tiles"
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
	p.waitText("Player")
	// The first row, <player>, the character alone, is selected, so
	// Enter opens the sheet at once.
	p.waitText("<player> (Tester)")
	p.page.Press("enter")
	p.waitText("Pick a tile: Tester")
	p.page.Press("escape")
	p.waitText("Tile Editor: adam-bolt")
	p.page.Press("tab") // Monsters
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
	// u takes the change away; then the dog gets the new one again.
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
	p.waitText("Saved this character's tiles")
	saved, err := os.ReadFile(filepath.Join(filepath.Dir(ed.v.File), "adam-bolt.prf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "monster:scruffy little dog:") {
		t.Errorf("the saved file lacks the dog:\n%s", saved)
	}

	// The fire bolt, from the Effects tab.
	p.page.Press("escape")    // clears the search
	p.page.Press("shift+tab") // Player
	p.page.Press("shift+tab") // Effects
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

// testEditor opens the Tile Editor without an engine, on Adam Bolt's
// sheet at tile size scale, in a window w by h: 60 monsters, a warrior,
// and a ghost with no tile; lava and the floor in four kinds of light;
// and a fire effect.
func testEditor(t *testing.T, scale, w, h int) (*Model, *tileEditor, *testkit.Terminal) {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	png := filepath.Join(filepath.Dir(here), "..", "..", "..", "engine", "lib", "tiles", "adam-bolt", "16x16.png")
	ts := proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: uint8(2 * scale), CellsH: uint8(scale), Name: "adam-bolt", Path: png}
	sheet, err := tiles.Load(ts)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{Width: w, Height: h})
	m.sheet, m.terms.TileSet, m.scale = sheet, &ts, scale
	tile := func(r, c int) *[2]int { return &[2]int{r, c} }
	v := proto.EventData{VisualSet: "adam-bolt", Lights: []string{"los", "torch", "lit", "dark"},
		Directions:   []string{"static", "0", "45", "90", "135"},
		SymbolColors: []string{"Dark", "White", "Slate", "Orange", "Red", "Green", "Blue", "Umber"}}
	for i := 0; i < 60; i++ {
		v.Entries = append(v.Entries, proto.VisualData{Kind: "monster", ID: i, Name: fmt.Sprintf("monster %02d", i),
			Char: "m", Tiles: []*[2]int{tile(i/10, i%10)}})
	}
	v.Entries = append(v.Entries,
		proto.VisualData{Kind: "terrain", ID: 1, Name: "lava", Char: "#", Tiles: []*[2]int{tile(20, 1), tile(20, 2), tile(20, 3), tile(20, 4)}},
		proto.VisualData{Kind: "effect", ID: 1, Name: "fire", Char: "*", Tiles: []*[2]int{tile(21, 0), tile(21, 1), tile(21, 2), tile(21, 3), tile(21, 4)}},
		// Adam Bolt's own floor and warrior, which has see-through pixels.
		proto.VisualData{Kind: "terrain", ID: 2, Name: "open floor", Char: ".", Tiles: []*[2]int{tile(0, 3), tile(0, 3), tile(0, 1), tile(0, 2)}},
		proto.VisualData{Kind: "monster", ID: 100, Name: "warrior", Char: "p", Tiles: []*[2]int{tile(18, 0)}},
		proto.VisualData{Kind: "monster", ID: 101, Name: "ghost", Char: "G", Tiles: []*[2]int{nil}})
	ed := newTileEditor(m, v, tiles.LayerCharacter)
	// These entries are all on the tabs after Player.
	ed.setTab(1)
	return m, ed, testkit.NewTerminal(uint16(w), uint16(h))
}

// drawEditor draws the editor on term as the model's View would.
func drawEditor(m *Model, ed *tileEditor, term *testkit.Terminal) {
	area := term.Area()
	env := &drawEnv{pal: newPalette(nil), sprite: m.sheet.Sprite}
	term.Draw(func(f *terminal.Frame) { ed.draw(f, ed.box(Box{0, 0, int(area.Width), int(area.Height)}), env) })
}

// previewRegion returns the picture column 2 drew, the only one inside
// box b.
func previewRegion(t *testing.T, term *testkit.Terminal, b Box) terminal.ImageRegion {
	t.Helper()
	var found []terminal.ImageRegion
	for _, r := range term.Frame().ImageRegionsSnapshot() {
		if int(r.Area.X) >= b.X && int(r.Area.X) < b.X+b.W && int(r.Area.Y) >= b.Y && int(r.Area.Y) < b.Y+b.H {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("column 2 has %d pictures, want 1", len(found))
	}
	return found[0]
}

// TestTileEditorColumns lays the editor out in three columns in a wide
// window, and in a narrow one shows column 3 in column 2's place while
// it has the keys.
func TestTileEditorColumns(t *testing.T) {
	m, ed, term := testEditor(t, 1, 120, 40)
	if b := ed.box(Box{0, 0, 120, 40}); b != (Box{2, 1, 116, 38}) {
		t.Errorf("the editor's box is %+v, want the window less its margin", b)
	}
	list, preview, side := ed.columns(ed.box(Box{0, 0, 120, 40}))
	if list.Empty() || preview.Empty() || side.Empty() {
		t.Fatalf("at 120 by 40 the columns are %+v %+v %+v, want three", list, preview, side)
	}
	if list.X != 2 || preview.X != list.X+list.W || side.X != preview.X+preview.W || side.X+side.W != 118 {
		t.Errorf("the columns %+v %+v %+v do not fill the box side by side", list, preview, side)
	}
	drawEditor(m, ed, term)
	screen := term.Snapshot()
	for _, want := range []string{"Tile Editor: adam-bolt", "Preview", "Pick a tile: monster 00", "monster 00"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the editor lacks %q:\n%s", want, screen)
		}
	}
	// The list's keys wrap rather than lose "Esc back".
	if !strings.Contains(screen, "Esc back") {
		t.Errorf("the list's footer lost Esc back:\n%s", screen)
	}

	m, ed, term = testEditor(t, 1, 80, 24)
	list, preview, side = ed.columns(ed.box(Box{0, 0, 80, 24}))
	if preview.Empty() || !side.Empty() {
		t.Fatalf("at 80 by 24 with the list's keys, the columns are %+v %+v %+v, want the list and the preview", list, preview, side)
	}
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	if ed.focus != focusSide || ed.side != sideTile {
		t.Fatal("Enter did not give the sheet the keys")
	}
	list, preview, side = ed.columns(ed.box(Box{0, 0, 80, 24}))
	if !preview.Empty() || side.Empty() || side.X != list.X+list.W {
		t.Errorf("at 80 by 24 with the sheet's keys, the columns are %+v %+v %+v, want the sheet in the preview's place", list, preview, side)
	}
	drawEditor(m, ed, term)
	if !strings.Contains(term.Snapshot(), "Pick a tile: monster 00") {
		t.Errorf("the narrow editor does not show the sheet:\n%s", term.Snapshot())
	}
	ed.key(driver.KeyEvent{Type: driver.KeyEsc})
	if ed.focus != focusList {
		t.Error("Escape on the sheet did not give the list the keys")
	}
}

// TestTileEditorPreview checks that column 2 shows the selected tile at
// the map's tile size, and changes with the row, the tile, the filter,
// and the slot being changed.
func TestTileEditorPreview(t *testing.T) {
	for _, scale := range []int{1, 2, 4} {
		m, ed, term := testEditor(t, scale, 160, 50)
		_, preview, _ := ed.columns(ed.box(Box{0, 0, 160, 50}))
		drawEditor(m, ed, term)
		r := previewRegion(t, term, preview)
		if int(r.Area.Width) != 6*scale || int(r.Area.Height) != 3*scale {
			t.Errorf("at tile size %d the preview is %dx%d cells, want %dx%d, three times the map's", scale, r.Area.Width, r.Area.Height, 6*scale, 3*scale)
		}
	}

	m, ed, term := testEditor(t, 2, 160, 50)
	_, preview, _ := ed.columns(ed.box(Box{0, 0, 160, 50}))
	drawEditor(m, ed, term)
	first := previewRegion(t, term, preview).Img

	// Another row.
	ed.key(driver.KeyEvent{Type: driver.KeyArrowDown})
	drawEditor(m, ed, term)
	second := previewRegion(t, term, preview).Img
	if second == first {
		t.Error("the preview did not change with the row")
	}

	// Another tile, picked on the sheet.
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	ed.key(driver.KeyEvent{Type: driver.KeyArrowRight})
	ed.key(driver.KeyEvent{Type: driver.KeyArrowRight})
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	if got := *ed.v.Entries[1].Tiles[0]; got != [2]int{0, 3} {
		t.Fatalf("monster 01 has tile %v, want 0, 3", got)
	}
	drawEditor(m, ed, term)
	third := previewRegion(t, term, preview).Img
	if third == second {
		t.Error("the preview did not change with the tile")
	}

	// A filter: the hue turns.
	ed.key(driver.KeyEvent{Type: driver.KeyRune, Ch: 'f'})
	if ed.focus != focusSide || ed.side != sideFilter {
		t.Fatal("f did not give the filter the keys")
	}
	drawEditor(m, ed, term)
	ed.key(driver.KeyEvent{Type: driver.KeyArrowRight})
	drawEditor(m, ed, term)
	fourth := previewRegion(t, term, preview).Img
	if fourth == third {
		t.Error("the preview did not change with the filter")
	}
	if !strings.Contains(term.Snapshot(), "Filter: monster 01") {
		t.Errorf("column 3 does not show the filter:\n%s", term.Snapshot())
	}
	ed.key(driver.KeyEvent{Type: driver.KeyEsc})

	// Lava's lit tile, then its torchlit one while the sheet changes it.
	ed.key(driver.KeyEvent{Type: driver.KeyTab})
	ed.key(driver.KeyEvent{Type: driver.KeyTab})
	ed.key(driver.KeyEvent{Type: driver.KeyTab})
	if ed.kind() != "terrain" {
		t.Fatalf("the tab is %s, want terrain", ed.kind())
	}
	drawEditor(m, ed, term)
	if !strings.Contains(term.Snapshot(), "Lit") {
		t.Errorf("the preview does not say which of lava's tiles it shows:\n%s", term.Snapshot())
	}
	lit := previewRegion(t, term, preview).Img
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	ed.key(driver.KeyEvent{Type: driver.KeyTab})
	ed.key(driver.KeyEvent{Type: driver.KeyTab})
	drawEditor(m, ed, term)
	if previewRegion(t, term, preview).Img == lit || !strings.Contains(term.Snapshot(), "By torchlight") {
		t.Errorf("changing lava by torchlight, the preview does not show that tile:\n%s", term.Snapshot())
	}
}

// TestPreviewShrinks shows a tile too big for column 2 at a smaller
// size, and says so.
func TestPreviewShrinks(t *testing.T) {
	_, ed, _ := testEditor(t, 4, 160, 50)
	if w, h, shrunk := ed.previewSize(Box{0, 0, 30, 40}); w != 24 || h != 12 || shrunk {
		t.Errorf("in a roomy column the preview is %dx%d, shrunk %v; want 24x12, three times the map's", w, h, shrunk)
	}
	if w, h, shrunk := ed.previewSize(Box{0, 0, 6, 40}); w != 4 || h != 2 || !shrunk {
		t.Errorf("in a column 6 wide the preview is %dx%d, shrunk %v; want 4x2, shrunk", w, h, shrunk)
	}
}

// TestEditorFloor shows tiles on the set's floor: the preview and the
// sheet, where a tile's see-through pixels show the floor, not black.
func TestEditorFloor(t *testing.T) {
	m, ed, term := testEditor(t, 2, 160, 50)
	warrior := slices.IndexFunc(ed.rows, func(e int) bool { return ed.v.Entries[e].Name == "warrior" })
	ed.state.Select(warrior)
	_, preview, side := ed.columns(ed.box(Box{0, 0, 160, 50}))
	drawEditor(m, ed, term)

	alone := m.sheet.Filtered(18, 0, tiles.NewFilter(), 0, 0, false, color.RGBA{}).(*image.RGBA)
	floor := m.sheet.Filtered(0, 1, tiles.NewFilter(), 0, 0, true, color.RGBA{}).(*image.RGBA)
	// Away from the edge, where the sheet's cursor is drawn in for
	// protocols other than Kitty's.
	var clear, solid image.Point
	for y := 1; y < 15; y++ {
		for x := 1; x < 15; x++ {
			if a := alone.RGBAAt(x, y).A; a == 0 {
				clear = image.Pt(x, y)
			} else if a == 255 {
				solid = image.Pt(x, y)
			}
		}
	}
	got := previewRegion(t, term, preview).Img.(*image.RGBA)
	if c := got.RGBAAt(clear.X, clear.Y); c != floor.RGBAAt(clear.X, clear.Y) {
		t.Errorf("under the warrior's see-through pixel %v the preview has %v, want the floor's %v", clear, c, floor.RGBAAt(clear.X, clear.Y))
	}
	if c := got.RGBAAt(solid.X, solid.Y); c != alone.RGBAAt(solid.X, solid.Y) {
		t.Errorf("at the warrior's pixel %v the preview has %v, want the warrior's %v", solid, c, alone.RGBAAt(solid.X, solid.Y))
	}

	// The sheet, with its cursor on the warrior.
	var sheet *image.RGBA
	for _, r := range term.Frame().ImageRegionsSnapshot() {
		if int(r.Area.X) >= side.X && r.ZIndex == 0 && int(r.Area.Width) > 8 {
			sheet, _ = r.Img.(*image.RGBA)
		}
	}
	if sheet == nil {
		t.Fatal("no sheet in column 3")
	}
	p := ed.picker
	at := image.Pt((p.col-p.left)*16+clear.X, (p.row-p.top)*16+clear.Y)
	if c := sheet.RGBAAt(at.X, at.Y); c != floor.RGBAAt(clear.X, clear.Y) {
		t.Errorf("under the warrior's see-through pixel the sheet has %v, want the floor's %v", c, floor.RGBAAt(clear.X, clear.Y))
	}
}

// TestSheetTileSize draws each tile of the sheet in the cells it takes
// on the map, at every tile size.
func TestSheetTileSize(t *testing.T) {
	for _, scale := range []int{1, 2, 3} {
		m, ed, term := testEditor(t, scale, 160, 50)
		_, _, side := ed.columns(ed.box(Box{0, 0, 160, 50}))
		drawEditor(m, ed, term)
		p := ed.picker
		if p.cw != 2*scale || p.ch != scale {
			t.Errorf("at tile size %d the sheet's tiles are %dx%d cells, want %dx%d as on the map", scale, p.cw, p.ch, 2*scale, scale)
		}
		found := false
		for _, r := range term.Frame().ImageRegionsSnapshot() {
			if int(r.Area.X) >= side.X && r.ZIndex == 0 && r.Img == p.sheet {
				found = true
				if int(r.Area.Width) != p.across*p.cw || int(r.Area.Height) != p.down*p.ch {
					t.Errorf("at tile size %d the sheet takes %dx%d cells for %dx%d tiles", scale, r.Area.Width, r.Area.Height, p.across, p.down)
				}
			}
		}
		if !found {
			t.Errorf("at tile size %d column 3 has no sheet", scale)
		}
	}
}

// TestSheetClick gives the selected entry a tile with one click on the
// sheet, and gives the list back the keys.
func TestSheetClick(t *testing.T) {
	m, ed, term := testEditor(t, 2, 160, 50)
	drawEditor(m, ed, term)
	p := ed.picker
	// The tile two right of monster 00's, at row 0, column 0.
	var at Box
	// The panel's zoom buttons come first; the first tile is the first
	// spot a tile in size.
	first := p.hotspots[slices.IndexFunc(p.hotspots, func(h hotspot) bool { return h.box.W == p.cw && h.box.H == p.ch })].box
	for _, h := range p.hotspots {
		if h.box.W == p.cw && h.box.H == p.ch && h.box.X == first.X+2*p.cw && h.box.Y == first.Y {
			at = h.box
		}
	}
	if at.Empty() {
		t.Fatal("no tile two right of the first on the sheet")
	}
	if !term.Click(uint16(at.X+1), uint16(at.Y)) {
		t.Fatal("nothing took the click on the sheet")
	}
	if got := *ed.v.Entries[0].Tiles[0]; got != [2]int{p.top, p.left + 2} {
		t.Errorf("one click gave monster 00 tile %v, want %v", got, [2]int{p.top, p.left + 2})
	}
	if ed.focus != focusList {
		t.Error("after the click the list does not have the keys")
	}
}

// clickText clicks the middle of the first place text shows on term.
func clickText(t *testing.T, term *testkit.Terminal, text string) {
	t.Helper()
	for y, line := range strings.Split(term.Snapshot(), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			x := len([]rune(line[:i])) + len([]rune(text))/2
			if !term.Click(uint16(x), uint16(y)) {
				t.Fatalf("nothing took a click on %q", text)
			}
			return
		}
	}
	t.Fatalf("no %q on the screen:\n%s", text, term.Snapshot())
}

// TestEditorButtons uses the editor with the mouse alone: the "Missing
// only" checkbox, and the Undo, Save, and Close buttons.
func TestEditorButtons(t *testing.T) {
	m, ed, term := testEditor(t, 1, 120, 40)
	drawEditor(m, ed, term)
	var box accessibility.Node
	for _, n := range term.AccessibilityTree() {
		if n.Role == accessibility.RoleCheckbox && n.Label == "Missing only" {
			box = n
		}
	}
	if box.Value != "false" {
		t.Errorf("the tree's Missing only checkbox is %+v, want one not checked", box)
	}

	clickText(t, term, "[ ] Missing only")
	if !ed.missing || len(ed.rows) != 1 || ed.v.Entries[ed.rows[0]].Name != "ghost" {
		t.Fatalf("after a click on Missing only, the list shows %d rows, want the ghost alone", len(ed.rows))
	}
	drawEditor(m, ed, term)
	clickText(t, term, "[x] Missing only")
	if ed.missing || len(ed.rows) < 60 {
		t.Fatalf("a second click on Missing only left %d rows", len(ed.rows))
	}

	// Give monster 00 another tile, then Undo.
	drawEditor(m, ed, term)
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	ed.key(driver.KeyEvent{Type: driver.KeyArrowRight})
	ed.key(driver.KeyEvent{Type: driver.KeyEnter})
	if ed.v.Entries[0].Own == nil {
		t.Fatal("monster 00 did not change")
	}
	drawEditor(m, ed, term)
	clickText(t, term, "[ Undo ]")
	if ed.v.Entries[0].Own != nil || *ed.v.Entries[0].Tiles[0] != [2]int{0, 0} {
		t.Errorf("after Undo monster 00 has %v, want its own tile", *ed.v.Entries[0].Tiles[0])
	}

	drawEditor(m, ed, term)
	if strings.Contains(term.Snapshot(), "[ Filter ]") {
		t.Error("the editor still has a Filter button beside its Filter tab")
	}

	drawEditor(m, ed, term)
	sent := m.nextID
	clickText(t, term, "[ Save ]")
	if m.nextID != sent+1 {
		t.Error("Save did not ask the engine to save")
	}

	// Undo left a change not saved, so Close asks first.
	drawEditor(m, ed, term)
	clickText(t, term, "[ Close ]")
	if n := len(m.stack); n == 0 {
		t.Error("Close with changes not saved did not ask about them")
	} else if ask, ok := m.stack[n-1].(*listScreen); !ok || ask.title != "Unsaved tiles" {
		t.Errorf("Close opened %T, want the question about unsaved tiles", m.stack[n-1])
	}
}

// TestEditorSymbol changes monster 00's letter and color on the Symbol
// tab, by keys and by clicks, and puts back the game's own; an effect
// takes only a color.
func TestEditorSymbol(t *testing.T) {
	m, ed, term := testEditor(t, 1, 120, 40)
	drawEditor(m, ed, term)
	clickText(t, term, " Symbol ")
	if ed.focus != focusSide || ed.side != sideSymbol || ed.symbol == nil || ed.symbol.e != 0 {
		t.Fatal("the Symbol tab did not open on monster 00 with the keys")
	}
	sent := m.nextID
	ed.key(driver.KeyEvent{Type: driver.KeyRune, Ch: 'X'})
	en := ed.v.Entries[0]
	if en.Char != "X" || en.OwnSymbol == nil || en.OwnSymbol.Char != "m" || m.nextID != sent+1 {
		t.Fatalf("typing X made monster 00 %+v, own %+v", en, en.OwnSymbol)
	}
	ed.key(driver.KeyEvent{Type: driver.KeyArrowDown})
	ed.key(driver.KeyEvent{Type: driver.KeyArrowRight})
	ed.key(driver.KeyEvent{Type: driver.KeyArrowRight})
	if c := ed.v.Entries[0].Color; c != 2 {
		t.Errorf("two steps right from color 0 gave color %d, want 2", c)
	}
	drawEditor(m, ed, term)
	screen := term.Snapshot()
	for _, want := range []string{"Symbol: monster 00", "Letter: X", "As a letter:  X", "symbol"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the editor lacks %q:\n%s", want, screen)
		}
	}
	// A click on the second color: the grid shows the letter in each,
	// 4 cells apart, the current one, the third, in brackets.
	for y, line := range strings.Split(screen, "\n") {
		if i := strings.Index(line, "[X]"); i >= 0 {
			x := len([]rune(line[:i])) - 4 + 1
			term.Click(uint16(x), uint16(y))
		}
	}
	if c := ed.v.Entries[0].Color; c != 1 {
		t.Errorf("a click on the second color gave color %d, want 1", c)
	}
	drawEditor(m, ed, term)
	clickText(t, term, "Take the change away")
	if en := ed.v.Entries[0]; en.Char != "m" || en.Color != 0 || en.OwnSymbol != nil {
		t.Errorf("after putting it back monster 00 is %+v", en)
	}

	// Fire, on the Effects tab: letters do nothing, colors work.
	ed.back()
	for ed.kind() != "effect" {
		ed.key(driver.KeyEvent{Type: driver.KeyTab})
	}
	drawEditor(m, ed, term)
	clickText(t, term, " Symbol ")
	ed.key(driver.KeyEvent{Type: driver.KeyRune, Ch: 'Q'})
	fire := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Name == "fire" })
	if ed.v.Entries[fire].Char == "Q" {
		t.Error("fire took a letter")
	}
	if r := ed.symbol.selectedRow(); r == nil || r.label != "Color" {
		t.Fatalf("on fire's Symbol tab the selection is on %+v, want Color", r)
	}
	ed.key(driver.KeyEvent{Type: driver.KeyArrowRight})
	if ed.v.Entries[fire].Color != 1 || ed.v.Entries[fire].OwnSymbol == nil {
		t.Errorf("fire after a color change: %+v", ed.v.Entries[fire])
	}
}

// TestPlayerRow puts the character first on the Player tab, as
// <player>, by their own entry. Without a tile of their own it shows
// their race and class's, or else the default's; choosing a tile there
// changes the character alone.
func TestPlayerRow(t *testing.T) {
	m, _, term := testEditor(t, 1, 160, 50)
	tile := func(r, c int) *[2]int { return &[2]int{r, c} }
	v := proto.EventData{VisualSet: "adam-bolt", PlayerEntry: 8, Entries: []proto.VisualData{
		{Kind: "player", ID: 0, Name: "Default for every race and class", Char: "@", Tiles: []*[2]int{tile(18, 0)}},
		{Kind: "player", ID: 1, Name: "Human Warrior", Group: "Warrior", Char: "@", Tiles: []*[2]int{tile(18, 0)}},
		{Kind: "player", ID: 8, Name: "Half-Troll Warrior", Group: "Warrior", Char: "@", Tiles: []*[2]int{tile(18, 7)}},
		{Kind: "player", ID: 12, Name: "Human Druid", Group: "Druid", Char: "@", Tiles: []*[2]int{nil}},
		{Kind: "player", ID: 100, Name: "Flor", You: true, Char: "@", Tiles: []*[2]int{nil}},
	}}
	ed := newTileEditor(m, v, tiles.LayerCharacter)
	drawEditor(m, ed, term)
	first := ed.items[0]
	if first.Label != "<player> (Flor)" || first.Tile == nil || first.Tile.FgCol != 7 || first.Hint != "uses Half-Troll Warrior" {
		t.Fatalf("first row %q with tile %+v, hint %q", first.Label, first.Tile, first.Hint)
	}
	if e := ed.selectedEntry(); e != 4 {
		t.Errorf("the first row selects entry %d, want the character's own", e)
	}
	if n := strings.Count(strings.Join(labels(ed.items), "|"), "Flor"); n != 1 {
		t.Errorf("Flor shows %d times: %q", n, labels(ed.items))
	}
	ed.set(ed.selectedEntry(), -1, [2]int{18, 3})
	if got := *ed.v.Entries[4].Tiles[0]; got != [2]int{18, 3} || *ed.v.Entries[2].Tiles[0] != [2]int{18, 7} {
		t.Errorf("choosing a tile for <player> gave Flor %v and the Half-Troll Warrior %v", got, *ed.v.Entries[2].Tiles[0])
	}
	if first := ed.items[0]; first.Tile == nil || first.Tile.FgCol != 3 || first.Hint != "changed" {
		t.Errorf("after the change the first row has tile %+v, hint %q", first.Tile, first.Hint)
	}

	// A race and class with no tile of its own: the default's.
	v.PlayerEntry = 12
	v.Entries[4].Tiles = []*[2]int{nil}
	ed = newTileEditor(m, v, tiles.LayerCharacter)
	if first := ed.items[0]; first.Tile == nil || first.Tile.FgCol != 0 || first.Hint != "uses the default tile" {
		t.Errorf("a druid's first row: tile %+v, hint %q", first.Tile, first.Hint)
	}
	// The default is not listed; a race and class without a tile of its
	// own shows the default's.
	for _, it := range ed.items {
		if strings.HasPrefix(it.Label, "Default") {
			t.Errorf("the default is listed: %q", labels(ed.items))
		}
		if it.Label == "Human Druid" && (it.Tile == nil || it.Tile.FgCol != 0 || it.Hint != "uses the default tile") {
			t.Errorf("the Human Druid's row: tile %+v, hint %q", it.Tile, it.Hint)
		}
	}
	// Missing only leaves the character out.
	ed.toggleMissing()
	if strings.Contains(strings.Join(labels(ed.items), "|"), "Flor") {
		t.Errorf("missing only lists the character: %q", labels(ed.items))
	}
}

// TestRereadKeepsLaterChange undoes a tile and picks a new one before the
// engine's answer to the undo's VISUALS comes back. That answer is older
// than the pick, so the editor must not show it; it asks again instead.
func TestRereadKeepsLaterChange(t *testing.T) {
	m, ed, _ := testEditor(t, 1, 160, 50)
	var sent sentLog
	m.sendHook = sent.hook
	e := slices.IndexFunc(ed.v.Entries, func(v proto.VisualData) bool { return v.Name == "monster 05" })
	own := *ed.v.Entries[e].Tiles[0]
	stale := ed.v
	stale.Entries = slices.Clone(ed.v.Entries)

	ed.set(e, -1, [2]int{own[0], own[1] + 1})
	ed.undo(e)
	first := sent.last("VISUALS")
	if first == nil {
		t.Fatal("undo sent no VISUALS")
	}
	ed.set(e, -1, [2]int{own[0], own[1] + 2})

	answerVisuals := func(q map[string]any, v proto.EventData) {
		t.Helper()
		id := int(q["id"].(float64))
		done, ok := m.pending[id]
		if !ok {
			t.Fatalf("no reader for VISUALS %d", id)
		}
		delete(m.pending, id)
		m.lastVisuals = v
		done(proto.EventData{ID: id, OK: true})
	}
	answerVisuals(first, stale)
	en := ed.v.Entries[e]
	if *en.Tiles[0] != [2]int{own[0], own[1] + 2} || en.Own == nil {
		t.Fatalf("after the old answer the entry has %v, own %v; want the later pick", *en.Tiles[0], en.Own)
	}
	again := sent.last("VISUALS")
	if again == nil || again["id"] == first["id"] {
		t.Fatal("the editor did not ask again after an old answer")
	}

	// The second answer has the pick, and the editor takes it.
	fresh := ed.v
	fresh.Entries = slices.Clone(ed.v.Entries)
	fresh.Entries[e].From = tiles.LayerCharacter
	answerVisuals(again, fresh)
	if ed.v.Entries[e].From != tiles.LayerCharacter {
		t.Error("the editor did not take the answer that came after the pick")
	}
}

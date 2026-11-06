package ui

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"
	"github.com/thebanri/limoni/widgets"

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
	p.waitFor("the layers' files", func(string) bool { return m.visualsFiles["character"] != "" })
	if want := filepath.Join(d.User, "tiles", "adam-bolt.prf"); m.visualsFiles["global"] != want {
		t.Errorf("the global layer's file is %s, want %s", m.visualsFiles["global"], want)
	}
	p.page.Press("escape")
	p.waitFor("the prompt", func(string) bool { return m.waiting })

	p.page.Press("f2")
	p.waitText("Tile Editor (Character): adam-bolt")
	p.page.Press("tab") // Monsters
	p.page.Press("/")
	p.page.Type("scruffy little dog")
	p.page.Press("enter")
	p.page.Press("f")
	p.waitText("Filter: scruffy little dog")
	ed := m.stack[0].(*tileEditor)
	dog := slices.IndexFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.Name == "scruffy little dog" })
	own := *ed.v.Entries[dog].Tiles[0]
	// Flip Horizontal is the first row, above the settings.
	if fs := ed.filter; fs == nil || fs.rows[0].label != "Flip Horizontal" || fs.rows[0].kind != optCheck {
		t.Fatalf("the filter tab's first row is not the Flip Horizontal box")
	}
	// Turn the hue 120 degrees.
	p.chooseRowIn(filterID, "Hue")
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
	// Flip it too: the same virtual tile, now mirrored as well.
	p.chooseRowIn(filterID, "[ ] Flip Horizontal")
	p.page.Press("enter")
	p.waitFor("the flip", func(string) bool {
		return m.sheet.Edits().Virtual[[2]uint8{uint8(vt[0]), uint8(vt[1])}].Filter.Flip
	})
	if v := m.sheet.Edits().Virtual[[2]uint8{uint8(vt[0]), uint8(vt[1])}]; v.Filter.Hue != 120 || *ed.v.Entries[dog].Tiles[0] != vt {
		t.Errorf("after the flip the dog shows %v through %+v, want the same virtual tile %v still turned 120°", *ed.v.Entries[dog].Tiles[0], v.Filter, vt)
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
	p.waitText("Saved this character's tiles")

	// F2 edits the character's layer, which lives in their own folder.
	mine := filepath.Dir(ed.v.File)
	if want := filepath.Join(d.User, "characters"); filepath.Dir(filepath.Dir(mine)) != want {
		t.Errorf("the character's tiles are in %s, want a folder in %s", mine, want)
	}
	saved, err := tiles.LoadEdits(filepath.Join(mine, "adam-bolt.json"))
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
	prf, _ := os.ReadFile(filepath.Join(mine, "adam-bolt.prf"))
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

// clickRune clicks the first place r shows on line y of term, through
// Limoni, or through the screen's own click when click is set.
func clickRune(t *testing.T, term *testkit.Terminal, y int, r rune, click func(x, y int)) {
	t.Helper()
	line := []rune(strings.Split(term.Snapshot(), "\n")[y])
	x := slices.Index(line, r)
	if x < 0 {
		t.Fatalf("no %q on line %d: %q", r, y, string(line))
	}
	if click != nil {
		click(x, y)
		return
	}
	if !term.Click(uint16(x), uint16(y)) {
		t.Fatalf("nothing took a click on %q", r)
	}
}

// lineOf returns the first line of term that holds text.
func lineOf(t *testing.T, term *testkit.Terminal, text string) int {
	t.Helper()
	for y, l := range strings.Split(term.Snapshot(), "\n") {
		if strings.Contains(l, text) {
			return y
		}
	}
	t.Fatalf("no %q on the screen:\n%s", text, term.Snapshot())
	return -1
}

// TestSettingArrows clicks the ◀ and ▶ of a setting: in the filter tab,
// at its top, where ◀ once raised it; in the symbol tab; and on the
// options screen, through Limoni and through the screen's own click.
func TestSettingArrows(t *testing.T) {
	m, ed, term := testEditor(t, 1, 160, 50)
	drawEditor(m, ed, term)
	ed.openFilter(ed.selectedEntry())
	drawEditor(m, ed, term)
	fs := ed.filter
	si := slices.IndexFunc(fs.rows, func(r *optRow) bool { return r.label == "Saturation" })
	sat := fs.rows[si]
	y := lineOf(t, term, "Saturation")
	for i := 0; i < 12; i++ {
		clickRune(t, term, y, '▶', nil)
		drawEditor(m, ed, term)
	}
	if sat.num != 100 || fs.state.Selected != si {
		t.Fatalf("▶ twelve times: saturation %d, row %d selected", sat.num, fs.state.Selected)
	}
	clickRune(t, term, y, '◀', nil)
	drawEditor(m, ed, term)
	if sat.num != 90 {
		t.Errorf("◀ at the top: saturation %d, want 90", sat.num)
	}
	clickRune(t, term, y, '◀', ed.click)
	if sat.num != 80 {
		t.Errorf("◀ through the editor's own click: saturation %d, want 80", sat.num)
	}

	// The symbol tab's color.
	ed.openSymbol(ed.selectedEntry())
	drawEditor(m, ed, term)
	color := ed.symbol.rows[1]
	before := color.choice
	clickRune(t, term, lineOf(t, term, "Color"), '▶', nil)
	if color.choice != (before+1)%len(color.choices) {
		t.Errorf("▶ on the color: choice %d, was %d", color.choice, before)
	}

	// The options screen.
	row := &optRow{kind: optNumber, label: "Number", num: 5, min: 0, max: 10, step: 1, editable: true}
	s := &optionsScreen{m: m, state: widgets.NewListState(), tabs: []*optTab{{name: "Display", rows: []*optRow{row}}}}
	s.state.Select(0)
	area := term.Area()
	drawOpts := func() {
		term.Draw(func(f *terminal.Frame) {
			s.draw(f, s.box(Box{0, 0, int(area.Width), int(area.Height)}), &drawEnv{pal: newPalette(nil)})
		})
	}
	drawOpts()
	oy := lineOf(t, term, "Number")
	clickRune(t, term, oy, '◀', nil)
	drawOpts()
	clickRune(t, term, oy, '◀', s.click)
	if row.num != 3 {
		t.Errorf("◀ twice on the options screen: %d, want 3", row.num)
	}
	clickRune(t, term, oy, '▶', s.click)
	if row.num != 4 {
		t.Errorf("▶ on the options screen: %d, want 4", row.num)
	}
}

// TestColorRangeSpectrum picks a range to start from, which brings its
// limits and the spectrum; moves a limit by click on the spectrum and
// both by a drag, which makes the range Custom; and goes back to every
// color.
func TestColorRangeSpectrum(t *testing.T) {
	m, ed, term := testEditor(t, 1, 160, 50)
	drawEditor(m, ed, term)
	ed.openFilter(ed.selectedEntry())
	drawEditor(m, ed, term)
	fs := ed.filter
	pick := func(name string) {
		t.Helper()
		for i, r := range fs.rows {
			if r.label == "Color range" {
				fs.state.Select(i)
				r.choice = slices.IndexFunc(filterRanges, func(fr struct {
					name string
					r    *tiles.Range
				}) bool {
					return fr.name == name
				})
				r.set(r)
				drawEditor(m, ed, term)
				return
			}
		}
		t.Fatal("no Color range row")
	}
	if strings.Contains(term.Snapshot(), "Hues from") {
		t.Error("limits show with every color")
	}
	pick("Reds")
	snap := term.Snapshot()
	for _, want := range []string{"Hues from", "Colorfulness to", "hues 330° to 30°, lightness 0–100%, colorfulness 20–100%"} {
		if !strings.Contains(snap, want) {
			t.Errorf("no %q with the reds:\n%s", want, snap)
		}
	}
	// The spectrum: its markers' line, and the line above is its bottom
	// row. Click a third of the way along, at green: 30° is nearer than
	// 330°, so the range's end moves there.
	y := lineOf(t, term, "▲") - 1
	line := []rune(strings.Split(snap, "\n")[y])
	left := slices.Index(line, '▀')
	width := 0
	for x := left; x < len(line) && line[x] == '▀'; x++ {
		width++
	}
	sw := width - 3 // less the gap and the grays
	clickAt(t, term, left+sw/3, y)
	drawEditor(m, ed, term)
	if rg := fs.f.Range; rg.From != 330 || rg.To < 115 || rg.To > 125 {
		t.Errorf("after a click at green: %+v", rg)
	}
	if !strings.Contains(term.Snapshot(), "Custom") {
		t.Error("the range is not Custom")
	}
	// A drag from a sixth of the way to half, over the bottom two of five
	// rows: lightness 0% to 40%.
	term.Mouse(driver.MouseEvent{Button: driver.MouseLeft, X: uint16(left + sw/6), Y: uint16(y - 1)})
	term.Drag(uint16(left+sw/2), uint16(y))
	drawEditor(m, ed, term)
	if rg := fs.f.Range; rg.From < 55 || rg.From > 65 || rg.To < 175 || rg.To > 185 || math.Abs(rg.LightTo-0.4) > 1e-9 || rg.LightFrom != 0 {
		t.Errorf("after a drag: %+v", rg)
	}
	pick("All colors")
	if fs.f.Range != nil || strings.Contains(term.Snapshot(), "Hues from") {
		t.Errorf("all colors: range %+v", fs.f.Range)
	}
	pick("Greys")
	if !strings.Contains(term.Snapshot(), "every hue, lightness 0–100%, colorfulness 0–20%") {
		t.Errorf("the greys:\n%s", term.Snapshot())
	}
}

// TestFilterColorRows puts each color right under its setting, once,
// even with an animation's second set of settings.
func TestFilterColorRows(t *testing.T) {
	m, ed, term := testEditor(t, 1, 160, 50)
	drawEditor(m, ed, term)
	ed.openFilter(ed.selectedEntry())
	fs := ed.filter
	fs.f.Anim = &tiles.Anim{To: fs.f.Look, Curve: tiles.CurveSmooth, Period: 1.5, Spread: tiles.SpreadTogether}
	fs.build()
	var labels []string
	for _, r := range fs.rows {
		labels = append(labels, strings.TrimSpace(r.label))
	}
	for _, pair := range [][2]string{{"Outline", "Outline color"}, {"Inner glow", "Glow color"}, {"Sparkle", "Sparkle color"}} {
		i := slices.Index(labels, pair[0])
		if i < 0 || i+1 >= len(labels) || labels[i+1] != pair[1] {
			t.Errorf("%q does not come right under %q: %q", pair[1], pair[0], labels)
		}
		n := 0
		for _, l := range labels {
			if l == pair[1] {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%q shows %d times", pair[1], n)
		}
	}
	// Red sparkles.
	for _, r := range fs.rows {
		if strings.TrimSpace(r.label) == "Sparkle color" {
			r.choice = slices.IndexFunc(filterColors, func(c struct {
				name string
				c    tiles.RGB
			}) bool {
				return c.name == "Red"
			})
			r.set(r)
		}
	}
	if fs.f.SparkleColor == nil || *fs.f.SparkleColor != (tiles.RGB{230, 40, 40}) {
		t.Errorf("sparkle color %v, want red", fs.f.SparkleColor)
	}
}

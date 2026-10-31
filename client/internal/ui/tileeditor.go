package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

// The Tile Editor: one window in three columns. Column 1 lists
// everything that can take a tile, with a tab for each kind. Column 2
// shows the selected entry's tile at the map's tile size, with its
// filter. Column 3 has two tabs: the tile sheet, to pick a tile, and the
// filter's settings (see filtereditor.go). While the list has the keys,
// column 3 follows the selected entry. Choosing a tile sends SET_VISUAL,
// so the map changes at once behind the editor. Save writes the player's
// file of changes with SAVE_VISUALS; leaving without saving sends
// RELOAD_VISUALS, which puts back what was saved.

// tileEditorID names the editor's list in Limoni's accessibility tree,
// for tests.
const tileEditorID = "tile-editor"

// tileKinds are the editor's tabs, in order, by the kind VISUALS names.
var tileKinds = []struct{ kind, tab string }{
	{"player", "Player"},
	{"monster", "Monsters"},
	{"object", "Objects"},
	{"flavor", "Flavors"},
	{"terrain", "Terrain"},
	{"trap", "Traps"},
	{"effect", "Effects"},
}

// tileEditor is the browser: the shared list screen, with a tab per
// kind.
type tileEditor struct {
	*listScreen
	m *Model
	// v is the VISUALS answer, kept up to date with the changes sent.
	v proto.EventData
	// rows are the entries the list shows, by their index in v.Entries.
	rows []int
	// missing shows only the entries that lack a tile.
	missing bool
	// dirty is set when there are changes not yet saved.
	dirty bool
	// users lists, for each tile, the entries using it; nil until needed.
	users map[[2]int][]int
	// widest is the width of the widest row in any tab, so the columns
	// stay put when the tab changes.
	widest int

	// focus is the column that has the keys: the list, or column 3.
	focus int
	// side is column 3's tab: sideTile or sideFilter.
	side int
	// picker and filter are column 3's tabs, each on the entry it shows;
	// sideWhy says why the filter tab has no filter to show, such as an
	// entry with no tile.
	picker  *tilePicker
	filter  *filterScreen
	symbol  *symbolScreen
	sideWhy string
	// filterFor is the entry the filter tab was made for, or -1.
	filterFor int
	// sideShown says column 3 was drawn last time; a narrow window leaves
	// it out while the list has the keys.
	sideShown bool
	// preview is column 2's picture, made again when previewKey changes.
	// previewMoves says its filter is animated, so ticks redraw it.
	preview      image.Image
	previewKey   string
	previewMoves bool
	// colHotspots are the columns and column 3's tabs, as last drawn.
	colHotspots []hotspot
}

// Which column has the keys.
const (
	focusList = iota
	focusSide
)

// Column 3's tabs.
const (
	sideTile = iota
	sideFilter
	sideSymbol
)

// sideTabs names column 3's tabs, in order.
var sideTabs = []string{"Tile", "Filter", "Symbol"}

func newTileEditor(m *Model, v proto.EventData) *tileEditor {
	ed := &tileEditor{m: m, v: v, filterFor: -1}
	ed.listScreen = newListScreen(tileEditorID, "Tile Editor: "+v.VisualSet, nil)
	for _, k := range tileKinds {
		ed.tabs = append(ed.tabs, k.tab)
	}
	ed.onTab = func(int) { ed.fill() }
	ed.searchable = true
	ed.clicked = func() { ed.focus = focusList }
	ed.choose = func(item int) { ed.pick(ed.rows[item]) }
	ed.cancel = ed.leave
	undo := func(item int) {
		if item >= 0 {
			ed.undo(ed.rows[item])
		}
	}
	save := func(int) { ed.save(nil) }
	ed.keys = []listKey{
		{Key: 'm', Label: "missing only", Do: func(int) { ed.toggleMissing() }},
		{Key: 'u', Label: "undo", Do: undo},
		{Key: 'f', Label: "filter", Do: func(item int) {
			if item >= 0 {
				ed.openFilter(ed.rows[item])
			}
		}},
		{Key: 's', Label: "save", Do: save},
	}
	ed.checks = []listCheck{{Label: "Missing only", On: func() bool { return ed.missing }, Toggle: ed.toggleMissing}}
	// Column 3's tabs give the sheet, the filter, and the symbol.
	ed.buttons = []listButton{
		{Label: "Undo", Do: undo},
		{Label: "Save", Do: save},
		{Label: "Close", Do: func(int) { ed.leave() }},
	}
	ed.fill()
	ed.settle(1)
	for _, e := range v.Entries {
		ed.widest = max(ed.widest, rowWidth(ed.item(e)))
	}
	return ed
}

// toggleMissing shows only the entries that lack a tile, or all again.
func (ed *tileEditor) toggleMissing() {
	ed.missing = !ed.missing
	ed.fill()
	ed.state.Select(0)
	ed.settle(1)
}

// rowWidth is the width a list row needs, as listWidth counts it.
func rowWidth(it listItem) int {
	w := len([]rune(it.Label)) + 9
	if len(it.More) > 0 {
		w += moreWidth(it) + 1
	}
	if it.Hint != "" {
		w += len([]rune(it.Hint)) + 2
	}
	return w
}

// box fills the window, less a margin of two cells at the sides and one
// row at the top and bottom, which look about the same.
func (ed *tileEditor) box(area Box) Box {
	return Box{area.X + 2, area.Y + 1, max(area.W-4, 1), max(area.H-2, 1)}
}

// Column widths: the list takes room for its rows, the preview room for
// the tile and its notes, and column 3 the rest.
const (
	listMinW    = 32
	listMaxW    = 50
	previewMinW = 32
	sideMinW    = 40
)

// columns splits the editor's box into its three columns. In a window too
// narrow for all three, column 3 takes column 2's place while it has the
// keys, as the separate windows used to; the empty box is the column
// left out.
func (ed *tileEditor) columns(b Box) (list, preview, side Box) {
	lw := min(max(ed.widest+2, listMinW), listMaxW, max(b.W/2, 1))
	pw := previewMinW
	if ts := ed.m.terms.TileSet; ts != nil {
		pw = max(pw, previewZoom*int(ts.CellsW)+4)
	}
	list = Box{b.X, b.Y, lw, b.H}
	rest := Box{b.X + lw, b.Y, b.W - lw, b.H}
	switch {
	case rest.W-pw >= sideMinW:
		return list, Box{rest.X, rest.Y, pw, rest.H}, Box{rest.X + pw, rest.Y, rest.W - pw, rest.H}
	case ed.focus == focusSide:
		return list, Box{}, rest
	}
	return list, rest, Box{}
}

func (ed *tileEditor) key(ev driver.KeyEvent) {
	if ed.focus == focusList {
		ed.listScreen.key(ev)
		return
	}
	switch {
	case ed.side == sideTile && ed.picker != nil:
		ed.picker.key(ev)
	case ed.side == sideFilter && ed.filter != nil:
		ed.filter.key(ev)
	case ed.side == sideSymbol && ed.symbol != nil:
		ed.symbol.key(ev)
	case ev.Type == driver.KeyEsc:
		ed.back()
	}
}

// back gives the keys back to the list.
func (ed *tileEditor) back() { ed.focus = focusList }

// selectedEntry returns the entry selected in the list, by its index in
// v.Entries, or -1.
func (ed *tileEditor) selectedEntry() int {
	if i := ed.selected(); i >= 0 && i < len(ed.rows) {
		return ed.rows[i]
	}
	return -1
}

// sideEntry returns the entry column 3 shows, or -1.
func (ed *tileEditor) sideEntry() int {
	switch {
	case ed.side == sideTile && ed.picker != nil:
		return ed.picker.e
	case ed.side == sideFilter && ed.filter != nil:
		return ed.filter.e
	case ed.side == sideSymbol && ed.symbol != nil:
		return ed.symbol.e
	}
	return -1
}

// follow makes column 3 show the selected entry while the list has the
// keys, so moving through the list shows where each tile sits on the
// sheet and what filter it has.
func (ed *tileEditor) follow() {
	if ed.focus == focusSide {
		return
	}
	e := ed.selectedEntry()
	if e < 0 {
		ed.forgetSide()
		return
	}
	if ed.picker == nil || ed.picker.e != e {
		ed.picker = ed.newPicker(e)
	}
	if ed.side == sideFilter && ed.filterFor != e {
		ed.filter, ed.sideWhy = ed.newFilter(e)
		ed.filterFor = e
	}
	if ed.side == sideSymbol && (ed.symbol == nil || ed.symbol.e != e) {
		ed.symbol = ed.newSymbol(e)
	}
}

// forgetSide drops column 3's tabs, which follow makes again.
func (ed *tileEditor) forgetSide() {
	ed.picker, ed.filter, ed.symbol, ed.sideWhy, ed.filterFor = nil, nil, nil, "", -1
}

func (ed *tileEditor) draw(f *terminal.Frame, b Box, env *drawEnv) {
	ed.colHotspots = ed.colHotspots[:0]
	ed.follow()
	list, preview, side := ed.columns(b)
	fillBox(f, b, screenBg)
	ed.colHotspot(f, list, true, ed.back)
	ed.focused = ed.focus == focusList
	ed.listScreen.draw(f, list, env)
	if !preview.Empty() {
		ed.drawPreview(f, preview, env)
	}
	ed.sideShown = !side.Empty()
	if ed.sideShown {
		ed.drawSide(f, side, env)
	}
}

// colHotspot makes part of the editor outside the list and column 3's
// own controls clickable: a whole column, a region, or a tab.
func (ed *tileEditor) colHotspot(f *terminal.Frame, b Box, region bool, do func()) {
	ed.colHotspots = append(ed.colHotspots, hotspot{b, do, region})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

// controls returns the parts a click acts on, as last drawn, the last
// drawn on top.
func (ed *tileEditor) controls() []hotspot {
	out := append(append([]hotspot(nil), ed.colHotspots...), ed.listScreen.hotspots...)
	switch {
	case !ed.sideShown:
	case ed.side == sideTile && ed.picker != nil:
		out = append(out, ed.picker.hotspots...)
	case ed.side == sideFilter && ed.filter != nil:
		out = append(out, ed.filter.hotspots...)
	case ed.side == sideSymbol && ed.symbol != nil:
		out = append(out, ed.symbol.hotspots...)
	}
	return out
}

// click handles a click that reaches Update: on column 3's controls,
// then the list's, then the columns and tabs.
func (ed *tileEditor) click(x, y int) {
	var side []hotspot
	switch {
	case !ed.sideShown:
	case ed.side == sideTile && ed.picker != nil:
		side = ed.picker.hotspots
	case ed.side == sideFilter && ed.filter != nil:
		side = ed.filter.hotspots
	case ed.side == sideSymbol && ed.symbol != nil:
		side = ed.symbol.hotspots
	}
	for _, hs := range [][]hotspot{side, ed.listScreen.hotspots, ed.colHotspots} {
		// The last drawn is on top.
		for i := len(hs) - 1; i >= 0; i-- {
			if h := hs[i]; x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
				h.do()
				return
			}
		}
	}
}

// drawSide draws column 3: its tabs, and the sheet or the filter.
func (ed *tileEditor) drawSide(f *terminal.Frame, b Box, env *drawEnv) {
	title := " Tile sheet "
	switch e := ed.sideEntry(); {
	case e >= 0 && ed.side == sideTile:
		title = " Pick a tile: " + ed.v.Entries[e].Name + " "
	case e >= 0 && ed.side == sideFilter:
		title = " Filter: " + ed.v.Entries[e].Name + " "
	case e >= 0:
		title = " Symbol: " + ed.v.Entries[e].Name + " "
	case ed.side == sideFilter:
		title = " Filter "
	}
	border := screenBorder
	if ed.focus == focusSide {
		border = screenFocus
	}
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(title).
		WithTitleStyle(screenTitle).WithBorderStyle(border), rect(b))
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}
	ed.colHotspot(f, b, true, func() {
		if ed.sideEntry() >= 0 {
			ed.focus = focusSide
		}
	})

	x := inner.X + 1
	for i, t := range sideTabs {
		label := " " + t + " "
		st := screenDim
		if i == ed.side {
			st = screenSelected
		}
		w := len(label)
		f.Text(x, inner.Y, label, st)
		ed.colHotspot(f, Box{x, inner.Y, w, 1}, false, func() { ed.openSide(i) })
		x += w + 1
	}
	content := Box{inner.X, inner.Y + 2, inner.W, inner.H - 2}
	switch {
	case ed.side == sideTile && ed.picker != nil:
		ed.picker.draw(f, content, env)
	case ed.side == sideFilter && ed.filter != nil:
		ed.filter.draw(f, content, env)
	case ed.side == sideSymbol && ed.symbol != nil:
		ed.symbol.draw(f, content, env)
	case ed.side == sideFilter && ed.sideWhy != "":
		f.RenderWidget(&styledView{text: proto.PlainText(colorWhite, ed.sideWhy), pal: env.pal},
			rect(Box{content.X + 1, content.Y, content.W - 2, content.H}))
	default:
		f.TextFit(content.X+1, content.Y, content.W-2, "Select a row in the list.", screenDim)
	}
}

// openSide opens column 3's tab t on the entry it shows, or the
// selected one, and gives it the keys.
func (ed *tileEditor) openSide(t int) {
	e := ed.sideEntry()
	if ed.focus == focusList || e < 0 {
		e = ed.selectedEntry()
	}
	if e < 0 {
		return
	}
	switch t {
	case sideTile:
		ed.pick(e)
	case sideFilter:
		ed.openFilter(e)
	default:
		ed.openSymbol(e)
	}
}

// previewEntry returns the entry column 2 shows: the one column 3 works
// on while it has the keys, or else the selected one.
func (ed *tileEditor) previewEntry() int {
	if e := ed.sideEntry(); ed.focus == focusSide && e >= 0 {
		return e
	}
	return ed.selectedEntry()
}

// previewTile returns the tile column 2 shows for entry e, and which of
// its tiles that is for an entry with several: the one the sheet is
// changing, or else the one beside its name in the list.
func (ed *tileEditor) previewTile(e int) (*[2]int, string) {
	en := ed.v.Entries[e]
	names := ed.slotNames(en)
	p := ed.picker
	if ed.focus == focusSide && ed.side == sideTile && p != nil && p.e == e && p.slot >= 0 && p.slot < len(en.Tiles) {
		label := ""
		if p.slot < len(names) {
			label = slotLabel(en.Kind, names[p.slot])
		}
		return en.Tiles[p.slot], label
	}
	t := ed.mainTile(en)
	if t == nil {
		for _, u := range en.Tiles {
			if u != nil {
				return u, ""
			}
		}
		return nil, ""
	}
	if names != nil {
		for i, u := range en.Tiles {
			if u == t && i < len(names) {
				return t, slotLabel(en.Kind, names[i])
			}
		}
	}
	return t, ""
}

// previewZoom is how many times larger than on the map column 2 shows
// a tile.
const previewZoom = 3

// previewSize returns the cells column 2's picture takes in box b:
// previewZoom times the cells a tile takes on the map, or fewer if they
// do not fit, and whether it had to shrink. The map's tile size is its
// cells' height.
func (ed *tileEditor) previewSize(b Box) (w, h int, shrunk bool) {
	ts := ed.m.terms.TileSet
	if ts == nil {
		return 0, 0, false
	}
	size := max(int(ts.CellsH), 1)
	bw, bh := max(int(ts.CellsW)/size, 1), max(int(ts.CellsH)/size, 1)
	want := previewZoom * size
	s := want
	for s > 1 && (bw*s > b.W-2 || bh*s > b.H/2) {
		s--
	}
	return bw * s, bh * s, s < want
}

// look is a tile as the map shows it now: the sheet's tile behind it,
// through its filter at the current step of its animation.
type look struct {
	base          [2]uint8
	f             tiles.Filter
	step, variant int
	terrain       bool
}

// lookOf returns tile t as the map shows it now.
func (ed *tileEditor) lookOf(t [2]int, terrain bool) look {
	m := ed.m
	at := [2]uint8{uint8(t[0]), uint8(t[1])}
	l := look{base: at, f: tiles.NewFilter(), terrain: terrain}
	if v, ok := m.sheet.Edits().Virtual[at]; ok {
		l.base, l.f = v.Base, v.Filter
	}
	var tm time.Duration
	if m.cfg.Animate {
		tm = time.Duration(m.tick) * tickTime
	}
	l.step, l.variant = l.f.StepAt(tm, 0, 0)
	return l
}

// key names the picture of l.
func (l look) key() string {
	f, _ := json.Marshal(l.f)
	return fmt.Sprintf("%v/%v/%s/%d/%d", l.base, l.terrain, f, l.step, l.variant)
}

// picture draws l, its see-through parts left see-through.
func (l look) picture(s *tiles.Sheet) image.Image {
	return s.Filtered(l.base[0], l.base[1], l.f, l.step, l.variant, l.terrain, color.RGBA{})
}

// floorTile returns the set's lit floor, which the editor shows tiles
// on, or nil if the set has none.
func (ed *tileEditor) floorTile() *[2]int {
	for _, e := range ed.v.Entries {
		if e.Kind == "terrain" && e.Name == "open floor" {
			return ed.mainTile(e)
		}
	}
	return nil
}

// editorBg is the color under tiles where the set has no floor to show
// them on: the window's own background.
func editorBg() color.RGBA {
	r, g, b := screenBg.Bg.RGB()
	return color.RGBA{r, g, b, 255}
}

// previewPicture returns tile t on the set's floor, through its filter
// at the current step, made again only when that changes, so Limoni
// sends it once.
func (ed *tileEditor) previewPicture(t [2]int, terrain bool) image.Image {
	m := ed.m
	top := ed.lookOf(t, terrain)
	looks := []look{top}
	if fl := ed.floorTile(); fl != nil && *fl != t {
		looks = []look{ed.lookOf(*fl, true), top}
	}
	var key strings.Builder
	moves := false
	for _, l := range looks {
		key.WriteString(l.key() + "|")
		moves = moves || l.f.Animated()
	}
	if ed.preview == nil || ed.previewKey != key.String() {
		tw, th := m.sheet.TileSize()
		out := image.NewRGBA(image.Rect(0, 0, tw, th))
		draw.Draw(out, out.Bounds(), image.NewUniform(editorBg()), image.Point{}, draw.Src)
		for _, l := range looks {
			draw.Draw(out, out.Bounds(), l.picture(m.sheet), image.Point{}, draw.Over)
		}
		ed.preview, ed.previewKey = out, key.String()
	}
	ed.previewMoves = moves && m.cfg.Animate
	return ed.preview
}

// animated reports whether the preview moves, so ticks redraw it.
func (ed *tileEditor) animated() bool { return ed.previewMoves }

// drawPreview draws column 2: the tile, notes about it, and the entry's
// details.
func (ed *tileEditor) drawPreview(f *terminal.Frame, b Box, env *drawEnv) {
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" Preview ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenBorder), rect(b))
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}
	ed.previewMoves = false
	y := inner.Y
	line := func(text string, st cell.Style) {
		if y < inner.Y+inner.H {
			f.TextFit(inner.X+1, y, inner.W-2, text, st)
		}
		y++
	}
	e := ed.previewEntry()
	if e < 0 {
		line("Nothing selected.", screenDim)
		return
	}
	en := ed.v.Entries[e]
	t, label := ed.previewTile(e)
	switch {
	case ed.m.sheet == nil:
		line("The preview needs a tile set.", screenDim)
	case t == nil:
		line("No tile: the map shows its letter.", screenDim)
	default:
		w, h, shrunk := ed.previewSize(inner)
		at := Box{inner.X + (inner.W-w)/2, y + 1, w, h}
		for yy := at.Y; yy < at.Y+at.H; yy++ {
			for xx := at.X; xx < at.X+at.W; xx++ {
				f.Buffer.SetCell(uint16(xx), uint16(yy), cell.Cell{Content: cell.RuneImage})
			}
		}
		f.RegisterImageRegion(terminal.ImageRegion{Area: rect(at), Img: ed.previewPicture(*t, en.Kind == "terrain")})
		y = at.Y + at.H + 1
		if label != "" {
			line(label, screenText)
		}
		if shrunk {
			line("Shown smaller, to fit.", screenDim)
		}
	}
	// The letter the map shows in its place when it draws letters.
	if y < inner.Y+inner.H {
		label := "As a letter: "
		f.TextFit(inner.X+1, y, inner.W-2, label, screenText)
		f.Text(inner.X+1+len(label), y, " "+en.Char+" ", cell.NewStyle().WithFg(env.pal.color(en.Color)).WithBg(limoni.RGB(0, 0, 0)))
		y++
	}
	if fs := ed.filter; ed.side == sideFilter && fs != nil && fs.e == e {
		fs.notes(func(text string, st cell.Style) {
			y++
			for _, l := range wrapPlain(text, inner.W-2) {
				line(l, st)
			}
		})
	}
	y++
	if rest := (Box{inner.X + 1, y, inner.W - 2, inner.Y + inner.H - y}); rest.H > 0 {
		f.RenderWidget(&styledView{text: ed.detailText(en), pal: env.pal}, rect(rest))
	}
}

// kind is the kind the current tab lists.
func (ed *tileEditor) kind() string { return tileKinds[ed.tab].kind }

// slotNames returns the names of an entry's tiles, or nil for one tile.
func (ed *tileEditor) slotNames(e proto.VisualData) []string {
	switch e.Kind {
	case "terrain", "trap":
		return ed.v.Lights
	case "effect":
		return ed.v.Directions
	}
	return nil
}

// lacks reports whether an entry lacks a tile in some slot. A flavored
// object kind shows its flavor's tile, so it lacks none of its own.
func lacks(e proto.VisualData) bool {
	if e.Flavored {
		return false
	}
	for _, t := range e.Tiles {
		if t == nil {
			return true
		}
	}
	return false
}

// mainTile is the tile drawn beside an entry's name: lit terrain and
// traps, and an effect's still tile.
func (ed *tileEditor) mainTile(e proto.VisualData) *[2]int {
	i := 0
	if e.Kind == "terrain" || e.Kind == "trap" {
		for n, l := range ed.v.Lights {
			if l == "lit" {
				i = n
			}
		}
	}
	if i < len(e.Tiles) {
		return e.Tiles[i]
	}
	return nil
}

func pict(t *[2]int) *proto.PictCell {
	if t == nil {
		return nil
	}
	return &proto.PictCell{FgRow: uint8(t[0]), FgCol: uint8(t[1]), BgRow: uint8(t[0]), BgCol: uint8(t[1])}
}

// fill lists the current tab's entries, under a heading for each group,
// such as the player's tiles for each class. A heading's row has no
// entry, -1.
func (ed *tileEditor) fill() {
	ed.rows = ed.rows[:0]
	var items []listItem
	if i, it, ok := ed.youRow(); ok {
		ed.rows = append(ed.rows, i)
		items = append(items, it)
	}
	group := ""
	for i, e := range ed.v.Entries {
		// The character comes first, and the default for every race and
		// class is not listed: the game keeps it, but nobody changes it.
		if e.Kind != ed.kind() || e.You || (e.Kind == "player" && e.ID == 0) || (ed.missing && !lacks(e)) {
			continue
		}
		if e.Group != "" && e.Group != group {
			ed.rows = append(ed.rows, -1)
			items = append(items, listItem{Label: e.Group, Header: true})
		}
		group = e.Group
		ed.rows = append(ed.rows, i)
		items = append(items, ed.item(e))
	}
	ed.items = items
	if ed.state.Selected >= len(items) || ed.state.Selected < 0 {
		ed.state.Select(min(max(ed.state.Selected, 0), len(items)-1))
	}
	ed.settle(1)
}

// youRow returns the Player tab's first row, <player>: the character
// alone, by their own entry. Without a tile of their own it shows the
// tile the map shows them with: their race and class's, or else the
// default. "Missing only" leaves it out, since the character needs no
// tile of their own.
func (ed *tileEditor) youRow() (int, listItem, bool) {
	if ed.kind() != "player" || ed.missing {
		return 0, listItem{}, false
	}
	you, pair, def := -1, -1, -1
	for i, e := range ed.v.Entries {
		switch {
		case e.Kind != "player":
		case e.You:
			you = i
		case e.ID == 0:
			def = i
		case e.ID == ed.v.PlayerEntry:
			pair = i
		}
	}
	if you < 0 {
		return 0, listItem{}, false
	}
	e := ed.v.Entries[you]
	it := ed.item(e)
	it.Label = "<player> (" + e.Name + ")"
	if lacks(e) {
		switch {
		case pair >= 0 && !lacks(ed.v.Entries[pair]):
			it.Tile = pict(ed.mainTile(ed.v.Entries[pair]))
			it.Hint = "uses " + ed.v.Entries[pair].Name
		case def >= 0:
			it.Tile = pict(ed.mainTile(ed.v.Entries[def]))
			it.Hint = "uses the default tile"
		}
	}
	return you, it, true
}

// item is an entry's row: its letter, its tile, its other tiles, and
// whether it changed.
func (ed *tileEditor) item(e proto.VisualData) listItem {
	it := listItem{Label: e.Name, Tile: pict(ed.mainTile(e)), Detail: ed.detailText(e)}
	if r := []rune(e.Char); len(r) > 0 {
		it.Icon, it.IconColor = r[0], e.Color
	}
	if len(e.Tiles) > 1 {
		for _, t := range e.Tiles {
			it.More = append(it.More, pict(t))
		}
	}
	switch {
	case ed.filtered(e) && e.Own != nil:
		it.Hint = "filter"
	case e.Own != nil:
		it.Hint = "changed"
	case e.OwnSymbol != nil:
		it.Hint = "symbol"
	case e.Flavored:
		it.Hint = "flavor's tile"
	case lacks(e) && e.Kind == "player" && e.ID > 0:
		// A race and class with no tile of its own shows the default.
		it.Hint = "uses the default tile"
		for _, d := range ed.v.Entries {
			if d.Kind == "player" && d.ID == 0 {
				it.Tile = pict(ed.mainTile(d))
			}
		}
	case lacks(e):
		it.Hint = "no tile"
	}
	return it
}

// refresh redraws the rows after a change, keeping the selection.
func (ed *tileEditor) refresh() {
	ed.users = nil
	if ed.focus == focusList {
		// Column 3 follows the list; it is made again from the change.
		ed.forgetSide()
	}
	sel := ed.state.Selected
	ed.fill()
	if sel < len(ed.items) {
		ed.state.Select(sel)
	}
}

// usersOf returns the entries that use a tile, in any slot.
func (ed *tileEditor) usersOf(t [2]int) []int {
	if ed.users == nil {
		ed.users = map[[2]int][]int{}
		for i, e := range ed.v.Entries {
			seen := map[[2]int]bool{}
			for _, u := range e.Tiles {
				if u != nil && !seen[*u] {
					seen[*u] = true
					ed.users[*u] = append(ed.users[*u], i)
				}
			}
		}
	}
	return ed.users[t]
}

// detailText describes an entry for the detail panel.
func (ed *tileEditor) detailText(e proto.VisualData) proto.StyledText {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", e.Name)
	names := ed.slotNames(e)
	for i, t := range e.Tiles {
		label := "Tile"
		if names != nil && i < len(names) {
			label = slotLabel(e.Kind, names[i])
		}
		where := "a letter"
		if t != nil {
			where = fmt.Sprintf("row %d, column %d", t[0], t[1])
			if v, ok := ed.virtual(t); ok {
				where = fmt.Sprintf("row %d, column %d, with a filter", v.Base[0], v.Base[1])
			}
		}
		fmt.Fprintf(&b, "%s: %s\n", label, where)
	}
	if e.Flavored {
		b.WriteString("\nThe map shows this object with its flavor's tile, on the Flavors tab, until you learn what it is.\n")
	}
	if e.Own != nil {
		b.WriteString("\nYou changed this. u puts back the set's own tile.\n")
	}
	b.WriteString("\nEnter picks a tile on the sheet. f gives it a filter: colors, an outline, or an animation. The Symbol tab changes the letter the map shows when it draws letters.")
	return proto.PlainText(colorWhite, b.String())
}

// virtual returns the virtual tile t, if it is one.
func (ed *tileEditor) virtual(t *[2]int) (tiles.Virtual, bool) {
	if t == nil || ed.m.sheet == nil {
		return tiles.Virtual{}, false
	}
	v, ok := ed.m.sheet.Edits().Virtual[[2]uint8{uint8(t[0]), uint8(t[1])}]
	return v, ok
}

// filtered reports whether any of an entry's tiles has a filter.
func (ed *tileEditor) filtered(e proto.VisualData) bool {
	for _, t := range e.Tiles {
		if _, ok := ed.virtual(t); ok {
			return true
		}
	}
	return false
}

// slotLabel names one of an entry's tiles for people.
func slotLabel(kind, name string) string {
	if kind != "effect" {
		switch name {
		case "los":
			return "In view"
		case "torch":
			return "By torchlight"
		case "lit":
			return "Lit"
		case "dark":
			return "Remembered"
		}
		return name
	}
	if name == "static" {
		return "Ball or explosion"
	}
	return "Bolt at " + name + "°"
}

// set gives slot i of entry e a tile, or every slot for i < 0, both here
// and in the engine.
func (ed *tileEditor) set(e, i int, t [2]int) {
	en := &ed.v.Entries[e]
	if en.Own == nil {
		en.Own = append([]*[2]int(nil), en.Tiles...)
	}
	args := map[string]any{"kind": en.Kind, "id": en.ID, "tile": t}
	for n := range en.Tiles {
		if i < 0 || n == i {
			tt := t
			en.Tiles[n] = &tt
		}
	}
	if i >= 0 {
		args["index"] = i
	}
	if sameTiles(en.Tiles, en.Own) {
		en.Own = nil
	}
	ed.dirty = true
	ed.refresh()
	ed.m.command("SET_VISUAL", args, ed.checkResult)
}

// undo gives entry e back the set's own tiles.
func (ed *tileEditor) undo(e int) {
	en := &ed.v.Entries[e]
	if en.Own == nil {
		ed.m.notify("This still has the set's own tile.")
		return
	}
	en.Tiles, en.Own = en.Own, nil
	ed.dirty = true
	ed.refresh()
	ed.m.command("SET_VISUAL", map[string]any{"kind": en.Kind, "id": en.ID, "reset": true}, ed.checkResult)
}

// checkResult reports a refused change and reads the tiles again, so the
// editor shows what the engine has.
func (ed *tileEditor) checkResult(r proto.EventData) {
	if r.OK {
		return
	}
	ed.m.notify("Cannot change the tile: " + r.Error)
	ed.reread()
}

// reread reads the entries from the engine again.
func (ed *tileEditor) reread() {
	ed.m.command("VISUALS", nil, func(r proto.EventData) {
		if r.OK && ed.m.lastVisuals.VisualSet == ed.v.VisualSet {
			ed.v = ed.m.lastVisuals
			ed.refresh()
		}
	})
}

func sameTiles(a, b []*[2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if (a[i] == nil) != (b[i] == nil) || a[i] != nil && *a[i] != *b[i] {
			return false
		}
	}
	return true
}

// save writes the changes, then runs then if it is set.
func (ed *tileEditor) save(then func()) {
	ed.m.command("SAVE_VISUALS", nil, func(r proto.EventData) {
		if !r.OK {
			ed.m.notify("Cannot save your tiles: " + r.Error)
			return
		}
		if err := ed.saveFilters(); err != nil {
			ed.m.notify("Cannot save your filters: " + err.Error())
			return
		}
		ed.dirty = false
		saved := "Saved your tiles to tiles/" + ed.v.VisualSet + ".prf and .json"
		if slices.ContainsFunc(ed.v.Entries, func(e proto.VisualData) bool { return e.OwnSymbol != nil }) {
			saved += ", and your symbols to symbols.prf"
		}
		ed.m.notify(saved + ".")
		if then != nil {
			then()
		}
	})
}

// saveFilters writes the filters beside the engine's file of changes.
func (ed *tileEditor) saveFilters() error {
	m := ed.m
	if m.sheet == nil {
		return nil
	}
	path := m.editsPath
	if path == "" && ed.v.File != "" {
		path = tiles.EditsPath(ed.v.File)
	}
	if path == "" {
		return errors.New("the engine did not say where your tiles go")
	}
	ed.dropUnused()
	m.editsPath = path
	return m.sheet.Edits().Save(path)
}

// leave closes the editor, asking first if there are changes not saved.
func (ed *tileEditor) leave() {
	m := ed.m
	if !ed.dirty {
		m.pop()
		return
	}
	ask := newMenu("tile-editor-leave", "Unsaved tiles", []menuEntry{
		{label: "Save", do: func() {
			m.pop()
			ed.save(m.pop)
		}},
		{label: "Discard", do: func() {
			m.pop()
			m.pop()
			sheet := m.sheet
			m.command("RELOAD_VISUALS", nil, func(r proto.EventData) {
				if !r.OK {
					m.notify("Cannot put back the saved tiles: " + r.Error)
				}
			})
			m.loadEdits(sheet)
		}},
		{label: "Keep editing", do: m.pop},
	})
	ask.intro = proto.PlainText(colorWhite, "You changed tiles without saving them. Save them for next time, or put back the ones you saved?")
	ask.cancel = m.pop
	ask.width = 50
	m.push(ask)
}

// boltOffsets returns, for each bolt direction, how many columns right
// of the first its tile sits, as the set's shared bolt tiles do: the
// effects' most common bolt tiles, if they lie side by side on one row.
// Otherwise they go in the order VISUALS lists them.
func (ed *tileEditor) boltOffsets() []int {
	n := len(ed.v.Directions) - 1
	plain := make([]int, n)
	for i := range plain {
		plain[i] = i
	}
	counts := map[string]int{}
	tiles := map[string][]*[2]int{}
	for _, e := range ed.v.Entries {
		if e.Kind != "effect" || len(e.Tiles) != n+1 {
			continue
		}
		k := fmt.Sprint(tileList(e.Tiles[1:]))
		counts[k]++
		tiles[k] = e.Tiles[1:]
	}
	best := ""
	for k, c := range counts {
		if c > counts[best] || c == counts[best] && k < best {
			best = k
		}
	}
	bolts := tiles[best]
	if len(bolts) != n {
		return plain
	}
	first := 1 << 30
	for _, t := range bolts {
		if t == nil || t[0] != bolts[0][0] {
			return plain
		}
		first = min(first, t[1])
	}
	out := make([]int, n)
	used := map[int]bool{}
	for i, t := range bolts {
		out[i] = t[1] - first
		if out[i] >= n || used[out[i]] {
			return plain
		}
		used[out[i]] = true
	}
	return out
}

func tileList(ts []*[2]int) [][2]int {
	out := make([][2]int, len(ts))
	for i, t := range ts {
		if t != nil {
			out[i] = *t
		} else {
			out[i] = [2]int{-1, -1}
		}
	}
	return out
}

// pick opens the sheet in column 3 on entry e and gives it the keys.
func (ed *tileEditor) pick(e int) {
	if ed.picker == nil || ed.picker.e != e {
		ed.picker = ed.newPicker(e)
	}
	ed.side, ed.focus = sideTile, focusSide
}

// newPicker makes the sheet for entry e, its cursor on the entry's tile.
func (ed *tileEditor) newPicker(e int) *tilePicker {
	p := &tilePicker{ed: ed, e: e, slot: -1}
	en := ed.v.Entries[e]
	if t := ed.mainTile(en); t != nil {
		p.row, p.col = t[0], t[1]
	} else {
		for _, t := range en.Tiles {
			if t != nil {
				p.row, p.col = t[0], t[1]
				break
			}
		}
	}
	return p
}

// tilePicker is column 3's Tile tab: the tile sheet with a cursor, to
// give one entry a tile.
type tilePicker struct {
	ed *tileEditor
	e  int
	// slot is the tile that changes: a kind of light, a bolt direction,
	// or every one for -1.
	slot int
	// row, col is the tile under the cursor, and top, left the first
	// row and column shown.
	row, col  int
	top, left int
	// across and down are how many tiles fit, and cw, ch the cells each
	// covers, as last drawn.
	across, down int
	cw, ch       int
	// sheet is the picture of the part shown, made again when it moves.
	sheet    image.Image
	sheetKey string
	hotspots []hotspot
}

func (p *tilePicker) entry() proto.VisualData { return p.ed.v.Entries[p.e] }

func (p *tilePicker) size() (cols, rows int) {
	if s := p.ed.m.sheet; s != nil {
		return s.Size()
	}
	return 1, 1
}

func (p *tilePicker) moveTo(row, col int) {
	cols, rows := p.size()
	p.row, p.col = min(max(row, 0), rows-1), min(max(col, 0), cols-1)
}

func (p *tilePicker) key(ev driver.KeyEvent) {
	page := pageRows(p.down)
	switch ev.Type {
	case driver.KeyEsc:
		p.ed.back()
	case driver.KeyEnter:
		p.use()
	case driver.KeyTab:
		if n := len(p.entry().Tiles); n > 1 {
			d := 1
			if ev.Shift {
				d = -1
			}
			// -1 (all) through n-1.
			p.slot = (p.slot+1+d+n+1)%(n+1) - 1
		}
	case driver.KeyArrowUp:
		p.moveTo(p.row-1, p.col)
	case driver.KeyArrowDown:
		p.moveTo(p.row+1, p.col)
	case driver.KeyArrowLeft:
		p.moveTo(p.row, p.col-1)
	case driver.KeyArrowRight:
		p.moveTo(p.row, p.col+1)
	case driver.KeyPageUp:
		p.moveTo(p.row-page, p.col)
	case driver.KeyPageDown:
		p.moveTo(p.row+page, p.col)
	case driver.KeyHome:
		p.moveTo(p.row, 0)
	case driver.KeyEnd:
		cols, _ := p.size()
		p.moveTo(p.row, cols-1)
	case driver.KeyRune:
		switch ev.Ch {
		case 'k', '8':
			p.moveTo(p.row-1, p.col)
		case 'j', '2':
			p.moveTo(p.row+1, p.col)
		case 'h', '4':
			p.moveTo(p.row, p.col-1)
		case 'l', '6':
			p.moveTo(p.row, p.col+1)
		case 'f':
			p.useBolts()
		case '+', '=':
			p.zoom(1)
		case '-':
			p.zoom(-1)
		case '0':
			p.setZoom(0)
		}
	}
}

// use gives the entry the tile under the cursor and goes back to the
// list.
func (p *tilePicker) use() {
	p.ed.set(p.e, p.slot, [2]int{p.row, p.col})
	p.ed.back()
}

// wheel scrolls the sheet by d notches of the mouse wheel, taking the
// cursor along when it would leave the rows shown.
func (p *tilePicker) wheel(d int) { p.scrollRows(p.top + d*wheelStep) }

// scrollRows moves the view to start at row top, taking the cursor along
// when it would leave the rows shown.
func (p *tilePicker) scrollRows(top int) {
	_, rows := p.size()
	p.top = min(max(top, 0), max(rows-p.down, 0))
	p.row = min(max(p.row, p.top), p.top+max(p.down, 1)-1)
}

// scrollCols moves the view to start at column left, taking the cursor
// along when it would leave the columns shown.
func (p *tilePicker) scrollCols(left int) {
	cols, _ := p.size()
	p.left = min(max(left, 0), max(cols-p.across, 0))
	p.col = min(max(p.col, p.left), p.left+max(p.across, 1)-1)
}

// pickerZoomMax is the largest zoom, as tile sizes go.
const pickerZoomMax = 4

// cells returns the cells each tile takes on the sheet: those it takes
// on the map, or at a zoom the player chose, the set's smallest tile
// size times the zoom.
func (p *tilePicker) cells() (int, int) {
	m := p.ed.m
	tw, th := m.sheet.TileSize()
	cw, ch := max((2*tw+th/2)/th, 1), 1
	if ts := m.terms.TileSet; ts != nil {
		cw, ch = max(int(ts.CellsW), 1), max(int(ts.CellsH), 1)
	}
	z := m.settings.PickerZoom
	if z <= 0 {
		return cw, ch
	}
	s := max(m.scale, 1)
	return max(cw/s, 1) * z, max(ch/s, 1) * z
}

// zoomNow returns the zoom the sheet shows, as tile sizes go, and
// whether it follows the map.
func (p *tilePicker) zoomNow() (int, bool) {
	if z := p.ed.m.settings.PickerZoom; z > 0 {
		return z, false
	}
	return max(p.ed.m.scale, 1), true
}

// zoom makes the sheet's tiles d sizes bigger, or smaller for d < 0, and
// remembers the zoom.
func (p *tilePicker) zoom(d int) {
	z, _ := p.zoomNow()
	p.setZoom(min(max(z+d, 1), pickerZoomMax))
}

// setZoom sets the sheet's zoom, or 0 for the map's own size.
func (p *tilePicker) setZoom(z int) {
	m := p.ed.m
	if m.settings.PickerZoom == z {
		return
	}
	m.settings.PickerZoom = z
	m.saveSettings()
}

// useBolts gives an effect all four bolt tiles from the cursor, in the
// order the set keeps them, and goes back to the list.
func (p *tilePicker) useBolts() {
	if p.entry().Kind != "effect" {
		return
	}
	offs := p.ed.boltOffsets()
	cols, _ := p.size()
	if p.col+slices.Max(offs) >= cols {
		p.ed.m.notify("The four bolt tiles do not fit right of the cursor.")
		return
	}
	for i, off := range offs {
		p.ed.set(p.e, i+1, [2]int{p.row, p.col + off})
	}
	p.ed.back()
}

// hotspot makes part of the sheet clickable; a click gives column 3 the
// keys before it acts.
func (p *tilePicker) hotspot(f *terminal.Frame, b Box, do func()) {
	act := func() {
		p.ed.focus = focusSide
		do()
	}
	p.hotspots = append(p.hotspots, hotspot{box: b, do: act})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { act() })
}

// clickTile gives the entry the tile clicked.
func (p *tilePicker) clickTile(row, col int) {
	p.moveTo(row, col)
	p.use()
}

// pickerPanel is the width of the panel beside the sheet, and
// pickerPanelH its height under the sheet, in a column too narrow for it
// beside.
const (
	pickerPanel  = 34
	pickerPanelH = 14
)

// draw draws the sheet in b, with the panel beside it, or under it in a
// narrow column.
func (p *tilePicker) draw(f *terminal.Frame, b Box, env *drawEnv) {
	m := p.ed.m
	p.hotspots = p.hotspots[:0]
	var panel, sheetBox Box
	if b.W >= pickerPanel+36 {
		panel = Box{b.X + b.W - pickerPanel, b.Y, pickerPanel, b.H}
		sheetBox = Box{b.X + 1, b.Y, b.W - pickerPanel - 2, b.H}
		for y := 0; y < b.H; y++ {
			f.Text(panel.X-1, b.Y+y, "│", screenBorder)
		}
	} else {
		h := min(pickerPanelH, b.H/2)
		panel = Box{b.X, b.Y + b.H - h, b.W, h}
		sheetBox = Box{b.X + 1, b.Y, b.W - 2, b.H - h - 1}
	}
	p.drawPanel(f, panel, env)

	sheet := m.sheet
	if sheet == nil || env.sprite == nil {
		f.TextFit(sheetBox.X, sheetBox.Y, sheetBox.W, "The Tile Editor needs a tile set.", screenDim)
		return
	}
	tw, th := sheet.TileSize()
	p.cw, p.ch = p.cells()
	cols, rows := sheet.Size()
	// Scroll bars right of and under the sheet, when it does not fit.
	fit := func(w, h int) (int, int) {
		return min(max(w/p.cw, 1), cols), min(max(h/p.ch, 1), rows)
	}
	w, h := sheetBox.W, sheetBox.H
	p.across, p.down = fit(w, h)
	vbar, hbar := false, false
	for i := 0; i < 2; i++ {
		if !vbar && p.down < rows {
			vbar, w = true, w-1
		}
		if !hbar && p.across < cols {
			hbar, h = true, h-1
		}
		p.across, p.down = fit(w, h)
	}
	// Keep the cursor in sight.
	p.top = min(max(p.top, p.row-p.down+1), p.row)
	p.left = min(max(p.left, p.col-p.across+1), p.col)
	p.top, p.left = min(max(p.top, 0), rows-p.down), min(max(p.left, 0), cols-p.across)

	kitty := m.cfg.Protocol == "Kitty"
	// The tiles sit on the set's floor, as the set draws it.
	var floor *look
	floorKey := ""
	if fl := p.ed.floorTile(); fl != nil {
		floor = &look{base: [2]uint8{uint8(fl[0]), uint8(fl[1])}, f: tiles.NewFilter(), terrain: true}
		floorKey = floor.key()
	}
	key := fmt.Sprint(p.top, p.left, p.down, p.across, floorKey)
	if !kitty {
		// Other protocols cannot lay the cursor over the sheet, so it is
		// drawn in.
		key += fmt.Sprint(p.row, p.col)
	}
	if p.sheet == nil || p.sheetKey != key {
		var under image.Image
		if floor != nil {
			under = floor.picture(sheet)
		}
		img := sheet.Window(p.top, p.left, p.down, p.across, under, editorBg())
		if !kitty {
			rgba := img.(*image.RGBA)
			drawFrame(rgba, image.Rect((p.col-p.left)*tw, (p.row-p.top)*th, (p.col-p.left+1)*tw, (p.row-p.top+1)*th))
		}
		p.sheet, p.sheetKey = img, key
	}
	area := Box{sheetBox.X, sheetBox.Y, p.across * p.cw, p.down * p.ch}
	for y := area.Y; y < area.Y+area.H; y++ {
		for x := area.X; x < area.X+area.W; x++ {
			f.Buffer.SetCell(uint16(x), uint16(y), cell.Cell{Content: cell.RuneImage})
		}
	}
	f.RegisterImageRegion(terminal.ImageRegion{Area: rect(area), Img: p.sheet})
	f.RenderWidget(mouseArea{handle: func(ev driver.MouseEvent) {
		switch ev.Button {
		case driver.MouseScrollUp, driver.MouseScrollDown:
			d := 1
			if ev.Button == driver.MouseScrollUp {
				d = -1
			}
			switch {
			case ev.Ctrl:
				p.zoom(-d)
			case ev.Shift:
				p.scrollCols(p.left + d*wheelStep)
			default:
				p.wheel(d)
			}
		case driver.MouseScrollLeft:
			p.scrollCols(p.left - wheelStep)
		case driver.MouseScrollRight:
			p.scrollCols(p.left + wheelStep)
		case driver.MouseNone:
			if env.pointer != nil {
				env.pointer(int(ev.X), int(ev.Y))
			}
		}
	}}, rect(area))
	if vbar {
		f.RenderWidget(scrollBar{vertical: true, total: rows, shown: p.down, offset: p.top, set: p.scrollRows},
			rect(Box{area.X + area.W, area.Y, 1, area.H}))
	}
	if hbar {
		f.RenderWidget(scrollBar{total: cols, shown: p.across, offset: p.left, set: p.scrollCols},
			rect(Box{area.X, area.Y + area.H, area.W, 1}))
	}
	if kitty {
		at := Box{area.X + (p.col-p.left)*p.cw, area.Y + (p.row-p.top)*p.ch, p.cw, p.ch}
		f.RegisterImageRegion(terminal.ImageRegion{Area: rect(at), Img: cursorFrame(tw, th), Transparent: true, ZIndex: 1})
	}
	for r := 0; r < p.down; r++ {
		for c := 0; c < p.across; c++ {
			row, col := p.top+r, p.left+c
			p.hotspot(f, Box{area.X + c*p.cw, area.Y + r*p.ch, p.cw, p.ch}, func() { p.clickTile(row, col) })
		}
	}
}

// drawPanel draws what the cursor is on and the keys.
func (p *tilePicker) drawPanel(f *terminal.Frame, b Box, env *drawEnv) {
	e := p.entry()
	y := b.Y
	line := func(text string, st cell.Style) {
		if y < b.Y+b.H {
			f.TextFit(b.X+1, y, b.W-2, text, st)
		}
		y++
	}
	line(e.Name, screenTitle)
	y++
	if names := p.ed.slotNames(e); names != nil {
		which := "All of them"
		if e.Kind == "terrain" || e.Kind == "trap" {
			which = "In every light"
		}
		if p.slot >= 0 && p.slot < len(names) {
			which = slotLabel(e.Kind, names[p.slot])
		}
		line("Changing: "+which, screenText)
		line("Tab picks another", screenDim)
		y++
	}
	line(fmt.Sprintf("Cursor: row %d, column %d", p.row, p.col), screenText)
	users := p.ed.usersOf([2]int{p.row, p.col})
	if len(users) == 0 {
		line("Nothing uses this tile.", screenDim)
	} else {
		line("Used by:", screenText)
		names := make([]string, 0, len(users))
		for _, u := range users {
			names = append(names, p.ed.v.Entries[u].Name)
		}
		sort.Strings(names)
		room := b.Y + b.H - y - 8
		for i, n := range names {
			if i == room-1 && len(names) > room {
				line(fmt.Sprintf("  and %d more", len(names)-i), screenDim)
				break
			}
			line("  "+n, screenDim)
		}
	}
	y = max(y+1, b.Y+b.H-8)
	z, follows := p.zoomNow()
	zoom := fmt.Sprintf("Zoom: size %d", z)
	if follows {
		zoom += ", as the map"
	}
	if y < b.Y+b.H {
		f.TextFit(b.X+1, y, b.W-2, zoom, screenText)
		x := b.X + 1 + len(zoom) + 2
		for _, c := range []struct {
			label string
			d     int
		}{{"[-]", -1}, {"[+]", 1}} {
			if x+len(c.label) <= b.X+b.W-1 {
				f.Text(x, y, c.label, screenText)
				d := c.d
				p.hotspot(f, Box{x, y, len(c.label), 1}, func() { p.zoom(d) })
				x += len(c.label) + 1
			}
		}
	}
	y++
	line("+ - zoom  0 the map's size", screenDim)
	line("Enter use this tile", screenDim)
	line("Click a tile to use it", screenDim)
	if e.Kind == "effect" {
		line("f all four bolts from here", screenDim)
	}
	line("Arrows move  PgUp PgDn page", screenDim)
	line("Wheel scrolls, Shift+wheel sideways", screenDim)
	line("Esc back", screenDim)
}

// cursorFrame returns a see-through picture with a bright border, to lay
// over the tile under the cursor.
var cursorFrames = map[[2]int]image.Image{}

func cursorFrame(w, h int) image.Image {
	if img, ok := cursorFrames[[2]int{w, h}]; ok {
		return img
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	drawFrame(img, img.Bounds())
	cursorFrames[[2]int{w, h}] = img
	return img
}

// drawFrame draws a bright border just inside r.
func drawFrame(img *image.RGBA, r image.Rectangle) {
	edge := image.NewUniform(color.RGBA{255, 220, 60, 255})
	t := max(r.Dx()/12, 1)
	for _, side := range []image.Rectangle{
		{r.Min, image.Pt(r.Max.X, r.Min.Y+t)},
		{image.Pt(r.Min.X, r.Max.Y-t), r.Max},
		{r.Min, image.Pt(r.Min.X+t, r.Max.Y)},
		{image.Pt(r.Max.X-t, r.Min.Y), r.Max},
	} {
		draw.Draw(img, side, edge, image.Point{}, draw.Src)
	}
}

// openTileEditorAt opens the editor on the entry of VISUALS kind with
// id.
func (m *Model) openTileEditorAt(kind string, id int) {
	m.openTileEditorThen(func(ed *tileEditor) {
		for t, k := range tileKinds {
			if k.kind == kind {
				ed.setTab(t)
			}
		}
		for n, i := range ed.rows {
			if i >= 0 && ed.v.Entries[i].ID == id {
				ed.selectItem(n)
			}
		}
	})
}

// openTileEditor reads the tiles from the engine and opens the editor.
func (m *Model) openTileEditor() { m.openTileEditorThen(nil) }

// openTileEditorThen opens the editor, and runs then on it first.
func (m *Model) openTileEditorThen(then func(ed *tileEditor)) {
	if m.sheet == nil || m.terms.TileSet == nil {
		m.notify("The Tile Editor needs a tile set: pick one on the Display tab of the options.")
		return
	}
	m.command("VISUALS", nil, func(r proto.EventData) {
		v := m.lastVisuals
		switch {
		case !r.OK:
			m.notify("Cannot read the tiles: " + r.Error)
		case v.VisualSet == "":
			m.notify("The Tile Editor needs a tile set: pick one on the Display tab of the options.")
		default:
			ed := newTileEditor(m, v)
			if then != nil {
				then(ed)
			}
			m.push(ed)
		}
	})
}

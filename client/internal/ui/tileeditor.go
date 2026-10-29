package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// The Tile Editor: a browser of everything that can take a tile, a tab
// for each kind, and a picker that shows the tile sheet. Choosing a tile
// sends SET_VISUAL, so the map changes at once behind the editor. Save
// writes the player's file of changes with SAVE_VISUALS; leaving without
// saving sends RELOAD_VISUALS, which puts back what was saved.

// tileEditorID names the editor's list in Limoni's accessibility tree,
// for tests.
const tileEditorID = "tile-editor"

// tileKinds are the editor's tabs, in order, by the kind VISUALS names.
var tileKinds = []struct{ kind, tab string }{
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
}

func newTileEditor(m *Model, v proto.EventData) *tileEditor {
	ed := &tileEditor{m: m, v: v}
	ed.listScreen = newListScreen(tileEditorID, "Tile Editor: "+v.VisualSet, nil)
	for _, k := range tileKinds {
		ed.tabs = append(ed.tabs, k.tab)
	}
	ed.onTab = func(int) { ed.fill() }
	ed.searchable = true
	ed.detail = true
	ed.choose = func(item int) { ed.pick(ed.rows[item]) }
	ed.cancel = ed.leave
	ed.keys = []listKey{
		{Key: 'm', Label: "missing only", Do: func(int) {
			ed.missing = !ed.missing
			ed.fill()
			ed.state.Select(0)
		}},
		{Key: 'u', Label: "undo", Do: func(item int) {
			if item >= 0 {
				ed.undo(ed.rows[item])
			}
		}},
		{Key: 's', Label: "save", Do: func(int) { ed.save(nil) }},
	}
	ed.fill()
	return ed
}

// box fills most of the window: the lists are long.
func (ed *tileEditor) box(area Box) Box {
	w, h := min(area.W, 100), min(area.H, 40)
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
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

// fill lists the current tab's entries.
func (ed *tileEditor) fill() {
	ed.rows = ed.rows[:0]
	var items []listItem
	for i, e := range ed.v.Entries {
		if e.Kind != ed.kind() || (ed.missing && !lacks(e)) {
			continue
		}
		ed.rows = append(ed.rows, i)
		items = append(items, ed.item(e))
	}
	ed.items = items
	if ed.state.Selected >= len(items) {
		ed.state.Select(len(items) - 1)
	}
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
	case e.Own != nil:
		it.Hint = "changed"
	case e.Flavored:
		it.Hint = "flavor's tile"
	case lacks(e):
		it.Hint = "no tile"
	}
	return it
}

// refresh redraws the rows after a change, keeping the selection.
func (ed *tileEditor) refresh() {
	ed.users = nil
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
		}
		fmt.Fprintf(&b, "%s: %s\n", label, where)
	}
	if e.Flavored {
		b.WriteString("\nThe map shows this object with its flavor's tile, on the Flavors tab, until you learn what it is.\n")
	}
	if e.Own != nil {
		b.WriteString("\nYou changed this. u puts back the set's own tile.\n")
	}
	b.WriteString("\nEnter picks a tile.")
	return proto.PlainText(colorWhite, b.String())
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
		ed.dirty = false
		ed.m.notify("Saved your tiles to tiles/" + ed.v.VisualSet + ".prf.")
		if then != nil {
			then()
		}
	})
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
			m.command("RELOAD_VISUALS", nil, func(r proto.EventData) {
				if !r.OK {
					m.notify("Cannot put back the saved tiles: " + r.Error)
				}
			})
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

// pick opens the picker on entry e.
func (ed *tileEditor) pick(e int) {
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
	ed.m.push(p)
}

// tilePicker shows the tile sheet with a cursor, to give one entry a
// tile.
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
	sheetKey [4]int
	hotspots []hotspot
	// The last tile clicked, and when, for double clicks.
	clicked   [2]int
	clickedAt time.Time
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
	m := p.ed.m
	page := max(p.down-1, 1)
	switch ev.Type {
	case driver.KeyEsc:
		m.pop()
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
		}
	}
}

// use gives the entry the tile under the cursor and goes back to the
// list.
func (p *tilePicker) use() {
	p.ed.set(p.e, p.slot, [2]int{p.row, p.col})
	p.ed.m.pop()
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
	p.ed.m.pop()
}

func (p *tilePicker) box(area Box) Box {
	w, h := min(area.W, 120), min(area.H, 44)
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (p *tilePicker) click(x, y int) {
	for _, h := range p.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

func (p *tilePicker) hotspot(f *terminal.Frame, b Box, do func()) {
	p.hotspots = append(p.hotspots, hotspot{b, do})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

// clickTile moves the cursor to a tile; a second click uses it.
func (p *tilePicker) clickTile(row, col int) {
	again := p.clicked == [2]int{row, col} && time.Since(p.clickedAt) <= doubleClick
	p.clicked, p.clickedAt = [2]int{row, col}, time.Now()
	p.moveTo(row, col)
	if again {
		p.clickedAt = time.Time{}
		p.use()
	}
}

// pickerPanel is the width of the panel beside the sheet.
const pickerPanel = 34

// pickerBg is the color under the sheet's see-through parts.
var pickerBg = color.RGBA{0, 0, 0, 255}

func (p *tilePicker) draw(f *terminal.Frame, b Box, env *drawEnv) {
	m := p.ed.m
	p.hotspots = p.hotspots[:0]
	e := p.entry()
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" Pick a tile: "+e.Name+" ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenBorder), rect(b))
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}
	panel := Box{inner.X + inner.W - pickerPanel, inner.Y, pickerPanel, inner.H}
	sheetBox := Box{inner.X + 1, inner.Y, inner.W - pickerPanel - 2, inner.H}
	for y := 0; y < inner.H; y++ {
		f.Text(panel.X-1, inner.Y+y, "│", screenBorder)
	}
	p.drawPanel(f, panel, env)

	sheet := m.sheet
	if sheet == nil || env.sprite == nil {
		f.TextFit(sheetBox.X, sheetBox.Y, sheetBox.W, "The Tile Editor needs a tile set.", screenDim)
		return
	}
	tw, th := sheet.TileSize()
	p.cw, p.ch = max((2*tw+th/2)/th, 1), 1
	cols, rows := sheet.Size()
	p.across, p.down = min(max(sheetBox.W/p.cw, 1), cols), min(max(sheetBox.H/p.ch, 1), rows)
	// Keep the cursor in sight.
	p.top = min(max(p.top, p.row-p.down+1), p.row)
	p.left = min(max(p.left, p.col-p.across+1), p.col)
	p.top, p.left = min(max(p.top, 0), rows-p.down), min(max(p.left, 0), cols-p.across)

	kitty := m.cfg.Protocol == "Kitty"
	key := [4]int{p.top, p.left, p.down, p.across}
	if !kitty {
		// Other protocols cannot lay the cursor over the sheet, so it is
		// drawn in.
		key = [4]int{p.top, p.left, p.down*1000 + p.row, p.across*1000 + p.col}
	}
	if p.sheet == nil || p.sheetKey != key {
		img := sheet.Window(p.top, p.left, p.down, p.across, pickerBg)
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
	y = max(y+1, b.Y+b.H-6)
	line("Enter use this tile", screenDim)
	line("Click a tile, twice to use it", screenDim)
	if e.Kind == "effect" {
		line("f all four bolts from here", screenDim)
	}
	line("Arrows move  PgUp PgDn page", screenDim)
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

// openTileEditor reads the tiles from the engine and opens the editor.
func (m *Model) openTileEditor() {
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
			m.push(newTileEditor(m, v))
		}
	})
}

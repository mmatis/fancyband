package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"time"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

// The filter panel of the Tile Editor: settings for one entry's filter,
// beside a preview of the tile before and after.
// Every change shows on the map at once. A filter on one entry needs a
// tile of its own, so the first change gives the entry a virtual tile
// (see tiles.Edits) through SET_VISUAL. The editor's save writes the
// filters with the engine's file of changes.

// filterID names the panel's list in Limoni's accessibility tree, for
// tests.
const filterID = "tile-filter"

// filterScreen edits one filter.
type filterScreen struct {
	ed *tileEditor
	// e is the entry the filter is for.
	e int
	// f is the filter as edited; tile is the real tile it changes, and
	// terrain says that tile is terrain.
	f       tiles.Filter
	tile    [2]uint8
	terrain bool

	rows     []*optRow
	state    *widgets.ListState
	hotspots []hotspot
	// before and after are the preview's pictures, made again only
	// when their key changes, so Limoni sends each once.
	before         image.Image
	after          image.Image
	afterKey       string
	animatedOnHand int
}

// Color ranges the panel offers, as hues from and to.
var filterRanges = []struct {
	name string
	r    *tiles.Range
}{
	{"All colors", nil},
	{"Reds", &tiles.Range{From: -30, To: 30}},
	{"Reds to yellows", &tiles.Range{From: -60, To: 60}},
	{"Yellows", &tiles.Range{From: 30, To: 90}},
	{"Greens", &tiles.Range{From: 90, To: 150}},
	{"Cyans", &tiles.Range{From: 150, To: 210}},
	{"Blues", &tiles.Range{From: 210, To: 270}},
	{"Purples", &tiles.Range{From: 270, To: 330}},
}

// Colors the panel offers for outlines and glows.
var filterColors = []struct {
	name string
	c    tiles.RGB
}{
	{"Gold", tiles.Gold},
	{"White", tiles.White},
	{"Black", tiles.RGB{0, 0, 0}},
	{"Red", tiles.RGB{230, 40, 40}},
	{"Orange", tiles.RGB{255, 140, 30}},
	{"Green", tiles.RGB{60, 220, 80}},
	{"Blue", tiles.RGB{60, 120, 255}},
	{"Purple", tiles.RGB{180, 80, 240}},
	{"Cyan", tiles.RGB{60, 220, 230}},
}

var filterCurves = []struct{ name, curve string }{
	{"Smooth", tiles.CurveSmooth},
	{"Triangle", tiles.CurveTriangle},
	{"Blink", tiles.CurveBlink},
	{"Flicker", tiles.CurveFlicker},
}

var filterSpreads = []struct{ name, spread string }{
	{"All together", tiles.SpreadTogether},
	{"Ripple", tiles.SpreadRipple},
	{"Random", tiles.SpreadRandom},
}

// openFilter opens the filter panel on entry e.
func (ed *tileEditor) openFilter(e int) {
	m := ed.m
	en := ed.v.Entries[e]
	if en.Flavored {
		m.notify("The map shows this object with its flavor's tile until you learn what it is. Give the flavor a filter, on the Flavors tab.")
		return
	}
	t := ed.mainTile(en)
	for _, u := range en.Tiles {
		if t == nil {
			t = u
		}
	}
	if t == nil || m.sheet == nil {
		m.notify("This has no tile yet. Give it one first: a filter changes a tile.")
		return
	}
	at := [2]uint8{uint8(t[0]), uint8(t[1])}
	edits := m.sheet.Edits()
	fs := &filterScreen{ed: ed, e: e, tile: at, terrain: en.Kind == "terrain", state: widgets.NewListState()}
	switch v, virtual := edits.Virtual[at]; {
	case virtual:
		fs.f, fs.tile = v.Filter.Clone(), v.Base
	default:
		fs.f = tiles.NewFilter()
	}
	fs.build()
	fs.state.Select(0)
	m.push(fs)
}

// apply puts the filter as edited into use.
func (fs *filterScreen) apply() {
	m := fs.ed.m
	if m.sheet == nil {
		return
	}
	if !fs.ed.setFilter(fs.e, fs.f) {
		return
	}
	fs.ed.dirty = true
	fs.ed.refresh()
}

// remove takes the filter away and closes the panel.
func (fs *filterScreen) remove() {
	m := fs.ed.m
	if m.sheet != nil {
		fs.ed.removeFilter(fs.e)
		fs.ed.dirty = true
		fs.ed.refresh()
	}
	m.pop()
}

// setFilter gives entry e the filter f: each of its tiles becomes a
// virtual tile showing that tile through f, made the first time. It
// reports false if the sheet has no room for more virtual tiles.
func (ed *tileEditor) setFilter(e int, f tiles.Filter) bool {
	sheet := ed.m.sheet
	edits := sheet.Edits()
	_, rows := sheet.Size()
	en := &ed.v.Entries[e]
	made := map[[2]int][2]int{}
	for i, t := range en.Tiles {
		if t == nil {
			continue
		}
		at := [2]uint8{uint8(t[0]), uint8(t[1])}
		if v, ok := edits.Virtual[at]; ok {
			v.Filter = f.Clone()
			edits.Virtual[at] = v
			sheet.Forget(at)
			continue
		}
		vt, ok := made[*t]
		if !ok {
			free, room := edits.FreeTile(rows)
			if !room {
				ed.m.notify("This tile set has no room for more filtered tiles.")
				return false
			}
			edits.Virtual[free] = tiles.Virtual{Base: at, Filter: f.Clone()}
			vt = [2]int{int(free[0]), int(free[1])}
			made[*t] = vt
		}
		ed.set(e, i, vt)
	}
	return true
}

// removeFilter gives entry e back the real tiles behind its virtual ones.
func (ed *tileEditor) removeFilter(e int) {
	sheet := ed.m.sheet
	edits := sheet.Edits()
	for i, t := range ed.v.Entries[e].Tiles {
		if t == nil {
			continue
		}
		at := [2]uint8{uint8(t[0]), uint8(t[1])}
		if v, ok := edits.Virtual[at]; ok {
			delete(edits.Virtual, at)
			ed.set(e, i, [2]int{int(v.Base[0]), int(v.Base[1])})
		}
	}
}

// dropUnused forgets the virtual tiles no entry uses any more, such as
// those of an entry given another tile or put back as it was.
func (ed *tileEditor) dropUnused() {
	if ed.m.sheet == nil {
		return
	}
	used := map[[2]uint8]bool{}
	for _, en := range ed.v.Entries {
		for _, t := range en.Tiles {
			if t != nil {
				used[[2]uint8{uint8(t[0]), uint8(t[1])}] = true
			}
		}
	}
	edits := ed.m.sheet.Edits()
	for t := range edits.Virtual {
		if !used[t] {
			delete(edits.Virtual, t)
		}
	}
}

// build makes the rows for the filter as it is.
func (fs *filterScreen) build() {
	f := &fs.f
	var rows []*optRow
	add := func(r *optRow) {
		r.editable = true
		rows = append(rows, r)
	}
	info := func(label string) { rows = append(rows, &optRow{kind: optInfo, label: label}) }
	signed := func(n int) string { return fmt.Sprintf("%+d%%", n) }
	share := func(n int) string { return fmt.Sprintf("%d%%", n) }
	num := func(label, desc string, v *float64, lo, hi, step int, format func(int) string) *optRow {
		return &optRow{kind: optNumber, label: label, desc: desc, num: int(math.Round(*v * 100)), min: lo, max: hi, step: step, format: format,
			set: func(r *optRow) { *v = float64(r.num) / 100; fs.apply() }}
	}
	look := func(l *tiles.Look, prefix string) {
		hue := &optRow{kind: optNumber, label: prefix + "Hue", desc: "Turns colors around the color wheel: 120° makes red green, 240° makes it blue.",
			num: int(math.Round(l.Hue)), min: -180, max: 180, step: 15, format: func(n int) string { return fmt.Sprintf("%+d°", n) },
			set: func(r *optRow) { l.Hue = float64(r.num); fs.apply() }}
		add(hue)
		add(num(prefix+"Saturation", "Makes colors richer, or grayer down to -100%.", &l.Saturation, -100, 100, 10, signed))
		add(num(prefix+"Lightness", "Makes the tile brighter or darker.", &l.Lightness, -100, 100, 10, signed))
		fade := &optRow{kind: optNumber, label: prefix + "Opacity", desc: "How much the tile covers what is under it: terrain for a monster or object, black for terrain.",
			num: int(math.Round((1 - l.Fade) * 100)), min: 0, max: 100, step: 10, format: share,
			set: func(r *optRow) { l.Fade = 1 - float64(r.num)/100; fs.apply() }}
		add(fade)
		add(num(prefix+"Outline", "Draws a colored edge on the tile's own colored pixels, where they meet its see-through ones.", &l.Outline, 0, 100, 25, share))
		add(num(prefix+"Inner glow", "Like the outline, but soft: lights the colored pixels near the edge, fading inward.", &l.Glow, 0, 100, 10, share))
		blurDesc := "Softens the monster or object before it is drawn on its terrain."
		if fs.terrain {
			blurDesc = "Softens the tile. On terrain it shows seams between grids, so turn on \"Blur and waver terrain\" too."
		}
		add(num(prefix+"Blur", blurDesc, &l.Blur, 0, 100, 10, share))
		add(num(prefix+"Waver", "Pushes pixels sideways along a moving wave, like heat haze or water.", &l.Waver, 0, 100, 10, share))
		add(num(prefix+"Sparkle", "Lights a few random pixels at each step.", &l.Sparkle, 0, 100, 10, share))
	}

	look(&f.Look, "")
	rangeRow := &optRow{kind: optChoice, label: "Color range", desc: "Limits hue, saturation, and lightness to some colors, such as reds to yellows.",
		choices: names(filterRanges, func(r struct {
			name string
			r    *tiles.Range
		}) string {
			return r.name
		})}
	for i, r := range filterRanges {
		if r.r != nil && f.Range != nil && *r.r == *f.Range || r.r == nil && f.Range == nil {
			rangeRow.choice = i
		}
	}
	rangeRow.set = func(r *optRow) {
		f.Range = nil
		if c := filterRanges[r.choice].r; c != nil {
			rr := *c
			f.Range = &rr
		}
		fs.apply()
	}
	add(rangeRow)
	add(fs.colorRow("Outline color", &f.OutlineColor))
	add(fs.colorRow("Glow color", &f.GlowColor))
	if fs.terrain {
		add(&optRow{kind: optCheck, label: "Blur and waver terrain", on: f.Terrain,
			desc: "Lets blur and waver change this terrain. They show seams between grids, so they are off for terrain unless you turn them on.",
			set:  func(r *optRow) { f.Terrain = r.on; fs.apply() }})
	}

	add(&optRow{kind: optCheck, label: "Animation", on: f.Anim != nil,
		desc: "Moves between the settings above and a second set over time.",
		set: func(r *optRow) {
			if r.on {
				f.Anim = &tiles.Anim{To: f.Look, Curve: tiles.CurveSmooth, Period: 1.5, Spread: tiles.SpreadTogether}
			} else {
				f.Anim = nil
			}
			fs.apply()
			sel := fs.state.Selected
			fs.build()
			fs.state.Select(sel)
		}})
	if a := f.Anim; a != nil {
		curveRow := &optRow{kind: optChoice, label: "  Curve", desc: "How it moves: smooth eases there and back, triangle goes straight, blink jumps, and flicker jumps at random."}
		for i, c := range filterCurves {
			curveRow.choices = append(curveRow.choices, c.name)
			if c.curve == a.Curve {
				curveRow.choice = i
			}
		}
		curveRow.set = func(r *optRow) { a.Curve = filterCurves[r.choice].curve; fs.apply() }
		add(curveRow)
		add(&optRow{kind: optNumber, label: "  Period", desc: "How long it takes to go there and back. Longer is slower.",
			num: int(math.Round(a.Period * 10)), min: 4, max: 100, step: 1,
			format: func(n int) string { return fmt.Sprintf("%.1f s", float64(n)/10) },
			set:    func(r *optRow) { a.Period = float64(r.num) / 10; fs.apply() }})
		spreadRow := &optRow{kind: optChoice, label: "  Spread", desc: "How grids that show this differ: all in step, a ripple along the diagonals, or each at random."}
		for i, s := range filterSpreads {
			spreadRow.choices = append(spreadRow.choices, s.name)
			if s.spread == a.Spread {
				spreadRow.choice = i
			}
		}
		spreadRow.set = func(r *optRow) { a.Spread = filterSpreads[r.choice].spread; fs.apply() }
		add(spreadRow)
		info("  It moves toward:")
		look(&a.To, "  ")
	}

	add(&optRow{kind: optButton, label: "Remove the filter", desc: "Takes the filter away and gives back the tile as the set draws it.", press: fs.remove})
	add(&optRow{kind: optButton, label: "Done", desc: "Back to the list. Save there keeps your filters for next time.", press: fs.ed.m.pop})
	fs.rows = rows
}

func names[T any](list []T, name func(T) string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = name(v)
	}
	return out
}

// colorRow picks a color for an outline or a glow.
func (fs *filterScreen) colorRow(label string, c *tiles.RGB) *optRow {
	r := &optRow{kind: optChoice, label: label, desc: "The color of the " + map[string]string{"Outline color": "outline", "Glow color": "inner glow"}[label] + "."}
	r.choice = -1
	for i, fc := range filterColors {
		r.choices = append(r.choices, fc.name)
		if fc.c == *c {
			r.choice = i
		}
	}
	if r.choice < 0 {
		r.choices = append(r.choices, fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]))
		r.choice = len(r.choices) - 1
	}
	own := *c
	r.set = func(r *optRow) {
		if r.choice < len(filterColors) {
			*c = filterColors[r.choice].c
		} else {
			*c = own
		}
		fs.apply()
	}
	return r
}

func (fs *filterScreen) selectedRow() *optRow {
	if fs.state.Selected < 0 || fs.state.Selected >= len(fs.rows) {
		return nil
	}
	return fs.rows[fs.state.Selected]
}

// animated reports whether the preview moves, so ticks redraw it.
func (fs *filterScreen) animated() bool { return fs.f.Animated() }

func (fs *filterScreen) key(ev driver.KeyEvent) {
	n := len(fs.rows)
	move := func(d int) {
		for i := 1; i <= n; i++ {
			j := ((fs.state.Selected+d*i)%n + n) % n
			if fs.rows[j].kind != optInfo {
				fs.state.Select(j)
				return
			}
		}
	}
	switch ev.Type {
	case driver.KeyEsc:
		fs.ed.m.pop()
	case driver.KeyArrowDown:
		move(1)
	case driver.KeyArrowUp:
		move(-1)
	case driver.KeyArrowLeft, driver.KeyArrowRight:
		d := 1
		if ev.Type == driver.KeyArrowLeft {
			d = -1
		}
		if r := fs.selectedRow(); r != nil && (r.kind == optNumber || r.kind == optChoice) {
			r.change(d)
		}
	case driver.KeyEnter, driver.KeySpace:
		if r := fs.selectedRow(); r != nil {
			r.change(1)
		}
	case driver.KeyRune:
		switch ev.Ch {
		case ' ':
			if r := fs.selectedRow(); r != nil {
				r.change(1)
			}
		case 'j':
			move(1)
		case 'k':
			move(-1)
		case 'h':
			fs.key(driver.KeyEvent{Type: driver.KeyArrowLeft})
		case 'l':
			fs.key(driver.KeyEvent{Type: driver.KeyArrowRight})
		}
	}
}

func (fs *filterScreen) box(area Box) Box {
	w, h := min(area.W, 96), min(area.H, 34)
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (fs *filterScreen) click(x, y int) {
	for _, h := range fs.hotspots {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

func (fs *filterScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	fs.hotspots = append(fs.hotspots, hotspot{b, do})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

// previewPanel is the width of the preview beside the settings.
const previewPanel = 34

func (fs *filterScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	fs.hotspots = fs.hotspots[:0]
	fillBox(f, b, screenBg)
	title := " Filter: " + fs.ed.v.Entries[fs.e].Name + " "
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(title).
		WithTitleStyle(screenTitle).WithBorderStyle(screenBorder), rect(b))
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}
	panel := Box{inner.X + inner.W - previewPanel, inner.Y, previewPanel, inner.H}
	left := Box{inner.X, inner.Y, inner.W - previewPanel - 1, inner.H}
	for y := 0; y < inner.H; y++ {
		f.Text(panel.X-1, inner.Y+y, "│", screenBorder)
	}

	foot := "↑↓ move  ←→ adjust  Space change  Esc back"
	f.TextFit(left.X+1, left.Y+left.H-1, left.W-2, foot, screenDim)
	if r := fs.selectedRow(); r != nil {
		f.RenderWidget(&styledView{text: proto.PlainText(colorWhite, r.desc), pal: env.pal},
			rect(Box{left.X + 1, left.Y + left.H - 4, left.W - 2, 3}))
	}
	listBox := Box{left.X, left.Y, left.W, left.H - 5}
	labels := make([]string, len(fs.rows))
	for i, r := range fs.rows {
		labels[i] = " " + r.text()
	}
	rowStyle := screenText.WithBg(screenBg.Bg)
	f.RenderWidget(&widgets.List{
		ID: filterID, Label: "Filter settings", Items: labels,
		Style: rowStyle, SelectedStyle: screenSelected, FocusedStyle: rowStyle,
		Scrollbar: true, State: fs.state,
	}, rect(listBox))
	for y := 0; y < listBox.H; y++ {
		n := fs.state.Offset + y
		if n >= len(fs.rows) {
			break
		}
		r := fs.rows[n]
		if r.kind == optInfo {
			f.TextFit(listBox.X, listBox.Y+y, listBox.W-1, labels[n], screenDim)
			continue
		}
		fs.hotspot(f, Box{listBox.X, listBox.Y + y, listBox.W, 1}, func() {
			if fs.state.Selected == n || r.kind == optCheck || r.kind == optButton {
				fs.state.Select(n)
				r.change(1)
				return
			}
			fs.state.Select(n)
		})
	}
	fs.drawPreview(f, panel)
}

// drawPreview draws the tile before and after the filter, and notes
// about the cost.
func (fs *filterScreen) drawPreview(f *terminal.Frame, b Box) {
	m := fs.ed.m
	y := b.Y
	line := func(text string, st cell.Style) {
		if y < b.Y+b.H {
			f.TextFit(b.X+1, y, b.W-2, text, st)
		}
		y++
	}
	sheet := m.sheet
	if sheet == nil {
		line("The filter needs a tile set.", screenDim)
		return
	}
	tw, th := sheet.TileSize()
	cw := max((2*tw+th/2)/th, 1)
	size := 6
	w, h := cw*size, size
	if 2*w+2 > b.W-2 {
		size = max((b.W-4)/(2*cw), 1)
		w, h = cw*size, size
	}
	line("Before", screenText)
	f.Text(b.X+1+w+2, y-1, "After", screenText)
	var t time.Duration
	if m.cfg.Animate {
		t = time.Duration(m.tick) * tickTime
	}
	step, variant := fs.f.StepAt(t, 0, 0)
	if fs.before == nil {
		fs.before = sheet.Filtered(fs.tile[0], fs.tile[1], tiles.NewFilter(), 0, 0, fs.terrain, pickerBg)
	}
	key, _ := json.Marshal(fs.f)
	akey := fmt.Sprintf("%s/%d/%d", key, step, variant)
	if fs.after == nil || fs.afterKey != akey {
		fs.after = sheet.Filtered(fs.tile[0], fs.tile[1], fs.f, step, variant, fs.terrain, pickerBg)
		fs.afterKey = akey
	}
	for i, img := range []image.Image{fs.before, fs.after} {
		at := Box{b.X + 1 + i*(w+2), y, w, h}
		for yy := at.Y; yy < at.Y+at.H; yy++ {
			for xx := at.X; xx < at.X+at.W; xx++ {
				f.Buffer.SetCell(uint16(xx), uint16(yy), cell.Cell{Content: cell.RuneImage})
			}
		}
		f.RegisterImageRegion(terminal.ImageRegion{Area: rect(at), Img: img})
	}
	y += h + 1
	if len(fs.ed.usersOf([2]int{int(fs.tile[0]), int(fs.tile[1])})) > 0 {
		line("Only this one changes; others", screenDim)
		line("with the same tile keep theirs.", screenDim)
	}
	if fs.f.Animated() && !m.cfg.Animate {
		y++
		line("Animations are off in the", screenDim)
		line("options, so the map shows the", screenDim)
		line("first step only.", screenDim)
	}
	if fs.f.Animated() && m.cfg.Protocol == "Sixel" {
		if n := m.animatedOnScreen(); n >= sixelAnimWarn {
			y++
			line(fmt.Sprintf("With Sixel, the %d animated", n), screenWarn)
			line("tiles on screen are each sent", screenWarn)
			line(fmt.Sprintf("again about %d times a second.", int(time.Second/tickTime)), screenWarn)
			line("Over a slow link, keep few.", screenWarn)
		}
	}
}

// sixelAnimWarn is how many animated tiles on screen make the panel warn
// that Sixel sends them all again at every step.
const sixelAnimWarn = 40

// animatedOnScreen counts the map's tiles that animate.
func (m *Model) animatedOnScreen() int {
	g := m.terms.Terms[0]
	if g == nil || m.sheet == nil {
		return 0
	}
	n := 0
	g.Tiles(func(_, _ int, c proto.PictCell) {
		if m.sheet.Animated(c) {
			n++
		}
	})
	return n
}

package ui

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

// The Tile Editor's filter tab, in its column 3: settings for one
// entry's filter. Column 2 shows the tile through it, and every change
// shows on the map at once. A filter on one entry needs a
// tile of its own, so the first change gives the entry a virtual tile
// (see tiles.Edits) through SET_VISUAL. The editor's save writes the
// filters with the engine's file of changes.

// filterID names the panel's list in Limoni's accessibility tree, for
// tests.
const filterID = "tile-filter"

// filterScreen edits one filter, in column 3 of the Tile Editor.
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
	// listHeight is how many rows the list showed when last drawn.
	listHeight int
}

// hueRange is a range of the colors with hues from and to, any
// lightness, and not gray.
func hueRange(from, to float64) *tiles.Range {
	return &tiles.Range{From: from, To: to, LightTo: 1, SatFrom: tiles.GrayLimit, SatTo: 1, Limits: true}
}

// Color ranges the panel offers to start from. A range that matches none
// is Custom, the last choice.
var filterRanges = []struct {
	name string
	r    *tiles.Range
}{
	{"All colors", nil},
	{"Reds", hueRange(330, 30)},
	{"Reds to yellows", hueRange(300, 60)},
	{"Yellows", hueRange(30, 90)},
	{"Greens", hueRange(90, 150)},
	{"Cyans", hueRange(150, 210)},
	{"Blues", hueRange(210, 270)},
	{"Purples", hueRange(270, 330)},
	{"Greys", &tiles.Range{LightTo: 1, SatTo: tiles.GrayLimit, Limits: true}},
	{"Custom", nil},
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

// openFilter opens the filter tab in column 3 on entry e and gives it
// the keys, or says why e cannot take a filter.
func (ed *tileEditor) openFilter(e int) {
	fs, why := ed.newFilter(e)
	if fs == nil {
		ed.m.notify(why)
		return
	}
	ed.filter, ed.sideWhy, ed.filterFor = fs, "", e
	ed.side, ed.focus = sideFilter, focusSide
}

// newFilter makes the filter tab for entry e, or says why e cannot take
// a filter.
func (ed *tileEditor) newFilter(e int) (*filterScreen, string) {
	m := ed.m
	en := ed.v.Entries[e]
	if en.Flavored {
		return nil, "The map shows this object with its flavor's tile until you learn what it is. Give the flavor a filter, on the Flavors tab."
	}
	t := ed.mainTile(en)
	for _, u := range en.Tiles {
		if t == nil {
			t = u
		}
	}
	if t == nil || m.sheet == nil {
		return nil, "This has no tile yet. Give it one first: a filter changes a tile."
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
	return fs, ""
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
	fs.ed.touch()
	fs.ed.refresh()
}

// remove takes the filter away and gives the keys back to the list.
func (fs *filterScreen) remove() {
	if fs.ed.m.sheet != nil {
		fs.ed.removeFilter(fs.e)
		fs.ed.touch()
		fs.ed.refresh()
	}
	fs.f = tiles.NewFilter()
	fs.build()
	fs.ed.back()
}

// setFilter gives entry e the filter f: each of its tiles becomes a
// virtual tile showing that tile through f, made the first time. A
// virtual tile another layer made stays as it is: this layer gets one of
// its own, showing the same real tile. It reports false if the sheet has
// no room for more virtual tiles.
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
		base := at
		if v, ok := edits.Virtual[at]; ok && v.Layer == ed.layer {
			v.Filter = f.Clone()
			edits.Virtual[at] = v
			sheet.Forget(at)
			continue
		} else if ok {
			base = v.Base
		}
		vt, ok := made[*t]
		if !ok {
			free, room := edits.FreeTileIn(rows, ed.layer)
			if !room {
				ed.m.notify("This tile set has no room for more filtered tiles.")
				return false
			}
			edits.Virtual[free] = tiles.Virtual{Base: base, Filter: f.Clone(), Layer: ed.layer}
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
			// Another layer's virtual tile stays, for that layer.
			if v.Layer == ed.layer {
				delete(edits.Virtual, at)
			}
			ed.set(e, i, [2]int{int(v.Base[0]), int(v.Base[1])})
		}
	}
}

// dropUnused forgets the layer's virtual tiles no entry uses any more,
// such as those of an entry given another tile or put back as it was.
// Only this layer and the ones above it can use them, and the ones above
// make their own.
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
	for t, v := range edits.Virtual {
		if v.Layer == ed.layer && !used[t] {
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
		hue := &optRow{kind: optNumber, label: prefix + "Hue", desc: "Turns colors around the color wheel: 120° makes red green, 240° makes it blue. With the Greys range it picks a tint instead: 0° red, 120° green, 240° blue.",
			num: int(math.Round(l.Hue)), min: -180, max: 180, step: 15, format: func(n int) string { return fmt.Sprintf("%+d°", n) },
			set: func(r *optRow) { l.Hue = float64(r.num); fs.apply() }}
		add(hue)
		add(num(prefix+"Saturation", "Makes colors richer, or grayer down to -100%. With the Greys range it sets how strong the tint is.", &l.Saturation, -100, 100, 10, signed))
		add(num(prefix+"Lightness", "Makes the tile brighter or darker.", &l.Lightness, -100, 100, 10, signed))
		fade := &optRow{kind: optNumber, label: prefix + "Opacity", desc: "How much the tile covers what is under it: terrain for a monster or object, black for terrain.",
			num: int(math.Round((1 - l.Fade) * 100)), min: 0, max: 100, step: 10, format: share,
			set: func(r *optRow) { l.Fade = 1 - float64(r.num)/100; fs.apply() }}
		add(fade)
		// The colors belong to the whole filter, so they come under the
		// first set of settings only.
		colors := prefix == ""
		add(num(prefix+"Outline", "Draws a colored edge on the tile's own colored pixels, where they meet its see-through ones.", &l.Outline, 0, 100, 25, share))
		if colors {
			add(fs.colorRow("  Outline color", "The color of the outline.", &f.OutlineColor))
		}
		add(num(prefix+"Inner glow", "Like the outline, but soft: lights the colored pixels near the edge, fading inward.", &l.Glow, 0, 100, 10, share))
		if colors {
			add(fs.colorRow("  Glow color", "The color of the inner glow.", &f.GlowColor))
		}
		blurDesc := "Softens the monster or object before it is drawn on its terrain."
		if fs.terrain {
			blurDesc = "Softens the tile. On terrain it shows seams between grids, so turn on \"Blur and waver terrain\" too."
		}
		add(num(prefix+"Blur", blurDesc, &l.Blur, 0, 100, 10, share))
		add(num(prefix+"Waver", "Pushes pixels sideways along a moving wave, like heat haze or water.", &l.Waver, 0, 100, 10, share))
		add(num(prefix+"Sparkle", "Lights a few random pixels at each step.", &l.Sparkle, 0, 100, 10, share))
		if colors {
			if f.SparkleColor == nil {
				w := tiles.White
				f.SparkleColor = &w
			}
			add(fs.colorRow("  Sparkle color", "The color the sparkles light pixels toward.", f.SparkleColor))
		}
	}

	look(&f.Look, "")
	fs.rangeRows(add)
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
	add(&optRow{kind: optButton, label: "Done", desc: "Back to the list. Save there keeps your filters for next time.", press: fs.ed.back})
	fs.rows = rows
}

// rangeRows adds the color range's rows: a choice of ranges to start
// from, and with a range, its three pairs of limits, which the spectrum
// under the list shows.
func (fs *filterScreen) rangeRows(add func(*optRow)) {
	f := &fs.f
	if f.Range != nil {
		// In place: the spectrum's drag holds on to it.
		*f.Range = f.Range.Normal()
	}
	custom := len(filterRanges) - 1
	rangeRow := &optRow{kind: optChoice, label: "Color range", choice: custom,
		desc: "Limits hue, saturation, and lightness to some pixels: start from a range here, then change its limits below or on the spectrum.",
		choices: names(filterRanges, func(r struct {
			name string
			r    *tiles.Range
		}) string {
			return r.name
		})}
	for i, r := range filterRanges {
		if r.r != nil && f.Range != nil && *r.r == *f.Range || i == 0 && f.Range == nil {
			rangeRow.choice = i
		}
	}
	rangeRow.set = func(r *optRow) {
		switch {
		case r.choice == custom:
			// Custom keeps the limits as they are, or starts from every
			// color.
			if f.Range == nil {
				f.Range = &tiles.Range{LightTo: 1, SatTo: 1, Limits: true}
			}
		case filterRanges[r.choice].r == nil:
			f.Range = nil
		default:
			rr := *filterRanges[r.choice].r
			f.Range = &rr
		}
		fs.apply()
		fs.rebuild()
	}
	add(rangeRow)
	if f.Range == nil {
		return
	}
	rg := f.Range
	degrees := func(n int) string { return fmt.Sprintf("%d°", n) }
	share := func(n int) string { return fmt.Sprintf("%d%%", n) }
	limit := func(label, desc string, v *float64, scale float64, hi, step int, format func(int) string, after func()) {
		add(&optRow{kind: optNumber, label: "  " + label, desc: desc, num: int(math.Round(*v * scale)), min: 0, max: hi, step: step, format: format,
			set: func(r *optRow) {
				*v = float64(r.num) / scale
				if after != nil {
					after()
				}
				fs.apply()
				fs.rebuild()
			}})
	}
	limit("Hues from", "The first hue in the range. The range goes up the color wheel from here; it wraps past red when Hues to is lower, and has every hue when the two are equal.",
		&rg.From, 1, 355, 5, degrees, nil)
	limit("Hues to", "The last hue in the range: 0° red, 120° green, 240° blue.", &rg.To, 1, 355, 5, degrees, nil)
	limit("Lightness from", "The darkest pixels in the range: 0% is black.", &rg.LightFrom, 100, 100, 5, share,
		func() { rg.LightTo = max(rg.LightTo, rg.LightFrom) })
	limit("Lightness to", "The brightest pixels in the range: 100% is the brightest a color can be.", &rg.LightTo, 100, 100, 5, share,
		func() { rg.LightFrom = min(rg.LightFrom, rg.LightTo) })
	limit("Colorfulness from", "The grayest pixels in the range: 0% takes in the grays from black to white. Grays have no hue, so they are in the range only with every hue.",
		&rg.SatFrom, 100, 100, 5, share, func() { rg.SatTo = max(rg.SatTo, rg.SatFrom) })
	limit("Colorfulness to", "The most colorful pixels in the range. Below 20% a pixel counts as gray.", &rg.SatTo, 100, 100, 5, share,
		func() { rg.SatFrom = min(rg.SatFrom, rg.SatTo) })
}

// rebuild makes the rows again for the filter as it is now, keeping the
// selected row.
func (fs *filterScreen) rebuild() {
	sel := fs.state.Selected
	fs.build()
	fs.state.Select(min(sel, len(fs.rows)-1))
}

func names[T any](list []T, name func(T) string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = name(v)
	}
	return out
}

// colorRow picks a color for an outline or a glow.
func (fs *filterScreen) colorRow(label, desc string, c *tiles.RGB) *optRow {
	r := &optRow{kind: optChoice, label: label, desc: desc}
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
	// jump selects row i, or the nearest row past it in direction d
	// that is not text only.
	jump := func(i, d int) {
		i = min(max(i, 0), n-1)
		for ; i >= 0 && i < n; i += d {
			if fs.rows[i].kind != optInfo {
				fs.state.Select(i)
				return
			}
		}
	}
	page := pageRows(fs.listHeight)
	switch ev.Type {
	case driver.KeyEsc:
		fs.ed.back()
	case driver.KeyArrowDown:
		move(1)
	case driver.KeyArrowUp:
		move(-1)
	case driver.KeyPageDown:
		jump(fs.state.Selected+page, 1)
	case driver.KeyPageUp:
		jump(fs.state.Selected-page, -1)
	case driver.KeyHome:
		jump(0, 1)
	case driver.KeyEnd:
		jump(n-1, -1)
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

// hotspot makes part of the settings clickable; a click gives column 3
// the keys before it acts.
func (fs *filterScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	act := func() {
		fs.ed.focus = focusSide
		do()
	}
	fs.hotspots = append(fs.hotspots, hotspot{box: b, do: act})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { act() })
}

// draw draws the settings in b, with the selected one's description
// under them.
func (fs *filterScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	fs.hotspots = fs.hotspots[:0]
	left := b

	foot := "↑↓ move  ←→ adjust  Space change  Esc back"
	f.TextFit(left.X+1, left.Y+left.H-1, left.W-2, foot, screenDim)
	if r := fs.selectedRow(); r != nil {
		f.RenderWidget(&styledView{text: proto.PlainText(colorWhite, r.desc), pal: env.pal},
			rect(Box{left.X + 1, left.Y + left.H - 4, left.W - 2, 3}))
	}
	listBox := Box{left.X, left.Y, left.W, left.H - 5}
	// The color range's spectrum, under the list.
	if fs.f.Range != nil && listBox.H >= 2*spectrumRows+4 && listBox.W >= 20 {
		listBox.H -= spectrumRows + 1
		spec := Box{left.X + 1, listBox.Y + listBox.H + 1, left.W - 2, spectrumRows}
		f.RenderWidget(spectrumView{rg: func() *tiles.Range { return fs.f.Range }, changed: func() {
			fs.ed.focus = focusSide
			fs.apply()
			fs.rebuild()
		}}, rect(spec))
	}
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
	fs.listHeight = listBox.H
	f.RenderWidget(wheelArea{scroll: func(d int) {
		wheelList(fs.state, len(fs.rows), fs.listHeight, d, func(i int) bool { return fs.rows[i].kind != optInfo })
	}, pointer: env.pointer}, rect(listBox))
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
		arrowHotspots(labels[n], listBox.X, listBox.Y+y, r, func() { fs.state.Select(n) },
			func(b Box, do func()) { fs.hotspot(f, b, do) })
	}
}

// notes writes, with say, what column 2 says about the filter under the
// tile, a paragraph at a time: who else it changes, and what it costs.
func (fs *filterScreen) notes(say func(string, cell.Style)) {
	m := fs.ed.m
	if slices.ContainsFunc(fs.ed.usersOf([2]int{int(fs.tile[0]), int(fs.tile[1])}), func(u int) bool { return u != fs.e }) {
		say("Only this one changes; others with the same tile keep theirs.", screenDim)
	}
	if fs.f.Animated() && !m.cfg.Animate {
		say("Animations are off in the options, so the map shows the first step only.", screenDim)
	}
	if fs.f.Animated() && m.cfg.Protocol == "Sixel" {
		if n := m.animatedOnScreen(); n >= sixelAnimWarn {
			say(fmt.Sprintf("With Sixel, the %d animated tiles on screen are each sent again about %d times a second. Over a slow link, keep few.",
				n, int(time.Second/tickTime)), screenWarn)
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

// spectrumRows is how many rows the color range's spectrum takes: the
// spectrum, a row of markers, and a line that sums up the range.
const spectrumRows = 7

// spectrumLevels is how many levels of lightness the spectrum shows, two
// to a row.
const spectrumLevels = 2 * (spectrumRows - 2)

// spectrumView shows a color range as a spectrum: hues from left to
// right, lightness from bright at the top to dark at the bottom, and a
// strip of grays at the right, with what is outside the range dimmed.
// A click moves the hue limit nearest it there; a drag sets both hue
// limits and both lightness limits to what it covers.
type spectrumView struct {
	rg      func() *tiles.Range
	changed func()
}

func (s spectrumView) Draw(ctx cell.Context, buf *buffer.Buffer) {
	rg := s.rg()
	if rg == nil {
		return
	}
	n := rg.Normal()
	a := ctx.Area
	sw := int(a.Width) - 4
	if sw < 8 {
		return
	}
	bg := screenBg.Bg
	br, bgG, bb := bg.RGB()
	shade := func(c color.RGBA, in bool) cell.Color {
		if !in {
			// Dimmed, most of the way to the background.
			mix := func(x, y uint8) uint8 { return uint8(float64(x)*0.25 + float64(y)*0.75) }
			return cell.NewColorRGB(mix(c.R, br), mix(c.G, bgG), mix(c.B, bb))
		}
		return cell.NewColorRGB(c.R, c.G, c.B)
	}
	level := func(k int) float64 { return 1 - (float64(k)+0.5)/spectrumLevels }
	hueAt := func(col int) float64 { return (float64(col) + 0.5) * 360 / float64(sw) }
	lit := func(v float64) bool { return v >= n.LightFrom-1e-9 && v <= n.LightTo+1e-9 }
	colorful := n.SatTo >= tiles.GrayLimit
	for row := 0; row < spectrumRows-2; row++ {
		for col := 0; col < sw; col++ {
			h := hueAt(col)
			up, down := level(2*row), level(2*row+1)
			top := shade(tiles.FromHSV(h, 1, up), colorful && rg.HasHue(h) && lit(up))
			bot := shade(tiles.FromHSV(h, 1, down), colorful && rg.HasHue(h) && lit(down))
			buf.SetCell(a.X+uint16(col), a.Y+uint16(row), cell.Cell{Content: '▀', Style: cell.NewStyle().WithFg(top).WithBg(bot)})
		}
		// The grays.
		for col := sw + 1; col < sw+3; col++ {
			up, down := level(2*row), level(2*row+1)
			top := shade(tiles.FromHSV(0, 0, up), rg.Has(0, 0, up))
			bot := shade(tiles.FromHSV(0, 0, down), rg.Has(0, 0, down))
			buf.SetCell(a.X+uint16(col), a.Y+uint16(row), cell.Cell{Content: '▀', Style: cell.NewStyle().WithFg(top).WithBg(bot)})
		}
	}
	// Markers under the hue limits, and the range summed up.
	marks := a.Y + uint16(spectrumRows-2)
	if n.From != n.To {
		for _, h := range []float64{n.From, n.To} {
			col := min(int(h*float64(sw)/360), sw-1)
			buf.SetCell(a.X+uint16(col), marks, cell.Cell{Content: '▲', Style: screenText})
		}
	}
	hues := fmt.Sprintf("hues %.0f° to %.0f°", n.From, n.To)
	if n.From == n.To {
		hues = "every hue"
	}
	sum := fmt.Sprintf("%s, lightness %.0f–%.0f%%, colorfulness %.0f–%.0f%%", hues,
		n.LightFrom*100, n.LightTo*100, n.SatFrom*100, n.SatTo*100)
	for i, r := range []rune(sum) {
		if i < int(a.Width) {
			buf.SetCell(a.X+uint16(i), marks+1, cell.Cell{Content: r, Style: screenDim})
		}
	}

	if ctx.RegisterMouse == nil || s.changed == nil {
		return
	}
	area := cell.Rect{X: a.X, Y: a.Y, Width: uint16(sw), Height: uint16(spectrumRows - 2)}
	snap := func(v, step float64) float64 { return math.Round(v/step) * step }
	colOf := func(x uint16) int { return min(max(int(x)-int(a.X), 0), sw-1) }
	rowOf := func(y uint16) int { return min(max(int(y)-int(a.Y), 0), spectrumRows-3) }
	capture := ctx.CaptureMouse
	ctx.RegisterMouse(area, func(ev driver.MouseEvent) {
		if ev.Button != driver.MouseLeft || ev.Drag {
			return
		}
		rg := s.rg()
		if rg == nil {
			return
		}
		// A click: the nearest hue limit moves here.
		h := math.Mod(snap(hueAt(colOf(ev.X)), 5), 360)
		dist := func(x float64) float64 {
			d := math.Abs(math.Mod(x-h+540, 360) - 180)
			return d
		}
		if dist(rg.From) <= dist(rg.To) && rg.From != rg.To {
			rg.From = h
		} else {
			rg.To = h
		}
		s.changed()
		x0, y0 := ev.X, ev.Y
		if capture == nil {
			return
		}
		capture(func(ev driver.MouseEvent) {
			if ev.Button == driver.MouseRelease || (ev.X == x0 && ev.Y == y0) {
				return
			}
			rg := s.rg()
			if rg == nil {
				return
			}
			c0, c1 := min(colOf(x0), colOf(ev.X)), max(colOf(x0), colOf(ev.X))
			r0, r1 := min(rowOf(y0), rowOf(ev.Y)), max(rowOf(y0), rowOf(ev.Y))
			rg.From = math.Mod(snap(float64(c0)*360/float64(sw), 5), 360)
			rg.To = math.Mod(snap(float64(c1+1)*360/float64(sw), 5), 360)
			rg.LightFrom = max(snap(1-float64(2*r1+2)/spectrumLevels, 0.05), 0)
			rg.LightTo = min(snap(1-float64(2*r0)/spectrumLevels, 0.05), 1)
			s.changed()
		})
	})
}

func (s spectrumView) SizeHint(max cell.Rect) (uint16, uint16) { return max.Width, max.Height }

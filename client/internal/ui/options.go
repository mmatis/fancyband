package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// optionsID names the options screen's list of settings in Limoni's
// accessibility tree, for tests.
const optionsID = "options"

// The screens and prompts a client can leave to the game's own text UI,
// with the names the options screen shows and the capabilities that
// hand them to the client.
var nativeScreens = []struct {
	name, label string
	caps        []string
}{
	{splashName, "Splash screen", nil},
	{"options", "Options screen", []string{"screen:options"}},
	{"death", "Death screen", []string{"screen:death"}},
	{"commands", "Command menu", []string{"screen:commands"}},
	{"inventory", "Inventory screen", []string{"screen:inventory"}},
	{"store", "Store screen", []string{"screen:store"}},
	{"birth", "Character creation", []string{"screen:birth"}},
	{"choices", "Item and spell choices", []string{"prompt:item", "prompt:spell", "prompt:spell_from_book"}},
	{"dialogs", "Quantity, yes-or-no, and direction prompts", []string{"prompt:quantity", "prompt:check", "prompt:direction"}},
	{"look", "Look, target, and aiming screens", []string{"screen:look", "screen:target", "prompt:aim"}},
	{"knowledge", "Knowledge screen", []string{"screen:knowledge"}},
	{compareName, "Equipment comparison", nil},
	{"character", "Character sheet", []string{"screen:character"}},
	{"messages", "Message history", []string{"screen:messages"}},
	{"help", "Help", []string{"screen:help"}},
	{"map", "Level map", []string{"screen:map"}},
	{"lists", "Monster and item lists", []string{"screen:lists"}},
}

// optKind is what kind of setting a row holds.
type optKind int

const (
	optCheck  optKind = iota // on or off
	optNumber                // a number between min and max
	optChoice                // one of choices
	optButton                // an action
	optInfo                  // text only
)

// optRow is one row of the options screen.
type optRow struct {
	kind  optKind
	label string
	// desc is shown under the list while the row is selected.
	desc     string
	editable bool
	// why is shown instead of desc when the row cannot change.
	why string

	on      bool
	num     int
	min     int
	max     int
	step    int
	format  func(int) string
	choice  int
	choices []string

	// set applies a change; press runs a button.
	set   func(r *optRow)
	press func()
}

func (r *optRow) text() string {
	switch r.kind {
	case optCheck:
		box := "[ ]"
		if r.on {
			box = "[x]"
		}
		return box + " " + r.label
	case optNumber:
		v := strconv.Itoa(r.num)
		if r.format != nil {
			v = r.format(r.num)
		}
		return fmt.Sprintf("%-34s ◀ %s ▶", r.label, v)
	case optChoice:
		return fmt.Sprintf("%-34s ◀ %s ▶", r.label, r.choices[r.choice])
	case optButton:
		return "» " + r.label
	}
	return r.label
}

// arrowHotspots makes the ◀ and ▶ of a number or choice row clickable,
// for a row drawn as label from x on line y: ◀ moves it down a step, and
// ▶ up. sel selects the row first, and add registers a hotspot. Register
// them after the row's own hotspot, so they win.
func arrowHotspots(label string, x, y int, r *optRow, sel func(), add func(Box, func())) {
	if (r.kind != optNumber && r.kind != optChoice) || !r.editable {
		return
	}
	runes := []rune(label)
	for i, c := range runes {
		d := map[rune]int{'◀': -1, '▶': 1}[c]
		if d == 0 {
			continue
		}
		at := x + limoni.StringWidth(string(runes[:i]))
		add(Box{at - 1, y, 3, 1}, func() {
			sel()
			r.change(d)
		})
	}
}

// change moves a number or choice by delta, or toggles a check.
func (r *optRow) change(delta int) {
	if !r.editable {
		return
	}
	switch r.kind {
	case optCheck:
		r.on = !r.on
	case optNumber:
		r.num = min(max(r.num+delta*max(r.step, 1), r.min), r.max)
	case optChoice:
		n := len(r.choices)
		r.choice = ((r.choice+delta)%n + n) % n
	case optButton:
		if r.press != nil {
			r.press()
		}
		return
	default:
		return
	}
	if r.set != nil {
		r.set(r)
	}
}

type optTab struct {
	name string
	rows []*optRow
}

// optionsScreen is the native options screen: a tab per page of
// Angband's options, and one for the client's own settings.
type optionsScreen struct {
	m    *Model
	tabs []*optTab
	tab  int
	// reqID is the engine's screen request, or 0 when the screen was
	// opened from a menu.
	reqID int
	state *widgets.ListState
	// listHeight is how many rows the list showed when last drawn.
	listHeight int

	hotspots []hotspot
}

func (s *optionsScreen) rows() []*optRow { return s.tabs[s.tab].rows }

// controls returns the parts a click acts on, as last drawn.
func (s *optionsScreen) controls() []hotspot { return s.hotspots }

func (s *optionsScreen) selectedRow() *optRow {
	rows := s.rows()
	if s.state.Selected < 0 || s.state.Selected >= len(rows) {
		return nil
	}
	return rows[s.state.Selected]
}

func (s *optionsScreen) setTab(t int) {
	n := len(s.tabs)
	s.tab = ((t % n) + n) % n
	s.state.Select(0)
}

func (s *optionsScreen) key(ev driver.KeyEvent) {
	rows := s.rows()
	switch ev.Type {
	case driver.KeyEsc:
		s.m.closeOptions(s)
		return
	case driver.KeyTab:
		if ev.Shift {
			s.setTab(s.tab - 1)
		} else {
			s.setTab(s.tab + 1)
		}
		return
	case driver.KeyArrowDown:
		if len(rows) > 0 {
			s.state.Select((s.state.Selected + 1) % len(rows))
		}
		return
	case driver.KeyArrowUp:
		if len(rows) > 0 {
			s.state.Select((s.state.Selected - 1 + len(rows)) % len(rows))
		}
		return
	case driver.KeyPageDown, driver.KeyPageUp, driver.KeyHome, driver.KeyEnd:
		if len(rows) == 0 {
			return
		}
		sel := map[driver.KeyType]int{
			driver.KeyPageDown: s.state.Selected + pageRows(s.listHeight),
			driver.KeyPageUp:   s.state.Selected - pageRows(s.listHeight),
			driver.KeyHome:     0,
			driver.KeyEnd:      len(rows) - 1,
		}[ev.Type]
		s.state.Select(min(max(sel, 0), len(rows)-1))
		return
	case driver.KeyArrowLeft, driver.KeyArrowRight:
		r := s.selectedRow()
		if r == nil {
			return
		}
		d := 1
		if ev.Type == driver.KeyArrowLeft {
			d = -1
		}
		if r.kind == optNumber || r.kind == optChoice {
			r.change(d)
		}
		return
	case driver.KeyEnter, driver.KeySpace:
		if r := s.selectedRow(); r != nil {
			r.change(1)
		}
		return
	case driver.KeyRune:
		switch {
		case ev.Ch == ' ':
			if r := s.selectedRow(); r != nil {
				r.change(1)
			}
		case ev.Ch == '[':
			s.setTab(s.tab - 1)
		case ev.Ch == ']':
			s.setTab(s.tab + 1)
		case ev.Ch >= '1' && ev.Ch <= '9' && int(ev.Ch-'1') < len(s.tabs):
			s.setTab(int(ev.Ch - '1'))
		case ev.Ch == 'j':
			s.key(driver.KeyEvent{Type: driver.KeyArrowDown})
		case ev.Ch == 'k':
			s.key(driver.KeyEvent{Type: driver.KeyArrowUp})
		}
	}
}

func (s *optionsScreen) box(area Box) Box {
	w, h := min(78, area.W), min(24, area.H)
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (s *optionsScreen) click(x, y int) {
	// The last drawn is on top, as a row's arrows are over the row.
	for i := len(s.hotspots) - 1; i >= 0; i-- {
		if h := s.hotspots[i]; x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

func (s *optionsScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	s.hotspots = append(s.hotspots, hotspot{box: b, do: do})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { do() })
}

func (s *optionsScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	s.hotspots = s.hotspots[:0]
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" Options ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenBorder), rect(b))
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}

	// The tabs, the current one highlighted.
	x := inner.X + 1
	for i, t := range s.tabs {
		label := " " + t.name + " "
		st := screenDim
		if i == s.tab {
			st = screenSelected
		}
		w := limoni.StringWidth(label)
		f.Text(x, inner.Y, label, st)
		i := i
		s.hotspot(f, Box{x, inner.Y, w, 1}, func() { s.setTab(i) })
		x += w + 1
	}

	// The footer and the selected row's description.
	foot := "Tab next page  ↑↓ move  Space change  ←→ adjust  Esc close"
	f.TextFit(inner.X+1, inner.Y+inner.H-1, inner.W-2, foot, screenDim)
	if r := s.selectedRow(); r != nil {
		desc := r.desc
		if !r.editable && r.why != "" {
			desc = r.why
		}
		f.RenderWidget(&styledView{text: proto.PlainText(colorWhite, desc), pal: env.pal},
			rect(Box{inner.X + 1, inner.Y + inner.H - 3, inner.W - 2, 2}))
	}

	listBox := Box{inner.X, inner.Y + 2, inner.W, inner.H - 6}
	rows := s.rows()
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = " " + r.text()
	}
	rowStyle := screenText.WithBg(screenBg.Bg)
	f.RenderWidget(&widgets.List{
		ID: optionsID, Label: s.tabs[s.tab].name, Items: labels,
		Style: rowStyle, SelectedStyle: screenSelected, FocusedStyle: rowStyle,
		Scrollbar: true, State: s.state,
	}, rect(listBox))
	s.listHeight = listBox.H
	f.RenderWidget(wheelArea{scroll: func(d int) { wheelList(s.state, len(rows), s.listHeight, d, nil) }, pointer: env.pointer}, rect(listBox))
	for y := 0; y < listBox.H; y++ {
		n := s.state.Offset + y
		if n >= len(rows) {
			break
		}
		r := rows[n]
		if !r.editable && r.kind != optInfo {
			st := screenDim
			if n == s.state.Selected {
				st = screenSelected
			}
			f.TextFit(listBox.X, listBox.Y+y, listBox.W-1, labels[n], st)
		}
		s.hotspot(f, Box{listBox.X, listBox.Y + y, listBox.W, 1}, func() {
			if s.state.Selected == n || r.kind == optCheck || r.kind == optButton {
				s.state.Select(n)
				r.change(1)
				return
			}
			s.state.Select(n)
		})
		arrowHotspots(labels[n], listBox.X, listBox.Y+y, r, func() { s.state.Select(n) },
			func(b Box, do func()) { s.hotspot(f, b, do) })
	}
}

// openOptions asks the engine for the options, then opens the options
// screen. reqID is the engine's screen request, or 0 from a menu.
func (m *Model) openOptions(reqID int) {
	m.command("OPTIONS", nil, func(r proto.EventData) {
		if !r.OK {
			m.notify("Cannot read the options: " + r.Error)
			if reqID != 0 {
				m.send(proto.NewReply(reqID, map[string]any{}))
			}
			return
		}
		s := &optionsScreen{m: m, reqID: reqID, state: widgets.NewListState()}
		s.state.Select(0)
		s.tabs = m.optionTabs(m.lastOptions, s)
		if !m.lastOptions.Loaded {
			// Before a character loads, only the client's own
			// settings can change; open on them.
			s.tab = len(s.tabs) - 1
		}
		m.push(s)
	})
}

// closeOptions closes the options screen, answering the engine's
// request if it opened the screen.
func (m *Model) closeOptions(s *optionsScreen) {
	m.pop()
	if s.reqID != 0 {
		m.send(proto.NewReply(s.reqID, map[string]any{}))
	}
}

// classicOptions closes the options screen and opens the game's own.
func (m *Model) classicOptions(s *optionsScreen) {
	m.closeAll()
	if s.reqID != 0 {
		m.send(proto.NewReply(s.reqID, map[string]any{"classic": true}))
		return
	}
	// From a menu: press '=' and answer its request with the classic
	// screen.
	m.classicNext = true
	m.sendKey(proto.Key{Code: '='})
}

// setOption sends SET_OPTION and puts the row back if the engine refuses.
func (m *Model) setOption(name string, value any, undo func()) {
	m.command("SET_OPTION", map[string]any{"name": name, "value": value}, func(r proto.EventData) {
		if !r.OK {
			undo()
			m.notify("Cannot change " + name + ": " + r.Error)
		}
	})
}

// optionTabs builds the options screen's pages from an OPTIONS answer.
func (m *Model) optionTabs(o proto.EventData, s *optionsScreen) []*optTab {
	page := func(name string) []*optRow {
		var rows []*optRow
		for _, opt := range o.Options {
			if opt.Page != name {
				continue
			}
			opt := opt
			r := &optRow{kind: optCheck, label: opt.Name, desc: opt.Description, on: opt.Value, editable: opt.Editable}
			switch {
			case !o.Loaded:
				r.why = opt.Description + " (the default for new characters; load a character to change it)"
			case name == "birth":
				r.why = opt.Description + " (set only while making a character)"
			}
			r.set = func(r *optRow) {
				on := r.on
				m.setOption(opt.Name, on, func() { r.on = !on })
			}
			if name == "cheat" {
				r.set = func(r *optRow) {
					if !r.on {
						m.setOption(opt.Name, false, func() { r.on = true })
						return
					}
					r.on = false
					m.confirmCheat(opt, r)
				}
			}
			rows = append(rows, r)
		}
		return rows
	}
	saveDefaults := func(name string) *optRow {
		return &optRow{kind: optButton, label: "Save this page as the defaults for new characters",
			desc: "New characters start with these options.", editable: o.Loaded,
			why: "Load a character first.",
			press: func() {
				m.command("SAVE_OPTION_DEFAULTS", map[string]any{"page": name}, func(r proto.EventData) {
					if r.OK {
						m.notify("Saved as the defaults for new characters.")
					} else {
						m.notify("Cannot save the defaults: " + r.Error)
					}
				})
			}}
	}

	iface := page("interface")
	// The game's own menu cannot open over the birth screen.
	more := "Load a character first."
	if m.birth != nil {
		more = "Make the character first."
	}
	iface = append(iface, saveDefaults("interface"),
		&optRow{kind: optButton, label: "More options", editable: o.Loaded && m.birth == nil, why: more,
			desc:  "The game's own options menu: keymaps, colors, what to ignore, and more.",
			press: func() { m.classicOptions(s) }})

	number := func(name, label, desc string, lo, hi, step int, format func(int) string) *optRow {
		r := &optRow{kind: optNumber, label: label, desc: desc, num: o.Numbers[name],
			min: lo, max: hi, step: step, format: format, editable: o.Loaded,
			why: desc + " (load a character to change it)"}
		r.set = func(r *optRow) {
			v, old := r.num, o.Numbers[name]
			m.setOption(name, v, func() { r.num = old })
		}
		return r
	}
	numbers := []*optRow{
		number("delay_factor", "Delay for animations", "How long the game pauses to show a missile or spell, in milliseconds.",
			0, 255, 5, func(v int) string { return fmt.Sprintf("%d ms", v) }),
		number("hitpoint_warn", "Warn when hit points fall below", "The game warns you, and colors your hit points, below this share of the maximum.",
			0, 9, 1, func(v int) string { return fmt.Sprintf("%d%%", v*10) }),
		number("lazymove_delay", "Diagonal move wait", "With two keys pressed together for a diagonal move, how long the game waits for the second, in milliseconds.",
			0, 255, 1, func(v int) string { return fmt.Sprintf("%d ms", v*10) }),
	}

	birth := page("birth")
	birth = append(birth, saveDefaults("birth"))

	return []*optTab{
		{"Interface", iface},
		{"Numbers", numbers},
		{"Birth", birth},
		{"Cheat", page("cheat")},
		{"Display", m.displayRows(o)},
	}
}

// confirmCheat asks before turning on a cheat option, which marks the
// character for good.
func (m *Model) confirmCheat(opt proto.OptionData, r *optRow) {
	ask := newMenu("cheat-warning", "Cheat option", []menuEntry{
		{label: "Turn it on", do: func() {
			m.pop()
			r.on = true
			m.setOption(opt.Name, true, func() { r.on = false })
		}},
		{label: "Cancel", do: m.pop},
	})
	ask.intro = proto.PlainText(colorWhite, opt.Description+"\n\nTurning on a cheat option marks this character as a cheater for good, "+
		"even if you turn it off again, and keeps it off the high score list.")
	ask.cancel = m.pop
	ask.width = 60
	ask.selectItem(1)
	m.push(ask)
}

// displayRows are the client's own settings, saved in its settings file.
func (m *Model) displayRows(o proto.EventData) []*optRow {
	var rows []*optRow

	sets := []string{"Text"}
	ids := []int{0}
	if o.Graphics != nil {
		for _, ts := range o.Graphics.Sets {
			sets = append(sets, ts.Name)
			ids = append(ids, ts.ID)
		}
	}
	tileRow := &optRow{kind: optChoice, label: "Tile set", choices: sets, editable: m.cfg.TilesBlocked == "",
		desc: "Draw the map with letters, or with one of the game's tile sets.",
		why:  "Tiles are off: " + m.cfg.TilesBlocked + "."}
	tileRow.choice = max(slices.Index(ids, m.cfg.Tiles), 0)
	tileRow.set = func(r *optRow) {
		id := ids[r.choice]
		m.settings.TileSet = &id
		m.saveSettings()
		m.command("GRAPHICS", map[string]any{"set": id, "scale": m.fittingScale(m.wantScale)}, func(res proto.EventData) {
			if !res.OK {
				m.notify("Cannot change the tile set: " + res.Error)
			}
		})
	}
	rows = append(rows, tileRow)

	sizeRow := &optRow{kind: optNumber, label: "Tile size", min: 1, max: maxScale, step: 1, num: m.wantScale,
		editable: m.cfg.TilesBlocked == "", why: tileRow.why,
		format: func(v int) string {
			s := strconv.Itoa(v)
			if !m.scaleFits(v) {
				s += " (too big for this window)"
			}
			return s
		}}
	sizeRow.desc = "How big each tile is. Bigger tiles show fewer grids of the map."
	if m.cfg.Protocol == "Sixel" {
		sizeRow.desc += " With Sixel, size 2 sends about four times the picture data per move, and size 3 nine times."
	}
	sizeRow.set = func(r *optRow) {
		m.wantScale = r.num
		m.settings.TileScale = r.num
		m.saveSettings()
		m.fitScale()
	}
	rows = append(rows, sizeRow)

	rows = append(rows, &optRow{kind: optCheck, label: "Animations", on: m.cfg.Animate, editable: true,
		desc: "Animate the tiles you give animated filters in the Tile Editor.",
		set: func(r *optRow) {
			m.cfg.Animate = r.on
			on := r.on
			m.settings.Animate = &on
			m.saveSettings()
			m.startTicking = r.on && !m.ticking
		}})
	rows = append(rows, &optRow{kind: optCheck, label: "Wall shadows", on: m.shadows(), editable: m.cfg.TilesBlocked == "",
		desc: "Darken the floor where it meets a wall, with tiles, so rooms and corridors stand out.",
		why:  tileRow.why,
		set: func(r *optRow) {
			on := r.on
			m.settings.Shadows = &on
			m.saveSettings()
		}})
	rows = append(rows, &optRow{kind: optCheck, label: "Level colors", on: m.levelColorsOn(), editable: m.cfg.TilesBlocked == "",
		desc: "Give each dungeon level's walls and floor a random color of their own, with tiles, rolled when you arrive. " +
			"The walls and the floor roll apart. The town keeps its usual colors.",
		why: tileRow.why,
		set: func(r *optRow) {
			on := r.on
			m.settings.LevelColors = &on
			m.saveSettings()
			m.pickLevelColors()
		}})
	lightRow := &optRow{kind: optChoice, label: "Light edges", editable: m.cfg.TilesBlocked == "", why: tileRow.why,
		choices: []string{"Off", "Corner blending", "Diagonal cuts", "Dithered fade"},
		desc: "Soften the steps where the floor's light changes or meets the unknown, with tiles. Corner blending turns the steps into a smooth slope. " +
			"Diagonal cuts turn them into slanted lines. Dithered fade is corner blending drawn as a pixel pattern, which costs the most to send."}
	lightRow.choice = max(slices.Index(lightEdgeKeys, m.settings.LightEdges), 0)
	lightRow.set = func(r *optRow) {
		m.settings.LightEdges = lightEdgeKeys[r.choice]
		m.saveSettings()
	}
	rows = append(rows, lightRow)
	rows = append(rows, &optRow{kind: optCheck, label: "Smooth movement", on: m.sliding(), editable: m.cfg.TilesBlocked == "",
		desc: "Slide the character and monsters from grid to grid, with tiles, instead of letting them jump. A key ends the slide at once.",
		why:  tileRow.why,
		set: func(r *optRow) {
			on := r.on
			m.settings.Slide = &on
			m.saveSettings()
			if !on {
				m.endMotion()
			}
		}})
	rows = append(rows, &optRow{kind: optCheck, label: "Directional Flipping", on: m.flipping(), editable: m.cfg.TilesBlocked == "",
		desc: "Turn the character and monsters the way they last stepped, with tiles, by mirroring the tile when they step left.",
		why:  tileRow.why,
		set: func(r *optRow) {
			on := r.on
			m.settings.Flip = &on
			m.saveSettings()
		}})
	scrollKeys := []string{scrollOff, scrollPixel, scrollCell, scrollMap}
	scrollRow := &optRow{kind: optChoice, label: "Map scrolling", choices: []string{"Off", "Pixels", "Whole cells", "Whole map"}, editable: true,
		desc: "How the map glides when you near its edge, instead of jumping. Pixels moves each tile and cuts the tiles at the edge exactly. " +
			"Whole cells cuts them a cell at a time, so a little can spill past the edge. Whole map moves the map as one picture: " +
			"few bytes a frame, but a large picture to send first. Text scrolls a whole cell at a time. Tiles scroll only with Kitty's image protocol."}
	scrollRow.choice = max(slices.Index(scrollKeys, m.scrollMethod()), 0)
	scrollRow.set = func(r *optRow) {
		m.settings.Scroll = scrollKeys[r.choice]
		m.saveSettings()
		m.scroll, m.pendingScroll = nil, nil
	}
	rows = append(rows, scrollRow)
	rows = append(rows, &optRow{kind: optCheck, label: "Side panels", on: m.cfg.Subwindows, editable: true,
		desc: "Show the message log and the list of monsters beside or below the map when the window has room.",
		set: func(r *optRow) {
			m.cfg.Subwindows = r.on
			on := r.on
			m.settings.Subwindows = &on
			m.saveSettings()
			m.relayout(m.layout.Width, m.layout.Height)
		}})
	rows = append(rows, &optRow{kind: optCheck, label: "Highlight under the mouse", on: m.settings.Hover, editable: true,
		desc: "Light up the button, row, or tab under the mouse pointer in Fancyband's own screens. " +
			"The terminal then reports every move of the mouse, which adds a little traffic over SSH.",
		set: func(r *optRow) {
			m.settings.Hover = r.on
			m.saveSettings()
			if !r.on && !m.describeOn() {
				m.pointerSet = false
			}
		}})
	rows = append(rows, &optRow{kind: optCheck, label: "Descriptions under the mouse", on: m.describeOn(), editable: true,
		desc: "Rest the mouse on an underlined name, such as a race, a stat, or Hold Life, to see what it does. " +
			"Clicking a name works either way. The terminal then reports every move of the mouse, which adds a little traffic over SSH.",
		set: func(r *optRow) {
			on := r.on
			m.settings.Describe = &on
			m.saveSettings()
			if !r.on && !m.settings.Hover {
				m.pointerSet = false
			}
			if !r.on {
				m.hoverMark = nil
				if m.desc != nil && m.desc.hover {
					m.closeDesc()
				}
			}
		}})
	rows = append(rows, &optRow{kind: optInfo, label: "Image protocol: " + m.cfg.Protocol,
		desc: "How pictures reach the terminal. The -graphics flag chooses another."})

	rows = append(rows, &optRow{kind: optCheck, label: "Native stats bar", on: m.nativeStats(), editable: true,
		desc: "Show the character's stats in a bar right of the map, and the status line under it, when the window has room. " +
			"Off brings back the game's own sidebar and status line.",
		set: func(r *optRow) {
			m.settings.Classic = slices.DeleteFunc(m.settings.Classic, func(s string) bool { return s == statsName })
			if !r.on {
				m.settings.Classic = append(m.settings.Classic, statsName)
			}
			m.saveSettings()
			m.relayout(m.layout.Width, m.layout.Height)
			m.fitScale()
		}})
	sideRow := &optRow{kind: optChoice, label: "Stats bar side", choices: []string{"Left", "Right"}, editable: true,
		desc: "Which side of the map the native stats bar sits on."}
	if m.settings.StatsRight {
		sideRow.choice = 1
	}
	sideRow.set = func(r *optRow) {
		m.settings.StatsRight = r.choice == 1
		m.saveSettings()
		m.relayout(m.layout.Width, m.layout.Height)
	}
	rows = append(rows, sideRow)

	for _, ns := range nativeScreens {
		ns := ns
		rows = append(rows, &optRow{kind: optCheck, label: "Native " + strings.ToLower(ns.label), editable: true,
			on:   !slices.Contains(m.settings.Classic, ns.name),
			desc: "Use Fancyband's own " + strings.ToLower(ns.label) + "; off brings back the game's classic one. Takes effect when the game next starts.",
			set: func(r *optRow) {
				m.settings.Classic = slices.DeleteFunc(m.settings.Classic, func(s string) bool { return s == ns.name })
				if !r.on {
					m.settings.Classic = append(m.settings.Classic, ns.name)
				}
				m.saveSettings()
			}})
	}
	return rows
}

package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// Looking and targeting: the engine hands look mode, target mode, and
// the aiming prompt to the client. Each is a list of what the character
// can see, nearest first, as the LOOK query finds it. Moving through the
// list moves a marker on the map, and the list moves out of the marker's
// way, to the top or the bottom of the window.

// IDs of the look screens in Limoni's accessibility tree, which tests
// use.
const (
	lookID = "look"
	aimID  = "aim"
)

// lookMode is what a look screen is for.
type lookMode int

const (
	modeLook   lookMode = iota // look around; t targets, g travels
	modeTarget                 // pick a target
	modeAim                    // pick a target or a direction, then fire
)

// lookScreen is the list for look mode, target mode, and aiming.
type lookScreen struct {
	*listScreen
	m      *Model
	mode   lookMode
	things []proto.LookThing
	// top puts the screen at the top of the window, out of the marker's
	// way; otherwise it sits at the bottom.
	top bool
	// drawn is where the screen was last drawn.
	drawn Box
	// lookAt runs for a grid clicked on the map that the list lacks.
	lookAt func(x, y int)
	// keyFirst, if set, sees each key before the list does, as the
	// aiming prompt's directions.
	keyFirst func(ev driver.KeyEvent) bool
}

// lookArgs are LOOK's args for a mode.
func lookArgs(mode lookMode) map[string]any {
	if mode == modeLook {
		return map[string]any{"mode": "look"}
	}
	return map[string]any{"mode": "target"}
}

// lookRow makes a list row for a grid LOOK lists.
func lookRow(th proto.LookThing) listItem {
	row := listItem{Label: capitalize(th.Name), Hint: th.Where}
	switch th.Kind {
	case "player":
		row.Label, row.Hint = "You", "here"
	case "monster":
		if th.Health != "" {
			row.Label += " (" + th.Health + ")"
		}
	}
	if th.Tile != nil {
		row.Tile = &proto.PictCell{FgRow: th.Tile.Row, FgCol: th.Tile.Col}
	} else if r := []rune(th.Char); len(r) > 0 {
		row.Icon, row.IconColor = r[0], th.Color
	}
	row.Detail = lookHead(th)
	return row
}

// lookHead is the start of a grid's detail panel: what look mode says
// about it, and the objects in a pile.
func lookHead(th proto.LookThing) proto.StyledText {
	text := proto.PlainText(colorWhite, th.Text)
	if len(th.Objects) > 1 {
		text = append(text, nil)
		for _, o := range th.Objects {
			text = append(text, []proto.Span{{Text: "  " + o.Name, Color: colorWhite}})
		}
	}
	return text
}

// recallArgs are RECALL's args for the first thing on a grid, or nil.
func recallArgs(th proto.LookThing) map[string]any {
	switch th.Kind {
	case "monster":
		return map[string]any{"monster": th.Monster}
	case "object", "pile":
		return map[string]any{"grid": map[string]any{"x": th.X, "y": th.Y}, "object": 0}
	case "trap":
		if th.Trap != nil {
			return map[string]any{"trap": *th.Trap}
		}
	case "terrain", "player":
		if th.Feature != nil {
			return map[string]any{"feature": *th.Feature}
		}
	}
	return nil
}

// lookRows makes the rows for a LOOK answer, and asks for each one's
// description.
func (m *Model) lookRows(things []proto.LookThing) []listItem {
	rows := make([]listItem, len(things))
	for i, th := range things {
		rows[i] = lookRow(th)
	}
	for i, th := range things {
		args := recallArgs(th)
		if args == nil {
			continue
		}
		row, head := &rows[i], lookHead(th)
		m.query("RECALL", args, func(a proto.EventData) {
			if a.Title == nil || len(a.Description) == 0 {
				return
			}
			detail := append(proto.StyledText{}, head...)
			detail = append(detail, nil, []proto.Span{{Text: capitalize(*a.Title), Color: colorYellow}}, nil)
			row.DetailTile, row.DetailHead = row.Tile, len(detail)-2
			row.Detail = append(detail, a.Description...)
		})
	}
	return rows
}

// openLook answers the look or target screen request r.
func (m *Model) openLook(r proto.RequestData) {
	mode := modeLook
	if r.Mode == "target" {
		mode = modeTarget
	}
	args := lookArgs(mode)
	if r.Grid != nil {
		args["x"], args["y"] = r.Grid.X, r.Grid.Y
	}
	m.query("LOOK", args, func(a proto.EventData) {
		s := m.newLookScreen(mode, a.Things)
		closeWith := func(reply map[string]any) {
			m.closeLook(s)
			m.send(proto.NewReply(r.ID, reply))
		}
		s.cancel = func() { closeWith(map[string]any{}) }
		target := func(item int) {
			if item < 0 {
				return
			}
			th := s.things[item]
			if th.Kind == "player" {
				return
			}
			if th.Kind == "monster" && th.Targetable {
				m.command("SET_TARGET", map[string]any{"monster": th.Midx}, nil)
			} else if mode == modeTarget {
				return
			} else {
				m.command("SET_TARGET", map[string]any{"x": th.X, "y": th.Y}, nil)
			}
			closeWith(map[string]any{})
		}
		s.choose = target
		s.keys = append(s.keys, listKey{Key: 't', Label: "target", Do: target})
		if r.Pathfind && mode == modeLook {
			s.keys = append(s.keys, listKey{Key: 'g', Label: "travel", Do: func(item int) {
				if item < 0 {
					return
				}
				th := s.things[item]
				m.nextID++
				closeWith(map[string]any{"then": map[string]any{
					"id": m.nextID, "cmd": "PATHFIND",
					"args": map[string]any{"point": map[string]any{"x": th.X, "y": th.Y}},
				}})
			}})
		}
		if mode == modeLook {
			s.hints = []string{"Space next"}
			s.lookAt = func(x, y int) {
				args := lookArgs(mode)
				args["x"], args["y"] = x, y
				m.query("LOOK", args, func(a proto.EventData) {
					if len(a.Things) == 0 || !a.Things[0].Free {
						return
					}
					s.things = append([]proto.LookThing{a.Things[0]}, s.things...)
					s.items = append(m.lookRows(a.Things[:1]), s.items...)
					s.state.Select(0)
					s.moved()
				})
			}
		}
		if len(s.things) == 0 {
			s.note = "Nothing in view can be targeted."
			if mode == modeLook {
				s.note = "Nothing in view."
			}
			s.detail = false
		}
		s.selectStart(r.Grid, r.Target)
		m.push(s)
		s.moved()
	})
}

// openAim answers an aiming prompt: a list of targets, with directions
// too. With nothing to target, it is the direction dialog.
func (m *Model) openAim(r proto.RequestData) {
	m.query("LOOK", lookArgs(modeAim), func(a proto.EventData) {
		if len(a.Things) == 0 {
			m.openDirection(r)
			return
		}
		s := m.newLookScreen(modeAim, a.Things)
		s.id, s.title = aimID, "Aim"
		answer := func(reply map[string]any) {
			m.closeLook(s)
			m.send(proto.NewReply(r.ID, reply))
		}
		fire := func(item int) {
			if item < 0 {
				return
			}
			m.command("SET_TARGET", map[string]any{"monster": s.things[item].Midx}, nil)
			answer(map[string]any{"value": 5})
		}
		s.cancel = func() { answer(nil) }
		s.choose = fire
		s.keys = []listKey{{Key: 't', Label: "fire at it", Do: fire}, {Key: '\'', Label: "closest", Do: func(int) { fire(0) }}}
		s.hints = []string{"Enter fire at it", "1-9 a direction"}
		s.keyFirst = func(ev driver.KeyEvent) bool {
			if ev.Type != driver.KeyRune || ev.Ctrl || ev.Alt {
				return false
			}
			dir := 0
			switch {
			case ev.Ch >= '1' && ev.Ch <= '9':
				dir = int(ev.Ch - '0')
			case ev.Ch == '.' || ev.Ch == '0':
				dir = 5
			default:
				dir = directionLetters[ev.Ch]
			}
			switch dir {
			case 0:
				return false
			case 5:
				fire(s.selected())
			default:
				answer(map[string]any{"value": dir})
			}
			return true
		}
		s.selectStart(nil, a.Target)
		m.push(s)
		s.moved()
	})
}

// newLookScreen makes the list for things.
func (m *Model) newLookScreen(mode lookMode, things []proto.LookThing) *lookScreen {
	title := map[lookMode]string{modeLook: "Look", modeTarget: "Target", modeAim: "Aim"}[mode]
	s := &lookScreen{listScreen: newListScreen(lookID, title, m.lookRows(things)), m: m, mode: mode, things: things}
	s.detail = true
	base := s.listScreen.pick
	s.listScreen.pick = func(ev driver.KeyEvent) bool {
		if s.keyFirst != nil && s.keyFirst(ev) {
			return true
		}
		if base != nil && base(ev) {
			return true
		}
		// As look mode: Space and + step forward, - back, and q leaves.
		if ev.Type == driver.KeySpace || ev.Type == driver.KeyRune && (ev.Ch == '+' || ev.Ch == ' ') {
			s.move(driver.KeyEvent{Type: driver.KeyArrowDown}, len(s.shown()))
			return true
		}
		if ev.Type == driver.KeyRune && !ev.Ctrl && !ev.Alt {
			switch ev.Ch {
			case '-':
				s.move(driver.KeyEvent{Type: driver.KeyArrowUp}, len(s.shown()))
				return true
			case 'q':
				if s.cancel != nil {
					s.cancel()
				}
				return true
			}
		}
		return false
	}
	return s
}

// selectStart selects the grid the player picked, or else the target.
func (s *lookScreen) selectStart(grid, target *proto.TargetData) {
	for i, th := range s.things {
		if grid != nil && th.X == grid.X && th.Y == grid.Y {
			s.selectItem(i)
			return
		}
	}
	if target == nil {
		return
	}
	for i, th := range s.things {
		if target.Monster > 0 && th.Midx == target.Monster || target.Monster == 0 && th.X == target.X && th.Y == target.Y {
			s.selectItem(i)
			return
		}
	}
}

// closeLook closes s and takes its marker off the map.
func (m *Model) closeLook(s *lookScreen) {
	m.marker = nil
	for i, t := range m.stack {
		if t == s {
			m.stack = append(m.stack[:i], m.stack[i+1:]...)
			return
		}
	}
}

func (s *lookScreen) key(ev driver.KeyEvent) {
	s.listScreen.key(ev)
	s.moved()
}

func (s *lookScreen) wheel(d int) {
	s.listScreen.wheel(d)
	s.moved()
}

// cellSize is how many cells a grid takes on the main term.
func (m *Model) cellSize() (int, int) {
	if ts := m.terms.TileSet; ts != nil && m.sheet != nil {
		return max(int(ts.CellsW), 1), max(int(ts.CellsH), 1)
	}
	return 1, 1
}

// markerFor returns the cells, on screen, of the grid th, if the map
// shows it.
func (m *Model) markerFor(th proto.LookThing) (Box, bool) {
	if th.Cell == nil {
		return Box{}, false
	}
	cw, ch := m.cellSize()
	main := m.layout.Main
	return Box{main.X + th.Cell[0], main.Y + th.Cell[1], cw, ch}, true
}

// moved puts the marker on the selected grid, and moves the screen out
// of its way if it would cover it.
func (s *lookScreen) moved() {
	m := s.m
	if !s.open() {
		return
	}
	m.marker = nil
	i := s.selected()
	if i < 0 {
		return
	}
	b, ok := m.markerFor(s.things[i])
	if !ok {
		return
	}
	m.marker = &b
	area := Box{0, 0, m.layout.Width, m.layout.Height}
	if s.boxAt(area, s.top).Overlaps(b) && !s.boxAt(area, !s.top).Overlaps(b) {
		s.top = !s.top
	}
}

// open reports whether s is still on the stack.
func (s *lookScreen) open() bool {
	for _, t := range s.m.stack {
		if t == s {
			return true
		}
	}
	return false
}

// boxAt is where the screen goes at the top of area or the bottom.
func (s *lookScreen) boxAt(area Box, top bool) Box {
	w := min(area.W, max(s.listWidth()+44, 76))
	h := min(max(len(s.items)+6, 12), max(area.H*11/20, 10), area.H)
	if len(s.items) == 0 {
		w, h = min(area.W, 48), min(area.H, 5)
	}
	y := area.Y + area.H - h
	if top {
		y = area.Y
	}
	return Box{area.X + (area.W-w)/2, y, w, h}
}

func (s *lookScreen) box(area Box) Box { return s.boxAt(area, s.top) }

func (s *lookScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	s.drawn = b
	s.listScreen.draw(f, b, env)
}

// click handles a click on the screen, or on the map: a grid in the list
// is selected, and in look mode another grid is looked at.
func (s *lookScreen) click(x, y int) {
	at := Box{x, y, 1, 1}
	if at.Overlaps(s.drawn) {
		s.listScreen.click(x, y)
		s.moved()
		return
	}
	m := s.m
	for i, th := range s.things {
		if b, ok := m.markerFor(th); ok && at.Overlaps(b) {
			s.selectItem(i)
			s.moved()
			return
		}
	}
	if s.lookAt == nil {
		return
	}
	// The grid under the click, worked out from the player's grid.
	main := m.layout.Main
	cw, ch := m.cellSize()
	for _, th := range s.things {
		if th.Kind == "player" && th.Cell != nil && at.Overlaps(main) {
			dx := floorDiv(x-main.X-th.Cell[0], cw)
			dy := floorDiv(y-main.Y-th.Cell[1], ch)
			s.lookAt(th.X+dx, th.Y+dy)
			return
		}
	}
}

// floorDiv divides, rounding down.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// ------------------------------------------------------------------------
// Descriptions from the monster list

// textScreen shows styled text that scrolls, such as a monster's
// description.
type textScreen struct {
	id, title string
	text      proto.StyledText
	// tile, if set, is drawn after the text's first line, when the map uses
	// tiles.
	tile   *proto.PictCell
	offset int
	// height is how many rows of text showed when last drawn.
	height   int
	close    func()
	hotspots []hotspot
}

const recallID = "recall"

func (t *textScreen) rows(width int) int { return styledHeight(t.text, width) }

func (t *textScreen) scroll(d int) {
	t.offset = max(t.offset+d, 0)
}

func (t *textScreen) key(ev driver.KeyEvent) {
	switch ev.Type {
	case driver.KeyEsc, driver.KeyEnter, driver.KeySpace:
		t.close()
	case driver.KeyArrowDown:
		t.scroll(1)
	case driver.KeyArrowUp:
		t.scroll(-1)
	case driver.KeyPageDown:
		t.scroll(pageRows(t.height))
	case driver.KeyPageUp:
		t.scroll(-pageRows(t.height))
	case driver.KeyHome:
		t.offset = 0
	case driver.KeyRune:
		switch ev.Ch {
		case 'q':
			t.close()
		case 'j', '2':
			t.scroll(1)
		case 'k', '8':
			t.scroll(-1)
		}
	}
}

func (t *textScreen) box(area Box) Box {
	w := min(area.W, 76)
	h := min(area.H, max(min(t.rows(w-4)+4, area.H*3/4), 8))
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (t *textScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	inner := Box{b.X + 2, b.Y + 1, b.W - 4, b.H - 3}
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" "+t.title+" ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: t.id, Role: accessibility.RoleDialog, Label: t.title, Bounds: rect(b)})
	rows := t.rows(inner.W)
	t.height = inner.H
	t.offset = min(t.offset, max(rows-inner.H, 0))
	f.RenderWidget(&styledView{text: t.text, pal: env.pal, Offset: t.offset}, rect(inner))
	if t.tile != nil && env.sprite != nil {
		drawHeadTile(f, inner, t.text, 0, t.offset, t.tile, env)
	}
	f.RenderWidget(wheelArea{scroll: func(d int) {
		t.offset = min(max(t.offset+d*wheelStep, 0), max(rows-inner.H, 0))
	}, pointer: env.pointer}, rect(inner))
	footer := "Esc close"
	if rows > inner.H {
		footer = fmt.Sprintf("↑↓ PgUp PgDn scroll  %s", footer)
	}
	f.TextFit(b.X+2, b.Y+b.H-2, b.W-4, footer, screenDim)
}

func (t *textScreen) click(x, y int) {}

// openRecallAt opens the description of the monster on row of the
// monster list pane, if a monster is there.
func (m *Model) openRecallAt(row int) {
	m.query("MONSTER_LIST", nil, func(a proto.EventData) {
		if row < 0 || row >= len(a.Rows) || a.Rows[row] == nil {
			return
		}
		m.query("RECALL", map[string]any{"monster": *a.Rows[row]}, func(r proto.EventData) {
			if r.Title == nil {
				return
			}
			t := &textScreen{id: recallID, title: capitalize(*r.Title), text: r.Description}
			if r.Glyph != nil && r.Glyph.Tile != nil {
				t.tile = &proto.PictCell{FgRow: r.Glyph.Tile.Row, FgCol: r.Glyph.Tile.Col}
				head := proto.StyledText{{{Text: capitalize(*r.Title), Color: colorYellow}}, nil}
				t.text = append(roomForHeadTile(head, 0), r.Description...)
			}
			t.close = func() {
				for i, s := range m.stack {
					if s == t {
						m.stack = append(m.stack[:i], m.stack[i+1:]...)
						return
					}
				}
			}
			m.push(t)
		})
	})
}

// paneAt returns the pane whose term holds the cell x, y, and the row
// of its term there.
func (m *Model) paneAt(x, y int) (Pane, int, bool) {
	for _, p := range m.layout.Panes {
		b := p.TermBox()
		if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
			return p, y - b.Y, true
		}
	}
	return Pane{}, 0, false
}

// ------------------------------------------------------------------------
// The marker

// markerColor is the frame a look screen draws round the grid it points
// at, with tiles.
var markerColor = color.RGBA{255, 214, 90, 255}

// drawMarker lights up the marked grid's cells, in text mode, as the
// selected row of a list is lit.
func (m *Model) drawMarker(f *terminal.Frame) {
	b := m.marker
	if b == nil {
		return
	}
	for y := b.Y; y < b.Y+b.H; y++ {
		for x := b.X; x < b.X+b.W; x++ {
			if c := f.Buffer.Get(uint16(x), uint16(y)); c != nil && c.Content != cell.RuneImage {
				c.Style = c.Style.WithFg(screenSelected.Fg).WithBg(screenSelected.Bg)
			}
		}
	}
}

// framed returns a tile's picture with the marker's frame round it. The
// same picture always gives the same framed one, so Limoni sends it to
// the terminal once.
func (m *Model) framed(img image.Image) image.Image {
	if f, ok := m.frames[img]; ok {
		return f
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	t := max(b.Dx()/16, 1)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if x < t || y < t || x >= b.Dx()-t || y >= b.Dy()-t {
				out.SetRGBA(x, y, markerColor)
			}
		}
	}
	if m.frames == nil || len(m.frames) > 64 {
		m.frames = map[image.Image]image.Image{}
	}
	m.frames[img] = out
	return out
}

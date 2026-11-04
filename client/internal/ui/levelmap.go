package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sort"
	"strings"

	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// The level map: the whole level as the player remembers it, from MAP,
// in a screen that pans and zooms. The arrow keys, the page keys, and the
// wheel pan it. A cursor picks a grid with other keys, and Enter or a click
// travels there with PATHFIND.
//
// In text mode it has two zooms: a letter per grid, as the map shows
// them, and an overview where each cell shows two grids, one above the
// other, as half blocks. With tiles it has three: the map's own tile
// size, half that, and a quarter. The tiles are drawn as one picture of
// the part in view, sent as one band per row of cells, so moving the
// cursor sends only the bands it crosses. It opens at the smallest tile
// size, and in text mode at a letter a grid.

// levelMapID names the level map in Limoni's accessibility tree.
const levelMapID = "levelmap"

// mapZoom is one zoom of the level map.
type mapZoom struct {
	name string
	// div divides the size a grid takes on the map: 1 is full size.
	div int
	// half draws two grids a cell, as half blocks, in text mode.
	half bool
}

var (
	textZooms = []mapZoom{{"Letters", 1, false}, {"Overview", 1, true}}
	tileZooms = []mapZoom{{"Full size", 1, false}, {"Half size", 2, false}, {"Quarter size", 4, false}}
)

// stairLegend is the legend's entry for the stairs when it shows their
// tiles. Each pair of letters holds the place of one tile. The legend is
// plain ASCII, so a byte's offset in a line is its cell.
const stairLegend = "<< >> stairs"

// tileZoomStart is the tile zoom the map opens at: Quarter size.
const tileZoomStart = 2

// viewUnset is the view before a draw has centered it.
var viewUnset = [2]int{-1, -1}

// Colors the level map marks grids with in pictures.
var (
	markPlayer = color.RGBA{255, 220, 60, 255}
	markUp     = color.RGBA{110, 230, 120, 255}
	markDown   = color.RGBA{90, 190, 255, 255}
	markShop   = color.RGBA{255, 150, 60, 255}
	markCursor = color.RGBA{255, 255, 255, 255}
)

// levelMap is the level map screen.
type levelMap struct {
	m  *Model
	id int
	// The level, grid by grid.
	w, h   int
	depth  int
	player [2]int
	chars  [][]rune
	attrs  [][]proto.Attr
	tiles  [][]proto.PictCell
	marks  map[[2]int]proto.LevelMark
	stairs proto.StairTiles

	zoom int
	// cur is the cursor's grid, and view the grid at the top left.
	cur, view [2]int
	// center is the grid the next draw centers on, when the view is unset.
	center [2]int
	// What the last draw showed: the grids across and down, and the
	// cells they took.
	across, down int
	area         Box

	// The picture of the part in view, cut into bands, and the bands the
	// cursor is drawn on.
	picKey   string
	pic      *image.RGBA
	bands    []image.Image
	bandRows [][2]int
	curKey   string
	curBands map[int]image.Image
	// shrunk holds tiles drawn small, by tile and size.
	shrunk map[shrunkKey]*image.RGBA
}

type shrunkKey struct {
	c    proto.PictCell
	w, h int
}

// openMap opens the level map for the screen request id.
func (m *Model) openMap(id int) {
	m.query("MAP", nil, func(a proto.EventData) {
		if a.Level == nil {
			m.send(proto.NewReply(id, map[string]any{}))
			return
		}
		m.push(m.newLevelMap(id, a.Level))
	})
}

func (m *Model) newLevelMap(id int, lv *proto.LevelMap) *levelMap {
	s := &levelMap{
		m: m, id: id, w: lv.Width, h: lv.Height, depth: lv.Depth,
		player: [2]int{lv.Player.X, lv.Player.Y},
		marks:  map[[2]int]proto.LevelMark{},
		stairs: lv.Stairs,
		shrunk: map[shrunkKey]*image.RGBA{},
	}
	for _, mk := range lv.Marks {
		s.marks[[2]int{mk.X, mk.Y}] = mk
	}
	for _, row := range lv.Rows {
		if len(row.Tiles) > 0 {
			cells := make([]proto.PictCell, s.w)
			for x := 0; x < s.w && 4*x+3 < len(row.Tiles); x++ {
				t := row.Tiles[4*x:]
				cells[x] = proto.PictCell{FgRow: uint8(t[0]), FgCol: uint8(t[1]), BgRow: uint8(t[2]), BgCol: uint8(t[3])}
			}
			s.tiles = append(s.tiles, cells)
			continue
		}
		chars := []rune(row.Text)
		attrs := make([]proto.Attr, len(row.Colors))
		for i, c := range row.Colors {
			attrs[i] = proto.Attr(c)
		}
		s.chars = append(s.chars, chars)
		s.attrs = append(s.attrs, attrs)
	}
	s.cur = s.player
	s.center = s.cur
	s.view = viewUnset
	if s.pictures() {
		s.zoom = tileZoomStart
	}
	return s
}

// pictures reports whether the map is drawn with tiles.
func (s *levelMap) pictures() bool {
	return s.tiles != nil && s.m.sheet != nil && s.m.terms.TileSet != nil
}

func (s *levelMap) zooms() []mapZoom {
	if s.pictures() {
		return tileZooms
	}
	return textZooms
}

func (s *levelMap) zoomNow() mapZoom {
	z := s.zooms()
	return z[min(s.zoom, len(z)-1)]
}

func (s *levelMap) setZoom(z int) {
	n := len(s.zooms())
	s.zoom = min(max(z, 0), n-1)
	// Center the cursor at the new size, or if the map was panned away from
	// it, what was in the middle.
	if s.view != viewUnset {
		s.center = s.cur
		if !s.cursorShows() {
			s.center = [2]int{s.view[0] + s.across/2, s.view[1] + s.down/2}
		}
	}
	s.view = viewUnset
}

// footprint returns the cells a grid takes at the current zoom, as
// fractions: cw/dx across and ch/dy down.
func (s *levelMap) footprint() (cw, ch, dx, dy int) {
	z := s.zoomNow()
	if s.pictures() {
		cw, ch = s.m.cellSize()
		return cw, ch, z.div, z.div
	}
	if z.half {
		return 1, 1, 1, 2
	}
	return 1, 1, 1, 1
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// fit returns how many grids fit in n cells, at c/div cells a grid, as a
// number that fills whole cells, and no more than the level needs.
func fit(n, c, div, level int) int {
	step := div / gcd(c, div)
	most := n * div / c
	most -= most % step
	need := (level + step - 1) / step * step
	return max(min(most, need), 0)
}

// cellsFor returns the cells g grids take at c/div cells a grid.
func cellsFor(g, c, div int) int { return g * c / div }

// The cursor and the view

func (s *levelMap) inLevel(x, y int) bool { return x >= 0 && y >= 0 && x < s.w && y < s.h }

// moveTo puts the cursor on grid x, y, inside the level.
func (s *levelMap) moveTo(x, y int) {
	s.cur = [2]int{min(max(x, 0), s.w-1), min(max(y, 0), s.h-1)}
	s.center = s.cur
	s.follow()
}

// cursorShows reports whether the cursor's grid is in view.
func (s *levelMap) cursorShows() bool {
	return s.cur[0] >= s.view[0] && s.cur[0] < s.view[0]+s.across &&
		s.cur[1] >= s.view[1] && s.cur[1] < s.view[1]+s.down
}

// follow scrolls the view so the cursor shows, a few grids from the
// edge where there is room.
func (s *levelMap) follow() {
	if s.across == 0 || s.down == 0 {
		return
	}
	for i, n := range [2]int{s.across, s.down} {
		margin := min(3, n/4)
		if s.cur[i] < s.view[i]+margin {
			s.view[i] = s.cur[i] - margin
		}
		if s.cur[i] >= s.view[i]+n-margin {
			s.view[i] = s.cur[i] - n + margin + 1
		}
	}
	s.clampView()
}

func (s *levelMap) clampView() {
	s.view[0] = min(max(s.view[0], 0), max(s.w-s.across, 0))
	s.view[1] = min(max(s.view[1], 0), max(s.h-s.down, 0))
}

// scrollView moves the view by dx, dy grids and leaves the cursor.
func (s *levelMap) scrollView(dx, dy int) {
	s.view[0] += dx
	s.view[1] += dy
	s.clampView()
}

// pan moves the view in direction dx, dy by about n rows of cells, or 2n
// columns, which look the same distance. It moves at least one grid.
func (s *levelMap) pan(dx, dy, n int) {
	cw, ch, gx, gy := s.footprint()
	s.scrollView(dx*max(n*2*gx/cw, 1), dy*max(n*gy/ch, 1))
}

// wheel scrolls the view by d notches, down or, with across, right.
func (s *levelMap) wheel(d int, across bool) {
	if across {
		s.pan(d, 0, wheelStep)
	} else {
		s.pan(0, d, wheelStep)
	}
}

// markName says what a grid is, for the line under the map.
func (s *levelMap) markName(at [2]int) string {
	if at == s.player {
		return "You are here"
	}
	mk, ok := s.marks[at]
	if !ok {
		return ""
	}
	switch mk.Kind {
	case "up":
		return "An up staircase"
	case "down":
		return "A down staircase"
	}
	return "The entrance to the " + mk.Name
}

// jump moves the cursor to the next mark of a kind, nearest the player
// first, after the one it is on.
func (s *levelMap) jump(kind string) {
	var at [][2]int
	for g, mk := range s.marks {
		if mk.Kind == kind {
			at = append(at, g)
		}
	}
	if len(at) == 0 {
		names := map[string]string{"up": "up staircases", "down": "down staircases", "shop": "shops"}
		s.m.notify("You know of no " + names[kind] + " here.")
		return
	}
	dist := func(g [2]int) int {
		dx, dy := g[0]-s.player[0], g[1]-s.player[1]
		return max(dx, -dx) + max(dy, -dy)
	}
	sort.Slice(at, func(i, j int) bool {
		if dist(at[i]) != dist(at[j]) {
			return dist(at[i]) < dist(at[j])
		}
		return at[i][1] < at[j][1] || at[i][1] == at[j][1] && at[i][0] < at[j][0]
	})
	next := at[0]
	for i, g := range at {
		if g == s.cur {
			next = at[(i+1)%len(at)]
		}
	}
	s.moveTo(next[0], next[1])
}

// travel closes the map and walks to grid at.
func (s *levelMap) travel(at [2]int) {
	m := s.m
	m.pop()
	m.nextID++
	id := m.nextID
	m.pending[id] = func(d proto.EventData) {
		if !d.OK && d.Error != "" {
			m.notify(strings.ToUpper(d.Error[:1]) + d.Error[1:] + ".")
		}
	}
	m.send(proto.NewReply(s.id, map[string]any{"then": map[string]any{
		"id": id, "cmd": "PATHFIND",
		"args": map[string]any{"point": map[string]any{"x": at[0], "y": at[1]}},
	}}))
}

// travelCursor walks to the cursor's grid. If the map was panned away from
// it, it first brings the cursor back, so nobody walks somewhere unseen.
func (s *levelMap) travelCursor() {
	if s.across > 0 && !s.cursorShows() {
		s.follow()
		return
	}
	s.travel(s.cur)
}

func (s *levelMap) close() { s.m.replyClose(s.id)() }

func (s *levelMap) key(ev driver.KeyEvent) {
	step := 1
	if ev.Shift {
		step = 10
	}
	move := func(dx, dy int) { s.moveTo(s.cur[0]+dx*step, s.cur[1]+dy*step) }
	pan := func(dx, dy int) { s.pan(dx, dy, step) }
	switch ev.Type {
	case driver.KeyEsc:
		s.close()
	case driver.KeyEnter:
		s.travelCursor()
	case driver.KeyArrowUp:
		pan(0, -1)
	case driver.KeyArrowDown:
		pan(0, 1)
	case driver.KeyArrowLeft:
		pan(-1, 0)
	case driver.KeyArrowRight:
		pan(1, 0)
	case driver.KeyPageUp:
		s.page(-1)
	case driver.KeyPageDown:
		s.page(1)
	case driver.KeyHome:
		s.moveTo(s.player[0], s.player[1])
	case driver.KeyRune:
		if ev.Ctrl || ev.Alt {
			return
		}
		dirs := map[rune][2]int{
			'h': {-1, 0}, 'j': {0, 1}, 'k': {0, -1}, 'l': {1, 0},
			'y': {-1, -1}, 'u': {1, -1}, 'b': {-1, 1}, 'n': {1, 1},
			'4': {-1, 0}, '2': {0, 1}, '8': {0, -1}, '6': {1, 0},
			'7': {-1, -1}, '9': {1, -1}, '1': {-1, 1}, '3': {1, 1},
			'H': {-1, 0}, 'J': {0, 1}, 'K': {0, -1}, 'L': {1, 0},
			'Y': {-1, -1}, 'U': {1, -1}, 'B': {-1, 1}, 'N': {1, 1},
		}
		if d, ok := dirs[ev.Ch]; ok {
			if ev.Ch >= 'A' && ev.Ch <= 'Z' {
				step = 10
			}
			move(d[0], d[1])
			return
		}
		switch ev.Ch {
		case 'q':
			s.close()
		case '@', '5', '.':
			s.moveTo(s.player[0], s.player[1])
		case '<':
			s.jump("up")
		case '>':
			s.jump("down")
		case 's', '_':
			s.jump("shop")
		case '+', '=':
			s.setZoom(s.zoom - 1)
		case '-':
			s.setZoom(s.zoom + 1)
		case 'z':
			s.setZoom((s.zoom + 1) % len(s.zooms()))
		case 't':
			s.travelCursor()
		}
	}
}

// page moves the view a screenful up or down and leaves the cursor.
func (s *levelMap) page(d int) { s.scrollView(0, d*pageRows(s.down)) }

func (s *levelMap) box(area Box) Box { return area }

// gridAt returns the grid under cell x, y of the map, preferring a
// marked grid when a cell shows more than one.
func (s *levelMap) gridAt(x, y int) ([2]int, bool) {
	a := s.area
	if x < a.X || y < a.Y || x >= a.X+a.W || y >= a.Y+a.H {
		return [2]int{}, false
	}
	cw, ch, dx, dy := s.footprint()
	// The grids the cell covers: those whose footprint overlaps it.
	x0, x1 := (x-a.X)*dx/cw, ((x-a.X+1)*dx+cw-1)/cw
	y0, y1 := (y-a.Y)*dy/ch, ((y-a.Y+1)*dy+ch-1)/ch
	best := [2]int{s.view[0] + x0, s.view[1] + y0}
	for gy := y0; gy < max(y1, y0+1); gy++ {
		for gx := x0; gx < max(x1, x0+1); gx++ {
			g := [2]int{s.view[0] + gx, s.view[1] + gy}
			if _, ok := s.marks[g]; ok || g == s.player {
				best = g
			}
		}
	}
	return best, s.inLevel(best[0], best[1])
}

func (s *levelMap) click(x, y int) {
	if g, ok := s.gridAt(x, y); ok {
		s.cur = g
		s.travel(g)
	}
}

func (s *levelMap) title() string {
	if s.depth == 0 {
		return "Map of the town"
	}
	return fmt.Sprintf("Map of level %d (%d ft)", s.depth, s.depth*50)
}

// footer returns the legend's entries, with the stairs' tiles or letters.
func (s *levelMap) footer(stairTiles bool) []string {
	stairs := "< > stairs"
	if stairTiles {
		stairs = stairLegend
	}
	return []string{"Arrows pan", "hjkl cursor", "Enter or click travel", stairs, "s shops", "@ you",
		"+ - zoom", "Esc close"}
}

// drawStairTiles draws the stairs' tiles over stairLegend's letters, at
// cell x, y.
func (s *levelMap) drawStairTiles(f *terminal.Frame, x, y int, env *drawEnv) {
	r, g, b := screenBg.Bg.RGB()
	bg := color.RGBA{r, g, b, 255}
	for i, t := range []*proto.TileSpot{s.stairs.Up, s.stairs.Down} {
		at := Box{x + i*(gearTileCells+1), y, gearTileCells, 1}
		for cx := at.X; cx < at.X+at.W; cx++ {
			f.Buffer.SetCell(uint16(cx), uint16(at.Y), cell.Cell{Content: cell.RuneImage})
		}
		f.RegisterImageRegion(terminal.ImageRegion{Area: rect(at), Img: env.picture(env.sprite(t.Row, t.Col, bg), at)})
	}
}

func (s *levelMap) draw(f *terminal.Frame, b Box, env *drawEnv) {
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" "+s.title()+" ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenFocus), rect(b))
	zoom := " " + s.zoomNow().name + " "
	f.TextFit(b.X+b.W-2-len(zoom), b.Y, len(zoom), zoom, screenDim)
	inner := Box{b.X + 2, b.Y + 1, b.W - 4, b.H - 2}

	// The keys, wrapped, at the bottom, and what the cursor is on above
	// them.
	stairTiles := env.sprite != nil && s.pictures() && s.stairs.Up != nil && s.stairs.Down != nil
	var lines []string
	line := ""
	for _, part := range s.footer(stairTiles) {
		switch {
		case line == "":
			line = part
		case len(line)+2+len(part) <= inner.W:
			line += "  " + part
		default:
			lines = append(lines, line)
			line = part
		}
	}
	lines = append(lines, line)
	for i, l := range lines {
		y := inner.Y + inner.H - len(lines) + i
		f.TextFit(inner.X, y, inner.W, l, screenDim)
		if at := strings.Index(l, stairLegend); stairTiles && at >= 0 {
			s.drawStairTiles(f, inner.X+at, y, env)
		}
	}
	inner.H -= len(lines) + 1
	info := s.markName(s.cur)
	where := fmt.Sprintf("%d, %d", s.cur[0], s.cur[1])
	f.TextFit(inner.X, inner.Y+inner.H, inner.W, info, screenText)
	f.TextFit(inner.X+inner.W-len(where), inner.Y+inner.H, len(where), where, screenDim)
	inner.H--
	if inner.W < 4 || inner.H < 2 {
		return
	}

	cw, ch, dx, dy := s.footprint()
	s.across, s.down = fit(inner.W, cw, dx, s.w), fit(inner.H, ch, dy, s.h)
	if s.across == 0 || s.down == 0 {
		return
	}
	if s.view == viewUnset {
		s.view = [2]int{s.center[0] - s.across/2, s.center[1] - s.down/2}
	}
	s.clampView()
	// A level smaller than the space sits in its middle.
	wc, hc := cellsFor(s.across, cw, dx), cellsFor(s.down, ch, dy)
	s.area = Box{inner.X + (inner.W-wc)/2, inner.Y + (inner.H-hc)/2, wc, hc}

	switch {
	case s.pictures():
		s.drawPictures(f)
	case s.zoomNow().half:
		s.drawOverview(f, env.pal)
	default:
		s.drawLetters(f, env.pal)
	}
	f.RenderWidget(mouseArea{handle: func(ev driver.MouseEvent) {
		switch ev.Button {
		case driver.MouseScrollUp, driver.MouseScrollDown:
			d := 1
			if ev.Button == driver.MouseScrollUp {
				d = -1
			}
			if ev.Ctrl {
				s.setZoom(s.zoom + d)
				return
			}
			s.wheel(d, ev.Shift)
		case driver.MouseScrollLeft:
			s.wheel(-1, true)
		case driver.MouseScrollRight:
			s.wheel(1, true)
		case driver.MouseRight:
			if g, ok := s.gridAt(int(ev.X), int(ev.Y)); ok {
				s.cur = g
			}
		case driver.MouseNone:
			if env.pointer != nil {
				env.pointer(int(ev.X), int(ev.Y))
			}
		}
	}}, rect(s.area))
	f.RegisterClickHandler(rect(s.area), func(ev driver.MouseEvent) { s.click(int(ev.X), int(ev.Y)) })
	f.RegisterAccessibility(accessibility.Node{ID: levelMapID, Role: accessibility.RoleDialog,
		Label: s.title(), Value: strings.TrimSpace(where + " " + info), Bounds: rect(b),
		Description: s.zoomNow().name})
}

// letter returns the letter and look of grid x, y, blank outside the
// level.
func (s *levelMap) letter(x, y int) (rune, proto.Attr) {
	if y < 0 || y >= len(s.chars) || x < 0 || x >= len(s.chars[y]) {
		return ' ', 0
	}
	var a proto.Attr
	if x < len(s.attrs[y]) {
		a = s.attrs[y][x]
	}
	return s.chars[y][x], a
}

// drawLetters draws a letter per grid, as the map does.
func (s *levelMap) drawLetters(f *terminal.Frame, pal palette) {
	for dy := 0; dy < s.down; dy++ {
		for dx := 0; dx < s.across; dx++ {
			x, y := s.view[0]+dx, s.view[1]+dy
			r, a := s.letter(x, y)
			st := pal.style(a)
			if [2]int{x, y} == s.cur {
				st = screenSelected
				if r == ' ' {
					r = '+'
				}
			}
			f.Buffer.SetCell(uint16(s.area.X+dx), uint16(s.area.Y+dy), cell.Cell{Content: r, Style: st})
		}
	}
}

// overviewColor is the color a grid takes in the overview: blank for an
// unknown grid, and much darker for a floor, so the walls outline rooms
// and corridors and what lies in them stands out.
func (s *levelMap) overviewColor(x, y int, pal palette) (cell.Color, bool) {
	r, a := s.letter(x, y)
	if r == ' ' {
		return pal[colorBlack], false
	}
	c := pal.color(a.Color())
	f := 1.0
	if r == '.' || r == '·' {
		f = 0.3
	}
	cr, cg, cb := c.RGB()
	return cell.NewColorRGB(uint8(float64(cr)*f), uint8(float64(cg)*f), uint8(float64(cb)*f)), true
}

// drawOverview draws two grids a cell, as half blocks. A cell that holds
// the player, stairs, or a shop shows its letter instead, and the cursor
// shows its grid's letter, highlighted.
func (s *levelMap) drawOverview(f *terminal.Frame, pal palette) {
	for cy := 0; cy < s.down/2; cy++ {
		for dx := 0; dx < s.across; dx++ {
			x, top := s.view[0]+dx, s.view[1]+2*cy
			at := [2]uint16{uint16(s.area.X + dx), uint16(s.area.Y + cy)}
			var mark *[2]int
			for _, g := range [][2]int{{x, top}, {x, top + 1}} {
				if g == s.cur {
					r, _ := s.letter(g[0], g[1])
					if r == ' ' {
						r = '+'
					}
					f.Buffer.SetCell(at[0], at[1], cell.Cell{Content: r, Style: screenSelected})
					mark = &g
					break
				}
			}
			if mark != nil {
				continue
			}
			for _, g := range [][2]int{{x, top}, {x, top + 1}} {
				if _, ok := s.marks[g]; ok || g == s.player {
					r, a := s.letter(g[0], g[1])
					f.Buffer.SetCell(at[0], at[1], cell.Cell{Content: r, Style: pal.style(a)})
					mark = &g
					break
				}
			}
			if mark != nil {
				continue
			}
			up, upOK := s.overviewColor(x, top, pal)
			down, downOK := s.overviewColor(x, top+1, pal)
			st := cell.NewStyle().WithFg(up).WithBg(down)
			r := '▀'
			if !upOK && !downOK {
				r = ' '
			}
			f.Buffer.SetCell(at[0], at[1], cell.Cell{Content: r, Style: st})
		}
	}
}

// gridPixels is the size of one grid in the picture at the current
// zoom.
func (s *levelMap) gridPixels() (int, int) {
	tw, th := s.m.sheet.TileSize()
	div := s.zoomNow().div
	return max(tw/div, 1), max(th/div, 1)
}

// tileAt returns grid x, y's picture at w by h pixels.
func (s *levelMap) tileAt(x, y, w, h int) *image.RGBA {
	c := s.tiles[y][x]
	k := shrunkKey{c, w, h}
	if p, ok := s.shrunk[k]; ok {
		return p
	}
	p := shrink(s.m.sheet.Picture(c), w, h)
	s.shrunk[k] = p
	return p
}

// shrink draws src at w by h pixels, each pixel the average of the ones
// it covers.
func shrink(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	if b.Dx() == w && b.Dy() == h {
		draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
		return out
	}
	for y := 0; y < h; y++ {
		sy0, sy1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+max((y+1)*b.Dy()/h, y*b.Dy()/h+1)
		for x := 0; x < w; x++ {
			sx0, sx1 := b.Min.X+x*b.Dx()/w, b.Min.X+max((x+1)*b.Dx()/w, x*b.Dx()/w+1)
			var r, g, bl, a, n uint32
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, bl, a, n = r+cr, g+cg, bl+cb, a+ca, n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)})
		}
	}
	return out
}

// markColor is the color a mark is drawn in.
func (s *levelMap) markColor(g [2]int) (color.RGBA, bool) {
	if g == s.player {
		return markPlayer, true
	}
	mk, ok := s.marks[g]
	if !ok {
		return color.RGBA{}, false
	}
	switch mk.Kind {
	case "up":
		return markUp, true
	case "down":
		return markDown, true
	}
	return markShop, true
}

// ring draws a border t pixels thick just inside r.
func ring(img *image.RGBA, r image.Rectangle, c color.RGBA, t int) {
	u := image.NewUniform(c)
	for _, side := range []image.Rectangle{
		{r.Min, image.Pt(r.Max.X, r.Min.Y+t)},
		{image.Pt(r.Min.X, r.Max.Y-t), r.Max},
		{r.Min, image.Pt(r.Min.X+t, r.Max.Y)},
		{image.Pt(r.Max.X-t, r.Min.Y), r.Max},
	} {
		draw.Draw(img, side.Intersect(img.Bounds()), u, image.Point{}, draw.Src)
	}
}

// gridRect is grid g's rectangle in the picture, which may lie outside
// it.
func (s *levelMap) gridRect(g [2]int) image.Rectangle {
	gw, gh := s.gridPixels()
	x, y := (g[0]-s.view[0])*gw, (g[1]-s.view[1])*gh
	return image.Rect(x, y, x+gw, y+gh)
}

// drawMark draws a mark on grid g: a frame where tiles are big enough
// to show inside one, or else a frame around the grid.
func drawMark(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	if r.Dx() >= 8 {
		ring(img, r, c, max(r.Dx()/10, 1))
		return
	}
	ring(img, r.Inset(-1), c, 1)
}

// picture draws the part of the level in view, with its marks.
func (s *levelMap) picture() *image.RGBA {
	gw, gh := s.gridPixels()
	key := fmt.Sprintf("%v %d %d %d %d %p", s.view, s.across, s.down, gw, gh, s.m.sheet)
	if s.pic != nil && s.picKey == key {
		return s.pic
	}
	img := image.NewRGBA(image.Rect(0, 0, s.across*gw, s.down*gh))
	draw.Draw(img, img.Bounds(), image.Black, image.Point{}, draw.Src)
	for dy := 0; dy < s.down; dy++ {
		y := s.view[1] + dy
		if y >= s.h || y >= len(s.tiles) {
			break
		}
		for dx := 0; dx < s.across; dx++ {
			x := s.view[0] + dx
			if x >= s.w {
				break
			}
			at := image.Rect(dx*gw, dy*gh, (dx+1)*gw, (dy+1)*gh)
			draw.Draw(img, at, s.tileAt(x, y, gw, gh), image.Point{}, draw.Src)
		}
	}
	for g := range s.marks {
		if c, ok := s.markColor(g); ok {
			drawMark(img, s.gridRect(g), c)
		}
	}
	drawMark(img, s.gridRect(s.player), markPlayer)
	s.pic, s.picKey = img, key
	s.bands, s.bandRows = nil, nil
	s.curBands, s.curKey = nil, ""
	return img
}

// drawPictures draws the tiles in view as bands of the picture, one per
// row of cells, with the cursor drawn into the bands it crosses.
func (s *levelMap) drawPictures(f *terminal.Frame) {
	img := s.picture()
	rows := s.area.H
	if s.bands == nil {
		h := img.Bounds().Dy()
		for r := 0; r < rows; r++ {
			y0, y1 := r*h/rows, (r+1)*h/rows
			band := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), max(y1-y0, 1)))
			draw.Draw(band, band.Bounds(), img, image.Pt(0, y0), draw.Src)
			s.bands = append(s.bands, band)
			s.bandRows = append(s.bandRows, [2]int{y0, y1})
		}
	}
	cur := s.gridRect(s.cur)
	if cur.Dx() < 8 {
		cur = cur.Inset(-2)
	} else {
		cur = cur.Inset(-1)
	}
	if key := fmt.Sprint(s.picKey, cur); key != s.curKey {
		s.curKey, s.curBands = key, map[int]image.Image{}
		for r, yr := range s.bandRows {
			if cur.Max.Y <= yr[0] || cur.Min.Y >= yr[1] || !cur.Overlaps(img.Bounds()) {
				continue
			}
			band := image.NewRGBA(s.bands[r].Bounds())
			draw.Draw(band, band.Bounds(), s.bands[r], image.Point{}, draw.Src)
			// Each band gets the part of the frame that falls in it.
			ring(band, cur.Sub(image.Pt(0, yr[0])), markCursor, max(cur.Dx()/12, 1))
			s.curBands[r] = band
		}
	}
	for y := s.area.Y; y < s.area.Y+s.area.H; y++ {
		for x := s.area.X; x < s.area.X+s.area.W; x++ {
			f.Buffer.SetCell(uint16(x), uint16(y), cell.Cell{Content: cell.RuneImage})
		}
	}
	for r := range s.bands {
		band := s.bands[r]
		if c, ok := s.curBands[r]; ok {
			band = c
		}
		f.RegisterImageRegion(terminal.ImageRegion{Area: rect(Box{s.area.X, s.area.Y + r, s.area.W, 1}), Img: s.m.sharp(band, s.area.W, 1)})
	}
}

// mouseArea gives every mouse event over its area that Limoni gives to
// a widget, such as the wheel, the right button, and the pointer's moves,
// to handle. Left clicks go to click handlers registered after it.
type mouseArea struct{ handle func(ev driver.MouseEvent) }

func (w mouseArea) Draw(ctx cell.Context, _ *buffer.Buffer) {
	if ctx.RegisterMouse != nil {
		ctx.RegisterMouse(ctx.Area, w.handle)
	}
}

func (w mouseArea) SizeHint(max cell.Rect) (uint16, uint16) { return max.Width, max.Height }

package ui

import (
	"image"
	"image/draw"
	"time"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// Scrolling the map: when the character nears the edge of the map, the
// game moves the map, by half its size unless "Center map continuously"
// is on. The engine says so in a SCROLL event before it redraws, so the
// client keeps the screen it has, and once the new one arrives glides
// from one to the other. Both views move together: a grid on both lines
// up, the new view brings in what scrolls into sight, and the old one
// what scrolls out.
//
// With tiles there are three ways to draw it, which the Display options
// choose between:
//
//   - pixel: each tile moves to the pixel, and tiles at the map's edge are
//     cut exactly.
//   - cell: each tile moves to the pixel, but tiles at the edge are cut
//     into cells, so up to a cell can spill past the edge.
//   - map: the whole map is drawn into one picture, sent once, and moved
//     and cut as one. Few bytes a frame, but a large picture to send
//     first.
//
// Tiles scroll only with Kitty's protocol, which moves pictures to the
// pixel. Text scrolls a whole cell at a time, with any method but off.
// A key, a click, or a native screen ends the scroll at once.

// The ways to scroll the map, as the settings name them.
const (
	scrollOff   = "off"
	scrollPixel = "pixel"
	scrollCell  = "cell"
	scrollMap   = "map"
)

// scrollTime is how long the map takes to scroll.
const scrollTime = 200 * time.Millisecond

// mapScroll is the map scrolling from the screen before.
type mapScroll struct {
	old *grid.Grid
	// shift is how far the map's contents move, in cells: a grid at x, y
	// on the old screen is at x+shift[0], y+shift[1] on the new one.
	shift [2]int
	// box is the cells of the main term that show the map.
	box Box
	// started is set at the first frame, at, when the scroll's time
	// starts.
	started bool
	at      time.Time
	// whole is the whole-map picture, made once, and union the cells of
	// the main term it covers, in the new screen's places.
	whole image.Image
	union Box
}

// scrollMethod returns how the player chose to scroll the map; pixel
// when they have not chosen.
func (m *Model) scrollMethod() string {
	switch m.settings.Scroll {
	case scrollOff, scrollCell, scrollMap:
		return m.settings.Scroll
	}
	return scrollPixel
}

// prepareScroll keeps the screen before the map moves, from a SCROLL
// event, to scroll from once the game has redrawn it. It returns whether
// to redraw.
func (m *Model) prepareScroll(d proto.EventData) bool {
	had := m.scroll != nil
	m.scroll, m.pendingScroll = nil, nil
	g := m.terms.Terms[0]
	if d.Busy || d.Map == nil || g == nil || m.scrollMethod() == scrollOff || len(m.stack) > 0 {
		return had
	}
	if m.sheet != nil && m.terms.TileSet != nil && m.cfg.Protocol != "Kitty" {
		// Pictures that move only a cell at a time would cost a whole
		// map of pictures a frame.
		return had
	}
	box := Box{d.Map.X, d.Map.Y, d.Map.W, d.Map.H}
	if box.Empty() || abs(d.Shift[0]) >= box.W || abs(d.Shift[1]) >= box.H {
		return had
	}
	m.pendingScroll = &mapScroll{old: g.Snapshot(), shift: d.Shift, box: box}
	return had
}

// beginScroll starts the scroll waiting for the redrawn map. A screen of
// another size, as after a resize, cannot scroll.
func (m *Model) beginScroll() {
	s := m.pendingScroll
	m.pendingScroll = nil
	g := m.terms.Terms[0]
	if g == nil || g.Width != s.old.Width || g.Height != s.old.Height {
		return
	}
	m.scroll = s
	m.startMotion = !m.motionTicking
}

// scrollOffset returns how far, in cells, the new screen is drawn from
// its place while the map scrolls, and the screen cells the map is cut
// to; all zero when it is not scrolling.
func (m *Model) scrollOffset() (ox, oy float64, clip cell.Rect) {
	s := m.scroll
	if s == nil {
		return 0, 0, cell.Rect{}
	}
	t := 0.0
	if s.started {
		t = float64(m.now().Sub(s.at)) / float64(scrollTime)
	}
	left := 1 - ease(t)
	main := m.layout.Main
	return -float64(s.shift[0]) * left, -float64(s.shift[1]) * left,
		rect(Box{main.X + s.box.X, main.Y + s.box.Y, s.box.W, s.box.H})
}

// textScroll is how termView draws the map while it scrolls: a whole
// cell at a time, from the new screen and the old one.
type textScroll struct {
	old   *grid.Grid
	box   Box
	shift [2]int
	off   [2]int
}

// textScrollNow returns the scroll as termView draws it, or nil.
func (m *Model) textScrollNow() *textScroll {
	s := m.scroll
	if s == nil {
		return nil
	}
	ox, oy, _ := m.scrollOffset()
	return &textScroll{old: s.old, box: s.box, shift: s.shift, off: [2]int{roundInt(ox), roundInt(oy)}}
}

// cell returns what shows at x, y of the map while it scrolls, and false
// outside the map.
func (ts *textScroll) cell(g *grid.Grid, x, y int) (grid.Cell, bool) {
	inBox := func(x, y int) bool {
		return x >= ts.box.X && y >= ts.box.Y && x < ts.box.X+ts.box.W && y < ts.box.Y+ts.box.H
	}
	if !inBox(x, y) {
		return grid.Cell{}, false
	}
	nx, ny := x-ts.off[0], y-ts.off[1]
	if inBox(nx, ny) {
		return g.Cell(nx, ny), true
	}
	if px, py := nx-ts.shift[0], ny-ts.shift[1]; inBox(px, py) {
		return ts.old.Cell(px, py), true
	}
	return grid.Cell{Rune: ' '}, true
}

// scrollPictures returns the map's pictures while it scrolls, in the
// chosen way, with the cells where sliding creatures end showing only
// their terrain.
func (m *Model) scrollPictures(g *grid.Grid, cw, ch int, ends map[[2]int]bool) []terminal.ImageRegion {
	s := m.scroll
	ox, oy, clip := m.scrollOffset()
	main := m.layout.Main
	if m.scrollMethod() == scrollMap {
		img := m.wholeMap(g, cw, ch, ends)
		u := s.union
		return []terminal.ImageRegion{{
			Area:        rect(Box{main.X + s.box.X, main.Y + s.box.Y, u.W, u.H}),
			Img:         img,
			Transparent: true,
			ShiftX:      float64(u.X-s.box.X) + ox,
			ShiftY:      float64(u.Y-s.box.Y) + oy,
			Clip:        clip,
		}}
	}

	byCell := m.scrollMethod() == scrollCell
	inBox := func(x, y float64, w, h int) bool {
		return x >= float64(s.box.X) && y >= float64(s.box.Y) &&
			x+float64(w) <= float64(s.box.X+s.box.W) && y+float64(h) <= float64(s.box.Y+s.box.H)
	}
	var regions []terminal.ImageRegion
	// add places the tile at x, y of its screen, shifted by sx, sy cells.
	add := func(x, y int, img image.Image, sx, sy float64) {
		at := Box{main.X + x, main.Y + y, cw, ch}
		if !byCell || inBox(float64(x)+sx, float64(y)+sy, cw, ch) {
			regions = append(regions, terminal.ImageRegion{Area: rect(at), Img: img, ShiftX: sx, ShiftY: sy, Clip: clip})
			return
		}
		// At the edge: the tile's cells, each kept while most of it is
		// on the map.
		for dy := 0; dy < ch; dy++ {
			for dx := 0; dx < cw; dx++ {
				cx, cy := float64(x+dx)+sx, float64(y+dy)+sy
				if !inBox(float64(roundInt(cx)), float64(roundInt(cy)), 1, 1) {
					continue
				}
				regions = append(regions, terminal.ImageRegion{
					Area:   rect(Box{at.X + dx, at.Y + dy, 1, 1}),
					Img:    m.piece(img, dx, dy, cw, ch),
					ShiftX: sx, ShiftY: sy,
				})
			}
		}
	}
	// What scrolls out comes from the old screen: its tiles that do not
	// land wholly on the map.
	sx, sy := float64(s.shift[0])+ox, float64(s.shift[1])+oy
	s.old.Tiles(func(x, y int, c proto.PictCell) {
		if inBox(float64(x+s.shift[0]), float64(y+s.shift[1]), cw, ch) {
			return
		}
		add(x, y, m.tilePicture(s.old, x, y, c), sx, sy)
	})
	g.Tiles(func(x, y int, c proto.PictCell) {
		if ends[[2]int{x, y}] {
			c.FgRow, c.FgCol = c.BgRow, c.BgCol
		}
		add(x, y, m.tilePicture(g, x, y, c), ox, oy)
	})
	return regions
}

// wholeMap returns the whole-map picture: the old screen's map where the
// new one does not reach, under the new screen's map, with nothing where
// neither shows a tile. It is made once a scroll, but the cells where
// sliding creatures end are drawn as terrain only, as on the map.
func (m *Model) wholeMap(g *grid.Grid, cw, ch int, ends map[[2]int]bool) image.Image {
	s := m.scroll
	if s.whole != nil {
		return s.whole
	}
	moved := Box{s.box.X + s.shift[0], s.box.Y + s.shift[1], s.box.W, s.box.H}
	u := Box{min(s.box.X, moved.X), min(s.box.Y, moved.Y), 0, 0}
	u.W = max(s.box.X+s.box.W, moved.X+moved.W) - u.X
	u.H = max(s.box.Y+s.box.H, moved.Y+moved.H) - u.Y
	s.union = u
	tw, th := m.sheet.TileSize()
	pw, ph := max(tw/cw, 1), max(th/ch, 1)
	out := image.NewRGBA(image.Rect(0, 0, u.W*pw, u.H*ph))
	put := func(x, y int, img image.Image) {
		at := image.Pt((x-u.X)*pw, (y-u.Y)*ph)
		draw.Draw(out, image.Rectangle{Min: at, Max: at.Add(img.Bounds().Size())}, img, img.Bounds().Min, draw.Src)
	}
	s.old.Tiles(func(x, y int, c proto.PictCell) {
		put(x+s.shift[0], y+s.shift[1], m.tilePicture(s.old, x, y, c))
	})
	// The new screen's map wins: clear the old under it first, so a grid
	// with no tile now shows nothing.
	draw.Draw(out, image.Rect((s.box.X-u.X)*pw, (s.box.Y-u.Y)*ph, (s.box.X-u.X+s.box.W)*pw, (s.box.Y-u.Y+s.box.H)*ph),
		image.Transparent, image.Point{}, draw.Src)
	g.Tiles(func(x, y int, c proto.PictCell) {
		if ends[[2]int{x, y}] {
			c.FgRow, c.FgCol = c.BgRow, c.BgCol
		}
		put(x, y, m.tilePicture(g, x, y, c))
	})
	s.whole = out
	return out
}

func abs(n int) int { return max(n, -n) }

func roundInt(f float64) int {
	if f < 0 {
		return -int(-f + 0.5)
	}
	return int(f + 0.5)
}

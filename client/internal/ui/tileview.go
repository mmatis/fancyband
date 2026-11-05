package ui

import (
	"image"
	"image/color"
	"image/draw"
	"slices"
	"time"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/graphics"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

// placeTiles works out a picture for each tile on the main term, which
// Limoni sends to the terminal once and places by reference afterwards.
// A tile partly under a covered box, such as a native screen, or partly
// off the term, shows only its other cells, each as a piece of its
// picture, so the map reaches right up to the screen's edge. It returns
// the pictures to register and the cells left with no picture, by term
// position, which termView draws blank: a cell left for a picture that
// never comes would show the terminal's own background.
func (m *Model) placeTiles(g *grid.Grid, covered []Box) ([]terminal.ImageRegion, map[[2]int]bool) {
	ts := m.terms.TileSet
	if m.sheet == nil || ts == nil {
		return nil, nil
	}
	var regions []terminal.ImageRegion
	dropped := map[[2]int]bool{}
	main := m.layout.Main
	termW, termH := min(main.W, g.Width), min(main.H, g.Height)
	cw, ch := max(int(ts.CellsW), 1), max(int(ts.CellsH), 1)
	// A native screen ends any slide; otherwise the cells where sliding
	// creatures end show only their terrain, under the slides.
	if len(covered) > 0 {
		m.endMotion()
	}
	ends := m.slideEnds()
	if m.scroll != nil {
		return append(m.scrollPictures(g, cw, ch, ends), m.slidePictures(cw, ch)...), nil
	}
	hidden := func(x, y int) bool {
		if x >= termW || y >= termH {
			return true
		}
		for _, c := range covered {
			if (Box{main.X + x, main.Y + y, 1, 1}).Overlaps(c) {
				return true
			}
		}
		return false
	}
	g.Tiles(func(x, y int, c proto.PictCell) {
		if ends[[2]int{x, y}] {
			c.FgRow, c.FgCol = c.BgRow, c.BgCol
		}
		at := Box{main.X + x, main.Y + y, cw, ch}
		whole := x+cw <= termW && y+ch <= termH
		for _, c := range covered {
			whole = whole && !at.Overlaps(c)
		}
		img := m.tilePicture(g, x, y, c)
		if mk := m.marker; mk != nil && mk.X == main.X+x && mk.Y == main.Y+y {
			img = m.framed(img)
		}
		if whole {
			regions = append(regions, terminal.ImageRegion{Area: rect(at), Img: img})
			return
		}
		for dy := 0; dy < ch; dy++ {
			for dx := 0; dx < cw; dx++ {
				if hidden(x+dx, y+dy) {
					dropped[[2]int{x + dx, y + dy}] = true
					continue
				}
				regions = append(regions, terminal.ImageRegion{
					Area: rect(Box{at.X + dx, at.Y + dy, 1, 1}),
					Img:  m.piece(img, dx, dy, cw, ch),
				})
			}
		}
	})
	return append(regions, m.slidePictures(cw, ch)...), dropped
}

// sharpKey names a picture at the size it is enlarged to.
type sharpKey struct {
	img  image.Image
	w, h int
}

// maxSharpBytes bounds the cache of enlarged pictures, which animated tiles
// and scrolling would grow without end.
const maxSharpBytes = 128 << 20

// sharp returns img as Limoni should get it for an area of cols by rows
// cells: enlarged to those cells' size in pixels, each new pixel a copy of
// the nearest old one. Limoni leaves a picture that fits alone. Given a
// smaller one, it blends pixels with a bilinear filter (Sixel and iTerm2) or
// leaves the terminal to enlarge it, often with a linear filter too (Kitty's
// protocol), and either way the tiles look blurred. The same picture at the
// same size is always the same value, so Limoni sends it only once.
func (m *Model) sharp(img image.Image, cols, rows int) image.Image {
	cw, ch := m.cellPx[0], m.cellPx[1]
	if cw == 0 || ch == 0 || img == nil || cols <= 0 || rows <= 0 {
		return img
	}
	b := img.Bounds()
	w, h := graphics.ContainSize(img, cols*cw, rows*ch)
	if w < b.Dx() || h < b.Dy() || (w == b.Dx() && h == b.Dy()) {
		return img
	}
	k := sharpKey{img, w, h}
	if out, ok := m.sharpened[k]; ok {
		return out
	}
	if m.sharpened == nil || m.sharpBytes > maxSharpBytes {
		m.sharpened, m.sharpBytes = map[sharpKey]image.Image{}, 0
	}
	out := tiles.Enlarge(img, w, h)
	m.sharpened[k] = out
	m.sharpBytes += len(out.Pix)
	return out
}

// placePaneTiles draws the tiles in a pane's monster or item list, each on
// two cells of its one-cell-high row: its own and the blank the engine leaves
// after it. A pane has no map, so the map's tile size does not apply.
func (m *Model) placePaneTiles(f *terminal.Frame, box Box, g *grid.Grid, sprite func(row, col uint8, bg color.RGBA) image.Image) {
	if sprite == nil {
		return
	}
	g.Tiles(func(x, y int, c proto.PictCell) {
		if x+paneTileCells > box.W || y >= box.H {
			return
		}
		at := Box{box.X + x, box.Y + y, paneTileCells, 1}
		for cx := at.X; cx < at.X+at.W; cx++ {
			f.Buffer.SetCell(uint16(cx), uint16(at.Y), cell.Cell{Content: cell.RuneImage})
		}
		img := m.sharp(sprite(c.FgRow, c.FgCol, color.RGBA{}), at.W, at.H)
		m.place(f, terminal.ImageRegion{Area: rect(at), Img: img, Transparent: true})
	})
}

// paneTileCells is how many cells wide a pane's tile is.
const paneTileCells = 2

// place puts a picture on the screen. While View draws, it holds the picture
// back until every screen has drawn, because a screen drawn later may cover
// it: a Kitty picture is not in the cells, so a window's cells do not hide it.
// The dungeon map leaves out what is covered by itself, in placeTiles.
func (m *Model) place(f *terminal.Frame, r terminal.ImageRegion) {
	if !m.holding {
		f.RegisterImageRegion(r)
		return
	}
	m.held = append(m.held, r)
}

// placeHeld places the pictures place held back, each only on the cells
// still marked as a picture.
func (m *Model) placeHeld(f *terminal.Frame) {
	m.holding = false
	for _, r := range m.held {
		m.placeUncovered(f, r)
	}
	m.held = m.held[:0]
}

// placeUncovered places r, or if anything was drawn over part of it, the
// parts left showing: each a rectangle of cells, joining the rows that have
// the same cells uncovered. A picture wholly covered is not placed.
func (m *Model) placeUncovered(f *terminal.Frame, r terminal.ImageRegion) {
	a := Box{int(r.Area.X), int(r.Area.Y), int(r.Area.Width), int(r.Area.Height)}
	type run struct{ x0, x1 int }
	type part struct {
		run
		y0 int
	}
	var open []part
	end := func(p part, y int) {
		f.RegisterImageRegion(m.slicePicture(r, a, Box{p.x0, p.y0, p.x1 - p.x0, y - p.y0}))
	}
	var runs []run
	whole := true
	buf := f.Buffer.Area
	for y := a.Y; y < a.Y+a.H; y++ {
		// The runs of cells uncovered on this row.
		runs = runs[:0]
		for x := a.X; x < a.X+a.W; x++ {
			if x >= int(buf.Width) || y >= int(buf.Height) || f.Buffer.CellAt(uint16(x), uint16(y)).Content != cell.RuneImage {
				continue
			}
			if n := len(runs); n > 0 && runs[n-1].x1 == x {
				runs[n-1].x1 = x + 1
			} else {
				runs = append(runs, run{x, x + 1})
			}
		}
		whole = whole && len(runs) == 1 && runs[0] == run{a.X, a.X + a.W}
		// A run the row above has goes on; the row above's other parts end.
		next := make([]part, 0, len(runs))
		for _, rn := range runs {
			p := part{rn, y}
			for _, o := range open {
				if o.run == rn {
					p = o
				}
			}
			next = append(next, p)
		}
		for _, o := range open {
			if !slices.Contains(next, o) {
				end(o, y)
			}
		}
		open = next
	}
	if whole {
		f.RegisterImageRegion(r)
		return
	}
	for _, o := range open {
		end(o, a.Y+a.H)
	}
}

// sliceKey names a part of a picture on the cells of a.
type sliceKey struct {
	img     image.Image
	a, part Box
}

// slicePicture returns r cut down to the cells of part, inside a, the cells
// r covers. The same part is always the same picture value, so Limoni sends
// it to the terminal only once.
func (m *Model) slicePicture(r terminal.ImageRegion, a, part Box) terminal.ImageRegion {
	r.Area = rect(part)
	k := sliceKey{r.Img, a, part}
	if p, ok := m.slices[k]; ok {
		r.Img = p
		return r
	}
	b := r.Img.Bounds()
	x0, x1 := part.X-a.X, part.X-a.X+part.W
	y0, y1 := part.Y-a.Y, part.Y-a.Y+part.H
	src := image.Rect(b.Min.X+x0*b.Dx()/a.W, b.Min.Y+y0*b.Dy()/a.H,
		b.Min.X+x1*b.Dx()/a.W, b.Min.Y+y1*b.Dy()/a.H)
	out := image.NewRGBA(image.Rect(0, 0, src.Dx(), src.Dy()))
	draw.Draw(out, out.Bounds(), r.Img, src.Min, draw.Src)
	if m.slices == nil || m.sliceBytes > maxSharpBytes {
		m.slices, m.sliceBytes = map[sliceKey]image.Image{}, 0
	}
	m.slices[k] = out
	m.sliceBytes += len(out.Pix)
	r.Img = out
	return r
}

// pieceKey names one cell's piece of a tile's picture.
type pieceKey struct {
	img            image.Image
	dx, dy, cw, ch int
}

// piece returns the part of a tile's picture that falls on cell dx, dy
// of the cw by ch cells the tile covers. The same piece is always the
// same picture value, so Limoni sends it to the terminal only once.
func (m *Model) piece(img image.Image, dx, dy, cw, ch int) image.Image {
	k := pieceKey{img, dx, dy, cw, ch}
	if p, ok := m.pieces[k]; ok {
		return p
	}
	b := img.Bounds()
	src := image.Rect(b.Min.X+dx*b.Dx()/cw, b.Min.Y+dy*b.Dy()/ch,
		b.Min.X+(dx+1)*b.Dx()/cw, b.Min.Y+(dy+1)*b.Dy()/ch)
	out := image.NewRGBA(image.Rect(0, 0, src.Dx(), src.Dy()))
	draw.Draw(out, out.Bounds(), img, src.Min, draw.Src)
	if m.pieces == nil || len(m.pieces) > maxPieces {
		m.pieces = map[pieceKey]image.Image{}
	}
	m.pieces[k] = out
	return out
}

// maxPieces bounds the cache of pieces, which animated tiles would grow
// without end.
const maxPieces = 4096

// tilePicture returns the picture for the tile at x, y on g: through
// the filters of its tiles, at the current step of their animations when
// animation is on, and a floor with the shadows of the walls beside it,
// when shadows are on.
func (m *Model) tilePicture(g *grid.Grid, x, y int, c proto.PictCell) image.Image {
	c.Flip = c.Flip && m.flipping()
	var mask uint8
	if m.shadows() && m.sheet.IsFloor(c) {
		mask = m.shadowMask(g, x, y)
	}
	var t time.Duration
	if m.cfg.Animate {
		t = time.Duration(m.tick) * tickTime
	}
	ts := m.terms.TileSet
	cw, ch := max(int(ts.CellsW), 1), max(int(ts.CellsH), 1)
	return m.sheet.At(c, t, x/cw, y/ch, mask, m.lightEdges(g, x, y, c))
}

// lightEdgeKeys names the light edges' styles in the settings, in the order
// of the Display page's choices. lightEdgeStyles gives each one's style.
var (
	lightEdgeKeys   = []string{"", "corners", "cuts", "dither"}
	lightEdgeStyles = []tiles.LightStyle{tiles.LightOff, tiles.LightCorners, tiles.LightCuts, tiles.LightDither}
)

// lightEdges returns how to draw the light on the floor tile at x, y on g.
func (m *Model) lightEdges(g *grid.Grid, x, y int, c proto.PictCell) tiles.LightEdges {
	i := slices.Index(lightEdgeKeys, m.settings.LightEdges)
	if i <= 0 {
		return tiles.LightEdges{}
	}
	own, ok := m.sheet.LightLevel(c)
	if !ok {
		return tiles.LightEdges{}
	}
	ts := m.terms.TileSet
	cw, ch := max(int(ts.CellsW), 1), max(int(ts.CellsH), 1)
	return m.sheet.LightAround(lightEdgeStyles[i], own, func(dx, dy int) (proto.PictCell, bool) {
		nx, ny := x+dx*cw, y+dy*ch
		if nx < 0 || ny < 0 || nx >= g.Width || ny >= g.Height {
			return proto.PictCell{}, false
		}
		n := g.Cell(nx, ny)
		return n.Pict, n.Tile
	})
}

// flipping reports whether creatures' tiles turn the way they last stepped.
// They do unless the settings turn it off. The engine keeps track either way,
// so turning it on shows each creature's facing at once.
func (m *Model) flipping() bool { return m.settings.Flip == nil || *m.settings.Flip }

// shadows reports whether wall shadows are on. They are unless the
// settings turn them off.
func (m *Model) shadows() bool { return m.settings.Shadows == nil || *m.settings.Shadows }

// shadowMask returns which grids around the tile at x, y on g are walls.
// Tiles sit on a grid of their own size, so a neighbor is one tile's
// cells away. A grid off the term counts as no wall.
func (m *Model) shadowMask(g *grid.Grid, x, y int) uint8 {
	ts := m.terms.TileSet
	cw, ch := max(int(ts.CellsW), 1), max(int(ts.CellsH), 1)
	return tiles.ShadowMask(func(dx, dy int) bool {
		nx, ny := x+dx*cw, y+dy*ch
		if nx < 0 || ny < 0 || nx >= g.Width || ny >= g.Height {
			return false
		}
		n := g.Cell(nx, ny)
		return n.Tile && m.sheet.IsWall(n.Pict)
	})
}

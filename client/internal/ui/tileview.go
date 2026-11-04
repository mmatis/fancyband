package ui

import (
	"image"
	"image/draw"
	"time"

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
	return m.sheet.At(c, t, x/cw, y/ch, mask)
}

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

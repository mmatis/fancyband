package ui

import (
	"image"
	"image/draw"

	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
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
		at := Box{main.X + x, main.Y + y, cw, ch}
		whole := x+cw <= termW && y+ch <= termH
		for _, c := range covered {
			whole = whole && !at.Overlaps(c)
		}
		if whole {
			regions = append(regions, terminal.ImageRegion{Area: rect(at), Img: m.tilePicture(x, y, c)})
			return
		}
		img := m.tilePicture(x, y, c)
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
	return regions, dropped
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

// tilePicture returns the picture for a tile, with lava shifted for the
// current step of the wave when animation is on.
func (m *Model) tilePicture(x, y int, c proto.PictCell) image.Image {
	if !m.cfg.Animate {
		return m.sheet.Picture(c)
	}
	return m.sheet.Shifted(c, lavaStep(m.tick, x, y))
}

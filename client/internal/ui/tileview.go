package ui

import (
	"image"

	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// placeTiles asks Limoni to draw a picture over each tile on the main
// term. termView has left those cells blank. Limoni sends each distinct
// picture to the terminal once and places it by reference afterwards.
// Tiles that overlap a covered box, such as a native screen, are left
// out, since a picture would show over the screen.
func (m *Model) placeTiles(f *terminal.Frame, g *grid.Grid, covered []Box) {
	ts := m.terms.TileSet
	if m.sheet == nil || ts == nil {
		return
	}
	main := m.layout.Main
	cw, ch := max(int(ts.CellsW), 1), max(int(ts.CellsH), 1)
	g.Tiles(func(x, y int, c proto.PictCell) {
		// Leave out tiles that would run off the main term's area.
		if x+cw > min(main.W, g.Width) || y+ch > min(main.H, g.Height) {
			return
		}
		at := Box{main.X + x, main.Y + y, cw, ch}
		for _, c := range covered {
			if at.Overlaps(c) {
				return
			}
		}
		f.RegisterImageRegion(terminal.ImageRegion{
			Area: rect(at),
			Img:  m.tilePicture(x, y, c),
		})
	})
}

// tilePicture returns the picture for a tile, with lava shifted for the
// current step of the wave when animation is on.
func (m *Model) tilePicture(x, y int, c proto.PictCell) image.Image {
	if !m.cfg.Animate {
		return m.sheet.Picture(c)
	}
	return m.sheet.Shifted(c, lavaStep(m.tick, x, y))
}

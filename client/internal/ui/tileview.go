package ui

import (
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// placeTiles asks Limoni to draw a picture over each tile on the main
// term. termView has left those cells blank. Limoni sends each distinct
// picture to the terminal once and places it by reference afterwards.
func (m *Model) placeTiles(f *terminal.Frame, g *grid.Grid) {
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
		f.RegisterImageRegion(terminal.ImageRegion{
			Area: rect(Box{main.X + x, main.Y + y, cw, ch}),
			Img:  m.sheet.Picture(c),
		})
	})
}

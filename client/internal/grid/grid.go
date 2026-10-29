// Package grid keeps a copy of each term the engine streams. It applies
// TEXT, WIPE, CLEAR and CURSOR messages to a pending grid and makes that
// grid visible on FRESH, the way the engine's own terminal layer does.
package grid

import (
	"strings"

	"fancyband/internal/proto"
)

// MaxTerms is the number of terms the engine can open.
const MaxTerms = 8

// Cell is one character cell. A cell shows text, or a tile when Tile is
// set. Covered marks the other cells of a big tile, which show nothing of
// their own.
type Cell struct {
	Rune    rune
	Attr    proto.Attr
	Tile    bool
	Pict    proto.PictCell
	Covered bool
}

var blank = Cell{Rune: ' '}

// Cursor is where a term's cursor sits.
type Cursor struct {
	X, Y    int
	Visible bool
	Big     bool
}

// Grid is one term.
type Grid struct {
	Width, Height int
	Name          string
	Cursor        Cursor

	pending []Cell
	shown   []Cell
	cursor  Cursor
}

// New returns a blank grid.
func New(width, height int) *Grid {
	g := &Grid{}
	g.Resize(width, height)
	return g
}

// Resize changes the size and blanks the grid. The engine redraws a term
// after resizing it.
func (g *Grid) Resize(width, height int) {
	g.Width, g.Height = width, height
	g.pending = make([]Cell, width*height)
	g.shown = make([]Cell, width*height)
	fill(g.pending, blank)
	fill(g.shown, blank)
}

// Snapshot returns a copy of the visible grid, which later changes to g
// leave alone.
func (g *Grid) Snapshot() *Grid {
	c := &Grid{Width: g.Width, Height: g.Height, Name: g.Name, Cursor: g.Cursor}
	c.shown = append([]Cell(nil), g.shown...)
	c.pending = append([]Cell(nil), g.shown...)
	return c
}

// Cell returns the visible cell at x, y, or a blank outside the grid.
func (g *Grid) Cell(x, y int) Cell {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return blank
	}
	return g.shown[y*g.Width+x]
}

// Line returns the visible row y as text.
func (g *Grid) Line(y int) string {
	if y < 0 || y >= g.Height {
		return ""
	}
	var b strings.Builder
	for _, c := range g.shown[y*g.Width : (y+1)*g.Width] {
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// String returns the visible grid as text, one line per row, with
// trailing spaces trimmed.
func (g *Grid) String() string {
	var b strings.Builder
	for y := 0; y < g.Height; y++ {
		b.WriteString(strings.TrimRight(g.Line(y), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// Contains reports whether any visible row contains s.
func (g *Grid) Contains(s string) bool {
	for y := 0; y < g.Height; y++ {
		if strings.Contains(g.Line(y), s) {
			return true
		}
	}
	return false
}

func (g *Grid) put(x, y int, c Cell) {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return
	}
	g.pending[y*g.Width+x] = c
}

func (g *Grid) text(m proto.Text) {
	x := int(m.X)
	for _, r := range m.Text {
		g.put(x, int(m.Y), Cell{Rune: r, Attr: m.Attr})
		x++
	}
}

// pict places tiles, one per cell from X, as the engine's pict_hook does.
// Each tile also covers the cells to its right and below, as the tile set
// says; with big tiles the engine sends each tile in a run of its own.
func (g *Grid) pict(m proto.Pict, cellsW, cellsH int) {
	for i, c := range m.Cells {
		x, y := int(m.X)+i, int(m.Y)
		for dy := 0; dy < cellsH; dy++ {
			for dx := 0; dx < cellsW; dx++ {
				g.put(x+dx, y+dy, Cell{Rune: ' ', Covered: true})
			}
		}
		g.put(x, y, Cell{Rune: ' ', Tile: true, Pict: c})
	}
}

// Tiles calls fn for each visible tile, with its top-left cell.
func (g *Grid) Tiles(fn func(x, y int, c proto.PictCell)) {
	for i, c := range g.shown {
		if c.Tile {
			fn(i%g.Width, i/g.Width, c.Pict)
		}
	}
}

func (g *Grid) wipe(m proto.Wipe) {
	for i := 0; i < int(m.N); i++ {
		g.put(int(m.X)+i, int(m.Y), blank)
	}
}

func (g *Grid) fresh() {
	copy(g.shown, g.pending)
	g.Cursor = g.cursor
}

// Set holds every open term.
type Set struct {
	Terms   [MaxTerms]*Grid
	Palette []proto.RGB
	// TileSet is the engine's tile set, or nil when the map is text.
	TileSet *proto.TileSet
}

// Apply updates the set from one message. It returns the term index that
// a FRESH message completed, or -1.
func (s *Set) Apply(m proto.Message) int {
	switch m := m.(type) {
	case proto.TermOpen:
		if int(m.Term) >= MaxTerms {
			return -1
		}
		g := s.Terms[m.Term]
		if g == nil {
			g = New(int(m.Width), int(m.Height))
			s.Terms[m.Term] = g
		} else if g.Width != int(m.Width) || g.Height != int(m.Height) {
			g.Resize(int(m.Width), int(m.Height))
		}
		g.Name = m.Name
	case proto.Palette:
		s.Palette = m.Colors
	case proto.Text:
		if g := s.term(m.Term); g != nil {
			g.text(m)
		}
	case proto.TileSet:
		ts := m
		s.TileSet = &ts
	case proto.Pict:
		if g := s.term(m.Term); g != nil {
			cw, ch := 1, 1
			if s.TileSet != nil {
				cw, ch = max(int(s.TileSet.CellsW), 1), max(int(s.TileSet.CellsH), 1)
			}
			g.pict(m, cw, ch)
		}
	case proto.Wipe:
		if g := s.term(m.Term); g != nil {
			g.wipe(m)
		}
	case proto.Clear:
		if g := s.term(m.Term); g != nil {
			fill(g.pending, blank)
		}
	case proto.Cursor:
		if g := s.term(m.Term); g != nil {
			g.cursor = Cursor{X: int(m.X), Y: int(m.Y), Visible: m.Visible(), Big: m.Big()}
		}
	case proto.Fresh:
		if g := s.term(m.Term); g != nil {
			g.fresh()
			return int(m.Term)
		}
	}
	return -1
}

func (s *Set) term(i uint8) *Grid {
	if int(i) >= MaxTerms {
		return nil
	}
	return s.Terms[i]
}

func fill(cells []Cell, c Cell) {
	for i := range cells {
		cells[i] = c
	}
}

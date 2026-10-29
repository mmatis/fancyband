package grid

import (
	"testing"

	"fancyband/internal/proto"
)

func TestChangesShowOnlyAfterFresh(t *testing.T) {
	var s Set
	s.Apply(proto.TermOpen{Term: 0, Width: 10, Height: 2, Name: "Main"})
	s.Apply(proto.Text{Term: 0, X: 2, Y: 1, Attr: 3, Text: "a█c"})
	if got := s.Terms[0].Line(1); got != "          " {
		t.Fatalf("before FRESH, line 1 = %q", got)
	}
	if term := s.Apply(proto.Fresh{Term: 0}); term != 0 {
		t.Fatalf("FRESH returned %d, want 0", term)
	}
	if got := s.Terms[0].Line(1); got != "  a█c     " {
		t.Errorf("after FRESH, line 1 = %q", got)
	}
	if c := s.Terms[0].Cell(3, 1); c.Rune != '█' || c.Attr != 3 {
		t.Errorf("cell (3,1) = %+v", c)
	}
}

func TestWipeClearAndClipping(t *testing.T) {
	var s Set
	s.Apply(proto.TermOpen{Term: 1, Width: 5, Height: 1})
	s.Apply(proto.Text{Term: 1, X: 3, Y: 0, Text: "xyz"}) // z falls off the edge
	s.Apply(proto.Fresh{Term: 1})
	if got := s.Terms[1].Line(0); got != "   xy" {
		t.Fatalf("line = %q", got)
	}
	s.Apply(proto.Wipe{Term: 1, X: 4, Y: 0, N: 1})
	s.Apply(proto.Fresh{Term: 1})
	if got := s.Terms[1].Line(0); got != "   x " {
		t.Errorf("after WIPE, line = %q", got)
	}
	s.Apply(proto.Clear{Term: 1})
	s.Apply(proto.Fresh{Term: 1})
	if got := s.Terms[1].Line(0); got != "     " {
		t.Errorf("after CLEAR, line = %q", got)
	}
}

func TestCursorAndUnknownTerms(t *testing.T) {
	var s Set
	s.Apply(proto.TermOpen{Term: 0, Width: 4, Height: 4})
	s.Apply(proto.Cursor{Term: 0, X: 1, Y: 2, Flags: proto.CursorVisible})
	if s.Terms[0].Cursor.Visible {
		t.Error("cursor moved before FRESH")
	}
	s.Apply(proto.Fresh{Term: 0})
	if c := s.Terms[0].Cursor; c != (Cursor{X: 1, Y: 2, Visible: true}) {
		t.Errorf("cursor = %+v", c)
	}
	// Messages for terms that were never opened are ignored.
	if term := s.Apply(proto.Fresh{Term: 5}); term != -1 {
		t.Errorf("FRESH on an unopened term returned %d", term)
	}
	s.Apply(proto.Text{Term: 7, Text: "x"})
}

func TestTermOpenResizes(t *testing.T) {
	var s Set
	s.Apply(proto.TermOpen{Term: 0, Width: 80, Height: 24})
	s.Apply(proto.TermOpen{Term: 0, Width: 100, Height: 30})
	if g := s.Terms[0]; g.Width != 100 || g.Height != 30 {
		t.Errorf("size = %dx%d, want 100x30", g.Width, g.Height)
	}
}

func TestTiles(t *testing.T) {
	var s Set
	s.Apply(proto.TermOpen{Term: 0, Width: 8, Height: 2})
	s.Apply(proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1})
	s.Apply(proto.Text{Term: 0, X: 0, Y: 0, Text: "abcdefgh"})
	// Two big tiles, at x=2 and x=4, cover cells 2 to 5.
	s.Apply(proto.Pict{Term: 0, X: 2, Y: 0, Cells: []proto.PictCell{{FgRow: 1, FgCol: 2, BgRow: 3, BgCol: 4}}})
	s.Apply(proto.Pict{Term: 0, X: 4, Y: 0, Cells: []proto.PictCell{{FgRow: 5, FgCol: 6, BgRow: 7, BgCol: 8}}})
	s.Apply(proto.Fresh{Term: 0})
	g := s.Terms[0]
	if got := g.Line(0); got != "ab    gh" {
		t.Errorf("line = %q", got)
	}
	if c := g.Cell(2, 0); !c.Tile || c.Pict != (proto.PictCell{FgRow: 1, FgCol: 2, BgRow: 3, BgCol: 4}) {
		t.Errorf("cell 2 = %+v", c)
	}
	if c := g.Cell(3, 0); c.Tile || !c.Covered {
		t.Errorf("cell 3 = %+v", c)
	}
	var seen []int
	g.Tiles(func(x, y int, c proto.PictCell) { seen = append(seen, x) })
	if len(seen) != 2 || seen[0] != 2 || seen[1] != 4 {
		t.Errorf("tiles at %v, want [2 4]", seen)
	}
	// Text over a tile replaces it.
	s.Apply(proto.Text{Term: 0, X: 2, Y: 0, Text: "XY"})
	s.Apply(proto.Fresh{Term: 0})
	if c := g.Cell(2, 0); c.Tile || c.Rune != 'X' {
		t.Errorf("cell 2 after text = %+v", c)
	}
}

func TestSnapshot(t *testing.T) {
	g := New(4, 2)
	g.text(proto.Text{X: 0, Y: 0, Text: "ab"})
	g.fresh()
	snap := g.Snapshot()
	g.text(proto.Text{X: 0, Y: 0, Text: "xy"})
	g.fresh()
	if snap.Line(0) != "ab  " || g.Line(0) != "xy  " {
		t.Errorf("snapshot %q, grid %q", snap.Line(0), g.Line(0))
	}
}

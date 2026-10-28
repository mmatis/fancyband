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

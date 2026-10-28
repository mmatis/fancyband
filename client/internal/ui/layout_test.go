package ui

import "testing"

func TestComputeTooSmall(t *testing.T) {
	for _, size := range [][2]int{{79, 24}, {80, 23}, {0, 0}} {
		if l := Compute(size[0], size[1], true); !l.TooSmall {
			t.Errorf("%dx%d is not too small", size[0], size[1])
		}
	}
}

func TestComputeNoSubwindows(t *testing.T) {
	l := Compute(200, 60, false)
	if l.TooSmall || l.Main != (Box{0, 0, 200, 59}) || len(l.Panes) != 0 || l.Divider != -1 {
		t.Errorf("got %+v", l)
	}
	if l.Gauges != (Box{0, 59, 200, 1}) {
		t.Errorf("gauges %+v, want the last row", l.Gauges)
	}
	// At exactly 80x24 the map needs every row.
	if l := Compute(80, 24, false); !l.Gauges.Empty() || l.Main.H != 24 {
		t.Errorf("80x24: %+v", l)
	}
}

func TestComputeSidePanel(t *testing.T) {
	l := Compute(120, 36, true)
	if !l.Vertical || l.Main.W < MinWidth || l.Main.H != 36 {
		t.Fatalf("got %+v", l)
	}
	msgs, ok1 := l.Pane(TermMessages)
	mons, ok2 := l.Pane(TermMonsters)
	if !ok1 || !ok2 {
		t.Fatalf("panes = %+v", l.Panes)
	}
	if msgs.Box.X != l.Main.W+1 || msgs.Box.X+msgs.Box.W != 120 {
		t.Errorf("messages pane %+v does not fill the right of the screen", msgs.Box)
	}
	if l.Gauges != (Box{msgs.Box.X, 0, msgs.Box.W, gaugeRows}) || msgs.Box.Y != gaugeRows {
		t.Errorf("gauges %+v should sit above messages %+v", l.Gauges, msgs.Box)
	}
	if mons.Box.Y != msgs.Box.Y+msgs.Box.H || mons.Box.Y+mons.Box.H != 36 {
		t.Errorf("monsters pane %+v does not sit below messages %+v", mons.Box, msgs.Box)
	}
	if tb := msgs.TermBox(); tb.Y != msgs.Box.Y+1 || tb.H != msgs.Box.H-1 {
		t.Errorf("term box %+v leaves no title row", tb)
	}
}

func TestComputeSidePanelNeverShrinksMain(t *testing.T) {
	for w := MinWidth; w <= 300; w++ {
		l := Compute(w, 30, true)
		if l.Main.W < MinWidth {
			t.Fatalf("width %d: main term is %d wide", w, l.Main.W)
		}
		used := l.Main.W
		for _, p := range l.Panes {
			if p.Box.X+p.Box.W > w {
				t.Fatalf("width %d: pane %+v runs off the screen", w, p.Box)
			}
		}
		if len(l.Panes) > 0 && l.Vertical && used+1+l.Panes[0].Box.W != w {
			t.Fatalf("width %d: columns do not add up", w)
		}
	}
}

func TestComputeBottomPanel(t *testing.T) {
	l := Compute(90, 40, true)
	if l.Vertical || len(l.Panes) != 1 || l.Panes[0].Term != TermMessages {
		t.Fatalf("got %+v", l)
	}
	if l.Main.H < MinHeight || l.Main.H+1+l.Panes[0].Box.H != 40 {
		t.Errorf("rows do not add up: %+v", l)
	}
	if l.Gauges != (Box{0, l.Divider, 90, 1}) {
		t.Errorf("gauges %+v, want the divider row", l.Gauges)
	}
	// Too short for a bottom panel, too narrow for a side panel.
	if l := Compute(90, 26, true); len(l.Panes) != 0 || l.Main.H != 25 || l.Gauges.Y != 25 {
		t.Errorf("90x26: %+v", l)
	}
}

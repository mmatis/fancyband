package ui

import "testing"

func TestComputeTooSmall(t *testing.T) {
	for _, size := range [][2]int{{79, 24}, {80, 23}, {0, 0}} {
		if l := Compute(size[0], size[1], true, false, true); !l.TooSmall {
			t.Errorf("%dx%d is not too small", size[0], size[1])
		}
	}
}

func TestComputeNoSubwindows(t *testing.T) {
	l := Compute(200, 60, false, false, true)
	if l.TooSmall || l.Main != (Box{0, 0, 200, 59}) || len(l.Panes) != 0 || l.Divider != -1 {
		t.Errorf("got %+v", l)
	}
	if l.Gauges != (Box{0, 59, 200, 1}) {
		t.Errorf("gauges %+v, want the last row", l.Gauges)
	}
	// At exactly 80x24 the map needs every row.
	if l := Compute(80, 24, false, false, true); !l.Gauges.Empty() || l.Main.H != 24 {
		t.Errorf("80x24: %+v", l)
	}
}

func TestComputeSidePanel(t *testing.T) {
	l := Compute(120, 36, true, false, true)
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
		l := Compute(w, 30, true, false, true)
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
	l := Compute(90, 40, true, false, true)
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
	if l := Compute(90, 26, true, false, true); len(l.Panes) != 0 || l.Main.H != 25 || l.Gauges.Y != 25 {
		t.Errorf("90x26: %+v", l)
	}
}

func TestComputeNative(t *testing.T) {
	// Room for the stats bar, the status line, and a side panel.
	l := Compute(150, 40, true, true, true)
	if !l.Native() || !l.Vertical {
		t.Fatalf("got %+v", l)
	}
	if l.Stats != (Box{l.Main.W + 1, 0, StatsWidth, 40}) {
		t.Errorf("stats %+v should sit right of the map %+v", l.Stats, l.Main)
	}
	if l.StatusLine != (Box{0, l.Main.H, l.Main.W, 1}) || l.Main.H != 39 {
		t.Errorf("status line %+v should sit under the map %+v", l.StatusLine, l.Main)
	}
	msgs, _ := l.Pane(TermMessages)
	if l.Divider != l.Stats.X+l.Stats.W || msgs.Box.X != l.Divider+1 || msgs.Box.X+msgs.Box.W != 150 || msgs.Box.Y != 0 {
		t.Errorf("messages %+v, divider %d, stats %+v", msgs.Box, l.Divider, l.Stats)
	}
	if !l.Gauges.Empty() {
		t.Errorf("gauges %+v outside the stats bar", l.Gauges)
	}

	// Too narrow for a side panel too: the stats bar alone, right of the
	// map, with the message log below both.
	l = Compute(110, 40, true, true, true)
	if !l.Native() || l.Vertical || len(l.Panes) != 1 {
		t.Fatalf("110x40: %+v", l)
	}
	if l.Main.W != 110-StatsWidth-1 || l.StatusLine.Y != l.Main.H || l.Divider != l.Main.H+1 || l.Stats.H != l.Divider {
		t.Errorf("110x40: main %+v status %+v divider %d stats %+v", l.Main, l.StatusLine, l.Divider, l.Stats)
	}
	if p := l.Panes[0]; p.Box.Y != l.Divider+1 || p.Box.Y+p.Box.H != 40 || p.Box.W != 110 {
		t.Errorf("110x40: messages %+v", p.Box)
	}

	// No room beside the smallest map: the game draws its own.
	for _, size := range [][2]int{{MinWidth + StatsWidth, 40}, {120, MinHeight}} {
		if l := Compute(size[0], size[1], true, true, true); l.Native() || !l.StatusLine.Empty() {
			t.Errorf("%dx%d: %+v", size[0], size[1], l)
		}
	}
	// Every native layout keeps the map at least the smallest size.
	for w := MinWidth; w <= 220; w += 7 {
		for h := MinHeight; h <= 50; h += 3 {
			for _, sub := range []bool{false, true} {
				l := Compute(w, h, sub, true, true)
				if l.Main.W < MinWidth || l.Main.H < MinHeight {
					t.Fatalf("%dx%d: map %+v", w, h, l.Main)
				}
				if l.Native() && (l.Stats.X+l.Stats.W > w || l.Stats.H > h) {
					t.Fatalf("%dx%d: stats %+v off the screen", w, h, l.Stats)
				}
			}
		}
	}
}

func TestComputeStatsLeft(t *testing.T) {
	right := Compute(150, 40, true, true, true)
	l := Compute(150, 40, true, true, false)
	if l.Stats != (Box{0, 0, StatsWidth, 40}) || l.StatsDivider != StatsWidth {
		t.Errorf("stats %+v, divider %d; want the left edge", l.Stats, l.StatsDivider)
	}
	if l.Main != (Box{StatsWidth + 1, 0, right.Main.W, right.Main.H}) {
		t.Errorf("map %+v, want it just right of the stats bar", l.Main)
	}
	if l.StatusLine != (Box{l.Main.X, l.Main.H, l.Main.W, 1}) {
		t.Errorf("status line %+v, want it under the map %+v", l.StatusLine, l.Main)
	}
	if l.Divider != right.Divider || len(l.Panes) != 2 || l.Panes[0].Box != right.Panes[0].Box {
		t.Errorf("the side panel moved: %+v", l.Panes)
	}
	if right.StatsDivider != right.Stats.X-1 || right.StatsDivider != right.Main.W {
		t.Errorf("right: divider %d, stats %+v, map %+v", right.StatsDivider, right.Stats, right.Main)
	}
}

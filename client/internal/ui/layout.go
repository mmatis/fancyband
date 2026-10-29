package ui

// This file has no Limoni imports, so the layout rules can be tested alone.

// MinWidth and MinHeight are the smallest main term the engine accepts.
const (
	MinWidth  = 80
	MinHeight = 24
)

// The subwindows the client opens. The engine's defaults put the message
// log in term 1 and the monster list in term 3 (see ui-init.c), so these
// show something useful with no setup.
const (
	TermMessages = 1
	TermMonsters = 3
)

// Space the panels need before they are shown.
const (
	sideMinWidth    = 24
	sideMaxWidth    = 60
	bottomMinHeight = 6
	bottomMaxHeight = 15
	// StatsWidth is the width of the native stats bar.
	StatsWidth = 24
)

// Box is a rectangle on the screen, in cells.
type Box struct{ X, Y, W, H int }

// Empty reports whether the box has no cells.
func (b Box) Empty() bool { return b.W <= 0 || b.H <= 0 }

// Overlaps reports whether the boxes share a cell.
func (b Box) Overlaps(o Box) bool {
	return !b.Empty() && !o.Empty() &&
		b.X < o.X+o.W && o.X < b.X+b.W && b.Y < o.Y+o.H && o.Y < b.Y+b.H
}

// Pane is one subwindow on screen: a title row and the term below it.
type Pane struct {
	Term  int
	Title string
	Box   Box // the title row and the term
}

// TermBox is where the pane's term goes, below its title.
func (p Pane) TermBox() Box {
	return Box{p.Box.X, p.Box.Y + 1, p.Box.W, p.Box.H - 1}
}

// Layout says where every term goes for one screen size.
type Layout struct {
	Width, Height int
	// TooSmall is set when the screen cannot hold the main term.
	TooSmall bool
	Main     Box
	// Divider is the column (side panel) or row (bottom panel) between
	// the main term and the panes, or -1.
	Divider  int
	Vertical bool // true when Divider is a column
	Panes    []Pane
	// Gauges is where the HP and mana gauges go, or an empty box. In a
	// side panel it is three rows: a title and one row per gauge. Below
	// the map it is a single row. With the stats bar the gauges are in
	// it, and this is empty.
	Gauges Box
	// Stats is the native stats bar, right of the map, and StatusLine
	// the native status line, under it. Both are empty when the window
	// has no room for them, and the game draws its own sidebar and
	// status line instead.
	Stats, StatusLine Box
	// StatsDivider is the column between the stats bar and the map.
	StatsDivider int
}

// Native reports whether the layout has the native stats bar and status
// line.
func (l Layout) Native() bool { return !l.Stats.Empty() }

// gaugeRows is the height of the gauges at the top of a side panel.
const gaugeRows = 3

// Compute lays out a screen of width by height. With subwindows on, a
// side panel holds the gauges, then the message log above the monster
// list, when the screen is wide enough; otherwise a bottom panel holds
// the message log when the screen is tall enough, with the gauges in the
// row above it. With no panel, a spare row below the map holds the
// gauges.
//
// With native on, the stats bar goes left of the map, or right of it
// with right set, before any side panel, and the status line under the
// map, when the window has room for both beside the smallest map the
// game accepts. The gauges are then in the stats bar.
func Compute(width, height int, subwindows, native, right bool) Layout {
	l := compute(width, height, subwindows, native)
	if !l.Native() {
		return l
	}
	l.StatsDivider = l.Stats.X - 1
	if !right {
		// Swap the stats bar and the map; the panels stay put.
		l.Stats.X = 0
		l.StatsDivider = StatsWidth
		l.Main.X = StatsWidth + 1
		l.StatusLine.X = l.Main.X
	}
	return l
}

// compute lays out the screen with any stats bar right of the map.
func compute(width, height int, subwindows, native bool) Layout {
	l := Layout{Width: width, Height: height, Divider: -1}
	if width < MinWidth || height < MinHeight {
		l.TooSmall = true
		return l
	}
	native = native && width >= MinWidth+1+StatsWidth && height >= MinHeight+1
	// The map and the panels share what the stats bar and the status
	// line leave.
	w, h := width, height
	stats := 0
	if native {
		stats = StatsWidth + 1
		w -= stats
		h--
	}
	l.Main = Box{0, 0, w, h}
	// place puts the stats bar and status line beside and under the map,
	// once the map's size is known; tall is how many rows the stats bar
	// spans.
	place := func(tall int) {
		if native {
			l.Stats = Box{l.Main.W + 1, 0, StatsWidth, tall}
			l.StatusLine = Box{0, l.Main.H, l.Main.W, 1}
		}
	}
	if !subwindows {
		if !native {
			l.gaugeRowBelow()
		}
		place(height)
		return l
	}

	if w >= MinWidth+1+sideMinWidth {
		side := min(max(sideMinWidth, w/3), sideMaxWidth, w-MinWidth-1)
		l.Main.W = w - side - 1
		l.Divider = l.Main.W + stats
		l.Vertical = true
		x := l.Divider + 1
		top := 0
		if !native {
			l.Gauges = Box{x, 0, side, gaugeRows}
			top = gaugeRows
		}
		rest := height - top
		half := rest / 2
		l.Panes = []Pane{
			{Term: TermMessages, Title: "Messages", Box: Box{x, top, side, half}},
			{Term: TermMonsters, Title: "Monsters", Box: Box{x, top + half, side, rest - half}},
		}
		place(height)
		return l
	}

	if h >= MinHeight+1+bottomMinHeight {
		bottom := min(max(bottomMinHeight, h/4), bottomMaxHeight, h-MinHeight-1)
		l.Main.H = h - bottom - 1
		l.Divider = height - bottom - 1
		if !native {
			l.Gauges = Box{0, l.Divider, width, 1}
		}
		l.Panes = []Pane{
			{Term: TermMessages, Title: "Messages", Box: Box{0, l.Divider + 1, width, bottom}},
		}
		place(l.Divider)
		return l
	}
	if !native {
		l.gaugeRowBelow()
	}
	place(height)
	return l
}

// gaugeRowBelow gives the last row to the gauges when the map can spare it.
func (l *Layout) gaugeRowBelow() {
	if l.Main.H > MinHeight {
		l.Main.H--
		l.Gauges = Box{0, l.Main.H, l.Width, 1}
	}
}

// Pane returns the pane showing term, if the layout shows it.
func (l Layout) Pane(term int) (Pane, bool) {
	for _, p := range l.Panes {
		if p.Term == term {
			return p, true
		}
	}
	return Pane{}, false
}

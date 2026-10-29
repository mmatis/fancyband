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
	// the map it is a single row.
	Gauges Box
}

// gaugeRows is the height of the gauges at the top of a side panel.
const gaugeRows = 3

// Compute lays out a screen of width by height. With subwindows on, a
// side panel holds the gauges, then the message log above the monster
// list, when the screen is wide enough; otherwise a bottom panel holds
// the message log when the screen is tall enough, with the gauges in the
// row above it. With no panel, a spare row below the map holds the
// gauges.
func Compute(width, height int, subwindows bool) Layout {
	l := Layout{Width: width, Height: height, Divider: -1}
	if width < MinWidth || height < MinHeight {
		l.TooSmall = true
		return l
	}
	l.Main = Box{0, 0, width, height}
	if !subwindows {
		l.gaugeRowBelow()
		return l
	}

	if width >= MinWidth+1+sideMinWidth {
		side := min(max(sideMinWidth, width/3), sideMaxWidth, width-MinWidth-1)
		l.Main.W = width - side - 1
		l.Divider = l.Main.W
		l.Vertical = true
		x := l.Main.W + 1
		l.Gauges = Box{x, 0, side, gaugeRows}
		rest := height - gaugeRows
		top := rest / 2
		l.Panes = []Pane{
			{Term: TermMessages, Title: "Messages", Box: Box{x, gaugeRows, side, top}},
			{Term: TermMonsters, Title: "Monsters", Box: Box{x, gaugeRows + top, side, rest - top}},
		}
		return l
	}

	if height >= MinHeight+1+bottomMinHeight {
		bottom := min(max(bottomMinHeight, height/4), bottomMaxHeight, height-MinHeight-1)
		l.Main.H = height - bottom - 1
		l.Divider = l.Main.H
		l.Gauges = Box{0, l.Divider, width, 1}
		l.Panes = []Pane{
			{Term: TermMessages, Title: "Messages", Box: Box{0, l.Main.H + 1, width, bottom}},
		}
		return l
	}
	l.gaugeRowBelow()
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

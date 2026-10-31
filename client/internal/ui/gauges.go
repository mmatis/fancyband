package ui

import (
	"fmt"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"
)

// gauges holds what the HP and mana gauges show. The engine reports both
// in EVENT frames; until the first one arrives the gauges stay hidden.
type gauges struct {
	known    bool
	hp, mhp  int
	sp, msp  int
	hasMagic bool
}

// IDs for the gauges in Limoni's accessibility tree, which tests use.
const (
	hpGaugeID  = "hp-gauge"
	spGaugeID  = "sp-gauge"
	foeGaugeID = "foe-gauge"
)

var (
	gaugeLabel = limoni.Fg(limoni.RGB(200, 200, 210))
	gaugeEmpty = limoni.Fg(limoni.RGB(60, 60, 70))
	manaStyle  = limoni.Fg(limoni.RGB(90, 140, 255))
)

// hpStyle colors the HP bar the way the game colors the HP number:
// green when healthy, yellow when hurt, red when in danger.
func hpStyle(hp, mhp int) cell.Style {
	switch {
	case mhp <= 0 || hp*10 >= mhp*7:
		return limoni.Fg(limoni.RGB(80, 200, 100))
	case hp*10 >= mhp*3:
		return limoni.Fg(limoni.RGB(230, 200, 60))
	default:
		return limoni.Fg(limoni.RGB(230, 70, 60))
	}
}

// drawGauges draws the gauges into box: on separate rows under a title
// when the box has room, or side by side on one row.
func drawGauges(f *terminal.Frame, box Box, g gauges) {
	if box.Empty() || !g.known {
		return
	}
	type row struct {
		id, label string
		cur, max  int
		style     cell.Style
	}
	rows := []row{{hpGaugeID, "HP", g.hp, g.mhp, hpStyle(g.hp, g.mhp)}}
	if g.hasMagic {
		rows = append(rows, row{spGaugeID, "SP", g.sp, g.msp, manaStyle})
	}

	draw := func(r row, x, y, w int) { drawGauge(f, x, y, w, r.id, r.label, r.cur, r.max, r.style) }

	if box.H >= 1+len(rows) {
		f.TextFit(box.X, box.Y, box.W, " Status ", limoni.Fg(limoni.RGB(90, 90, 110)).Bold())
		for i, r := range rows {
			draw(r, box.X+1, box.Y+1+i, box.W-2)
		}
		return
	}
	// One row: share it, leaving a gap between the gauges.
	w := min(box.W/len(rows), 40)
	for i, r := range rows {
		draw(r, box.X+1+i*(w+1), box.Y, w-1)
	}
}

// drawGauge draws one gauge on a row w wide: its label, a bar, and the
// numbers. A row too narrow for a bar shows only the label and numbers.
func drawGauge(f *terminal.Frame, x, y, w int, id, label string, cur, most int, style cell.Style) {
	drawGaugeText(f, x, y, w, id, label, cur, most, style, fmt.Sprintf(" %d/%d", cur, most))
}

// drawGaugeText is drawGauge with nums in place of the numbers.
func drawGaugeText(f *terminal.Frame, x, y, w int, id, label string, cur, most int, style cell.Style, nums string) {
	if w < 8 {
		return
	}
	label += " "
	f.Text(x, y, label, gaugeLabel.Bold())
	barW := w - len(label) - len(nums)
	if barW >= 3 {
		bar := widgets.ProgressBar{
			ID:          id,
			Value:       float64(max(cur, 0)),
			Max:         float64(max(most, 1)),
			FilledStyle: style,
			EmptyStyle:  gaugeEmpty,
		}
		f.RenderWidget(bar, rect(Box{x + len(label), y, barW, 1}))
	}
	f.TextFit(x+w-len(nums), y, len(nums), nums, gaugeLabel)
}

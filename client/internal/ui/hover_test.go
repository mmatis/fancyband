package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/proto"
)

// TestHoverSetting turns hover on and off with the Display page's
// checkbox, which puts the terminal in the mouse mode that reports every
// move, and back.
func TestHoverSetting(t *testing.T) {
	var modes []driver.MouseMode
	m := New(Config{Width: 100, Height: 30, setMouseMode: func(md driver.MouseMode) error {
		modes = append(modes, md)
		return nil
	}})
	if m.wantMouseMode() != driver.MouseClicks {
		t.Error("hover is on by default")
	}
	rows := m.displayRows(proto.EventData{})
	i := slices.IndexFunc(rows, func(r *optRow) bool { return r.label == "Highlight under the mouse" })
	if i < 0 {
		t.Fatal("the Display page has no hover checkbox")
	}
	term := testkit.NewTerminal(100, 30)
	draw := func() { term.Draw(func(f *terminal.Frame) { m.View(f) }) }

	rows[i].change(1)
	draw()
	if !m.settings.Hover || !slices.Equal(modes, []driver.MouseMode{driver.MouseMotion}) {
		t.Errorf("after turning hover on, hover %v and mouse modes %v", m.settings.Hover, modes)
	}
	draw()
	if len(modes) != 1 {
		t.Errorf("a second frame changed the mouse mode again: %v", modes)
	}
	rows[i].change(1)
	draw()
	if m.settings.Hover || !slices.Equal(modes, []driver.MouseMode{driver.MouseMotion, driver.MouseClicks}) {
		t.Errorf("after turning hover off, hover %v and mouse modes %v", m.settings.Hover, modes)
	}
}

// TestHover lights up the button and the row under the mouse pointer,
// redraws only when the control under it changes, and leaves the
// selected row its own color.
func TestHover(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	m.settings.Hover = true
	var items []listItem
	for i := 0; i < 40; i++ {
		items = append(items, listItem{Label: fmt.Sprintf("item %02d", i)})
	}
	s := newListScreen("test-list", "Test", items)
	s.height = 20
	s.buttons = []listButton{{Label: "Load", Do: func(int) {}}, {Label: "Cancel", Do: func(int) {}}}
	m.push(s)
	term := testkit.NewTerminal(100, 30)
	draw := func() { term.Draw(func(f *terminal.Frame) { m.View(f) }) }
	bgAt := func(x, y int) any { return term.Frame().Buffer.CellAt(uint16(x), uint16(y)).Style.Bg }
	draw()

	// A button, found by its click region.
	var load Box
	for _, h := range s.hotspots {
		if h.box.W == len("Load")+4 && h.box.H == 1 {
			load = h.box
		}
	}
	if load.Empty() {
		t.Fatal("no Load button")
	}
	if bgAt(load.X+1, load.Y) == hoverBg {
		t.Fatal("the Load button is lit before the pointer comes")
	}
	if r := m.Update(engine.MouseMotionMsg{Position: pointAt(load.X+1, load.Y)}); !r.Redraw {
		t.Error("the pointer onto Load asked for no redraw")
	}
	draw()
	if bgAt(load.X+1, load.Y) != hoverBg {
		t.Error("the Load button is not lit under the pointer")
	}
	if r := m.Update(engine.MouseMotionMsg{Position: pointAt(load.X+2, load.Y)}); r.Redraw {
		t.Error("a move within Load asked for a redraw")
	}

	// The fourth row, whose click region is as wide as the list: Limoni
	// gives the move to the list's wheel area.
	var row Box
	for _, h := range s.hotspots {
		if h.box.W > row.W {
			row = h.box
		}
	}
	x, y := row.X+3, row.Y+3
	if !term.Mouse(driver.MouseEvent{X: uint16(x), Y: uint16(y), Button: driver.MouseNone}) {
		t.Fatal("nothing took the pointer's move over the list")
	}
	draw()
	if bgAt(x, y) != hoverBg || bgAt(load.X+1, load.Y) == hoverBg {
		t.Error("the row under the pointer is not lit, or Load still is")
	}
	// The selected row keeps its color.
	s.state.Select(s.state.Offset + 3)
	draw()
	if bgAt(x, y) == hoverBg {
		t.Error("the selected row under the pointer lost its color")
	}

	// Off, nothing is lit.
	m.settings.Hover = false
	m.pointerSet = false
	draw()
	if bgAt(load.X+1, load.Y) == hoverBg || bgAt(x, y) == hoverBg {
		t.Error("with hover off something is lit")
	}
}

func pointAt(x, y int) cell.Point { return cell.Point{X: uint16(x), Y: uint16(y)} }

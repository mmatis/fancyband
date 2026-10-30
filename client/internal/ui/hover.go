package ui

import (
	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
)

// Hover: with the "Highlight under the mouse" setting on, the native
// screens light up the button, row, or tab under the mouse pointer. The
// terminal must then report every move of the mouse, which it does not by
// default, since that costs a message a cell over SSH. Limoni gives a move
// over a list to the list's wheelArea, which passes it on through
// drawEnv.pointer; it gives every other move to Update. Either way the
// pointer lands in pointerAt, and View lights up the control under it.

// hoverBg is the background of the control under the mouse pointer.
var hoverBg = limoni.RGB(44, 44, 66)

// wantMouseMode is the mouse mode the settings ask for: every move for
// hover, or else clicks and the wheel alone.
func (m *Model) wantMouseMode() driver.MouseMode {
	if m.settings.Hover {
		return driver.MouseMotion
	}
	return driver.MouseClicks
}

// syncMouseMode puts the terminal in the mouse mode the settings ask for.
// It runs in View, which Limoni calls on the goroutine that writes to the
// terminal, so the mode's escape sequence never lands inside a frame.
func (m *Model) syncMouseMode() {
	want := m.wantMouseMode()
	if want == m.mouseMode || m.cfg.setMouseMode == nil {
		return
	}
	if err := m.cfg.setMouseMode(want); err != nil {
		m.log.Printf("cannot change the mouse mode: %v", err)
		return
	}
	m.mouseMode = want
}

// pointerAt notes the cell under the mouse pointer, while hover is on.
func (m *Model) pointerAt(x, y int) {
	if !m.settings.Hover {
		return
	}
	m.pointer, m.pointerSet = cell.Point{X: uint16(x), Y: uint16(y)}, true
}

// hoverBox returns the control under the mouse pointer on the top
// screen, as last drawn, if hover is on and there is one.
func (m *Model) hoverBox() (Box, bool) {
	n := len(m.stack)
	if !m.settings.Hover || !m.pointerSet || n == 0 {
		return Box{}, false
	}
	c, ok := m.stack[n-1].(interface{ controls() []hotspot })
	if !ok {
		return Box{}, false
	}
	x, y := int(m.pointer.X), int(m.pointer.Y)
	hs := c.controls()
	// The last drawn is on top.
	for i := len(hs) - 1; i >= 0; i-- {
		h := hs[i]
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			if h.region {
				return Box{}, false
			}
			return h.box, true
		}
	}
	return Box{}, false
}

// pointerMoved handles a move of the mouse that reached Update. It
// returns whether to redraw: only when the control under the pointer
// changes.
func (m *Model) pointerMoved(x, y int) bool {
	if !m.settings.Hover {
		return false
	}
	m.pointerAt(x, y)
	b, _ := m.hoverBox()
	return b != m.hovered
}

// drawHover lights up the control under the mouse pointer: the cells of
// its box that show the screen's own background, or none. A selected
// row keeps its color, and pictures stay as they are.
func (m *Model) drawHover(f *terminal.Frame) {
	b, ok := m.hoverBox()
	m.hovered = b
	if !ok {
		return
	}
	for y := b.Y; y < b.Y+b.H; y++ {
		for x := b.X; x < b.X+b.W; x++ {
			c := f.Buffer.Get(uint16(x), uint16(y))
			if c == nil || c.Content == cell.RuneImage {
				continue
			}
			if bg := c.Style.Bg; bg == screenBg.Bg || bg.Type() == cell.ColorDefault {
				c.Style = c.Style.WithBg(hoverBg)
			}
		}
	}
}

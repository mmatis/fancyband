package ui

import (
	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/widgets"
)

// Everything that scrolls takes the same input: the mouse wheel, the
// arrow keys, and Page Up and Page Down. The arrows move one row, the
// page keys a screenful, and the wheel scrolls whatever is under the
// pointer, whichever part of the screen has the keys.

// wheelStep is how many rows one notch of the mouse wheel scrolls.
const wheelStep = 3

// wheelArea takes the mouse wheel over its area and calls scroll with 1
// for each notch down and -1 for each notch up. Limoni gives a wheel
// event to the last area drawn under the pointer that takes the wheel,
// and does not pass it on to Update, so a wheelArea is drawn after the
// list it scrolls: Limoni's own list scrolls its view but then turns it
// back to the selected row. A click on a wheelArea does nothing, so it
// goes under the click handlers of the rows. Limoni gives it the
// pointer's moves too, which it passes to pointer, if set, for hover.
type wheelArea struct {
	scroll  func(d int)
	pointer func(x, y int)
}

func (w wheelArea) Draw(ctx cell.Context, _ *buffer.Buffer) {
	if ctx.RegisterMouse == nil || w.scroll == nil {
		return
	}
	ctx.RegisterMouse(ctx.Area, func(ev driver.MouseEvent) {
		switch ev.Button {
		case driver.MouseScrollUp:
			w.scroll(-1)
		case driver.MouseScrollDown:
			w.scroll(1)
		case driver.MouseNone:
			if w.pointer != nil {
				w.pointer(int(ev.X), int(ev.Y))
			}
		}
	})
}

func (w wheelArea) SizeHint(max cell.Rect) (uint16, uint16) { return max.Width, max.Height }

// wheelList scrolls the view of a list of n rows, height of them shown,
// by d notches of the wheel. The selection stays where it is unless the
// view leaves it; then it moves to the nearest row shown that ok allows,
// or any row when ok is nil.
func wheelList(st *widgets.ListState, n, height, d int, ok func(int) bool) {
	if n == 0 || height <= 0 {
		return
	}
	st.Offset = min(max(st.Offset+d*wheelStep, 0), max(n-height, 0))
	last := min(st.Offset+height, n) - 1
	sel := min(max(st.Selected, st.Offset), last)
	if ok != nil && !ok(sel) {
		for i := 1; i < height; i++ {
			if sel+i <= last && ok(sel+i) {
				sel += i
				break
			}
			if sel-i >= st.Offset && ok(sel-i) {
				sel -= i
				break
			}
		}
	}
	st.Selected = sel
}

// pageRows is how far Page Up and Page Down move in a list showing
// height rows: a screenful less one row, so the row at the edge stays in
// sight. A list not drawn yet moves 10.
func pageRows(height int) int {
	if height <= 0 {
		return 10
	}
	return max(height-1, 1)
}

// scrollBar is a scroll bar beside or under what it scrolls: an arrow at
// each end, and between them a track with a thumb whose size shows how
// much is in view and whose place shows where. Its units are whatever
// it scrolls by, such as rows or tiles.
type scrollBar struct {
	vertical bool
	// total is how many units there are, shown how many are in view, and
	// offset the first one in view.
	total, shown, offset int
	// set moves the view to start at offset, which it keeps in range.
	set func(offset int)
}

// thumb returns where the thumb starts on the track and its length.
func (s scrollBar) thumb(track int) (at, length int) {
	if s.total <= s.shown || track <= 0 {
		return 0, track
	}
	length = min(max(track*s.shown/s.total, 1), track)
	at = (track - length) * s.offset / max(s.total-s.shown, 1)
	return min(max(at, 0), track-length), length
}

func (s scrollBar) Draw(ctx cell.Context, buf *buffer.Buffer) {
	n := int(ctx.Area.Height)
	first, last := '▲', '▼'
	if !s.vertical {
		n = int(ctx.Area.Width)
		first, last = '◀', '▶'
	}
	if n < 3 {
		return
	}
	put := func(i int, r rune, st cell.Style) {
		x, y := ctx.Area.X, ctx.Area.Y
		if s.vertical {
			y += uint16(i)
		} else {
			x += uint16(i)
		}
		buf.SetCell(x, y, cell.Cell{Content: r, Style: st})
	}
	track := n - 2
	at, length := s.thumb(track)
	put(0, first, screenText)
	put(n-1, last, screenText)
	for i := 0; i < track; i++ {
		if i >= at && i < at+length {
			put(1+i, '█', screenText)
		} else {
			put(1+i, '░', screenDim)
		}
	}
	if ctx.RegisterMouse == nil || s.set == nil {
		return
	}
	start := int(ctx.Area.X)
	if s.vertical {
		start = int(ctx.Area.Y)
	}
	pos := func(ev driver.MouseEvent) int {
		if s.vertical {
			return int(ev.Y) - start
		}
		return int(ev.X) - start
	}
	capture := ctx.CaptureMouse
	ctx.RegisterMouse(ctx.Area, func(ev driver.MouseEvent) {
		switch ev.Button {
		case driver.MouseScrollUp, driver.MouseScrollLeft:
			s.set(s.offset - wheelStep)
			return
		case driver.MouseScrollDown, driver.MouseScrollRight:
			s.set(s.offset + wheelStep)
			return
		}
		if ev.Button != driver.MouseLeft || ev.Drag {
			return
		}
		p := pos(ev)
		switch {
		case p <= 0:
			s.set(s.offset - 1)
		case p >= n-1:
			s.set(s.offset + 1)
		case p-1 < at:
			s.set(s.offset - max(s.shown-1, 1))
		case p-1 >= at+length:
			s.set(s.offset + max(s.shown-1, 1))
		default:
			// Drag the thumb: the place it was grabbed stays under the
			// pointer.
			grab, from := p, s.offset
			if capture != nil {
				capture(func(ev driver.MouseEvent) {
					if ev.Button == driver.MouseRelease || track <= length {
						return
					}
					s.set(from + (pos(ev)-grab)*(s.total-s.shown)/(track-length))
				})
			}
		}
	})
}

func (s scrollBar) SizeHint(max cell.Rect) (uint16, uint16) { return max.Width, max.Height }

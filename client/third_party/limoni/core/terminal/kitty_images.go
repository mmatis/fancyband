package terminal

import (
	"image"
	"math"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/graphics"
)

// kittyImages is what kitty has been sent. Each picture is sent once, under
// an id of its own, and shown with placements. A picture moving, or
// scrolling half out of view (a graphics.Clip), costs a placement of a few
// dozen bytes instead of the whole picture again. One no longer shown is
// freed in kitty.
//
// Fancyband fork: each placement keeps an id of its own, tied to the cells
// it covers, and a frame changes only the placements that differ from the
// last frame's. Upstream deleted every placement and made them all again
// whenever any picture changed, which for a map of a thousand tiles cost
// about 58 KB a frame, and which Konsole showed as a flash of the empty
// screen between the two.
type kittyImages struct {
	ids   map[kittyKey]*kittyEntry
	next  uint32
	stamp uint64
	// gen is graphics.Generation when the pictures were sent; a ForgetImage
	// since means one may have been rewritten in place.
	gen uint64
	// stale is set when the terminal may have none of them (a resume).
	stale bool

	// placed is what is on screen, by the cells each placement covers.
	placed  map[placeKey]*kittyPlaced
	nextPID uint32
	// seen counts the pictures on each area in the frame being placed.
	seen map[cell.Rect]int
}

// placeKey names a placement by its cells. n tells apart pictures that
// share their cells, in the order the frame gives them. A moving picture
// is named by its track instead (Fancyband fork).
type placeKey struct {
	area  cell.Rect
	n     int
	track uint32
}

// kittyKey is a picture as sent: kitty is sent it scaled to its cells.
type kittyKey struct {
	img          image.Image
	cols, rows   uint16
	cellW, cellH uint16
	transparent  bool
	// fullSize is set for a picture sent at exactly its cells' size in
	// pixels, to be cut to the pixel (Fancyband fork).
	fullSize bool
}

type kittyEntry struct {
	id    uint32
	stamp uint64
}

// kittyPlaced is one placement on screen: picture id, under placement id
// pid, showing the source rectangle x, y, w, h at depth z.
type kittyPlaced struct {
	id, pid    uint32
	x, y, w, h int
	z          int
	stamp      uint64
	// at is the cell it is placed on and offX, offY its offset in pixels
	// within that cell, for a shifted picture, and cols, rows the cells it
	// is scaled to, 0 for its own size (Fancyband fork).
	at         [2]int
	offX, offY int
	cols, rows uint16
}

// clipped works out how to show the part of a shifted picture inside its
// Clip, as a pixel rectangle on screen. It returns ok false when none of
// it shows, and cut false when all of it does. Otherwise the part shown
// is vis, in screen pixels, and src is where it is in the picture, in
// pixels of the picture at its cells' size.
//
// Fancyband fork: added for scrolling the map.
func clipped(reg ImageRegion, ax, ay, offX, offY int, cellW, cellH uint16) (vis, src image.Rectangle, cut, ok bool) {
	cw, ch := int(cellW), int(cellH)
	corner := image.Pt(ax*cw+offX, ay*ch+offY)
	r := image.Rectangle{Min: corner, Max: corner.Add(image.Pt(int(reg.Area.Width)*cw, int(reg.Area.Height)*ch))}
	c := reg.Clip
	box := image.Rect(int(c.X)*cw, int(c.Y)*ch, int(c.X+c.Width)*cw, int(c.Y+c.Height)*ch)
	vis = r.Intersect(box)
	if vis.Empty() {
		return vis, src, false, false
	}
	if vis == r {
		return vis, src, false, true
	}
	return vis, vis.Sub(r.Min), true, true
}

// shifted returns the cell a region's picture starts in, after its
// shift, and the offset in pixels within that cell.
//
// Fancyband fork: added for sliding sprites.
func shifted(reg ImageRegion, cellW, cellH uint16) (x, y, offX, offY int) {
	x, y = int(reg.Area.X), int(reg.Area.Y)
	if reg.ShiftX == 0 && reg.ShiftY == 0 {
		return x, y, 0, 0
	}
	cw, ch := max(int(cellW), 1), max(int(cellH), 1)
	px := int(math.Round(reg.ShiftX * float64(cw)))
	py := int(math.Round(reg.ShiftY * float64(ch)))
	// Floor division, so a shift left or up starts in the cell before.
	dx, dy := px/cw, py/ch
	if px%cw < 0 {
		dx--
	}
	if py%ch < 0 {
		dy--
	}
	return x + dx, y + dy, px - dx*cw, py - dy*ch
}

// place appends what shows regions: new pictures, then the placements that
// changed, then the deletion of placements and pictures no longer shown.
// With redraw set, as after the screen was cleared or a layer changed,
// every placement is made again.
func (k *kittyImages) place(dst []byte, regions []ImageRegion, cellW, cellH uint16, redraw bool) []byte {
	if gen := graphics.Generation(); k.stale || gen != k.gen {
		dst = k.freeAll(dst)
		k.gen, k.stale = gen, false
	}
	if k.ids == nil {
		k.ids = make(map[kittyKey]*kittyEntry)
	}
	if k.placed == nil {
		k.placed = make(map[placeKey]*kittyPlaced)
		k.seen = make(map[cell.Rect]int)
	}
	clear(k.seen)
	if redraw {
		dst = append(dst, graphics.KittyDeletePlacements...)
		clear(k.placed)
	}
	k.stamp++
	for _, reg := range regions {
		full, cols, rows := reg.Img, reg.Area.Width, reg.Area.Height
		x, y, w, h := 0, 0, 0, 0
		if c, ok := reg.Img.(*graphics.Clip); ok && c.Rows > 0 && c.Bottom > c.Top {
			full, cols, rows = c.Full, uint16(c.Cols), uint16(c.Rows)
			// The source rectangle is in the pixels of the picture sent,
			// which KittyCanvas may have made smaller than the cells.
			fw, fh := graphics.KittyCanvas(full, cols, rows, cellW, cellH)
			w, y, h = fw, c.Top*fh/c.Rows, (c.Bottom-c.Top)*fh/c.Rows
		}
		// Fancyband fork: a picture cut by its Clip shows only the part
		// inside. A cut on whole cells scales that part to them; a cut
		// within a cell shows it at its own size, from a copy sent at
		// exactly its cells' size.
		ax, ay, offX, offY := shifted(reg, cellW, cellH)
		placeCols, placeRows := reg.Area.Width, reg.Area.Height
		fullSize := false
		if reg.Clip.Width > 0 && reg.Clip.Height > 0 && w == 0 {
			vis, src, cut, ok := clipped(reg, ax, ay, offX, offY, cellW, cellH)
			if !ok {
				continue
			}
			if cut {
				cw, ch := int(cellW), int(cellH)
				ax, ay = vis.Min.X/cw, vis.Min.Y/ch
				offX, offY = vis.Min.X%cw, vis.Min.Y%ch
				if offX == 0 && offY == 0 && vis.Dx()%cw == 0 && vis.Dy()%ch == 0 {
					fw, fh := graphics.KittyCanvas(full, cols, rows, cellW, cellH)
					sw, sh := int(cols)*cw, int(rows)*ch
					x, y = src.Min.X*fw/sw, src.Min.Y*fh/sh
					w, h = max(src.Dx()*fw/sw, 1), max(src.Dy()*fh/sh, 1)
					placeCols, placeRows = uint16(vis.Dx()/cw), uint16(vis.Dy()/ch)
				} else {
					fullSize = true
					x, y, w, h = src.Min.X, src.Min.Y, src.Dx(), src.Dy()
					placeCols, placeRows = 0, 0
				}
			}
		}
		key := kittyKey{full, cols, rows, cellW, cellH, reg.Transparent, fullSize}
		e := k.ids[key]
		if e == nil {
			k.next++
			e = &kittyEntry{id: 0x4c690000 + k.next}
			seq := graphics.EncodeKittyTransmit(full, cols, rows, cellW, cellH, e.id, reg.Transparent)
			if fullSize {
				seq = graphics.EncodeKittyTransmitSized(full, int(cols)*int(cellW), int(rows)*int(cellH), e.id)
			}
			if seq == "" {
				continue
			}
			dst = append(dst, seq...)
			k.ids[key] = e
		}
		e.stamp = k.stamp
		z := reg.ZIndex
		if z == 0 {
			z = -1
		}

		pk := placeKey{reg.Area, k.seen[reg.Area], 0}
		if reg.Track != 0 {
			pk = placeKey{track: reg.Track}
		} else {
			k.seen[reg.Area]++
		}
		at := [2]int{ax, ay}
		p := k.placed[pk]
		if p != nil && p.id == e.id && p.x == x && p.y == y && p.w == w && p.h == h && p.z == z &&
			p.at == at && p.offX == offX && p.offY == offY && p.cols == placeCols && p.rows == placeRows {
			p.stamp = k.stamp
			continue
		}
		old := uint32(0)
		if p == nil {
			k.nextPID++
			p = &kittyPlaced{pid: k.nextPID}
			k.placed[pk] = p
		} else if p.id != e.id {
			// A placement id belongs to one picture: place the new picture,
			// then delete the old placement, with nothing missing between.
			old = p.id
		}
		p.id, p.x, p.y, p.w, p.h, p.z, p.stamp = e.id, x, y, w, h, z, k.stamp
		p.at, p.offX, p.offY, p.cols, p.rows = at, offX, offY, placeCols, placeRows
		if ax < 0 || ay < 0 {
			// A cursor cannot go off the screen; leave the picture out.
			continue
		}
		dst = buffer.AppendCursor(dst, uint16(ax), uint16(ay))
		dst = graphics.AppendKittyPlaceOffset(dst, e.id, p.pid, placeCols, placeRows, x, y, w, h, z, offX, offY)
		if old != 0 {
			dst = graphics.AppendKittyDeletePlacement(dst, old, p.pid)
		}
	}
	for pk, p := range k.placed {
		if p.stamp != k.stamp {
			dst = graphics.AppendKittyDeletePlacement(dst, p.id, p.pid)
			delete(k.placed, pk)
		}
	}
	for key, e := range k.ids {
		if e.stamp != k.stamp {
			dst = graphics.AppendKittyFree(dst, e.id)
			delete(k.ids, key)
		}
	}
	return dst
}

// freeAll appends the deletion of every picture and forgets them.
func (k *kittyImages) freeAll(dst []byte) []byte {
	clear(k.ids)
	clear(k.placed)
	return append(dst, graphics.KittyFreeAll...)
}

package terminal

import (
	"image"

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
// share their cells, in the order the frame gives them.
type placeKey struct {
	area cell.Rect
	n    int
}

// kittyKey is a picture as sent: kitty is sent it scaled to its cells.
type kittyKey struct {
	img          image.Image
	cols, rows   uint16
	cellW, cellH uint16
	transparent  bool
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
		key := kittyKey{full, cols, rows, cellW, cellH, reg.Transparent}
		e := k.ids[key]
		if e == nil {
			k.next++
			e = &kittyEntry{id: 0x4c690000 + k.next}
			seq := graphics.EncodeKittyTransmit(full, cols, rows, cellW, cellH, e.id, reg.Transparent)
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

		pk := placeKey{reg.Area, k.seen[reg.Area]}
		k.seen[reg.Area]++
		p := k.placed[pk]
		if p != nil && p.id == e.id && p.x == x && p.y == y && p.w == w && p.h == h && p.z == z {
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
		dst = buffer.AppendCursor(dst, reg.Area.X, reg.Area.Y)
		dst = graphics.AppendKittyPlaceID(dst, e.id, p.pid, reg.Area.Width, reg.Area.Height, x, y, w, h, z)
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

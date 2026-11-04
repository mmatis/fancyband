package tiles

import (
	"image"
	"math"

	"fancyband/internal/proto"
)

// Wall shadows darken the floor where it meets a wall, as ambient
// occlusion does: along each side that touches a wall, and into each
// corner where only a diagonal neighbor is a wall. Like filters,
// they change pictures as the client draws them, never the sheet.

// The terrain that casts shadows, and the terrain they fall on, by the
// names in the tile sets' graf-*.prf files.
var (
	shadowWalls = map[string]bool{
		"GRANITE": true, "PERM": true, "MAGMA": true, "QUARTZ": true,
		"MAGMA_K": true, "QUARTZ_K": true, "RUBBLE": true,
	}
	shadowFloors = map[string]bool{
		"FLOOR": true, "PASS_RUBBLE": true, "OPEN": true, "BROKEN": true,
		"LESS": true, "MORE": true,
	}
)

// Neighbors, as bits of a shadow mask.
const (
	ShadowN uint8 = 1 << iota
	ShadowE
	ShadowS
	ShadowW
	ShadowNE
	ShadowSE
	ShadowSW
	ShadowNW
)

const (
	// shadowDepth is how dark the shadow is right at the wall, as a share
	// of the light taken away.
	shadowDepth = 0.55
	// shadowReach is how far the shadow reaches into the tile, as a share
	// of its size.
	shadowReach = 0.28
)

func loadShadowFeats(feats map[[2]uint8]string) (walls, floors map[[2]uint8]bool) {
	walls, floors = map[[2]uint8]bool{}, map[[2]uint8]bool{}
	for at, name := range feats {
		switch {
		case shadowWalls[name]:
			walls[at] = true
		case shadowFloors[name]:
			floors[at] = true
		}
	}
	return walls, floors
}

// IsWall reports whether a cell's terrain casts wall shadows.
func (s *Sheet) IsWall(c proto.PictCell) bool { return s.walls[s.base(c.BgRow, c.BgCol)] }

// IsFloor reports whether wall shadows fall on a cell's terrain.
func (s *Sheet) IsFloor(c proto.PictCell) bool { return s.floors[s.base(c.BgRow, c.BgCol)] }

// ShadowMask returns which of a floor cell's neighbors are walls, from
// wall, which reports whether the neighbor dx, dy grids away is one. A
// diagonal counts only when neither side next to it does, since a side
// already shades that corner.
func ShadowMask(wall func(dx, dy int) bool) uint8 {
	var m uint8
	sides := []struct {
		bit    uint8
		dx, dy int
	}{{ShadowN, 0, -1}, {ShadowE, 1, 0}, {ShadowS, 0, 1}, {ShadowW, -1, 0}}
	for _, sd := range sides {
		if wall(sd.dx, sd.dy) {
			m |= sd.bit
		}
	}
	corners := []struct {
		bit, a, b uint8
		dx, dy    int
	}{
		{ShadowNE, ShadowN, ShadowE, 1, -1}, {ShadowSE, ShadowS, ShadowE, 1, 1},
		{ShadowSW, ShadowS, ShadowW, -1, 1}, {ShadowNW, ShadowN, ShadowW, -1, -1},
	}
	for _, c := range corners {
		if m&(c.a|c.b) == 0 && wall(c.dx, c.dy) {
			m |= c.bit
		}
	}
	return m
}

// Shaded returns the picture for a floor cell with the shadows of the
// walls in mask. Only the floor darkens; whatever stands on it keeps its
// colors. A mask of 0 gives the plain picture.
func (s *Sheet) Shaded(c proto.PictCell, mask uint8) image.Image {
	return s.At(c, 0, 0, 0, mask, LightEdges{})
}

// shade darkens img along the sides and into the corners in mask. Each
// pixel takes the darkest of the shadows that reach it, which fall off
// smoothly from the wall.
func shade(img *image.RGBA, mask uint8) {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	reachX, reachY := math.Max(w*shadowReach, 1), math.Max(h*shadowReach, 1)
	// falloff turns a distance from the wall, as a share of the reach,
	// into how much light to take away.
	falloff := func(d float64) float64 {
		if d >= 1 {
			return 0
		}
		return shadowDepth * (1 - d) * (1 - d)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			// Distances from each side, at the pixel's center, in reaches.
			fx, fy := float64(x-b.Min.X)+0.5, float64(y-b.Min.Y)+0.5
			n, sth := fy/reachY, (h-fy)/reachY
			wst, e := fx/reachX, (w-fx)/reachX
			dark := 0.0
			add := func(bit uint8, d float64) {
				if mask&bit != 0 {
					dark = math.Max(dark, falloff(d))
				}
			}
			add(ShadowN, n)
			add(ShadowS, sth)
			add(ShadowW, wst)
			add(ShadowE, e)
			add(ShadowNE, math.Hypot(n, e))
			add(ShadowSE, math.Hypot(sth, e))
			add(ShadowSW, math.Hypot(sth, wst))
			add(ShadowNW, math.Hypot(n, wst))
			if dark == 0 {
				continue
			}
			i := img.PixOffset(x, y)
			for j := 0; j < 3; j++ {
				img.Pix[i+j] = uint8(float64(img.Pix[i+j])*(1-dark) + 0.5)
			}
		}
	}
}

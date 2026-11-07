package tiles

import (
	"fmt"
	"image"
	"math/rand/v2"
)

// Level colors give each dungeon level's walls and floor a color of their
// own, rolled when the character arrives. Like filters and wall shadows,
// they change pictures as the client draws them, never the sheet, and
// only the map's: sprites and the Tile Editor show tiles as they are.
//
// Most tile sets draw their stone in grays, which have no hue to turn, so
// a shift tints the grays with its hue, as a filter's color range does,
// and turns the hue of colored pixels by the same amount. The gold in a
// vein with treasure keeps its color, so the treasure still shows.

// LevelColors is the shift for one level's walls and one for its floor.
// The zero value changes nothing, as in the town.
type LevelColors struct {
	Wall  Shift `json:"wall"`
	Floor Shift `json:"floor"`
}

// Shift is one color shift. Hue is in degrees: the tint for grays, and
// how far colored pixels turn. Saturation is how strong the tint is,
// added to a gray's colorfulness, and scales a colored pixel's.
// Lightness scales brightness: -1 takes it away, 0 keeps it.
type Shift struct {
	Hue        float64 `json:"hue"`
	Saturation float64 `json:"saturation"`
	Lightness  float64 `json:"lightness"`
}

// String gives the shift in numbers, for a developer tuning the rolls:
// the hue in degrees, then the tint and the lightness.
func (s Shift) String() string {
	return fmt.Sprintf("%.0f°, tint %.0f%%, light %+.0f%%", s.Hue, s.Saturation*100, s.Lightness*100)
}

// The limits of a roll. The tint stays a tint, so stone still looks like
// stone, and floors stay a little weaker than walls, so the walls still
// stand out.
const (
	wallTintMin, wallTintMax   = 0.15, 0.40
	floorTintMin, floorTintMax = 0.10, 0.30
	lightMin, lightMax         = -0.10, 0.10
)

// RollLevelColors returns a new random shift for walls and one for the
// floor, each rolled on its own.
func RollLevelColors(r *rand.Rand) LevelColors {
	roll := func(tintMin, tintMax float64) Shift {
		return Shift{
			Hue:        r.Float64() * 360,
			Saturation: tintMin + r.Float64()*(tintMax-tintMin),
			Lightness:  lightMin + r.Float64()*(lightMax-lightMin),
		}
	}
	return LevelColors{Wall: roll(wallTintMin, wallTintMax), Floor: roll(floorTintMin, floorTintMax)}
}

// levelKind says which shift a terrain tile takes.
type levelKind uint8

const (
	levelWall  levelKind = iota + 1 // stone: every pixel
	levelVein                       // a vein with treasure: its grays only
	levelFloor                      // the plain floor
)

// levelTerrain names the terrain level colors change, by the names in the
// tile sets' graf-*.prf files. Doors, stairs, rubble, and the rest keep
// their colors, so they stand out as they always have.
var levelTerrain = map[string]levelKind{
	"GRANITE": levelWall, "PERM": levelWall, "MAGMA": levelWall, "QUARTZ": levelWall,
	"MAGMA_K": levelVein, "QUARTZ_K": levelVein,
	"FLOOR": levelFloor,
}

// Ranges for recolor: every pixel, and the grays alone.
var (
	everyPixel = &Range{SatTo: 1, LightTo: 1, Limits: true}
	graysOnly  = &Range{SatTo: grayLimit, LightTo: 1, Limits: true}
)

func loadLevelKinds(feats map[[2]uint8]string) map[[2]uint8]levelKind {
	kinds := map[[2]uint8]levelKind{}
	for at, name := range feats {
		if k := levelTerrain[name]; k != 0 {
			kinds[at] = k
		}
	}
	return kinds
}

// LevelColors returns the shift the map's walls and floor take now.
func (s *Sheet) LevelColors() LevelColors { return s.level }

// SetLevelColors draws the map's walls and floor with c from now on, and
// forgets the pictures made with the old colors.
func (s *Sheet) SetLevelColors(c LevelColors) {
	if c == s.level {
		return
	}
	s.level = c
	s.frames = map[frameKey]image.Image{}
}

// levelShift recolors img, the terrain tile at in the sheet, with the
// level's shift for it, if it takes one.
func (s *Sheet) levelShift(img *image.RGBA, at [2]uint8) {
	var sh Shift
	r := everyPixel
	switch s.levelKinds[at] {
	case levelWall:
		sh = s.level.Wall
	case levelVein:
		sh, r = s.level.Wall, graysOnly
	case levelFloor:
		sh = s.level.Floor
	default:
		return
	}
	if sh == (Shift{}) {
		return
	}
	recolor(img, Look{Hue: sh.Hue, Saturation: sh.Saturation, Lightness: sh.Lightness}, r)
}

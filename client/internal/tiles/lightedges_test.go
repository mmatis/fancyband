package tiles

import (
	"image"
	"path/filepath"
	"testing"

	"fancyband/internal/proto"
)

// Adam Bolt's floors, which are flat, and a wall.
var (
	lightDark = proto.PictCell{BgRow: 0, BgCol: 2, FgRow: 0, FgCol: 2}
	lightLit  = proto.PictCell{BgRow: 0, BgCol: 3, FgRow: 0, FgCol: 3}
	lightWall = proto.PictCell{BgRow: 0, BgCol: 4, FgRow: 0, FgCol: 4}
	// An unknown grid.
	lightUnknownGrid = proto.PictCell{BgRow: 0, BgCol: 0, FgRow: 0, FgCol: 0}
)

func adamBolt(t *testing.T) *Sheet {
	t.Helper()
	sheet, err := filepath.Abs("../../../engine/lib/tiles/adam-bolt/16x16.png")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load(proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1, Name: "Adam Bolt", Path: sheet})
	if err != nil {
		t.Skip("no engine tiles in this checkout")
	}
	return s
}

// lightEdgesAt returns the cell at x, y of rows and how to draw its light,
// where 'D' is dark floor, 'L' floor in view, '#' a wall, 'U' an unknown grid,
// and a space nothing.
func lightEdgesAt(s *Sheet, st LightStyle, rows []string, x, y int) (proto.PictCell, LightEdges) {
	at := func(x, y int) (proto.PictCell, bool) {
		if y < 0 || y >= len(rows) || x < 0 || x >= len(rows[y]) {
			return proto.PictCell{}, false
		}
		switch rows[y][x] {
		case 'D':
			return lightDark, true
		case 'L':
			return lightLit, true
		case '#':
			return lightWall, true
		case 'U':
			return lightUnknownGrid, true
		}
		return proto.PictCell{}, false
	}
	c, _ := at(x, y)
	own, _ := s.LightLevel(c)
	return c, s.LightAround(st, own, func(dx, dy int) (proto.PictCell, bool) { return at(x+dx, y+dy) })
}

func lightPicture(s *Sheet, st LightStyle, rows []string, x, y int) *image.RGBA {
	c, e := lightEdgesAt(s, st, rows, x, y)
	return s.At(c, 0, x, y, 0, e).(*image.RGBA)
}

func red(p *image.RGBA, x, y int) int { return int(p.RGBAAt(x, y).R) }

// TestLightLevels reads the floor's three lights, and leaves out the rest.
func TestLightLevels(t *testing.T) {
	s := adamBolt(t)
	if l, ok := s.LightLevel(lightDark); !ok || l != 0 {
		t.Errorf("dark floor: level %d, %v", l, ok)
	}
	if l, ok := s.LightLevel(proto.PictCell{BgCol: 1, FgCol: 1}); !ok || l != 1 {
		t.Errorf("lit floor: level %d, %v", l, ok)
	}
	if l, ok := s.LightLevel(lightLit); !ok || l != 2 {
		t.Errorf("floor in view: level %d, %v", l, ok)
	}
	if _, ok := s.LightLevel(lightWall); ok {
		t.Error("a wall has a light level")
	}
}

// TestLightPlainFloor gives floor with one light around it the plain picture,
// whatever else is beside it, so it is not sent again for each style.
func TestLightPlainFloor(t *testing.T) {
	s := adamBolt(t)
	for _, st := range []LightStyle{LightOff, LightCorners, LightCuts, LightDither} {
		for name, rows := range map[string][]string{
			"open floor": {"DDD", "DDD", "DDD"},
			"walls":      {"###", "#D#", "###"},
			"unknown":    {"   ", " D ", "   "},
		} {
			c, e := lightEdgesAt(s, st, rows, 1, 1)
			if e != (LightEdges{}) || s.At(c, 0, 1, 1, 0, e) != s.Picture(c) {
				t.Errorf("style %d, %s: got %+v, not the plain picture", st, name, e)
			}
		}
	}
}

// TestLightSeams checks that the blend meets where two tiles do, so no step
// shows, and that a tile matches its own light away from the edge.
func TestLightSeams(t *testing.T) {
	s := adamBolt(t)
	rows := []string{"DDLL", "DDLL", "DDLL"}
	dark, lit := s.lums[0][0], s.lums[0][2]
	a, b := lightPicture(s, LightCorners, rows, 1, 1), lightPicture(s, LightCorners, rows, 2, 1)
	// The two pixels beside the seam differ only by the blend's slope, far
	// less than the whole step between the lights.
	if diff := red(b, 0, 8) - red(a, 15, 8); diff < 0 || float64(diff) > (lit-dark)/5 {
		t.Errorf("a step of %d where the tiles meet", diff)
	}
	if got := float64(red(a, 0, 8)); got > dark+1 {
		t.Errorf("the dark tile's far side is %v, its own light is %v", got, dark)
	}
	if got := float64(red(b, 15, 8)); got < lit-1 {
		t.Errorf("the lit tile's far side is %v, its own light is %v", got, lit)
	}
}

// TestLightCornersOnDiagonal checks that the blend meets at a corner too,
// where only tiles on the diagonal differ.
func TestLightCornersOnDiagonal(t *testing.T) {
	s := adamBolt(t)
	rows := []string{"LLD", "LDD", "DDD"}
	// The dark tile at 1, 1 has lit tiles north and west of it.
	p := lightPicture(s, LightCorners, rows, 1, 1)
	if red(p, 0, 0) <= red(p, 15, 15) {
		t.Errorf("the corner toward the light is %d, the far one %d", red(p, 0, 0), red(p, 15, 15))
	}
	if red(p, 15, 15) > int(s.lums[0][0])+1 {
		t.Errorf("the far corner is %d, its own light is %v", red(p, 15, 15), s.lums[0][0])
	}
}

// TestLightCuts fills the corner between two brighter sides up to the
// diagonal, and cuts only the darker tile, so steps make one line.
func TestLightCuts(t *testing.T) {
	s := adamBolt(t)
	rows := []string{"LLD", "LDD", "DDD"}
	dark, lit := int(s.lums[0][0]+0.5), int(s.lums[0][2]+0.5)
	p := lightPicture(s, LightCuts, rows, 1, 1)
	if got := red(p, 1, 1); got != lit {
		t.Errorf("inside the cut: %d, want the light's %d", got, lit)
	}
	if got := red(p, 14, 14); got != dark {
		t.Errorf("past the cut: %d, want its own %d", got, dark)
	}
	if bright := lightPicture(s, LightCuts, rows, 1, 0); red(bright, 15, 15) != lit {
		t.Errorf("the brighter tile changed: %d", red(bright, 15, 15))
	}
	// One brighter side is no step to cut.
	one := lightPicture(s, LightCuts, []string{"DLD", "DDD", "DDD"}, 1, 1)
	if red(one, 1, 1) != dark {
		t.Errorf("a tile with one brighter side was cut: %d", red(one, 1, 1))
	}
}

// TestLightDither draws only whole light levels, and more of the brighter
// one nearer the light.
func TestLightDither(t *testing.T) {
	s := adamBolt(t)
	p := lightPicture(s, LightDither, []string{"DDLL", "DDLL", "DDLL"}, 1, 1)
	dark, lit := int(s.lums[0][0]+0.5), int(s.lums[0][2]+0.5)
	near, far := 0, 0
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			r := red(p, x, y)
			if r != dark && r != lit && r != int(s.lums[0][1]+0.5) {
				t.Fatalf("pixel %d, %d is %d, not a light level", x, y, r)
			}
			if r != dark && x >= 12 {
				near++
			}
			if r != dark && x < 4 {
				far++
			}
		}
	}
	if near <= far {
		t.Errorf("%d brighter pixels near the light, %d far from it", near, far)
	}
}

// TestLightLeavesObjects draws what stands on the floor as it is, and walls
// and animated floors plain.
func TestLightLeavesObjects(t *testing.T) {
	s := adamBolt(t)
	rows := []string{"LLL", "DDD", "DDD"}
	_, e := lightEdgesAt(s, LightCorners, rows, 1, 1)
	onFloor := proto.PictCell{BgRow: 0, BgCol: 2, FgRow: 1, FgCol: 1}
	plain := s.Picture(onFloor).(*image.RGBA)
	lit := s.At(onFloor, 0, 1, 1, 0, e).(*image.RGBA)
	tile := image.NewRGBA(image.Rect(0, 0, 16, 16))
	s.draw(tile, 1, 1)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if tile.RGBAAt(x, y).A == 255 && lit.RGBAAt(x, y) != plain.RGBAAt(x, y) {
				t.Fatalf("the object's pixel at %d, %d changed", x, y)
			}
		}
	}
	if s.At(lightWall, 0, 1, 1, 0, e) != s.Picture(lightWall) {
		t.Error("a wall took light edges")
	}

	at := [2]uint8{0, 60}
	edits := NewEdits()
	edits.Virtual[at] = Virtual{Base: [2]uint8{0, 2}, Filter: Filter{Anim: &Anim{}}}
	s.SetEdits(edits)
	animated := proto.PictCell{BgRow: at[0], BgCol: at[1], FgRow: at[0], FgCol: at[1]}
	if s.At(animated, 0, 1, 1, 0, e) != s.At(animated, 0, 1, 1, 0, LightEdges{}) {
		t.Error("an animated floor took light edges")
	}
}

// TestLightFadesIntoUnknown fades floor toward an unknown grid, as in the town
// at night, in each style, keeps a walled-in tile plain, and keeps a narrow
// known path visible.
func TestLightFadesIntoUnknown(t *testing.T) {
	s := adamBolt(t)
	const last = 15

	// A step: unknown to the north and east of the floor at the middle.
	stair := []string{"UUU", "DDU", "DDD"}
	edge := lightPicture(s, LightCorners, stair, 1, 1)
	plain := s.Picture(lightDark).(*image.RGBA)
	if red(edge, last, 0) >= red(plain, last, 0) {
		t.Errorf("the corner by the unknown grids is %d, the plain floor %d", red(edge, last, 0), red(plain, last, 0))
	}
	if red(edge, 0, last) <= red(edge, last, 0) {
		t.Errorf("no fade: far corner %d, near corner %d", red(edge, 0, last), red(edge, last, 0))
	}
	// The picture comes from the unknown grid, not from having no cell.
	if _, e := lightEdgesAt(s, LightCorners, []string{"   ", " D ", "   "}, 1, 1); e != (LightEdges{}) {
		t.Errorf("no neighbors at all gave %+v", e)
	}
	// Walls take no part.
	if _, e := lightEdgesAt(s, LightCorners, []string{"###", "#D#", "###"}, 1, 1); e != (LightEdges{}) {
		t.Errorf("walls alone gave %+v", e)
	}

	// A known path one grid wide stays visible in every style.
	path := []string{"UUU", "DDD", "UUU"}
	for _, st := range []LightStyle{LightCorners, LightCuts, LightDither} {
		p := lightPicture(s, st, path, 1, 1)
		if red(p, 8, 8) == 0 && red(plain, 8, 8) != 0 {
			t.Errorf("style %d blacks out a path", st)
		}
	}

	// Cuts: the floor's corner between two unknown sides goes dark, and the
	// corner away from them stays as it was.
	cut := lightPicture(s, LightCuts, stair, 1, 1)
	if red(cut, last, 0) >= red(plain, last, 0) {
		t.Errorf("the cut corner is %d, the plain floor %d", red(cut, last, 0), red(plain, last, 0))
	}
	if red(cut, 0, last) != red(plain, 0, last) {
		t.Errorf("the far corner changed: %d, plain %d", red(cut, 0, last), red(plain, 0, last))
	}

	// Dither: pixels are the unknown's dark or the floor's own.
	dark := lightPicture(s, LightDither, stair, 1, 1)
	changed := false
	for y := 0; y <= last; y++ {
		for x := 0; x <= last; x++ {
			if red(dark, x, y) != red(plain, x, y) {
				changed = true
			}
		}
	}
	if !changed {
		t.Error("dither left the floor as it was")
	}
}

// TestLightNearDarkEdges reaches the dark along an edge that faces it, keeps a
// known path lit along its middle, and shows no seam between tiles.
func TestLightNearDarkEdges(t *testing.T) {
	s := adamBolt(t)
	const last = 15
	plain := s.Picture(lightDark).(*image.RGBA)
	// Floor with unknown grids to the north: the top row comes down to their
	// dark, and the middle of the tile keeps its light. The pixel at 8, 8 is
	// just off the exact middle, so it can round a step away.
	near := func(got, want, within int) bool { return got-want <= within && want-got <= within }
	unknown := int(s.unknownLums[0])
	edge := lightPicture(s, LightCorners, []string{"UUU", "DDD", "DDD"}, 1, 1)
	if got := red(edge, 8, 0); !near(got, unknown, 2) {
		t.Errorf("the edge facing the dark is %d, the dark is %d", got, unknown)
	}
	if got, want := red(edge, 8, 8), red(plain, 8, 8); !near(got, want, 1) {
		t.Errorf("the middle is %d, want %d", got, want)
	}
	// A path one grid wide is lit along its middle, across the join of two
	// tiles too.
	path := []string{"UUUU", "DDDD", "UUUU"}
	a := lightPicture(s, LightCorners, path, 1, 1)
	if got, want := red(a, 8, 8), red(plain, 8, 8); !near(got, want, 1) {
		t.Errorf("the path's middle is %d, want %d", got, want)
	}
	if got, want := red(a, last, 8), red(plain, last, 8); !near(got, want, 1) {
		t.Errorf("the path goes dark where tiles join: %d, want %d", got, want)
	}
	// No seam: each pair of tiles side by side agrees along their join, on
	// shapes with the dark, a change of light, and a wall.
	for _, rows := range [][]string{
		{"UUUU", "DDDD", "UUUU"},
		{"UUUU", "DDDU", "DDDD"},
		{"UUDD", "UDDD", "DDDD"},
		{"UDLL", "DDLL", "DDLL"},
		{"#UUD", "#DDD", "#DDD"},
		{"UUUU", "UDLU", "UUUU"},
	} {
		x, y := 1, 1
		c1, _ := lightEdgesAt(s, LightCorners, rows, x, y)
		c2, _ := lightEdgesAt(s, LightCorners, rows, x+1, y)
		p1, p2 := lightPicture(s, LightCorners, rows, x, y), lightPicture(s, LightCorners, rows, x+1, y)
		base1, base2 := s.Picture(c1).(*image.RGBA), s.Picture(c2).(*image.RGBA)
		for row := 0; row <= last; row++ {
			// As shares of each tile's own light, since the two may differ.
			l1 := float64(red(p1, last, row)) / float64(max(red(base1, last, row), 1)) * lumOf(s, c1)
			l2 := float64(red(p2, 0, row)) / float64(max(red(base2, 0, row), 1)) * lumOf(s, c2)
			if diff := l1 - l2; diff > 6 || diff < -6 {
				t.Errorf("%q: a seam at row %d: %.0f beside %.0f", rows, row, l1, l2)
				break
			}
		}
	}
}

// lumOf is the plain floor's brightness at a cell's light.
func lumOf(s *Sheet, c proto.PictCell) float64 {
	own, _ := s.LightLevel(c)
	return s.lums[0][own]
}

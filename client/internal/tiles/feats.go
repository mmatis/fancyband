package tiles

import (
	"bufio"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// loadFeats reads which terrain each sheet tile shows from the tile
// set's graf-*.prf files, which sit beside its sheet. Lines look like
// "feat:LAVA:torch:0x98:0xB8": the terrain's name, the light, and the
// row and column with the high bit set. A tile two kinds of terrain
// share keeps the first name.
func loadFeats(sheetPath string) map[[2]uint8]string {
	feats := map[[2]uint8]string{}
	scanFeats(sheetPath, func(_ int, name, _ string, at [2]uint8) {
		if _, seen := feats[at]; !seen {
			feats[at] = name
		}
	})
	return feats
}

// scanFeats calls found for each terrain line in the tile set's graf-*.prf
// files, with the number of the file, the terrain's name in capitals, its
// light, and its tile.
func scanFeats(sheetPath string, found func(group int, name, light string, at [2]uint8)) {
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(sheetPath), "graf-*.prf"))
	for group, name := range files {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Split(strings.TrimSpace(sc.Text()), ":")
			if len(fields) != 5 || fields[0] != "feat" {
				continue
			}
			row, err1 := strconv.ParseUint(fields[3], 0, 8)
			col, err2 := strconv.ParseUint(fields[4], 0, 8)
			if err1 == nil && err2 == nil {
				found(group, strings.ToUpper(fields[1]), fields[2], [2]uint8{uint8(row) & 0x7F, uint8(col) & 0x7F})
			}
		}
		f.Close()
	}
}

func toHSV(p color.RGBA) (h, s, v float64) {
	r, g, b := float64(p.R)/255, float64(p.G)/255, float64(p.B)/255
	hi, lo := max(r, g, b), min(r, g, b)
	v = hi
	if hi == 0 {
		return 0, 0, 0
	}
	s = (hi - lo) / hi
	if hi == lo {
		return 0, s, v
	}
	switch hi {
	case r:
		h = 60 * (g - b) / (hi - lo)
	case g:
		h = 60 * (2 + (b-r)/(hi-lo))
	default:
		h = 60 * (4 + (r-g)/(hi-lo))
	}
	if h < 0 {
		h += 360
	}
	return h, s, v
}

func fromHSV(h, s, v float64, a uint8) color.RGBA {
	for h < 0 {
		h += 360
	}
	c := v * s
	hp := h / 60
	x := c * (1 - abs(mod2(hp)-1))
	var r, g, b float64
	switch int(hp) % 6 {
	case 0:
		r, g = c, x
	case 1:
		r, g = x, c
	case 2:
		g, b = c, x
	case 3:
		g, b = x, c
	case 4:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := v - c
	to8 := func(f float64) uint8 { return uint8(min(max((f+m)*255+0.5, 0), 255)) }
	return color.RGBA{to8(r), to8(g), to8(b), a}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// mod2 returns f modulo 2, for f >= 0.
func mod2(f float64) float64 {
	return f - 2*float64(int(f/2))
}

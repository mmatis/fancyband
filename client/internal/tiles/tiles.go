// Package tiles cuts the engine's tile sheet into pictures for the map.
// It has no Limoni imports; internal/ui hands its pictures to Limoni.
package tiles

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"fancyband/internal/proto"
)

// Sheet is a loaded tile set.
type Sheet struct {
	img          image.Image
	tileW, tileH int
	cache        map[proto.PictCell]image.Image
	// lava holds the sheet positions of lava tiles, and shifted their
	// pictures shifted toward yellow (see Shifted).
	lava    map[[2]uint8]bool
	shifted map[shiftKey]image.Image
	sprites map[spriteKey]image.Image
}

// spriteKey names a sprite drawn on one background color.
type spriteKey struct {
	row, col uint8
	bg       color.RGBA
}

// Load reads the sheet the engine named in TILESET. The client and the
// engine always run on the same machine, so the path is local.
func Load(ts proto.TileSet) (*Sheet, error) {
	f, err := os.Open(ts.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("tiles: %s: %w", ts.Path, err)
	}
	if ts.TileW == 0 || ts.TileH == 0 {
		return nil, fmt.Errorf("tiles: %s: zero tile size", ts.Name)
	}
	return &Sheet{
		img:     img,
		tileW:   int(ts.TileW),
		tileH:   int(ts.TileH),
		cache:   map[proto.PictCell]image.Image{},
		lava:    loadLava(ts.Path),
		shifted: map[shiftKey]image.Image{},
		sprites: map[spriteKey]image.Image{},
	}, nil
}

// Picture returns the picture for a cell: its tile drawn over its terrain
// tile, on black. Every sheet the game ships has see-through pixels, and
// the SDL2 front end always blends, whatever the tile set's alpha flag
// says, so this does too. The black base keeps the terminal's own
// background from showing through. The same cell always gets the same
// picture value, which lets Limoni send each picture to the terminal once.
func (s *Sheet) Picture(c proto.PictCell) image.Image {
	if p, ok := s.cache[c]; ok {
		return p
	}
	out := image.NewRGBA(image.Rect(0, 0, s.tileW, s.tileH))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	s.draw(out, c.BgRow, c.BgCol)
	if c.FgRow != c.BgRow || c.FgCol != c.BgCol {
		s.draw(out, c.FgRow, c.FgCol)
	}
	s.cache[c] = out
	return out
}

func (s *Sheet) draw(dst *image.RGBA, row, col uint8) {
	b := s.img.Bounds()
	src := image.Pt(b.Min.X+int(col)*s.tileW, b.Min.Y+int(row)*s.tileH)
	draw.Draw(dst, dst.Bounds(), s.img, src, draw.Over)
}

// Sprite returns the tile at row, col alone, its transparent parts showing
// bg, for drawing outside the map, such as beside a name in a list.
// Pictures go to the terminal without their transparency, which Sixel
// lacks, so the background is drawn in.
func (s *Sheet) Sprite(row, col uint8, bg color.RGBA) image.Image {
	k := spriteKey{row, col, bg}
	if p, ok := s.sprites[k]; ok {
		return p
	}
	out := image.NewRGBA(image.Rect(0, 0, s.tileW, s.tileH))
	draw.Draw(out, out.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	s.draw(out, row, col)
	s.sprites[k] = out
	return out
}

// Len reports how many pictures are cached.
func (s *Sheet) Len() int { return len(s.cache) }

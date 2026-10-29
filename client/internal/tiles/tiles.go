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
	"time"

	"fancyband/internal/proto"
)

// Sheet is a loaded tile set.
type Sheet struct {
	img          image.Image
	tileW, tileH int
	// edits are the player's filters and virtual tiles (see Edits).
	edits *Edits
	// frames caches each picture the map has asked for, and sprites the
	// tiles drawn alone.
	frames  map[frameKey]image.Image
	sprites map[spriteKey]image.Image
	// walls and floors hold the sheet positions of wall and floor tiles.
	walls, floors map[[2]uint8]bool
}

// spriteKey names a sprite drawn on one background color.
type spriteKey struct {
	row, col uint8
	bg       color.RGBA
}

// layerKey names one of a filtered tile's pictures: a step of its
// animation and a random variant.
type layerKey struct{ step, variant uint8 }

// frameKey names one picture for a map grid: its cell, the picture of
// each layer, and the wall shadows on its floor.
type frameKey struct {
	cell   proto.PictCell
	fg, bg layerKey
	mask   uint8
}

// maxFrames bounds the cache, which animated tiles in many places fill.
// Starting it again costs only sending some pictures again.
const maxFrames = 8192

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
	feats := loadFeats(ts.Path)
	walls, floors := loadShadowFeats(feats)
	return &Sheet{
		img:     img,
		tileW:   int(ts.TileW),
		tileH:   int(ts.TileH),
		edits:   NewEdits(),
		frames:  map[frameKey]image.Image{},
		sprites: map[spriteKey]image.Image{},
		walls:   walls,
		floors:  floors,
	}, nil
}

// Edits returns the filters in use. Change them only through SetEdits.
func (s *Sheet) Edits() *Edits { return s.edits }

// SetEdits uses e from now on, and forgets the pictures made with the
// old edits.
func (s *Sheet) SetEdits(e *Edits) {
	if e == nil {
		e = NewEdits()
	}
	s.edits = e
	s.frames = map[frameKey]image.Image{}
	s.sprites = map[spriteKey]image.Image{}
}

// Forget drops the pictures that use the tiles at, after a change to
// their edits in place. Other pictures keep their values, so Limoni does
// not send them again.
func (s *Sheet) Forget(at ...[2]uint8) {
	gone := map[[2]uint8]bool{}
	for _, a := range at {
		gone[a] = true
	}
	s.forget(gone)
}

func (s *Sheet) forget(gone map[[2]uint8]bool) {
	for k := range s.frames {
		if gone[[2]uint8{k.cell.FgRow, k.cell.FgCol}] || gone[[2]uint8{k.cell.BgRow, k.cell.BgCol}] {
			delete(s.frames, k)
		}
	}
	for k := range s.sprites {
		if gone[[2]uint8{k.row, k.col}] {
			delete(s.sprites, k)
		}
	}
}

// filterOf returns the real tile behind the tile at row, col and the
// filter it is drawn through, or nil for none.
func (s *Sheet) filterOf(row, col uint8) ([2]uint8, *Filter) {
	at := [2]uint8{row, col}
	if v, ok := s.edits.Virtual[at]; ok {
		return v.Base, &v.Filter
	}
	return at, nil
}

// base returns the real tile behind the tile at row, col.
func (s *Sheet) base(row, col uint8) [2]uint8 {
	if v, ok := s.edits.Virtual[[2]uint8{row, col}]; ok {
		return v.Base
	}
	return [2]uint8{row, col}
}

// Picture returns the picture for a cell: its tile drawn over its terrain
// tile, on black, each through its filter as it is at rest. Every sheet
// the game ships has see-through pixels, and the SDL2 front end always
// blends, whatever the tile set's alpha flag says, so this does too. The
// black base keeps the terminal's own background from showing through.
// The same cell always gets the same picture value, which lets Limoni
// send each picture to the terminal once.
func (s *Sheet) Picture(c proto.PictCell) image.Image {
	return s.frame(frameKey{cell: c})
}

// At returns the picture for a cell at a grid gx, gy on the map, at time
// t into the animations, with the shadows of the walls in mask on its
// floor. Grids differ only where a filter spreads its animation or picks
// a random variant.
func (s *Sheet) At(c proto.PictCell, t time.Duration, gx, gy int, mask uint8) image.Image {
	k := frameKey{cell: c}
	if mask != 0 && s.IsFloor(c) {
		k.mask = mask
	}
	if _, f := s.filterOf(c.BgRow, c.BgCol); f != nil {
		step, variant := f.StepAt(t, gx, gy)
		k.bg = layerKey{uint8(step), uint8(variant)}
	}
	if c.FgRow != c.BgRow || c.FgCol != c.BgCol {
		if _, f := s.filterOf(c.FgRow, c.FgCol); f != nil {
			step, variant := f.StepAt(t, gx, gy)
			k.fg = layerKey{uint8(step), uint8(variant)}
		}
	}
	return s.frame(k)
}

// Animated reports whether a cell's picture changes over time.
func (s *Sheet) Animated(c proto.PictCell) bool {
	_, bg := s.filterOf(c.BgRow, c.BgCol)
	_, fg := s.filterOf(c.FgRow, c.FgCol)
	return bg.Animated() || fg.Animated()
}

func (s *Sheet) frame(k frameKey) image.Image {
	if p, ok := s.frames[k]; ok {
		return p
	}
	c := k.cell
	out := image.NewRGBA(image.Rect(0, 0, s.tileW, s.tileH))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), s.layer(c.BgRow, c.BgCol, k.bg, true), image.Point{}, draw.Over)
	if k.mask != 0 {
		shade(out, k.mask)
	}
	if c.FgRow != c.BgRow || c.FgCol != c.BgCol {
		draw.Draw(out, out.Bounds(), s.layer(c.FgRow, c.FgCol, k.fg, false), image.Point{}, draw.Over)
	}
	if len(s.frames) >= maxFrames {
		s.frames = map[frameKey]image.Image{}
	}
	s.frames[k] = out
	return out
}

// layer returns the tile at row, col alone on see-through pixels,
// through its filter at a step.
func (s *Sheet) layer(row, col uint8, k layerKey, terrain bool) *image.RGBA {
	at, f := s.filterOf(row, col)
	out := image.NewRGBA(image.Rect(0, 0, s.tileW, s.tileH))
	s.draw(out, at[0], at[1])
	if !f.Plain() {
		f.apply(out, int(k.step), int(k.variant), terrain)
	}
	return out
}

// Filtered returns the tile at row, col through f, at a step and
// variant, on bg, for a preview of a filter that is not in use yet.
func (s *Sheet) Filtered(row, col uint8, f Filter, step, variant int, terrain bool, bg color.RGBA) image.Image {
	at := s.base(row, col)
	out := image.NewRGBA(image.Rect(0, 0, s.tileW, s.tileH))
	draw.Draw(out, out.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	tile := image.NewRGBA(out.Bounds())
	s.draw(tile, at[0], at[1])
	f.apply(tile, step, variant, terrain)
	draw.Draw(out, out.Bounds(), tile, image.Point{}, draw.Over)
	return out
}

func (s *Sheet) draw(dst *image.RGBA, row, col uint8) {
	b := s.img.Bounds()
	src := image.Pt(b.Min.X+int(col)*s.tileW, b.Min.Y+int(row)*s.tileH)
	draw.Draw(dst, dst.Bounds(), s.img, src, draw.Over)
}

// TileSize returns the size of one tile on the sheet, in pixels.
func (s *Sheet) TileSize() (w, h int) { return s.tileW, s.tileH }

// Sprite returns the tile at row, col alone, its transparent parts showing
// bg, for drawing outside the map, such as beside a name in a list. A
// picture placed without transparency loses its see-through parts, so
// the background is drawn in; a bg with zero alpha keeps them
// see-through, for a picture placed with transparency.
func (s *Sheet) Sprite(row, col uint8, bg color.RGBA) image.Image {
	k := spriteKey{row, col, bg}
	if p, ok := s.sprites[k]; ok {
		return p
	}
	out := image.NewRGBA(image.Rect(0, 0, s.tileW, s.tileH))
	draw.Draw(out, out.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), s.layer(row, col, layerKey{}, false), image.Point{}, draw.Over)
	s.sprites[k] = out
	return out
}

// Size returns how many tiles the sheet has across and down.
func (s *Sheet) Size() (cols, rows int) {
	b := s.img.Bounds()
	return b.Dx() / s.tileW, b.Dy() / s.tileH
}

// Window returns part of the sheet, cols tiles across and rows down from
// the tile at row, col, drawn on bg, for a tile picker. Tiles past the
// sheet's edge are bg.
func (s *Sheet) Window(row, col, rows, cols int, bg color.RGBA) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, cols*s.tileW, rows*s.tileH))
	draw.Draw(out, out.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	b := s.img.Bounds()
	src := image.Pt(b.Min.X+col*s.tileW, b.Min.Y+row*s.tileH)
	draw.Draw(out, out.Bounds(), s.img, src, draw.Over)
	return out
}

// Len reports how many pictures are cached.
func (s *Sheet) Len() int { return len(s.frames) }

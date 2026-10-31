package tiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Edits are the player's filters for one tile set. A filter on one
// monster, object, or terrain alone needs a tile of its own, since the
// engine names only a tile, not what stands on it. So the editor gives
// such an entry a virtual tile: a row and column past the end of the
// sheet, which the engine passes through to the client like any other.
// Edits say which real tile each virtual one shows, through which
// filter.
type Edits struct {
	Virtual map[[2]uint8]Virtual
}

// Virtual is a virtual tile: a real tile on the sheet seen through a
// filter. Layer names the layer whose file it belongs to, or "" for
// none.
type Virtual struct {
	Base   [2]uint8
	Filter Filter
	Layer  string
}

// The layers of changes, as the engine names them, lowest first: the
// defaults Fancyband ships, the player's own for every character, and
// one character's. Each has its own file of virtual tiles, and a higher
// layer's wins.
const (
	LayerDefaults  = "defaults"
	LayerGlobal    = "global"
	LayerCharacter = "character"
)

// Layers lists the layers, lowest first.
var Layers = []string{LayerDefaults, LayerGlobal, LayerCharacter}

// band returns the columns a layer's new virtual tiles take, so two
// layers never pick the same one: the global layer's start at column
// 0, where they were before there were layers.
func band(layer string) (lo, hi int) {
	switch layer {
	case LayerGlobal:
		return 0, 42
	case LayerCharacter:
		return 43, 85
	case LayerDefaults:
		return 86, 127
	}
	return 0, 127
}

// MaxRow is the last row a virtual tile can use. The engine refuses rows
// past 126, since it treats an attribute of 255 as a marker.
const MaxRow = 126

// NewEdits returns edits with no virtual tiles.
func NewEdits() *Edits {
	return &Edits{Virtual: map[[2]uint8]Virtual{}}
}

// EditsPath returns the file that holds the edits, beside the engine's
// file of changes, prf: tiles/<set>.json for tiles/<set>.prf.
func EditsPath(prf string) string {
	return strings.TrimSuffix(prf, filepath.Ext(prf)) + ".json"
}

type editsFile struct {
	Virtual []virtualFile `json:"virtual,omitempty"`
}

type virtualFile struct {
	Tile   [2]uint8 `json:"tile"`
	Base   [2]uint8 `json:"base"`
	Filter Filter   `json:"filter"`
}

// LoadEdits reads the edits at path. A missing file gives no edits.
func LoadEdits(path string) (*Edits, error) {
	e := NewEdits()
	return e, e.load(path, "")
}

// LoadLayers reads each layer's edits, from the files paths names by
// layer, lowest first, so a higher layer's virtual tile wins. A layer
// with no path or no file adds nothing. It returns the first error and
// keeps what it could read.
func LoadLayers(paths map[string]string) (*Edits, error) {
	e := NewEdits()
	var first error
	for _, l := range Layers {
		if paths[l] == "" {
			continue
		}
		if err := e.load(paths[l], l); err != nil && first == nil {
			first = err
		}
	}
	return e, first
}

// load adds the edits at path to e, as layer's.
func (e *Edits) load(path, layer string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	var f editsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, v := range f.Virtual {
		if v.Tile[0] > MaxRow || v.Tile[1] > 127 {
			continue
		}
		e.Virtual[v.Tile] = Virtual{Base: v.Base, Filter: v.Filter, Layer: layer}
	}
	return nil
}

// Save writes every virtual tile to path, or removes the file when there
// are none.
func (e *Edits) Save(path string) error {
	return e.save(path, func(Virtual) bool { return true })
}

// SaveLayer writes a layer's virtual tiles to path, or removes the file
// when it has none.
func (e *Edits) SaveLayer(path, layer string) error {
	return e.save(path, func(v Virtual) bool { return v.Layer == layer })
}

func (e *Edits) save(path string, keep func(Virtual) bool) error {
	var f editsFile
	for t, v := range e.Virtual {
		if keep(v) {
			f.Virtual = append(f.Virtual, virtualFile{Tile: t, Base: v.Base, Filter: v.Filter})
		}
	}
	sort.Slice(f.Virtual, func(i, j int) bool {
		a, b := f.Virtual[i].Tile, f.Virtual[j].Tile
		return a[0] < b[0] || a[0] == b[0] && a[1] < b[1]
	})
	if len(f.Virtual) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func sameFilter(a, b Filter) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Clone returns a copy that can change without changing e.
func (e *Edits) Clone() *Edits {
	c := &Edits{Virtual: make(map[[2]uint8]Virtual, len(e.Virtual))}
	for t, v := range e.Virtual {
		v.Filter = v.Filter.Clone()
		c.Virtual[t] = v
	}
	return c
}

// Clone returns a copy of f that shares nothing with it.
func (f Filter) Clone() Filter {
	if f.Range != nil {
		r := *f.Range
		f.Range = &r
	}
	if f.Anim != nil {
		a := *f.Anim
		f.Anim = &a
	}
	if f.SparkleColor != nil {
		c := *f.SparkleColor
		f.SparkleColor = &c
	}
	return f
}

// FreeTile returns a virtual tile no edit uses yet, in the rows below
// the sheet's last, rows high. It reports false when every one is taken.
func (e *Edits) FreeTile(rows int) ([2]uint8, bool) {
	return e.FreeTileIn(rows, "")
}

// FreeTileIn is FreeTile in the columns a layer's new virtual tiles take.
func (e *Edits) FreeTileIn(rows int, layer string) ([2]uint8, bool) {
	lo, hi := band(layer)
	for r := rows; r <= MaxRow; r++ {
		for c := lo; c <= hi; c++ {
			t := [2]uint8{uint8(r), uint8(c)}
			if _, used := e.Virtual[t]; !used {
				return t, true
			}
		}
	}
	return [2]uint8{}, false
}

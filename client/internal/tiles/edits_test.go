package tiles

import (
	"path/filepath"
	"testing"
)

// TestFreeTileBands checks that each layer's new virtual tiles take
// their own columns, so two layers never pick the same tile.
func TestFreeTileBands(t *testing.T) {
	e := NewEdits()
	seen := map[[2]uint8]string{}
	for _, l := range Layers {
		lo, hi := band(l)
		for i := 0; i < 3; i++ {
			v, ok := e.FreeTileIn(30, l)
			if !ok || v[0] != 30 || int(v[1]) < lo || int(v[1]) > hi {
				t.Fatalf("%s's tile %d: %v %v, want row 30, columns %d to %d", l, i, v, ok, lo, hi)
			}
			if other, dup := seen[v]; dup {
				t.Fatalf("%s took %v, which %s has", l, v, other)
			}
			seen[v] = l
			e.Virtual[v] = Virtual{Layer: l}
		}
	}
	// A full band goes on to the next row.
	e = NewEdits()
	for c := 43; c <= 85; c++ {
		e.Virtual[[2]uint8{30, uint8(c)}] = Virtual{}
	}
	if v, ok := e.FreeTileIn(30, LayerCharacter); !ok || v != [2]uint8{31, 43} {
		t.Errorf("past a full row: %v %v", v, ok)
	}
}

// TestLoadLayers loads three layers' files: a higher layer's virtual
// tile wins, each tile knows its layer, and saving a layer writes only
// its own.
func TestLoadLayers(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{}
	write := func(layer string, tiles map[[2]uint8]Virtual) {
		e := NewEdits()
		for k, v := range tiles {
			e.Virtual[k] = v
		}
		paths[layer] = filepath.Join(dir, layer+".json")
		if err := e.Save(paths[layer]); err != nil {
			t.Fatal(err)
		}
	}
	shared := [2]uint8{40, 1}
	write(LayerDefaults, map[[2]uint8]Virtual{shared: {Base: [2]uint8{1, 1}}, {40, 90}: {Base: [2]uint8{2, 2}}})
	write(LayerCharacter, map[[2]uint8]Virtual{shared: {Base: [2]uint8{3, 3}}})
	// No global file: that layer adds nothing.
	paths[LayerGlobal] = filepath.Join(dir, "missing.json")

	e, err := LoadLayers(paths)
	if err != nil {
		t.Fatal(err)
	}
	if v := e.Virtual[shared]; v.Base != [2]uint8{3, 3} || v.Layer != LayerCharacter {
		t.Errorf("the shared tile: %+v, want the character's", v)
	}
	if v := e.Virtual[[2]uint8{40, 90}]; v.Layer != LayerDefaults {
		t.Errorf("the defaults' own tile: %+v", v)
	}

	out := filepath.Join(dir, "out.json")
	if err := e.SaveLayer(out, LayerDefaults); err != nil {
		t.Fatal(err)
	}
	got, err := LoadEdits(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Virtual) != 1 || got.Virtual[[2]uint8{40, 90}].Base != [2]uint8{2, 2} {
		t.Errorf("the defaults saved: %+v", got.Virtual)
	}
}

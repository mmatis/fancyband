package tiles

import (
	"os"
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

// TestMirrorDir checks that the copy ends up the same as the source: new
// and changed files arrive, files the source lost go, and a folder is its
// own copy.
func TestMirrorDir(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "made", "copy")
	write := func(dir, name, text string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(src, "a.prf", "new")
	write(src, "b.json", "same")
	write(dst, "a.prf", "old")
	write(dst, "b.json", "same")
	write(dst, "gone.prf", "removed from the source")
	if err := MirrorDir(src, dst); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a.prf": "new", "b.json": "same"} {
		if got, err := os.ReadFile(filepath.Join(dst, name)); err != nil || string(got) != want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "gone.prf")); !os.IsNotExist(err) {
		t.Errorf("gone.prf is still there: %v", err)
	}
	if err := MirrorDir(src, src); err != nil {
		t.Errorf("mirroring a folder onto itself: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(src, "a.prf")); string(got) != "new" {
		t.Errorf("mirroring a folder onto itself left a.prf as %q", got)
	}
}

package e2e

import (
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// options asks for OPTIONS and returns the answer.
func (g *game) options(id int) proto.EventData {
	g.t.Helper()
	if r := g.command(id, "OPTIONS", nil); !r.OK {
		g.t.Fatalf("OPTIONS: %+v", r)
	}
	return g.lastEvent("OPTIONS")
}

func (g *game) option(id int, name string) proto.OptionData {
	g.t.Helper()
	o, ok := g.options(id).Option(name)
	if !ok {
		g.t.Fatalf("no option %s", name)
	}
	return o
}

// TestOptions reads and sets options with OPTIONS and SET_OPTION: an
// interface option, a number, a birth option outside birth, and a cheat
// option, which marks the character.
func TestOptions(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits"})
	g.waitEvent("WAIT")
	before := g.options(1)
	if before.Loaded {
		t.Error("OPTIONS says a character loaded at the splash screen")
	}
	if o, ok := before.Option("solid_walls"); !ok || o.Editable || o.Page != "interface" {
		t.Errorf("solid_walls before a character: %+v", o)
	}
	if r := g.command(2, "SET_OPTION", map[string]any{"name": "solid_walls", "value": true}); r.OK {
		t.Error("SET_OPTION worked with no character")
	}

	g.createCharacter()
	g.waitEvent("WAIT")
	if o := g.option(3, "solid_walls"); o.Value || !o.Editable {
		t.Fatalf("solid_walls in the game: %+v", o)
	}
	if r := g.command(4, "SET_OPTION", map[string]any{"name": "solid_walls", "value": true}); !r.OK {
		t.Fatalf("SET_OPTION solid_walls: %+v", r)
	}
	if o := g.option(5, "solid_walls"); !o.Value {
		t.Error("solid_walls is still off")
	}

	if r := g.command(6, "SET_OPTION", map[string]any{"name": "hitpoint_warn", "value": 5}); !r.OK {
		t.Fatalf("SET_OPTION hitpoint_warn: %+v", r)
	}
	if n := g.options(7).Numbers["hitpoint_warn"]; n != 5 {
		t.Errorf("hitpoint_warn %d, want 5", n)
	}
	if r := g.command(8, "SET_OPTION", map[string]any{"name": "hitpoint_warn", "value": 10}); r.OK {
		t.Error("hitpoint_warn took 10")
	}

	if o := g.option(9, "birth_randarts"); o.Editable {
		t.Errorf("birth_randarts is editable after birth: %+v", o)
	}
	if r := g.command(10, "SET_OPTION", map[string]any{"name": "birth_randarts", "value": true}); r.OK {
		t.Error("a birth option changed after birth")
	}

	if r := g.command(11, "SET_OPTION", map[string]any{"name": "cheat_hear", "value": true}); !r.OK {
		t.Fatalf("SET_OPTION cheat_hear: %+v", r)
	}
	if o := g.option(12, "score_hear"); !o.Value || o.Editable {
		t.Errorf("score_hear after cheating: %+v", o)
	}
	if r := g.command(13, "SET_OPTION", map[string]any{"name": "score_hear", "value": false}); r.OK {
		t.Error("the cheat mark was cleared")
	}
}

// TestOptionDefaults saves the interface page as the defaults and sees a
// new character start with it.
func TestOptionDefaults(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits"})
	g.createCharacter()
	g.waitEvent("WAIT")
	g.command(1, "SET_OPTION", map[string]any{"name": "solid_walls", "value": true})
	if r := g.command(2, "SAVE_OPTION_DEFAULTS", map[string]any{"page": "interface"}); !r.OK {
		t.Fatalf("SAVE_OPTION_DEFAULTS: %+v", r)
	}
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()

	// A second character in a save folder of its own, so the game does
	// not offer to base it on the first; the defaults are in the user
	// folder.
	d2 := enginetest.NewDirs(t)
	d2.User, d2.Archive = d.User, d.Archive
	g = startWith(t, dargs(d2, "-n"), []string{"events", "waits"})
	g.waitEvent("WAIT")
	if o := g.option(1, "solid_walls"); !o.Value {
		t.Error("the defaults for a new character lack solid_walls")
	}
	g.createCharacter()
	g.waitEvent("WAIT")
	if o := g.option(2, "solid_walls"); !o.Value {
		t.Error("the new character does not have solid_walls")
	}
}

// TestGraphics switches from text to tiles, to a bigger size, to another
// set, and back to text, checking each TILESET and the map.
func TestGraphics(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits"})
	g.createCharacter()
	g.waitEvent("WAIT")
	tiles := func() int {
		n := 0
		g.screen().Tiles(func(int, int, proto.PictCell) { n++ })
		return n
	}
	if g.set.TileSet != nil || tiles() != 0 {
		t.Fatal("tiles before GRAPHICS")
	}
	steps := []struct {
		set, scale     int
		cellsW, cellsH uint8
	}{
		{2, 1, 2, 1},
		{2, 2, 4, 2},
		{3, 1, 2, 1},
		{0, 1, 0, 0},
	}
	for i, st := range steps {
		if r := g.command(10+i, "GRAPHICS", map[string]any{"set": st.set, "scale": st.scale}); !r.OK {
			t.Fatalf("GRAPHICS %+v: %+v", st, r)
		}
		// The redraw may have come with the command's result.
		redrawn := func(*grid.Grid) bool {
			return st.set == 0 && tiles() == 0 || st.set > 0 && tiles() > 20
		}
		if !redrawn(nil) {
			g.waitFor("the redraw", redrawn)
		}
		ts := g.set.TileSet
		if st.set == 0 {
			if ts != nil && ts.ID != 0 {
				t.Errorf("TILESET after set 0: %+v", *ts)
			}
			continue
		}
		if ts == nil || int(ts.ID) != st.set || ts.CellsW != st.cellsW || ts.CellsH != st.cellsH {
			t.Errorf("TILESET after %+v: %+v", st, ts)
		}
		if g.screen().Contains("@") {
			t.Errorf("the player is a letter with tile set %d", st.set)
		}
	}
	if !g.screen().Contains("@") {
		t.Error("no @ after going back to text")
	}
	gr := g.options(21).Graphics
	if gr == nil || gr.Set != 0 || len(gr.Sets) < 4 || gr.Sets[1].ID != 2 || gr.Sets[1].Name != "Adam Bolt's tiles" {
		t.Errorf("OPTIONS graphics %+v", gr)
	}
	if r := g.command(20, "GRAPHICS", map[string]any{"set": 99}); r.OK {
		t.Error("GRAPHICS took tile set 99")
	}
}

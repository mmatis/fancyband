package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
	"fancyband/internal/session"
	"fancyband/internal/tiles"
)

// levelColorsModel returns a model in a game, with Adam Bolt's tiles.
func levelColorsModel(t *testing.T, cfg Config) *Model {
	t.Helper()
	sheet, err := filepath.Abs("../../../engine/lib/tiles/adam-bolt/16x16.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sheet); err != nil {
		t.Skip("no engine tiles in this checkout")
	}
	cfg.Tiles = 2
	m := New(cfg)
	m.handleBatch(session.Batch{Msgs: []proto.Message{
		proto.TermOpen{Term: 0, Width: 80, Height: 24},
		proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1, Name: "Adam Bolt's tiles", Path: sheet},
		proto.Fresh{Term: 0},
	}})
	m.handleEvent(proto.Event{JSON: []byte(`{"event": "ENTER_GAME"}`)})
	return m
}

// newLevel sends the NEW_LEVEL event the engine sends on arriving at
// depth, which leaves the depth out for the town.
func newLevel(m *Model, depth int) bool {
	ev := `{"event": "NEW_LEVEL"}`
	if depth > 0 {
		ev = fmt.Sprintf(`{"event": "NEW_LEVEL", "depth": %d}`, depth)
	}
	return m.handleEvent(proto.Event{JSON: []byte(ev)})
}

// TestLevelColorsRoll checks that each dungeon level rolls new colors for
// the sheet, that the town has none, and that the setting turns them off.
func TestLevelColorsRoll(t *testing.T) {
	m := levelColorsModel(t, Config{Width: 80, Height: 24})
	if !newLevel(m, 3) {
		t.Error("NEW_LEVEL does not redraw")
	}
	first := m.sheet.LevelColors()
	if first == (tiles.LevelColors{}) || first != m.levelColors {
		t.Fatalf("level 3 colors %+v, sheet %+v", m.levelColors, first)
	}
	if first.Wall == first.Floor {
		t.Error("the walls and the floor rolled the same shift")
	}
	newLevel(m, 4)
	if m.sheet.LevelColors() == first {
		t.Error("a new level kept the old colors")
	}
	newLevel(m, 0)
	if m.sheet.LevelColors() != (tiles.LevelColors{}) {
		t.Errorf("the town has colors %+v", m.sheet.LevelColors())
	}
	off := false
	m.settings.LevelColors = &off
	newLevel(m, 5)
	if m.sheet.LevelColors() != (tiles.LevelColors{}) {
		t.Error("level colors with the setting off")
	}
	// A new tile set takes the level's colors too.
	on := true
	m.settings.LevelColors = &on
	m.rollLevelColors()
	want := m.levelColors
	m.useTileSet(*m.terms.TileSet)
	if m.sheet.LevelColors() != want {
		t.Error("the reloaded tile set lost the level's colors")
	}
}

// screenRows draws the model and returns the screen as text rows.
func screenRows(m *Model, w, h int) []string {
	buf := buffer.NewBuffer(cell.NewRect(0, 0, uint16(w), uint16(h)))
	f := terminal.NewFrame(buf, terminal.NewFocusManager())
	m.View(f)
	var out []string
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			if c := buf.CellAt(uint16(x), uint16(y)); c.Content != 0 {
				b.WriteRune(c.Content)
			}
		}
		out = append(out, b.String())
	}
	return out
}

// TestRerollButton checks that the developer's button shows only in
// developer mode and in the dungeon, and that it rolls new colors.
func TestRerollButton(t *testing.T) {
	shown := func(m *Model) bool {
		return strings.Contains(strings.Join(screenRows(m, m.layout.Width, m.layout.Height), "\n"), rerollLabel)
	}
	m := levelColorsModel(t, Config{Width: 130, Height: 40, Subwindows: true})
	newLevel(m, 3)
	if shown(m) {
		t.Error("the button shows without -dev")
	}
	dev := levelColorsModel(t, Config{Width: 130, Height: 40, Subwindows: true, DefaultsDir: t.TempDir()})
	newLevel(dev, 0)
	if shown(dev) {
		t.Error("the button shows in the town")
	}
	newLevel(dev, 3)
	if !shown(dev) {
		t.Fatal("no button in the dungeon with -dev")
	}
	if b := dev.rerollBox(); !b.Overlaps(dev.layout.Stats) {
		t.Errorf("button at %+v, not in the stats bar %+v", b, dev.layout.Stats)
	}
	before := dev.levelColors
	b := dev.rerollAt
	r := dev.Update(engine.MousePressMsg{Position: cell.Point{X: uint16(b.X + 2), Y: uint16(b.Y)}, Button: driver.MouseLeft})
	if dev.levelColors == before || dev.sheet.LevelColors() != dev.levelColors {
		t.Error("a click on the button did not roll new colors")
	}
	if !r.Redraw {
		t.Error("a click on the button does not redraw")
	}
	if !strings.HasPrefix(dev.notice, "Walls ") || !strings.Contains(dev.notice, ". Floor ") {
		t.Errorf("notice %q", dev.notice)
	}
	// With no stats bar, the button sits on the message pane's title row.
	dev.settings.Classic = []string{statsName}
	dev.relayout(130, 40)
	b = dev.rerollBox()
	if b.Empty() || b.Y != dev.layout.Panes[0].Box.Y {
		t.Errorf("button at %+v without a stats bar, panes %+v", b, dev.layout.Panes)
	}
	if !shown(dev) {
		t.Error("no button without a stats bar")
	}
}

// arrive sends the NEW_LEVEL event the engine sends when a character
// arrives on the level made at turn.
func arrive(m *Model, savefile string, depth, turn int) {
	ev := fmt.Sprintf(`{"event": "NEW_LEVEL", "depth": %d, "level_turn": %d, "savefile": %q}`, depth, turn, savefile)
	m.handleEvent(proto.Event{JSON: []byte(ev)})
}

// TestLevelColorsKept checks that a level's colors are kept in the
// character's folder, so loading the level brings them back, and that
// another level, or a reroll, replaces them.
func TestLevelColorsKept(t *testing.T) {
	user := t.TempDir()
	cfg := Config{Width: 80, Height: 24, UserDir: user, DefaultsDir: t.TempDir()}
	m := levelColorsModel(t, cfg)
	arrive(m, "Tester", 4, 2000)
	kept := m.levelColors
	path := filepath.Join(user, "characters", "Tester", levelColorsFile)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the colors were not kept: %v", err)
	}

	// Loading the same level brings its colors back.
	m = levelColorsModel(t, cfg)
	arrive(m, "Tester", 4, 2000)
	if m.levelColors != kept || m.sheet.LevelColors() != kept {
		t.Errorf("loaded level has %+v, want the kept %+v", m.levelColors, kept)
	}
	// The town leaves the kept colors alone.
	arrive(m, "Tester", 0, 2500)
	if m.sheet.LevelColors() != (tiles.LevelColors{}) {
		t.Error("the town has colors")
	}
	m = levelColorsModel(t, cfg)
	arrive(m, "Tester", 4, 2000)
	if m.levelColors != kept {
		t.Error("a visit to the town lost the kept colors")
	}

	// A reroll is kept too.
	m.rerollLevelColors()
	rerolled := m.levelColors
	m = levelColorsModel(t, cfg)
	arrive(m, "Tester", 4, 2000)
	if m.levelColors != rerolled {
		t.Error("the rerolled colors were not kept")
	}

	// Another level at the same depth, or another character, rolls anew.
	m = levelColorsModel(t, cfg)
	arrive(m, "Tester", 4, 3000)
	if m.levelColors == rerolled {
		t.Error("a new level at the same depth got the old level's colors")
	}
	other := levelColorsModel(t, cfg)
	arrive(other, "Other", 4, 3000)
	if other.levelColors == m.levelColors {
		t.Error("another character got this one's colors")
	}

	// A savefile name that would leave the characters folder keeps nothing.
	odd := levelColorsModel(t, cfg)
	arrive(odd, "../escape", 4, 3000)
	if odd.levelColorsPath() != "" {
		t.Errorf("path %q for a savefile with a slash", odd.levelColorsPath())
	}
}

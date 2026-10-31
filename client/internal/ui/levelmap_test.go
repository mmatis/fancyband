package ui

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

// fakeLevel is a MAP answer for a level w by h: a room with walls round
// it, an up staircase near its top left, a down staircase near its
// bottom right, a shop entrance in the top wall, and the player at 4, 4.
// The rest of the level is unknown. With tiled, the rows hold tiles: the
// floor at 0, 1, walls at 0, 2, and the rest at 1, x%8 over the floor.
func fakeLevel(w, h int, tiled bool) *proto.LevelMap {
	room := func(x, y int) rune {
		switch {
		case x == 4 && y == 4:
			return '@'
		case x == 2 && y == 2:
			return '<'
		case x == 17 && y == 9:
			return '>'
		case x == 5 && y == 1:
			return '1'
		case x < 1 || y < 1 || x > 18 || y > 10:
			return ' '
		case x == 1 || y == 1 || x == 18 || y == 10:
			return '#'
		}
		return '.'
	}
	lv := &proto.LevelMap{Width: w, Height: h, Depth: 3, Player: proto.TargetData{X: 4, Y: 4},
		Marks: []proto.LevelMark{{X: 2, Y: 2, Kind: "up"}, {X: 17, Y: 9, Kind: "down"}, {X: 5, Y: 1, Kind: "shop", Name: "General Store"}}}
	for y := 0; y < h; y++ {
		var row proto.LevelRow
		var text []rune
		for x := 0; x < w; x++ {
			r := room(x, y)
			text = append(text, r)
			color := 1
			if r == '@' {
				color = 11
			}
			row.Colors = append(row.Colors, color)
			switch r {
			case ' ':
				row.Tiles = append(row.Tiles, 0, 0, 0, 0)
			case '.':
				row.Tiles = append(row.Tiles, 0, 1, 0, 1)
			case '#':
				row.Tiles = append(row.Tiles, 0, 2, 0, 2)
			default:
				row.Tiles = append(row.Tiles, 1, x%8, 0, 1)
			}
		}
		if tiled {
			row.Colors = nil
		} else {
			row.Text, row.Tiles = string(text), nil
		}
		lv.Rows = append(lv.Rows, row)
	}
	return lv
}

// openFakeMap opens the level map on lv for screen request 9.
func openFakeMap(t *testing.T, m *Model, sent *sentLog, lv *proto.LevelMap) *levelMap {
	t.Helper()
	m.openMap(9)
	answerQuery(t, m, sent, "MAP", "", proto.EventData{Level: lv})
	s, ok := top(m).(*levelMap)
	if !ok {
		t.Fatalf("top screen is %T", top(m))
	}
	return s
}

func drawMap(s *levelMap, term *testkit.Terminal, sprite bool) {
	area := term.Area()
	env := &drawEnv{pal: newPalette(nil)}
	if sprite {
		env.sprite = s.m.sheet.Sprite
	}
	term.Draw(func(f *terminal.Frame) { s.draw(f, s.box(Box{0, 0, int(area.Width), int(area.Height)}), env) })
}

// TestLevelMapLetters draws a small level a letter a grid in the middle
// of the screen, moves the cursor to the stairs and the shop, and travels
// to the down staircase with Enter.
func TestLevelMapLetters(t *testing.T) {
	m := New(Config{Width: 80, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	s := openFakeMap(t, m, &sent, fakeLevel(20, 12, false))
	term := testkit.NewTerminal(80, 30)
	drawMap(s, term, false)
	snap := term.Snapshot()
	for _, want := range []string{"Map of level 3 (150 ft)", "Letters", "#<...", "#..@..", "You are here", "4, 4"} {
		if !strings.Contains(snap, want) {
			t.Errorf("no %q on the screen:\n%s", want, snap)
		}
	}
	if s.across != 20 || s.down != 12 {
		t.Errorf("shows %dx%d grids, want the whole level, 20x12", s.across, s.down)
	}
	// The level sits in the middle.
	if s.area.X != 30 || s.area.W != 20 {
		t.Errorf("map area %+v", s.area)
	}

	s.key(runeKey('>'))
	if s.cur != [2]int{17, 9} {
		t.Errorf("> moved the cursor to %v", s.cur)
	}
	s.key(runeKey('s'))
	drawMap(s, term, false)
	if !strings.Contains(term.Snapshot(), "The entrance to the General Store") {
		t.Errorf("the shop is not named:\n%s", term.Snapshot())
	}
	s.key(runeKey('<'))
	s.key(keyOf(driver.KeyArrowRight))
	s.key(keyOf(driver.KeyArrowDown))
	if s.cur != [2]int{3, 3} {
		t.Errorf("the arrows moved the cursor to %v", s.cur)
	}
	s.key(runeKey('@'))
	if s.cur != [2]int{4, 4} {
		t.Errorf("@ moved the cursor to %v", s.cur)
	}

	s.key(runeKey('>'))
	s.key(keyOf(driver.KeyEnter))
	if top(m) != nil {
		t.Errorf("the map stayed open")
	}
	r := sent.last("")
	then, _ := r["then"].(map[string]any)
	if then == nil || then["cmd"] != "PATHFIND" {
		t.Fatalf("reply %v", r)
	}
	pt := then["args"].(map[string]any)["point"].(map[string]any)
	if pt["x"] != float64(17) || pt["y"] != float64(9) {
		t.Errorf("travels to %v", pt)
	}
	// A refusal shows as a notice.
	id := int(then["id"].(float64))
	m.pending[id](proto.EventData{ID: id, Error: "you know no way there"})
	if m.notice != "You know no way there." {
		t.Errorf("notice %q", m.notice)
	}
}

// TestLevelMapOverview draws two grids a cell as half blocks, with the
// stairs, the shop, and the player shown by their letters.
func TestLevelMapOverview(t *testing.T) {
	m := New(Config{Width: 80, Height: 30})
	var sent sentLog
	m.sendHook = sent.hook
	s := openFakeMap(t, m, &sent, fakeLevel(20, 12, false))
	s.key(runeKey('-'))
	term := testkit.NewTerminal(80, 30)
	drawMap(s, term, false)
	snap := term.Snapshot()
	if !strings.Contains(snap, "Overview") || s.down != 12 || s.area.H != 6 {
		t.Fatalf("overview shows %d grids down in %d rows:\n%s", s.down, s.area.H, snap)
	}
	lines := strings.Split(snap, "\n")
	row := func(y int) string { return string([]rune(lines[s.area.Y+y])[s.area.X : s.area.X+s.area.W]) }
	// Rows 0 and 1 of the level: the top wall in row 1, the shop at 5.
	if got := row(0); got != " ▀▀▀▀1▀▀▀▀▀▀▀▀▀▀▀▀▀ " {
		t.Errorf("first row %q", got)
	}
	// Rows 2 and 3: the up staircase at 2.
	if got := row(1); !strings.HasPrefix(got, " ▀<▀") {
		t.Errorf("second row %q", got)
	}
	// Rows 4 and 5: the cursor on the player.
	if got := []rune(row(2)); got[4] != '@' {
		t.Errorf("third row %q", string(got))
	}
	// A click on the down staircase's cell travels there.
	clickAt(t, term, s.area.X+17, s.area.Y+4)
	r := sent.last("")
	pt := r["then"].(map[string]any)["args"].(map[string]any)["point"].(map[string]any)
	if pt["x"] != float64(17) || pt["y"] != float64(9) {
		t.Errorf("a click travels to %v", pt)
	}
}

func clickAt(t *testing.T, term *testkit.Terminal, x, y int) {
	t.Helper()
	if !term.Click(uint16(x), uint16(y)) {
		t.Fatalf("nothing took a click at %d, %d", x, y)
	}
}

// TestLevelMapScrolls scrolls a level bigger than the screen with the
// wheel, the arrows, and the page keys, and the view follows the cursor.
func TestLevelMapScrolls(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	var sent sentLog
	m.sendHook = sent.hook
	s := openFakeMap(t, m, &sent, fakeLevel(198, 66, false))
	term := testkit.NewTerminal(80, 24)
	drawMap(s, term, false)
	if s.across != 76 || s.down >= 66 {
		t.Fatalf("shows %dx%d grids", s.across, s.down)
	}
	if s.view != [2]int{0, 0} {
		t.Errorf("view %v, want the top left, where the player is", s.view)
	}
	mouse := func(b driver.MouseButton, shift bool) {
		term.Mouse(driver.MouseEvent{Button: b, X: uint16(s.area.X + 5), Y: uint16(s.area.Y + 5), Shift: shift})
	}
	mouse(driver.MouseScrollDown, false)
	if s.view[1] != wheelStep || s.cur != [2]int{4, 4} {
		t.Errorf("the wheel: view %v, cursor %v", s.view, s.cur)
	}
	mouse(driver.MouseScrollDown, true)
	if s.view[0] != 2*wheelStep {
		t.Errorf("shift and the wheel: view %v", s.view)
	}
	mouse(driver.MouseScrollUp, false)
	mouse(driver.MouseScrollUp, true)
	if s.view != [2]int{0, 0} {
		t.Errorf("back up: view %v", s.view)
	}

	s.key(keyOf(driver.KeyPageDown))
	if s.cur[1] != 4+pageRows(s.down) || s.view[1] == 0 {
		t.Errorf("Page Down: cursor %v, view %v", s.cur, s.view)
	}
	s.key(keyOf(driver.KeyPageUp))
	if s.cur[1] != 4 || s.view[1] != 0 {
		t.Errorf("Page Up: cursor %v, view %v", s.cur, s.view)
	}
	for i := 0; i < 100; i++ {
		s.key(keyOf(driver.KeyArrowRight))
	}
	if s.cur[0] != 104 || s.cur[0] < s.view[0] || s.cur[0] >= s.view[0]+s.across {
		t.Errorf("the view lost the cursor: cursor %v, view %v", s.cur, s.view)
	}
	s.key(driver.KeyEvent{Type: driver.KeyArrowRight, Shift: true})
	s.key(runeKey('L'))
	if s.cur[0] != 124 {
		t.Errorf("Shift moves 10: cursor %v", s.cur)
	}
	s.key(keyOf(driver.KeyEsc))
	if top(m) != nil || sent.last("")["then"] != nil {
		t.Errorf("Escape: screen %v, reply %v", top(m), sent.last(""))
	}
}

// TestLevelMapTiles draws the level as pictures at each zoom: a band per
// row of cells, tiles at a half and a quarter of their size, and the
// cursor drawn into the bands it crosses alone.
func TestLevelMapTiles(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	png := filepath.Join(filepath.Dir(here), "..", "..", "..", "engine", "lib", "tiles", "adam-bolt", "16x16.png")
	ts := proto.TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1, Name: "adam-bolt", Path: png}
	sheet, err := tiles.Load(ts)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{Width: 100, Height: 40})
	m.sheet, m.terms.TileSet = sheet, &ts
	var sent sentLog
	m.sendHook = sent.hook
	s := openFakeMap(t, m, &sent, fakeLevel(60, 30, true))
	term := testkit.NewTerminal(100, 40)

	for _, z := range []struct {
		name         string
		across, down int
		cellsW       int
		px           int
	}{
		{"Full size", 48, 30, 96, 16},
		{"Half size", 60, 30, 60, 8},
		{"Quarter size", 60, 32, 30, 4},
	} {
		drawMap(s, term, true)
		if !strings.Contains(term.Snapshot(), z.name) {
			t.Errorf("%s: not named on the screen", z.name)
		}
		if s.across != z.across || s.down != z.down || s.area.W != z.cellsW {
			t.Errorf("%s: %dx%d grids in %+v", z.name, s.across, s.down, s.area)
		}
		regions := term.Frame().ImageRegionsSnapshot()
		if len(regions) != s.area.H {
			t.Fatalf("%s: %d pictures for %d rows", z.name, len(regions), s.area.H)
		}
		if w := regions[0].Img.Bounds().Dx(); w != s.across*z.px {
			t.Errorf("%s: band %d pixels wide, want %d", z.name, w, s.across*z.px)
		}
		// Moving the cursor one grid changes only the bands it was on and
		// is on now.
		before := map[int]any{}
		for i, r := range regions {
			before[i] = r.Img
		}
		s.key(keyOf(driver.KeyArrowRight))
		drawMap(s, term, true)
		changed := 0
		for i, r := range term.Frame().ImageRegionsSnapshot() {
			if before[i] != any(r.Img) {
				changed++
			}
		}
		if changed == 0 || changed > 3 {
			t.Errorf("%s: moving the cursor changed %d bands", z.name, changed)
		}
		s.key(keyOf(driver.KeyArrowLeft))
		s.key(runeKey('-'))
	}
}

// TestLevelMapClassic leaves the level map to the game when its switch
// is off.
func TestLevelMapClassic(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	if !slices.Contains(m.Caps(), "screen:map") {
		t.Errorf("caps %v lack screen:map", m.Caps())
	}
	m.settings.Classic = []string{"map"}
	if slices.Contains(m.Caps(), "screen:map") {
		t.Errorf("caps %v claim the map with its switch off", m.Caps())
	}
}

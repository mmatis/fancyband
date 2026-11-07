package ui

import (
	"fmt"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
)

// Terrain and traps have a tile for each light the map shows them in,
// and effects one for a ball and one for each way a bolt flies. The Tile
// Editor calls these an entry's states. Each one's name says when the map
// shows it, as the engine decides in map_info (cave-map.c) and bolt_pict
// (ui-display.c).

// stateText is how people see one state: its name, a short name for the
// buttons that pick it, and when the map shows it.
type stateText struct {
	name, short, help string
}

// lightStates are the lights, by the engine's names for them.
var lightStates = map[string]stateText{
	"los":   {"In view", "In view", "Lit grids you can see."},
	"torch": {"In your light", "Your light", "Grids lit by your own light source, with the view_yellow_light option on. With it off, they look In view."},
	"lit":   {"Remembered", "Remembered", "Grids out of sight, as you remember them: most of the map you have explored."},
	"dark":  {"Unlight", "Unlight", "Grids a necromancer sees in the dark with Unlight."},
}

// effectStates are an effect's tiles, by the engine's names for them.
var effectStates = map[string]stateText{
	"static": {"Ball or explosion", "Ball", "Balls, breaths, and explosions, and an effect that does not move."},
	"0":      {"Bolt up or down", "Bolt |", "A bolt flying straight up or down."},
	"45":     {"Bolt along /", "Bolt /", "A bolt flying up and right, or down and left."},
	"90":     {"Bolt sideways", "Bolt -", "A bolt flying left or right."},
	"135":    {"Bolt along \\", "Bolt \\", "A bolt flying up and left, or down and right."},
}

// stateOf returns how people see an entry's state name.
func stateOf(kind, name string) stateText {
	states := lightStates
	if kind == "effect" {
		states = effectStates
	}
	if s, ok := states[name]; ok {
		return s
	}
	return stateText{name, name, ""}
}

// slotLabel names one of an entry's tiles for people.
func slotLabel(kind, name string) string { return stateOf(kind, name).name }

// allStates names every state of an entry at once, and says what that
// changes.
func allStates(kind string) stateText {
	if kind == "effect" {
		return stateText{"Every one", "All", "The ball and every bolt direction get the same tile."}
	}
	return stateText{"Every light", "All", "Every light gets the same tile."}
}

// stateChip is one button of a state bar: -1 for all the states.
type stateChip struct {
	slot  int
	label string
	tile  *[2]int
	w     int
}

// stateChips lays out the buttons for an entry's states in rows no wider
// than width: All first, then each state with its tile.
func stateChips(e proto.VisualData, names []string, width int, tiles bool) [][]stateChip {
	all := allStates(e.Kind).short
	chips := []stateChip{{slot: -1, label: all, w: len(all) + 2}}
	for i, n := range names {
		c := stateChip{slot: i, label: stateOf(e.Kind, n).short}
		if i < len(e.Tiles) {
			c.tile = e.Tiles[i]
		}
		// The tile, a space, the label, a space; or with no tiles, the
		// label between spaces.
		c.w = limoni.StringWidth(c.label) + 2
		if tiles {
			c.w += 2
		}
		chips = append(chips, c)
	}
	var rows [][]stateChip
	var row []stateChip
	used := 0
	for _, c := range chips {
		if len(row) > 0 && used+1+c.w > width {
			rows, row, used = append(rows, row), nil, 0
		}
		if len(row) > 0 {
			used++
		}
		row = append(row, c)
		used += c.w
	}
	return append(rows, row)
}

// drawStateBar draws the buttons that pick which of an entry's states
// changes, at the top of b, with the chosen one, selected, lit up, and
// under them what the chosen one is. hotspot makes each button
// clickable. It returns the rows it took.
func drawStateBar(f *terminal.Frame, b Box, env *drawEnv, e proto.VisualData, names []string, selected int, hotspot func(Box, func()), pick func(int)) int {
	tiles := env.sprite != nil
	y := b.Y
	f.TextFit(b.X, y, b.W, "Change the tile for:", screenText)
	y++
	for _, row := range stateChips(e, names, b.W, tiles) {
		x := b.X
		for _, c := range row {
			st, bg := screenText.WithBg(screenBg.Bg), screenBg.Bg
			if c.slot == selected {
				st, bg = screenSelected, screenSelected.Bg
			}
			at := Box{x, y, c.w, 1}
			fillBox(f, at, cell.NewStyle().WithBg(bg))
			tx := x + 1
			if c.slot >= 0 && tiles {
				if c.tile != nil {
					env.tile(f, Box{x, y, 2, 1}, uint8(c.tile[0]), uint8(c.tile[1]), bg)
				} else {
					f.Text(x, y, " ·", st)
				}
				tx = x + 3
			}
			f.Text(tx, y, c.label, st)
			slot := c.slot
			hotspot(at, func() { pick(slot) })
			x += c.w + 1
		}
		y++
	}
	help := allStates(e.Kind).help
	if selected >= 0 && selected < len(names) {
		help = stateOf(e.Kind, names[selected]).help
	}
	for _, l := range wrapPlain(help, b.W) {
		f.TextFit(b.X, y, b.W, l, screenDim)
		y++
	}
	return y - b.Y
}

// drawStateList draws, in column 2, each of an entry's states: its tile,
// its name, and where its tile is, with active marked, or every state for
// active -1 when they change together. A click on one opens the sheet on
// that state. It returns the rows it took.
func (ed *tileEditor) drawStateList(f *terminal.Frame, b Box, env *drawEnv, e int, active int) int {
	en := ed.v.Entries[e]
	names := ed.slotNames(en)
	y := b.Y
	for i, n := range names {
		if y >= b.Y+b.H {
			break
		}
		on := active < 0 || i == active
		st, mark := screenDim, "  "
		if on {
			st, mark = screenText, "▶ "
		}
		f.Text(b.X, y, mark, screenTitle)
		x := b.X + 2
		var t *[2]int
		if i < len(en.Tiles) {
			t = en.Tiles[i]
		}
		if env.sprite != nil {
			if t != nil {
				env.tile(f, Box{x, y, 2, 1}, uint8(t[0]), uint8(t[1]), screenBg.Bg)
			} else {
				f.Text(x, y, " ·", screenDim)
			}
			x += 3
		}
		where := "no tile"
		if t != nil {
			where = fmt.Sprintf("%d, %d", t[0], t[1])
			if v, ok := ed.virtual(t); ok {
				where = fmt.Sprintf("%d, %d + filter", v.Base[0], v.Base[1])
			}
		}
		label := slotLabel(en.Kind, n)
		f.TextFit(x, y, b.X+b.W-x, label, st)
		if wx := b.X + b.W - limoni.StringWidth(where); wx > x+limoni.StringWidth(label) {
			f.Text(wx, y, where, screenDim)
		}
		slot := i
		ed.colHotspot(f, Box{b.X, y, b.W, 1}, false, func() { ed.pickSlot(e, slot) })
		y++
	}
	return y - b.Y
}

// pickSlot opens the sheet on entry e's state i, to change that tile
// alone.
func (ed *tileEditor) pickSlot(e, i int) {
	ed.pick(e)
	ed.picker.selectSlot(i)
}

// selectSlot picks which state the sheet changes, -1 for all, and moves
// the cursor to that state's tile, so the sheet shows where it is.
func (p *tilePicker) selectSlot(i int) {
	en := p.entry()
	if i < -1 || i >= len(en.Tiles) {
		return
	}
	p.slot = i
	if i >= 0 {
		if t := en.Tiles[i]; t != nil {
			p.moveTo(t[0], t[1])
		}
	}
}

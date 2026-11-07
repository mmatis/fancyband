package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/tiles"
)

// Level colors give each dungeon level's walls and floor a random color
// shift of their own, with tiles (see tiles.LevelColors). The client rolls
// them when the character first arrives on a level, and keeps them in the
// character's folder, apart from the savefile, so the savefile stays one
// the game itself can load. A level loaded from a save gets its colors
// back from there. The town keeps the sheet's colors.

// levelColorsFile is the file in a character's folder that keeps the
// colors of the level they are on.
const levelColorsFile = "level-colors.json"

// levelID names one level: whose it is, how deep, and the game turn it was
// made, which the savefile keeps. No two levels of a character share a
// turn.
type levelID struct {
	Savefile string `json:"-"`
	Depth    int    `json:"depth"`
	Turn     int    `json:"level_turn"`
}

// savedLevelColors is what levelColorsFile holds: one level and its
// colors. Arriving on another level replaces it.
type savedLevelColors struct {
	levelID
	tiles.LevelColors
}

// levelColorsOn reports whether level colors are on. They are unless the
// settings turn them off.
func (m *Model) levelColorsOn() bool {
	return m.settings.LevelColors == nil || *m.settings.LevelColors
}

// arriveOnLevel notes the level the character has just arrived on, from a
// NEW_LEVEL event, and picks its colors.
func (m *Model) arriveOnLevel(l levelID) {
	m.level, m.depth = l, l.Depth
	m.pickLevelColors()
}

// pickLevelColors gives the level the colors kept for it, or rolls and
// keeps new ones, or takes them away in the town or with level colors off.
func (m *Model) pickLevelColors() {
	m.levelColors = tiles.LevelColors{}
	if m.depth > 0 && m.levelColorsOn() {
		if c, ok := m.loadLevelColors(); ok {
			m.levelColors = c
		} else {
			m.rollLevelColors()
			return
		}
	}
	m.applyLevelColors()
}

// rollLevelColors rolls new colors for the level and keeps them.
func (m *Model) rollLevelColors() {
	m.levelColors = tiles.RollLevelColors(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))
	m.saveLevelColors()
	m.applyLevelColors()
}

// levelColorsPath returns the file that keeps the colors of the
// character's level, or "" when there is no folder for it.
func (m *Model) levelColorsPath() string {
	name := m.level.Savefile
	if m.cfg.UserDir == "" || name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return ""
	}
	return filepath.Join(m.cfg.UserDir, "characters", name, levelColorsFile)
}

// loadLevelColors returns the colors kept for the level, if the file
// keeps this level's.
func (m *Model) loadLevelColors() (tiles.LevelColors, bool) {
	path := m.levelColorsPath()
	if path == "" {
		return tiles.LevelColors{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			m.log.Printf("cannot read %s: %v", path, err)
		}
		return tiles.LevelColors{}, false
	}
	var saved savedLevelColors
	if err := json.Unmarshal(b, &saved); err != nil {
		m.log.Printf("cannot read %s: %v", path, err)
		return tiles.LevelColors{}, false
	}
	l := m.level
	if saved.Depth != l.Depth || saved.Turn != l.Turn || saved.LevelColors == (tiles.LevelColors{}) {
		return tiles.LevelColors{}, false
	}
	return saved.LevelColors, true
}

// saveLevelColors keeps the level's colors in the character's folder. The
// file is written whole, then moved into place, so a crash leaves the old
// one.
func (m *Model) saveLevelColors() {
	path := m.levelColorsPath()
	if path == "" {
		return
	}
	b, err := json.MarshalIndent(savedLevelColors{m.level, m.levelColors}, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		tmp := path + ".new"
		if err = os.WriteFile(tmp, append(b, '\n'), 0o644); err == nil {
			err = os.Rename(tmp, path)
		}
	}
	if err != nil {
		m.log.Printf("cannot keep the level's colors in %s: %v", path, err)
	}
}

// applyLevelColors hands the level's colors to the tile sheet, if the
// map uses one.
func (m *Model) applyLevelColors() {
	if m.sheet != nil {
		m.sheet.SetLevelColors(m.levelColors)
	}
}

// rerollLabel names the developer's button that rolls the level's colors again.
const rerollLabel = "New level colors"

// rerollShown reports whether the main screen shows the button that rolls
// the level's colors again: in developer mode, in the dungeon, with tiles
// and level colors on.
func (m *Model) rerollShown() bool {
	return m.dev() && m.inGame && m.sheet != nil && m.terms.TileSet != nil && m.depth > 0 && m.levelColorsOn()
}

// rerollBox returns where the button goes: the last row of the stats bar,
// which then has one row fewer, or else the right of the first pane's
// title row. It returns an empty box when there is neither.
func (m *Model) rerollBox() Box {
	w := len(rerollLabel) + 4
	if l := m.layout; l.Native() && l.Stats.H > 1 && l.Stats.W >= w+2 {
		return Box{l.Stats.X + 1, l.Stats.Y + l.Stats.H - 1, w, 1}
	}
	if len(m.layout.Panes) > 0 {
		p := m.layout.Panes[0].Box
		if p.W >= w+len(m.layout.Panes[0].Title)+4 {
			return Box{p.X + p.W - w - 1, p.Y, w, 1}
		}
	}
	return Box{}
}

// drawReroll draws the button in b.
func (m *Model) drawReroll(f *terminal.Frame, b Box) {
	f.RenderWidget(widgets.Button{
		Label:        rerollLabel,
		Style:        screenTitle,
		FocusedStyle: screenTitle,
		OnPress:      m.rerollLevelColors,
	}, rect(b))
}

// rerollLevelColors rolls the level's colors again and says what came up,
// for a developer tuning the rolls.
func (m *Model) rerollLevelColors() {
	m.rollLevelColors()
	c := m.levelColors
	m.notify("Walls " + c.Wall.String() + ". Floor " + c.Floor.String() + ".")
}

// Package settings reads and writes fancyband-tui.json, the client's own
// settings. Angband's options live in the savefile and stay in the
// engine; this file holds only what belongs to the client.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// FileName is the settings file's name in the user folder.
const FileName = "fancyband-tui.json"

// Settings are the client's settings. A missing field keeps its zero
// value, so an old file still loads.
type Settings struct {
	// LastCharacter is the savefile name of the character played last,
	// which Continue in the start menu loads.
	LastCharacter string `json:"last_character,omitempty"`
	// BorgWarned lists the characters, by savefile name, that have seen
	// the warning about the borg.
	BorgWarned []string `json:"borg_warned,omitempty"`

	// TileSet is the tile set to draw the map with, 0 for text, or nil
	// if none was chosen. The -tiles flag wins over it.
	TileSet *int `json:"tile_set,omitempty"`
	// TileScale is the tile size, 1 to 3; 0 means 1.
	TileScale int `json:"tile_scale,omitempty"`
	// Animate turns the animated tile filters on or off, or is
	// nil for the default, on. The -animate flag wins over it.
	Animate *bool `json:"animate,omitempty"`
	// Subwindows shows the side panels when there is room, or is nil for
	// the default, on.
	Subwindows *bool `json:"subwindows,omitempty"`
	// Shadows darkens the floor beside walls, with tiles, or is nil for
	// the default, on.
	Shadows *bool `json:"shadows,omitempty"`
	// Slide moves creatures smoothly from grid to grid, with tiles, or is
	// nil for the default, on.
	Slide *bool `json:"slide,omitempty"`
	// Scroll is how the map scrolls when the character nears its edge:
	// "pixel", "cell", "map", or "off". Empty means "pixel".
	Scroll string `json:"scroll,omitempty"`
	// StatsRight puts the native stats bar right of the map instead of
	// left of it.
	StatsRight bool `json:"stats_right,omitempty"`
	// Classic lists the screens to leave to the game's own text UI, such
	// as "options" or "death".
	Classic []string `json:"classic,omitempty"`
}

// Scale returns the tile size, at least 1.
func (s Settings) Scale() int { return max(s.TileScale, 1) }

// Load reads the settings at path. A missing file gives the defaults.
func Load(path string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

// Save writes the settings to path, replacing the file in one step so a
// crash cannot leave half a file.
func (s Settings) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Warned reports whether the character has seen the borg warning.
func (s Settings) Warned(character string) bool {
	return slices.Contains(s.BorgWarned, character)
}

// SetWarned records that the character has seen the borg warning.
func (s *Settings) SetWarned(character string) {
	if !s.Warned(character) {
		s.BorgWarned = append(s.BorgWarned, character)
	}
}

package ui

import (
	"os"
	"path/filepath"
	"testing"

	"fancyband/internal/settings"
)

func TestKonsoleGraphics(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"KONSOLE_VERSION": "250401"}, "kitty"},
		{map[string]string{"KONSOLE_VERSION": "250401", "LIMONI_GRAPHICS": "sixel"}, ""},
		{map[string]string{"KONSOLE_VERSION": "250401", "TMUX": "/tmp/tmux-1000/default"}, ""},
		{map[string]string{"KITTY_WINDOW_ID": "1"}, ""},
	}
	for _, c := range cases {
		if got := konsoleGraphics(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%v: got %q, want %q", c.env, got, c.want)
		}
	}
}

// TestBlockedTilesStayOff checks that a tile set saved in the settings does
// not turn tiles back on in a terminal that cannot show them: Run reads the
// settings, finds the terminal cannot, and New reads the settings again.
func TestBlockedTilesStayOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), settings.FileName)
	if err := os.WriteFile(path, []byte(`{"tile_set": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if m := New(Config{SettingsPath: path, TilesBlocked: "no pictures"}); m.cfg.Tiles != 0 {
		t.Errorf("blocked: tile set %d, want 0", m.cfg.Tiles)
	}
	if m := New(Config{SettingsPath: path}); m.cfg.Tiles != 2 {
		t.Errorf("not blocked: tile set %d, want the saved 2", m.cfg.Tiles)
	}
}

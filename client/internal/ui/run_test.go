package ui

import "testing"

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

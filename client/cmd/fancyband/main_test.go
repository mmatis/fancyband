package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUserFolder keeps the client's settings and logs with the game's user
// folder, so a test or a second player with -duser is kept away from the
// usual one.
func TestUserFolder(t *testing.T) {
	usual := filepath.Join("/home/p", ".angband", "Fancyband")
	if got := userFolder("/home/p", nil); got != usual {
		t.Errorf("with no options: %s", got)
	}
	if got := userFolder("/home/p", []string{"-uCorvias", "-duser=/tmp/u"}); got != "/tmp/u" {
		t.Errorf("with -duser: %s", got)
	}
}

// TestEngineHome starts a release's engine above bin/, where lib/ is, and
// any other engine in its own folder.
func TestEngineHome(t *testing.T) {
	top := t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		filepath.Join(top, "bin", "engine"):              top,
		filepath.Join(top, "fancyband"):                  top,
		filepath.Join(top, "elsewhere", "bin", "engine"): filepath.Join(top, "elsewhere", "bin"),
		filepath.Join(top, "build", "game", "fancyband"): filepath.Join(top, "build", "game"),
	}
	for engine, want := range cases {
		if got := engineHome(engine); got != want {
			t.Errorf("engineHome(%s) = %s, want %s", engine, got, want)
		}
	}
}

// TestBesideEngine keeps the search for the engine from finding this program,
// which a release calls fancyband.
func TestBesideEngine(t *testing.T) {
	exe := filepath.Join("/opt", "game", "fancyband")
	if got, want := besideEngine(exe), filepath.Join("/opt", "game", "bin", "engine"); got != want {
		t.Errorf("besideEngine = %s, want %s", got, want)
	}
}

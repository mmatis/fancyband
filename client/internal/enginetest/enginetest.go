// Package enginetest helps tests run the real fancyband engine. Only
// tests import it.
package enginetest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Binary returns the engine to test: $FANCYBAND_BIN, or the one
// scripts/check builds at build/engine/game/fancyband. When FANCYBAND_BIN
// is set a missing binary fails the test; otherwise the test is skipped.
func Binary(t testing.TB) string {
	t.Helper()
	if bin := os.Getenv("FANCYBAND_BIN"); bin != "" {
		if _, err := os.Stat(bin); err != nil {
			t.Fatalf("FANCYBAND_BIN: %v", err)
		}
		return bin
	}
	_, here, _, _ := runtime.Caller(0)
	bin := filepath.Join(filepath.Dir(here), "..", "..", "..", "build", "engine", "game", "fancyband")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no engine at %s; run scripts/check or set FANCYBAND_BIN", bin)
	}
	return bin
}

// Dirs is a scratch copy of every folder the engine writes to, so tests
// never touch the player's ~/.angband/Fancyband or the build's lib/save.
type Dirs struct {
	User, Archive, Save, Panic, Scores string
}

// NewDirs makes the folders under a test's temporary folder.
func NewDirs(t testing.TB) Dirs {
	t.Helper()
	root := t.TempDir()
	d := Dirs{
		User:    filepath.Join(root, "user"),
		Archive: filepath.Join(root, "user", "archive"),
		Save:    filepath.Join(root, "save"),
		Panic:   filepath.Join(root, "panic"),
		Scores:  filepath.Join(root, "scores"),
	}
	for _, p := range []string{d.User, d.Archive, d.Save, d.Panic, d.Scores} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// Args returns the engine's arguments for a character named "tester",
// without -mnet. The -d options must come before -u, which reads the save
// folder when it is parsed.
func (d Dirs) Args(extra ...string) []string {
	args := []string{
		"-duser=" + d.User,
		"-darchive=" + d.Archive,
		"-dsave=" + d.Save,
		"-dpanic=" + d.Panic,
		"-dscores=" + d.Scores,
		"-utester",
	}
	return append(args, extra...)
}

// Savefile is where the tester character is saved.
func (d Dirs) Savefile() string { return filepath.Join(d.Save, "tester") }

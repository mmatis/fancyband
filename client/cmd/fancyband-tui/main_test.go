package main

import (
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

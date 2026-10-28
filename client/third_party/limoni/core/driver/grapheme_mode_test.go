//go:build !js

package driver

import (
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/grapheme"
)

// Mode 2027 asks the terminal to measure grapheme clusters the way the layout
// does. It must be sent when clusters are on, withdrawn on exit, and not sent
// at all in code-point mode, where the layout assumes per-code-point widths.
func TestSetupNegotiatesGraphemeClusterMode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		clusters bool
		inline   uint16
	}{
		{"full screen, clusters", true, 0},
		{"full screen, code points", false, 0},
		{"inline, clusters", true, 3},
		{"inline, code points", false, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grapheme.SetClusters(tc.clusters)
			defer grapheme.SetClusters(true)

			io := NewMemoryTerminalIO(nil, 40, 10)
			backend := NewPortableBackend(io)
			backend.SetInline(tc.inline)
			if err := backend.Setup(); err != nil {
				t.Fatal(err)
			}
			setup := string(io.Output())
			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
			teardown := strings.TrimPrefix(string(io.Output()), setup)

			if got := strings.Contains(setup, "\x1b[?2027h"); got != tc.clusters {
				t.Errorf("setup sends ?2027h = %v, want %v: %q", got, tc.clusters, setup)
			}
			if got := strings.Contains(teardown, "\x1b[?2027l"); got != tc.clusters {
				t.Errorf("teardown sends ?2027l = %v, want %v: %q", got, tc.clusters, teardown)
			}
		})
	}
}

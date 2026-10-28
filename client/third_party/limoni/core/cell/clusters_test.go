package cell

import "testing"

// Past the cap, a new cluster degrades to its first code point instead of
// growing the table without bound.
func TestClusterTableCapDegradesGracefully(t *testing.T) {
	clusters.mu.Lock()
	saved := maxClusters
	maxClusters = len(clusters.text) // full as of now
	clusters.mu.Unlock()
	defer func() {
		clusters.mu.Lock()
		maxClusters = saved
		clusters.mu.Unlock()
	}()

	got := ClusterContent("x\u0301\u0302\u0303-cap-test", 1)
	if IsCluster(got) {
		t.Fatal("a cluster was interned past the cap")
	}
	if got != 'x' {
		t.Errorf("degraded content = %q, want the first code point 'x'", got)
	}
}

func TestSingleCodePointsNeverUseTheTable(t *testing.T) {
	for _, s := range []string{"a", "\u65E5", "\U0001F642", "\u2764"} {
		if IsCluster(ClusterContent(s, StringWidth(s))) {
			t.Errorf("%q was interned; single code points are stored as runes", s)
		}
	}
}

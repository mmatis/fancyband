package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
	"fancyband/internal/settings"
)

func TestStatText(t *testing.T) {
	for v, want := range map[int]string{8: "8", 18: "18", 20: "18/02", 118: "18/100", 238: "18/***"} {
		if got := statText(v); got != want {
			t.Errorf("statText(%d) = %q, want %q", v, got, want)
		}
	}
}

// TestStatsBar plays with the native stats bar and status line, moves
// the bar to the right, then turns both off in the settings and sees the
// game's own come back.
func TestStatsBar(t *testing.T) {
	d := enginetest.NewDirs(t)
	path := filepath.Join(t.TempDir(), settings.FileName)
	p, m := startPlayerWith(t, 130, 36, true, d.Args("-n"), path)
	p.createCharacter()
	// The game's sidebar never names the character; the stats bar does,
	// on the left by default.
	screen := p.waitFor("the stats bar", func(s string) bool { return strings.HasPrefix(s, " Tester ") })
	if strings.Contains(screen, "LEVEL") || strings.Contains(screen, "Cur AC") {
		t.Errorf("the game's sidebar shows beside the stats bar:\n%s", screen)
	}
	for _, want := range []string{"\n Human Warrior", "\n Rookie", "\n STR", "\n Armor"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the stats bar lacks %q", want)
		}
	}
	p.page.Expect(p.page.GetByID(hpGaugeID)).ToHaveValue("100%")
	p.page.Expect(p.page.GetByID(statusLineID)).ToHaveValue(statusValue(m))
	if !strings.Contains(statusValue(m), "Light") {
		t.Errorf("the status line reads %q", statusValue(m))
	}
	p.page.Press("ctrl+c")
	p.page.ExpectExit()
	m.Session().Wait()

	s, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.StatsRight = true
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	p, m = startPlayerWith(t, 130, 36, true, d.Args(), path)
	p.choose(startMenuID, "Continue")
	p.waitFor("the stats bar on the right", func(s string) bool { return strings.Contains(s, "│ Tester ") })
	p.page.Press("ctrl+c")
	p.page.ExpectExit()
	m.Session().Wait()

	s.Classic = append(s.Classic, statsName)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	p, _ = startPlayerWith(t, 130, 36, true, d.Args(), path)
	p.choose(startMenuID, "Continue")
	p.waitFor("the game's sidebar", func(s string) bool {
		return strings.Contains(s, "LEVEL") && !strings.Contains(s, " Tester ")
	})
}

// statusValue is the status line as its tree node reads.
func statusValue(m *Model) string {
	if m.status == nil {
		return ""
	}
	var parts []string
	for _, sp := range m.status.Line {
		parts = append(parts, sp.Text)
	}
	return strings.Join(parts, " ")
}

// TestStatsLayoutFollowsSetting checks that a model without the setting
// lays out no stats bar, and that one with it asks for it.
func TestStatsLayoutFollowsSetting(t *testing.T) {
	m := New(Config{Width: 130, Height: 36, Subwindows: true})
	if m.layout.Stats.X != 0 {
		t.Errorf("the stats bar starts at column %d, not on the left", m.layout.Stats.X)
	}
	if !m.layout.Native() {
		t.Error("no stats bar by default")
	}
	m.settings.Classic = []string{statsName}
	m.relayout(130, 36)
	if m.layout.Native() {
		t.Error("a stats bar with the setting off")
	}
	m.status = &proto.StatusData{}
	if m.handleSnapshot(proto.Snapshot{JSON: []byte(`{"status": {"name": "X"}}`)}) || m.status.Name != "X" {
		t.Errorf("snapshot handling: %+v", m.status)
	}
}

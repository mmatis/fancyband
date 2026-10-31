package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

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

// TestStatsLayoutFollowsSetting checks that the stats bar shows only
// during a game, from ENTER_GAME to the death screen, and only with the
// setting on.
func TestStatsLayoutFollowsSetting(t *testing.T) {
	m := New(Config{Width: 130, Height: 36, Subwindows: true})
	if m.layout.Native() {
		t.Error("a stats bar before the game starts")
	}
	m.handleEvent(proto.Event{JSON: []byte(`{"event": "ENTER_GAME"}`)})
	if !m.layout.Native() {
		t.Error("no stats bar by default in a game")
	}
	if m.layout.Stats.X != 0 {
		t.Errorf("the stats bar starts at column %d, not on the left", m.layout.Stats.X)
	}
	m.handleRequest(proto.Request{JSON: []byte(`{"id": 3, "prompt": "screen", "name": "death"}`)})
	if m.layout.Native() {
		t.Error("a stats bar at death")
	}
	m.handleEvent(proto.Event{JSON: []byte(`{"event": "ENTER_GAME"}`)})
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

// TestFoeGauge draws the tracked monster's health as a gauge under its
// name, set apart from the character's HP, and as an empty one when the
// player cannot tell.
func TestFoeGauge(t *testing.T) {
	st := &proto.StatusData{Name: "Tester", HP: 20, MHP: 20, AC: 10,
		Health: &proto.HealthStatus{Color: 4, Percent: 40, Name: "Cave spider"}}
	rows := func() []string {
		buf := buffer.NewBuffer(cell.NewRect(0, 0, 24, 40))
		f := terminal.NewFrame(buf, terminal.NewFocusManager())
		drawStats(f, Box{0, 0, 24, 40}, st, newPalette(nil), nil)
		var out []string
		for y := uint16(0); y < 40; y++ {
			var b strings.Builder
			for x := uint16(0); x < 24; x++ {
				if c := buf.CellAt(x, y); c.Content != 0 {
					b.WriteRune(c.Content)
				}
			}
			out = append(out, strings.TrimRight(b.String(), " "))
		}
		return out
	}
	got := rows()
	hp, name, foe := -1, -1, -1
	for i, r := range got {
		switch {
		case strings.HasPrefix(strings.TrimSpace(r), "HP "):
			hp = i
		case strings.TrimSpace(r) == "Cave spider":
			name = i
		case strings.HasPrefix(strings.TrimSpace(r), "Foe "):
			foe = i
		}
	}
	if hp < 0 || name != hp+2 || foe != name+1 {
		t.Fatalf("HP on row %d, name on %d, foe on %d:\n%s", hp, name, foe, strings.Join(got, "\n"))
	}
	if !strings.HasSuffix(got[foe], " 40%") || strings.Contains(got[foe], "*") {
		t.Errorf("foe gauge %q", got[foe])
	}
	st.Health = &proto.HealthStatus{Color: 1, Percent: -1}
	got = rows()
	text := strings.Join(got, "\n")
	if !strings.Contains(text, "Unseen foe") || !strings.Contains(text, " ?") {
		t.Errorf("an unseen foe:\n%s", text)
	}
}

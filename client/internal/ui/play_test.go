package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thebanri/limoni/uitest"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
	"fancyband/internal/session"
	"fancyband/internal/settings"
)

const playWait = 15 * time.Second

// player drives the real client, on an in-memory terminal, against the
// real engine.
type player struct {
	t    *testing.T
	page *uitest.Page
}

func startPlayer(t *testing.T, width, height uint16, subwindows bool, args []string) (*player, *Model) {
	t.Helper()
	return startPlayerWith(t, width, height, subwindows, args, filepath.Join(t.TempDir(), settings.FileName))
}

// startPlayerWith starts the client with its settings file at
// settingsPath.
func startPlayerWith(t *testing.T, width, height uint16, subwindows bool, args []string, settingsPath string) (*player, *Model) {
	t.Helper()
	bin := enginetest.Binary(t)
	var stderr bytes.Buffer
	m := New(Config{
		Start: func(args, module []string, terms []proto.TermSize, caps []string) (*session.Session, error) {
			return session.Start(session.Options{
				Binary:     bin,
				Args:       append([]string{"-mnet"}, args...),
				ModuleArgs: module,
				Stderr:     &stderr,
				Terms:      terms,
				Caps:       caps,
			})
		},
		Width:        int(width),
		Height:       int(height),
		Subwindows:   subwindows,
		EngineArgs:   args,
		SettingsPath: settingsPath,
	})
	p := &player{t: t, page: uitest.Program(t, width, height, m)}
	t.Cleanup(func() {
		if s := m.Session(); s != nil {
			s.Kill()
			s.Wait()
		}
		if t.Failed() {
			t.Logf("engine stderr:\n%s", stderr.String())
			t.Logf("last screen:\n%s", p.page.Screen())
		}
	})
	return p, m
}

// waitFor polls the screen until ok passes. Screen does not wait by
// itself, and the game's grid has no widgets for a locator to find.
func (p *player) waitFor(what string, ok func(screen string) bool) string {
	p.t.Helper()
	deadline := time.Now().Add(playWait)
	for {
		s := p.page.Screen()
		if ok(s) {
			return s
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *player) waitText(text string) {
	p.t.Helper()
	p.waitFor(text, func(s string) bool { return strings.Contains(s, text) })
}

// heroAt returns the row and column of the @ on the map, or -1, -1.
func heroAt(screen string) (int, int) {
	for y, line := range strings.Split(screen, "\n") {
		// Neither the game's sidebar nor the stats bar holds an @.
		runes := []rune(line)
		for x := 0; x < len(runes); x++ {
			if runes[x] == '@' {
				return y, x
			}
		}
	}
	return -1, -1
}

// inTownScreen reports whether the screen shows the town, with the
// game's sidebar or the native stats bar.
func inTownScreen(s string) bool {
	y, _ := heroAt(s)
	classic := strings.Contains(s, "LEVEL") && strings.Contains(s, "\nTown ")
	// The stats bar is on the left, or right of the map after a divider.
	native := (strings.Contains(s, "\n Level") || strings.Contains(s, "│ Level")) &&
		(strings.Contains(s, "\n Town ") || strings.Contains(s, "│ Town"))
	return y >= 0 && (classic || native)
}

// createCharacter makes Tester from the start menu, in the savefile
// Tester.
func (p *player) createCharacter() {
	p.t.Helper()
	p.choose(startMenuID, "New character")
	p.birth("Tester")
}

// birth makes a Human Warrior named name through the native birth
// screen, from its first step.
func (p *player) birth(name string) {
	p.t.Helper()
	p.page.Expect(p.page.GetByID(birthID)).ToBeVisible()
	if strings.Contains(p.page.Screen(), "Make a character like your last one") {
		p.page.Press("n")
	}
	p.page.Expect(p.page.GetByID(birthRacesID)).ToBeVisible()
	p.page.Press("home") // Human
	p.page.Press("enter")
	p.page.Expect(p.page.GetByID(birthClassesID)).ToBeVisible()
	p.page.Press("home") // Warrior
	p.page.Press("enter")
	p.waitText("Points left")
	p.page.Press("enter")
	p.waitText("What is your character's name?")
	p.page.Type(name) // the first key replaces the suggested name
	p.page.Press("enter")
	p.waitText("Your character's history:")
	p.page.Press("enter")
	p.waitText("Start over")
	p.page.Press("enter")
	p.waitFor("the town", inTownScreen)
}

// TestPlay is Phase 3's automated play test: create a character through
// the client, walk, see the map change, and quit with Ctrl+C, which must
// save.
func TestPlay(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, m := startPlayer(t, 80, 24, false, d.Args("-n"))
	p.createCharacter()

	// Walk until the @ moves; some directions may be blocked by walls.
	before := p.page.Screen()
	y0, x0 := heroAt(before)
	moved := false
	for _, key := range []string{"left", "right", "up", "down", "4", "6", "8", "2"} {
		p.page.Press(key)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			y, x := heroAt(p.page.Screen())
			if y >= 0 && (y != y0 || x != x0) {
				moved = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if moved {
			break
		}
	}
	if !moved {
		t.Fatal("the @ never moved")
	}

	p.page.Press("ctrl+c")
	p.page.ExpectExit()
	if m.ByeReason() != "quit" {
		t.Errorf("BYE %q, want \"quit\"", m.ByeReason())
	}
	if m.Err() != nil {
		t.Errorf("session error: %v", m.Err())
	}
	m.Session().Wait()
	if _, err := os.Stat(filepath.Join(d.Save, "Tester")); err != nil {
		t.Errorf("no savefile after Ctrl+C: %v", err)
	}
}

// TestSidePanel checks that a wide screen shows the monster list beside
// the map and the stats bar.
func TestSidePanel(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, _ := startPlayer(t, 140, 36, true, d.Args("-n"))
	p.createCharacter()
	p.waitText("Monsters")
	// The monster list starts "You can see", even when it is empty.
	p.waitText("You can see")

	// The native HP gauge, fed by EVENT frames, shows a healthy warrior.
	p.page.Expect(p.page.GetByID(hpGaugeID)).ToHaveValue("100%")
	p.waitText("HP ")
}

// TestTooSmall checks the notice for a screen under 80x24.
func TestTooSmall(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, _ := startPlayer(t, 60, 20, true, d.Args("-n"))
	p.waitText("needs a window of at least 80x24")
}

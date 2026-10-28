package ui

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thebanri/limoni/uitest"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
	"fancyband/internal/session"
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
	bin := enginetest.Binary(t)
	var stderr bytes.Buffer
	m := New(Config{
		Start: func(terms []proto.TermSize) (*session.Session, error) {
			return session.Start(session.Options{
				Binary: bin,
				Args:   append([]string{"-mnet"}, args...),
				Stderr: &stderr,
				Terms:  terms,
			})
		},
		Width:      int(width),
		Height:     int(height),
		Subwindows: subwindows,
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
		// The sidebar is 13 columns wide; the map starts after it.
		runes := []rune(line)
		for x := 13; x < len(runes); x++ {
			if runes[x] == '@' {
				return y, x
			}
		}
	}
	return -1, -1
}

func inTownScreen(s string) bool {
	y, _ := heroAt(s)
	return y >= 0 && strings.Contains(s, "LEVEL") && strings.Contains(s, "\nTown ")
}

func (p *player) createCharacter() {
	p.t.Helper()
	p.waitText("[Press any key to continue]")
	p.page.Press("x")
	p.waitText("a) Human")
	p.page.Press("enter")
	p.waitText("Warrior")
	p.page.Press("enter")
	p.waitText("Point-based")
	p.page.Press("enter")
	p.waitText("'Enter' to accept")
	p.page.Press("enter")
	p.waitText("Enter a name for your character")
	p.page.Type("Tester")
	p.page.Press("enter")
	p.waitText("Accept character history? [y/n]")
	p.page.Press("y")
	p.waitText("any other key to continue")
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
	if _, err := os.Stat(d.Savefile()); err != nil {
		t.Errorf("no savefile after Ctrl+C: %v", err)
	}
}

// TestSidePanel checks that a wide screen shows the monster list beside
// the map.
func TestSidePanel(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, _ := startPlayer(t, 120, 36, true, d.Args("-n"))
	p.createCharacter()
	p.waitText("Monsters")
	// The town always has someone in it at the start.
	p.waitText("You can see")
}

// TestTooSmall checks the notice for a screen under 80x24.
func TestTooSmall(t *testing.T) {
	d := enginetest.NewDirs(t)
	p, _ := startPlayer(t, 60, 20, true, d.Args("-n"))
	p.waitText("needs a window of at least 80x24")
}

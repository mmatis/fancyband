package e2e

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// TestBorgStress lets Angband's automatic player, the borg, play through
// the net front end for FANCYBAND_BORG_SECONDS seconds, logs what the
// engine sent, and checks that the game still quits and saves. It is
// skipped unless that variable is set, because it takes a while. Set
// FANCYBAND_BORG_TILES to a tile set's number to play with tiles.
func TestBorgStress(t *testing.T) {
	secs, _ := strconv.Atoi(os.Getenv("FANCYBAND_BORG_SECONDS"))
	if secs <= 0 {
		t.Skip("set FANCYBAND_BORG_SECONDS to run the borg")
	}
	d := enginetest.NewDirs(t)
	cfg, err := os.ReadFile("../../engine/src/borg/borg.txt")
	if err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(filepath.Join(d.User, "borg.txt"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	var caps []string
	if ts := os.Getenv("FANCYBAND_BORG_TILES"); ts != "" {
		caps = []string{"tiles:" + ts}
	}
	g := startWith(t, dargs(d, "-n"), caps)
	g.createCharacter()
	// Ctrl+Z, two warnings, a check, then 'z' to start playing.
	g.key(proto.Ctrl('Z'))
	g.waitText("borg commands!")
	g.key(proto.KeyEnter)
	g.waitText("may become corrupted!")
	g.key(proto.KeyEnter)
	g.waitText("Are you sure")
	g.key('y')
	g.waitText("Borg command:")
	g.key('z')
	// Let the borg play, counting what the engine sends.
	start := time.Now()
	var msgs, bytes, freshes int
	g.s.SetReadDeadline(start.Add(time.Duration(secs) * time.Second))
	for {
		m, err := g.s.Recv()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			t.Fatal(err)
		}
		g.observe(m)
		g.set.Apply(m)
		msgs++
		bytes += len(m.Frame().Bytes())
		if f, ok := m.(proto.Fresh); ok && f.Term == 0 {
			freshes++
		}
	}
	g.s.SetReadDeadline(time.Time{})
	el := time.Since(start).Seconds()
	t.Logf("borg for %.0fs: %d messages, %.0f KB/s, %.0f frames/s", el, msgs, float64(bytes)/1024/el, float64(freshes)/el)
	t.Logf("screen:\n%s", g.screen())

	// Any key stops the borg; then the game must still quit and save.
	g.key(proto.KeyEscape)
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()
	requireSavefile(t, d)
}

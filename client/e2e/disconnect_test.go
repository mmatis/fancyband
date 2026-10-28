package e2e

import (
	"os"
	"syscall"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// These tests drop the connection at awkward moments. Each time the
// engine must exit cleanly with a good save, never a panic save.

// requireCleanSave checks that the engine left a save and no panic save,
// and that the save loads into the town with the character alive.
func requireCleanSave(t *testing.T, d enginetest.Dirs) {
	t.Helper()
	requireSavefile(t, d)
	if panics, _ := os.ReadDir(d.Panic); len(panics) > 0 {
		t.Errorf("the engine left a panic save: %s", panics[0].Name())
	}
	b := startBot(t, d.Args(), []string{"snapshots"})
	b.waitFor("game")
	p := b.snap.Player
	if p == nil || p.Name != "Tester" || p.Depth != 0 {
		t.Fatalf("loaded player: %+v", p)
	}
	if err := b.s.Send(proto.Quit{}); err != nil {
		t.Fatal(err)
	}
	for b.bye == "" {
		b.next()
	}
	if err := b.s.Wait(); err != nil {
		t.Fatalf("engine exit after loading: %v", err)
	}
}

// TestDisconnectAtPrompt drops the connection while the game asks which
// item to take off. A new warrior always wears something.
func TestDisconnectAtPrompt(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	g.key('t')
	g.waitText("Take off or unwield which item?")
	g.s.Close()
	g.waitExit()
	requireCleanSave(t, d)
}

// TestHangup is what happens when an SSH session dies: the engine gets
// SIGHUP, and the client dies with the terminal, which closes the
// connection. The engine ignores the signal and saves when the
// connection closes.
func TestHangup(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	if err := g.s.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	// The engine still answers.
	g.key('C')
	g.waitText("['c' to change name")
	g.s.Close()
	g.waitExit()
	requireCleanSave(t, d)
}

// TestTerminate sends the engine SIGTERM, as a shutdown would. It saves
// properly, not with Angband's panic save, and says why in BYE.
func TestTerminate(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	if err := g.s.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if reason := g.waitBye(); reason != "terminated" {
		t.Errorf("BYE %q, want \"terminated\"", reason)
	}
	g.waitExit()
	requireCleanSave(t, d)
}

// TestHeadlessSignals checks the same in headless mode, which never runs
// Angband's own signal set-up: SIGHUP is ignored, and SIGTERM saves.
func TestHeadlessSignals(t *testing.T) {
	d := enginetest.NewDirs(t)
	b := startBot(t, d.Args("-n"), []string{"events", "snapshots"})
	b.birth()
	if err := b.s.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	b.do("WALK", map[string]any{"direction": 5})
	b.waitForAny()
	if err := b.s.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	for b.bye == "" {
		b.next()
	}
	if b.bye != "terminated" {
		t.Errorf("BYE %q, want \"terminated\"", b.bye)
	}
	b.waitExit()
	requireCleanSave(t, d)
}

// TestDisconnectMidTurn sends a burst of moves and drops the connection
// at once, while the engine still has keys to act on.
func TestDisconnectMidTurn(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	for i := 0; i < 20; i++ {
		g.send(proto.Key{Code: uint32("2468"[i%4]), Mods: proto.ModKeypad})
	}
	g.s.Close()
	g.waitExit()
	requireCleanSave(t, d)
}

// TestDisconnectInStore walks a headless character into a shop and drops
// the connection there.
func TestDisconnectInStore(t *testing.T) {
	d := enginetest.NewDirs(t)
	b := startBot(t, d.Args("-n"), []string{"events", "snapshots"})
	b.birth()
	enterShop(t, b)
	b.s.Close()
	b.waitExit()
	requireCleanSave(t, d)
}

// TestDisconnectAtRequest drops the connection while a headless engine
// waits for the reply to a REQUEST.
func TestDisconnectAtRequest(t *testing.T) {
	d := enginetest.NewDirs(t)
	b := startBot(t, d.Args("-n"), []string{"events", "snapshots"})
	b.birth()
	b.noReply = true
	b.nextID++
	if err := b.s.Send(proto.NewCommand(b.nextID, "WALK", nil)); err != nil {
		t.Fatal(err)
	}
	for {
		if _, ok := b.next().(proto.Request); ok {
			break
		}
	}
	b.s.Close()
	b.waitExit()
	requireCleanSave(t, d)
}

// enterShop walks a headless character into a shop with the pathfinder.
// Townspeople interrupt the walk, so it tries many times, and it moves
// on to another shop when something blocks the way.
func enterShop(t *testing.T, b *bot) {
	t.Helper()
	m := b.snap.Map
	var shops [][2]int
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			if m.At(x, y) == 's' {
				shops = append(shops, [2]int{x, y})
			}
		}
	}
	if len(shops) == 0 {
		t.Fatal("no shop on the known town map")
	}
	for _, shop := range shops {
		stuck := 0
		for try := 0; try < 40 && stuck < 3; try++ {
			x, y := b.here()
			b.do("PATHFIND", map[string]any{"point": map[string]any{"x": shop[0], "y": shop[1]}})
			b.waitForAny()
			if b.context == "store" {
				return
			}
			if nx, ny := b.here(); nx == x && ny == y {
				stuck++
			} else {
				stuck = 0
			}
		}
	}
	t.Fatalf("could not reach any of the shops at %v from (%d,%d)", shops, b.snap.Player.X, b.snap.Player.Y)
}

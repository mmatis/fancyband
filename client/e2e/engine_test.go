// Package e2e runs the real fancyband engine with its net front end and
// plays it over the socket, the way fancyband-tui will.
//
// The tests need a built engine; see enginetest.Binary.
package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
	"fancyband/internal/session"
)

// waitTime bounds each wait for the screen to change.
const waitTime = 15 * time.Second

// dargs returns the engine's arguments for the net front end.
func dargs(d enginetest.Dirs, extra ...string) []string {
	return append([]string{"-mnet"}, d.Args(extra...)...)
}

type game struct {
	t      *testing.T
	s      *session.Session
	set    grid.Set
	stderr *bytes.Buffer
	bye    *proto.Bye
}

func start(t *testing.T, args []string) *game {
	t.Helper()
	g := &game{t: t, stderr: &bytes.Buffer{}}
	s, err := session.Start(session.Options{
		Binary: enginetest.Binary(t),
		Args:   args,
		Stderr: g.stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	g.s = s
	t.Cleanup(func() {
		s.Kill()
		s.Wait()
		if t.Failed() {
			t.Logf("engine stderr:\n%s", g.stderr)
			if g.set.Terms[0] != nil {
				t.Logf("last screen:\n%s", g.set.Terms[0])
			}
		}
	})
	return g
}

// screen returns the main term as last refreshed.
func (g *game) screen() *grid.Grid { return g.set.Terms[0] }

// waitFor reads frames until the main term, after a FRESH, passes ok.
func (g *game) waitFor(what string, ok func(*grid.Grid) bool) {
	g.t.Helper()
	g.s.SetReadDeadline(time.Now().Add(waitTime))
	defer g.s.SetReadDeadline(time.Time{})
	for {
		m, err := g.s.Recv()
		if err != nil {
			g.t.Fatalf("waiting for %s: %v", what, err)
		}
		if bye, isBye := m.(proto.Bye); isBye {
			g.t.Fatalf("waiting for %s: engine said BYE %q", what, bye.Reason)
		}
		if g.set.Apply(m) == 0 && ok(g.screen()) {
			return
		}
	}
}

// waitText waits until the main term shows s.
func (g *game) waitText(s string) {
	g.t.Helper()
	g.waitFor(strings.TrimSpace(s), func(gr *grid.Grid) bool { return gr.Contains(s) })
}

// waitBye reads frames until BYE and returns its reason.
func (g *game) waitBye() string {
	g.t.Helper()
	g.s.SetReadDeadline(time.Now().Add(waitTime))
	for {
		m, err := g.s.Recv()
		if err != nil {
			g.t.Fatalf("waiting for BYE: %v", err)
		}
		if bye, ok := m.(proto.Bye); ok {
			return bye.Reason
		}
		g.set.Apply(m)
	}
}

func (g *game) send(m proto.Message) {
	g.t.Helper()
	if err := g.s.Send(m); err != nil {
		g.t.Fatalf("sending %T: %v", m, err)
	}
}

func (g *game) key(code uint32) { g.send(proto.Key{Code: code}) }

func (g *game) typeText(s string) {
	for _, r := range s {
		g.key(uint32(r))
	}
}

// waitExit waits for the engine to exit and checks that it succeeded.
func (g *game) waitExit() {
	g.t.Helper()
	done := make(chan error, 1)
	go func() { done <- g.s.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			g.t.Fatalf("engine exit: %v", err)
		}
	case <-time.After(waitTime):
		g.t.Fatal("engine did not exit")
	}
}

// inTown reports whether the main term shows the town map, with the
// sidebar's level and its depth line reading "Town".
func inTown(gr *grid.Grid) bool {
	if !gr.Contains("@") || !gr.Contains("LEVEL") {
		return false
	}
	for y := 0; y < gr.Height; y++ {
		if strings.HasPrefix(gr.Line(y), "Town ") {
			return true
		}
	}
	return false
}

// createCharacter goes from the splash screen to the town, taking the
// default choice at every step.
func (g *game) createCharacter() {
	g.t.Helper()
	g.waitText("[Press any key to continue]")
	g.key('x')
	g.waitText("a) Human")
	g.key(proto.KeyEnter) // race
	g.waitText("Warrior")
	g.key(proto.KeyEnter) // class
	g.waitText("Point-based")
	g.key(proto.KeyEnter) // stat roller
	g.waitText("'Enter' to accept")
	g.key(proto.KeyEnter) // stats
	g.waitText("Enter a name for your character")
	g.typeText("Tester") // the first key replaces the default name
	g.key(proto.KeyEnter)
	g.waitText("Accept character history? [y/n]")
	g.key('y')
	g.waitText("any other key to continue")
	g.key(proto.KeyEnter)
	g.waitFor("the town", inTown)
}

func requireSavefile(t *testing.T, d enginetest.Dirs) {
	t.Helper()
	info, err := os.Stat(d.Savefile())
	if err != nil {
		t.Fatalf("no savefile: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("the savefile is empty")
	}
}

// TestHandshake checks the server's HELLO and the terms it opens.
func TestHandshake(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	if g.s.Server.Version != proto.Version {
		t.Errorf("server version %d, want %d", g.s.Server.Version, proto.Version)
	}
	if !strings.Contains(g.s.Server.Name, "Fancyband") {
		t.Errorf("server name %q does not name Fancyband", g.s.Server.Name)
	}
	g.waitText("[Press any key to continue]")
	if g.screen().Width != 80 || g.screen().Height != 24 {
		t.Errorf("main term is %dx%d, want 80x24", g.screen().Width, g.screen().Height)
	}
	if len(g.set.Palette) != 32 {
		t.Errorf("palette has %d colors, want 32", len(g.set.Palette))
	}
	g.send(proto.Quit{})
	if reason := g.waitBye(); reason != "quit" {
		t.Errorf("BYE %q, want \"quit\"", reason)
	}
	g.waitExit()
}

// TestCreateCharacterQuitAndLoad is the Phase 2 exit test: create a
// character with keys alone, reach the town, quit, and find the save.
// It then loads the save to show the save is good.
func TestCreateCharacterQuitAndLoad(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	g.send(proto.Quit{})
	if reason := g.waitBye(); reason != "quit" {
		t.Errorf("BYE %q, want \"quit\"", reason)
	}
	g.waitExit()
	requireSavefile(t, d)

	g = start(t, dargs(d))
	g.waitText("[Press any key to continue]")
	g.key('x')
	g.waitFor("the loaded game", func(gr *grid.Grid) bool {
		return inTown(gr) && gr.Contains("Warrior")
	})
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()
}

// TestDisconnectSaves drops the connection mid-game. The engine must
// save and exit cleanly.
func TestDisconnectSaves(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	g.s.Close()
	g.waitExit()
	requireSavefile(t, d)
}

// TestDisconnectDuringBirth drops the connection while the character is
// still being made. The engine exits without saving.
func TestDisconnectDuringBirth(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.waitText("[Press any key to continue]")
	g.key('x')
	g.waitText("a) Human")
	g.s.Close()
	g.waitExit()
	if _, err := os.Stat(d.Savefile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("savefile after quitting in birth: %v", err)
	}
}

// TestResize asks for a larger main term and checks the engine uses it.
func TestResize(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	g.send(proto.Resize{Term: 0, Width: 100, Height: 30})
	g.waitFor("the town at 100x30", func(gr *grid.Grid) bool {
		return gr.Width == 100 && gr.Height == 30 && inTown(gr)
	})

	// The main term cannot shrink below 80x24.
	g.send(proto.Resize{Term: 0, Width: 40, Height: 10})
	g.waitFor("the town at 80x24", func(gr *grid.Grid) bool {
		return gr.Width == 80 && gr.Height == 24 && inTown(gr)
	})
	g.send(proto.Quit{})
	g.waitBye()
	g.waitExit()
}

// TestSubwindow opens a second term and checks it is announced.
func TestSubwindow(t *testing.T) {
	d := enginetest.NewDirs(t)
	bin := enginetest.Binary(t)
	var stderr bytes.Buffer
	s, err := session.Start(session.Options{
		Binary: bin,
		Args:   dargs(d, "-n"),
		Stderr: &stderr,
		Terms:  []proto.TermSize{{Index: 0, Width: 80, Height: 24}, {Index: 1, Width: 60, Height: 12}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.Kill()
		s.Wait()
	}()
	s.SetReadDeadline(time.Now().Add(waitTime))
	seen := map[uint8]proto.TermOpen{}
	for len(seen) < 2 {
		m, err := s.Recv()
		if err != nil {
			t.Fatalf("waiting for TERM_OPEN: %v\n%s", err, stderr.String())
		}
		if open, ok := m.(proto.TermOpen); ok {
			seen[open.Term] = open
		}
	}
	if sub := seen[1]; sub.Width != 60 || sub.Height != 12 {
		t.Errorf("term 1 is %dx%d, want 60x12", sub.Width, sub.Height)
	}
}

// TestBadClientVersion checks that the engine refuses a client that
// speaks another protocol version.
func TestBadClientVersion(t *testing.T) {
	bin := enginetest.Binary(t)
	d := enginetest.NewDirs(t)

	fds, err := socketpair()
	if err != nil {
		t.Fatal(err)
	}
	ours, theirs := fds[0], fds[1]
	defer ours.Close()

	cmd := exec.Command(bin, append(dargs(d), "--", "--fd", "3")...)
	cmd.Dir = filepath.Dir(bin)
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
	cmd.ExtraFiles = []*os.File{theirs}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	theirs.Close()

	proto.WriteFrame(ours, proto.Hello{Version: 99, Name: "future"}.Frame())
	ours.SetReadDeadline(time.Now().Add(waitTime))
	var reason string
	for reason == "" {
		f, err := proto.ReadFrame(ours)
		if err != nil {
			t.Fatalf("reading: %v", err)
		}
		if f.Type == proto.TypeBye {
			reason = string(f.Payload)
		}
	}
	if !strings.Contains(reason, "99") {
		t.Errorf("BYE %q does not mention the version", reason)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("engine exited with success after refusing the client")
	}
}

// socketpair returns both ends of a Unix stream socket pair. The first
// end is non-blocking so the test can set deadlines on it.
func socketpair() ([2]*os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return [2]*os.File{}, err
	}
	if err := syscall.SetNonblock(fds[0], true); err != nil {
		return [2]*os.File{}, err
	}
	return [2]*os.File{os.NewFile(uintptr(fds[0]), "client"), os.NewFile(uintptr(fds[1]), "engine")}, nil
}

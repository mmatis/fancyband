package e2e

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
	"fancyband/internal/session"
)

// bot plays a headless engine with commands only: no keys, no screen.
type bot struct {
	t      *testing.T
	s      *session.Session
	stderr *bytes.Buffer
	nextID int
	// The last snapshot and the context of the last WAIT.
	snap    proto.SnapshotData
	context string
	// Requests the engine sent, answered by answer unless noReply is set.
	answer  func(proto.RequestData) map[string]any
	noReply bool
	// Every event name seen, in order.
	events []string
	bye    string
}

func startBot(t *testing.T, args []string, caps []string) *bot {
	t.Helper()
	b := &bot{t: t, stderr: &bytes.Buffer{}}
	s, err := session.Start(session.Options{
		Binary:     enginetest.Binary(t),
		Args:       append([]string{"-mnet"}, args...),
		ModuleArgs: []string{"--headless"},
		Stderr:     b.stderr,
		Terms:      []proto.TermSize{},
		Caps:       caps,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.s = s
	t.Cleanup(func() {
		s.Kill()
		s.Wait()
		if t.Failed() {
			t.Logf("engine stderr:\n%s", b.stderr)
		}
	})
	return b
}

// next reads one message, keeping the latest snapshot and answering
// requests. It returns the message.
func (b *bot) next() proto.Message {
	b.t.Helper()
	b.s.SetReadDeadline(time.Now().Add(waitTime))
	m, err := b.s.Recv()
	if err != nil {
		b.t.Fatalf("reading: %v (events so far: %v)", err, b.lastEvents())
	}
	switch m := m.(type) {
	case proto.Snapshot:
		d, err := m.Parse()
		if err != nil {
			b.t.Fatalf("bad snapshot: %v\n%s", err, m.JSON)
		}
		b.snap = d
	case proto.Event:
		d, err := m.Parse()
		if err != nil {
			b.t.Fatalf("bad event: %v\n%s", err, m.JSON)
		}
		b.events = append(b.events, d.Event)
		if d.Event == "WAIT" {
			b.context = d.Context
		}
	case proto.Request:
		d, err := m.Parse()
		if err != nil {
			b.t.Fatalf("bad request: %v\n%s", err, m.JSON)
		}
		if b.noReply {
			break
		}
		var fields map[string]any
		if b.answer != nil {
			fields = b.answer(d)
		}
		if err := b.s.Send(proto.NewReply(d.ID, fields)); err != nil {
			b.t.Fatal(err)
		}
	case proto.Bye:
		b.bye = m.Reason
	}
	return m
}

func (b *bot) lastEvents() []string {
	if len(b.events) > 20 {
		return b.events[len(b.events)-20:]
	}
	return b.events
}

// waitFor reads until the engine waits for a command in context.
func (b *bot) waitFor(context string) {
	b.t.Helper()
	for {
		m := b.next()
		if ev, ok := m.(proto.Event); ok {
			d, _ := ev.Parse()
			if d.Event == "WAIT" {
				if d.Context != context {
					b.t.Fatalf("engine waits in %q, want %q", d.Context, context)
				}
				return
			}
		}
		if _, ok := m.(proto.Bye); ok {
			b.t.Fatalf("engine said BYE %q while waiting for %s", b.bye, context)
		}
	}
}

// waitForAny reads until the engine waits for a command, in any context.
func (b *bot) waitForAny() {
	b.t.Helper()
	for {
		m := b.next()
		if ev, ok := m.(proto.Event); ok {
			if d, _ := ev.Parse(); d.Event == "WAIT" {
				return
			}
		}
		if _, ok := m.(proto.Bye); ok {
			b.t.Fatalf("engine said BYE %q", b.bye)
		}
	}
}

// waitExit waits for the engine to exit and checks that it succeeded.
func (b *bot) waitExit() {
	b.t.Helper()
	done := make(chan error, 1)
	go func() { done <- b.s.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			b.t.Fatalf("engine exit: %v", err)
		}
	case <-time.After(waitTime):
		b.t.Fatal("engine did not exit")
	}
}

// do sends a command and checks the engine accepts it. It does not wait
// for the command to run; call waitFor next.
func (b *bot) do(name string, args map[string]any) {
	b.t.Helper()
	b.nextID++
	id := b.nextID
	if err := b.s.Send(proto.NewCommand(id, name, args)); err != nil {
		b.t.Fatal(err)
	}
	for {
		m := b.next()
		ev, ok := m.(proto.Event)
		if !ok {
			continue
		}
		d, _ := ev.Parse()
		if d.Event == "COMMAND" && d.ID == id {
			if !d.OK {
				b.t.Fatalf("%s refused: %s", name, d.Error)
			}
			return
		}
	}
}

// refused sends a command and returns the engine's reason for refusing
// it, or fails if it was accepted.
func (b *bot) refused(name string, args map[string]any) string {
	b.t.Helper()
	b.nextID++
	id := b.nextID
	b.s.Send(proto.NewCommand(id, name, args))
	for {
		ev, ok := b.next().(proto.Event)
		if !ok {
			continue
		}
		d, _ := ev.Parse()
		if d.Event == "COMMAND" && d.ID == id {
			if d.OK {
				b.t.Fatalf("%s was accepted", name)
			}
			return d.Error
		}
	}
}

// birth makes a human warrior named Tester with point-based stats.
func (b *bot) birth() {
	b.t.Helper()
	b.waitFor("birth")
	if b.snap.Birth == nil || len(b.snap.Birth.Races) == 0 {
		b.t.Fatalf("no birth choices in the snapshot: %+v", b.snap)
	}
	race := slices.Index(b.snap.Birth.Races, "Human")
	class := slices.Index(b.snap.Birth.Classes, "Warrior")
	if race < 0 || class < 0 {
		b.t.Fatalf("no Human or Warrior in %+v", b.snap.Birth)
	}
	steps := []struct {
		cmd  string
		args map[string]any
	}{
		{"CHOOSE_RACE", map[string]any{"choice": race}},
		{"CHOOSE_CLASS", map[string]any{"choice": class}},
		{"RESET_STATS", map[string]any{"choice": 1}},
		{"NAME_CHOICE", map[string]any{"name": "Tester"}},
		{"ACCEPT_CHARACTER", nil},
	}
	for i, step := range steps {
		b.do(step.cmd, step.args)
		if i < len(steps)-1 {
			b.waitFor("birth")
		}
	}
	b.waitFor("game")
}

func (b *bot) here() (int, int) {
	return b.snap.Player.X, b.snap.Player.Y
}

// The direction numbers, as on the number pad, for each step.
var dirs = map[[2]int]int{
	{-1, 1}: 1, {0, 1}: 2, {1, 1}: 3, {-1, 0}: 4,
	{1, 0}: 6, {-1, -1}: 7, {0, -1}: 8, {1, -1}: 9,
}

// TestHeadlessSmoke is the Phase 4 exit test: create a character, walk
// to the down staircase using snapshots, descend, save, and quit, all
// through commands.
func TestHeadlessSmoke(t *testing.T) {
	d := enginetest.NewDirs(t)
	b := startBot(t, d.Args("-n"), []string{"events", "snapshots"})
	b.birth()

	p := b.snap.Player
	if p == nil || p.Name != "Tester" || p.Race != "Human" || p.Class != "Warrior" || p.Depth != 0 {
		t.Fatalf("player after birth: %+v", p)
	}
	if b.snap.Map == nil || len(b.snap.Map.Rows) != b.snap.Map.Height {
		t.Fatalf("no map in the snapshot")
	}
	stairX, stairY, ok := b.snap.Map.Find('>')
	if !ok {
		t.Fatal("no down staircase on the known town map")
	}

	// Commands from the wrong context are refused.
	if why := b.refused("CHOOSE_RACE", map[string]any{"choice": 0}); !strings.Contains(why, "not allowed") {
		t.Errorf("CHOOSE_RACE in the game: %q", why)
	}
	if why := b.refused("WIZ_CURE_ALL", nil); !strings.Contains(why, "not allowed") {
		t.Errorf("a debug command: %q", why)
	}

	// Step off the staircase. The first walk leaves out its direction,
	// so the engine asks for it with a REQUEST.
	x, y := b.here()
	step := -1
	for delta, dir := range dirs {
		if b.snap.Map.At(x+delta[0], y+delta[1]) == '.' {
			step = dir
			break
		}
	}
	if step < 0 {
		t.Fatal("no open floor next to the player")
	}
	asked := false
	b.answer = func(r proto.RequestData) map[string]any {
		if r.Prompt == "direction" {
			asked = true
			return map[string]any{"value": step}
		}
		return nil
	}
	b.do("WALK", nil)
	b.waitFor("game")
	if !asked {
		t.Error("WALK without a direction did not ask for one")
	}
	if nx, ny := b.here(); nx == x && ny == y {
		t.Fatalf("the player did not move from (%d,%d)", x, y)
	}

	// Walk back to the staircase with the pathfinder, using the snapshot
	// to see where it is. A monster can interrupt, so try a few times.
	for try := 0; try < 10; try++ {
		if px, py := b.here(); px == stairX && py == stairY {
			break
		}
		b.do("PATHFIND", map[string]any{"point": map[string]any{"x": stairX, "y": stairY}})
		b.waitFor("game")
	}
	if px, py := b.here(); px != stairX || py != stairY {
		t.Fatalf("the player is at (%d,%d), not on the stairs at (%d,%d)", px, py, stairX, stairY)
	}

	b.do("GO_DOWN", nil)
	b.waitFor("game")
	if b.snap.Player.Depth != 1 {
		t.Fatalf("depth %d after GO_DOWN, want 1", b.snap.Player.Depth)
	}
	if !slices.Contains(b.events, "DUNGEONLEVEL") || !slices.Contains(b.events, "HP") {
		t.Errorf("missing events; saw %v", b.lastEvents())
	}

	if err := b.s.Send(proto.Quit{}); err != nil {
		t.Fatal(err)
	}
	for b.bye == "" {
		b.next()
	}
	if b.bye != "quit" {
		t.Errorf("BYE %q, want \"quit\"", b.bye)
	}
	if err := b.s.Wait(); err != nil {
		t.Fatalf("engine exit: %v", err)
	}
	if _, err := os.Stat(d.Savefile()); err != nil {
		t.Fatalf("no savefile: %v", err)
	}

	// The save loads into a headless game at depth 1.
	b = startBot(t, d.Args(), []string{"snapshots"})
	b.waitFor("game")
	if b.snap.Player == nil || b.snap.Player.Depth != 1 || b.snap.Player.Name != "Tester" {
		t.Fatalf("loaded player: %+v", b.snap.Player)
	}
}

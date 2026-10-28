package e2e

import (
	"errors"
	"os"
	"testing"
	"time"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
	"fancyband/internal/session"
)

// traffic is what the engine sent for one action.
type traffic struct {
	msgs, bytes, freshes int
	// emptyFreshes counts FRESH messages with nothing new before them.
	emptyFreshes int
	// lag is the time from the key to the first FRESH of the main term.
	lag time.Duration
}

// measure reads until the engine has been quiet for quiet, counting what
// it sent since start.
func (g *game) measure(start time.Time, quiet time.Duration) traffic {
	g.t.Helper()
	var tr traffic
	drawn := false
	for {
		g.s.SetReadDeadline(time.Now().Add(quiet))
		m, err := g.s.Recv()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				g.s.SetReadDeadline(time.Time{})
				return tr
			}
			g.t.Fatal(err)
		}
		g.observe(m)
		g.set.Apply(m)
		tr.msgs++
		tr.bytes += len(m.Frame().Bytes())
		switch m := m.(type) {
		case proto.Fresh:
			if m.Term != 0 {
				break
			}
			if tr.freshes == 0 {
				tr.lag = time.Since(start)
			}
			tr.freshes++
			if !drawn {
				tr.emptyFreshes++
			}
			drawn = false
		case proto.Text, proto.Wipe, proto.Clear, proto.Cursor, proto.Pict:
			drawn = true
		}
	}
}

// TestSpeed logs what the engine sends to enter the town and for each
// move, and checks that it never sends a FRESH with nothing new.
func TestSpeed(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	for i, k := range "2486" {
		t0 := time.Now()
		g.send(proto.Key{Code: uint32(k), Mods: proto.ModKeypad})
		tr := g.measure(t0, 200*time.Millisecond)
		t.Logf("move %d: %d messages, %d bytes, %d FRESH, first FRESH after %v", i, tr.msgs, tr.bytes, tr.freshes, tr.lag)
		if tr.emptyFreshes > 0 {
			t.Errorf("move %d: %d FRESH messages with nothing new", i, tr.emptyFreshes)
		}
	}
}

// TestOneBatchPerMove checks that a move reaches the client in one batch,
// so the client redraws once. The engine refreshes the screen after every
// map grid it changes, 20 to 40 times a move; it now sends them all
// together when the game next waits for a key.
func TestOneBatchPerMove(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	g.createCharacter()
	// Let the rest of the town's drawing arrive first.
	g.measure(time.Now(), 300*time.Millisecond)
	stream := make(chan session.Batch, 64)
	go g.s.Stream(stream)
	for i, k := range "24862486" {
		g.send(proto.Key{Code: uint32(k), Mods: proto.ModKeypad})
		batches := 0
		quiet := time.After(300 * time.Millisecond)
	wait:
		for {
			select {
			case b := <-stream:
				if b.Err != nil {
					t.Fatal(b.Err)
				}
				batches++
			case <-quiet:
				break wait
			}
		}
		// A big redraw can fill the socket buffer and split; a move
		// should not.
		if batches > 1 {
			t.Errorf("move %d arrived in %d batches", i, batches)
		}
	}
}

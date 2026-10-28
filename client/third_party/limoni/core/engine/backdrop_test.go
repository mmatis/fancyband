package engine

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
)

// tickingScene changes colour every frame and counts how often it is drawn.
type tickingScene struct{ renders atomic.Int32 }

func (s *tickingScene) Interval() time.Duration { return 10 * time.Millisecond }

func (s *tickingScene) Render(dst *buffer.Buffer, t time.Duration) {
	s.renders.Add(1)
	v := uint8(t / time.Millisecond)
	for i := range dst.Content {
		dst.Content[i] = cell.Cell{Content: ' ', Style: cell.Style{Bg: cell.NewColorRGB(v, 0, 0)}}
	}
}

// A backdrop moves on its own: a Program without a frame rate still draws
// only when something happens to it, while the scene behind it keeps going.
func TestRunTerminalMovesTheBackdropWithoutCallingView(t *testing.T) {
	t.Setenv("LIMONI_PROBE", "0")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("LIMONI_BACKDROP", "")
	scene := &tickingScene{}
	m, io := startIdle(t, WithBackdrop(scene))
	io.mu.Lock()
	before := len(io.out)
	io.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	if n := m.views.Load(); n != 1 {
		t.Fatalf("View ran %d times; the backdrop should move without it", n)
	}
	if n := scene.renders.Load(); n < 5 {
		t.Fatalf("the backdrop was drawn %d times in 200ms at 100 fps", n)
	}
	io.mu.Lock()
	after := len(io.out)
	io.mu.Unlock()
	if after == before {
		t.Fatal("the backdrop's frames never reached the terminal")
	}
}

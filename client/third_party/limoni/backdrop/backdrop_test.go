package backdrop

import (
	"fmt"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

// sizes the tests run every scene at: a common one, a small one, and odd
// ones that catch an off-by-one at an edge.
var sizes = [][2]int{{120, 40}, {40, 12}, {81, 27}, {3, 2}, {1, 1}, {200, 60}}

func scenes() map[string]terminal.Backdrop {
	m := map[string]terminal.Backdrop{}
	for _, n := range Names() {
		m[n] = New(n)
	}
	m["dim(city)"] = Dim(City(), 0.6)
	return m
}

func newBuf(w, h int) *buffer.Buffer {
	return buffer.NewBuffer(cell.NewRect(0, 0, uint16(w), uint16(h)))
}

// Render must paint every cell, since dst still holds the last frame, and
// with glyphs one column wide, since the application's cells are laid over
// the scene one for one.
func TestScenesPaintEveryCellWithNarrowGlyphs(t *testing.T) {
	sentinel := cell.Cell{Content: 'Z', Style: cell.Style{Fg: cell.NewColorANSI(9), Bg: cell.NewColorANSI(9)}}
	for name, s := range scenes() {
		for _, sz := range sizes {
			for _, at := range []time.Duration{0, 1234 * time.Millisecond, 97 * time.Second} {
				b := newBuf(sz[0], sz[1])
				for i := range b.Content {
					b.Content[i] = sentinel
				}
				s.Render(b, at)
				for i, c := range b.Content {
					if c == sentinel {
						t.Fatalf("%s %v at %v: cell %d not painted", name, sz, at, i)
					}
					if c.Style.Bg.Type() != cell.ColorRGB {
						t.Fatalf("%s %v at %v: cell %d has no background colour: %+v", name, sz, at, i, c)
					}
					if w := cell.RuneWidth(c.Content); w != 1 {
						t.Fatalf("%s %v at %v: cell %d holds %q, %d columns wide", name, sz, at, i, c.Content, w)
					}
				}
			}
		}
	}
}

// A scene is a function of time and size: DrawBackdrop redraws a moment the
// application's frame already showed, and a resize rebuilds the scene, so
// neither may change what a moment looks like.
func TestScenesAreFunctionsOfTime(t *testing.T) {
	for _, name := range Names() {
		a, b := New(name), New(name)
		x, y := newBuf(90, 30), newBuf(90, 30)
		a.Render(x, 3*time.Second)
		a.Render(newBuf(50, 20), time.Second) // another size in between
		a.Render(x, 7*time.Second)
		b.Render(y, 7*time.Second)
		for i := range x.Content {
			if x.Content[i] != y.Content[i] {
				t.Fatalf("%s: cell %d differs between two scenes at the same moment", name, i)
			}
		}
	}
}

// A scene that never changes is a bug in an animation; so is one that
// changes most of the screen every frame.
func TestScenesMove(t *testing.T) {
	for _, name := range Names() {
		s := New(name)
		first, cur := newBuf(120, 40), newBuf(120, 40)
		s.Render(first, 0)
		moved := false
		for f := 1; f <= 90 && !moved; f++ {
			s.Render(cur, time.Duration(f)*s.Interval())
			for i := range cur.Content {
				if cur.Content[i] != first.Content[i] {
					moved = true
					break
				}
			}
		}
		if !moved {
			t.Errorf("%s: nothing moved in 90 frames", name)
		}
	}
}

func TestScenesDoNotAllocate(t *testing.T) {
	for name, s := range scenes() {
		b := newBuf(120, 40)
		s.Render(b, 0)
		at := time.Duration(0)
		allocs := testing.AllocsPerRun(50, func() {
			at += s.Interval()
			s.Render(b, at)
		})
		if allocs != 0 {
			t.Errorf("%s: %v allocations a frame", name, allocs)
		}
	}
}

func TestDimScalesColours(t *testing.T) {
	src, dim := newBuf(40, 12), newBuf(40, 12)
	Starfield().Render(src, time.Second)
	Dim(Starfield(), 0.5).Render(dim, time.Second)
	for i := range src.Content {
		r, g, b := src.Content[i].Style.Bg.RGB()
		dr, dg, db := dim.Content[i].Style.Bg.RGB()
		if dr != uint8(uint32(r)*128>>8) || dg != uint8(uint32(g)*128>>8) || db != uint8(uint32(b)*128>>8) {
			t.Fatalf("cell %d: %d,%d,%d dimmed to %d,%d,%d", i, r, g, b, dr, dg, db)
		}
	}
	if Dim(City(), 0.3).Interval() != City().Interval() {
		t.Fatal("Dim changed the scene's pace")
	}
}

// traffic plays a scene for ten seconds at its own pace through the real
// encoder, and returns the bytes sent a second and the share of a full
// repaint that is.
func traffic(s terminal.Backdrop, w, h int) (perSecond int, share float64) {
	opts := buffer.DiffOptions{TrueColor: true, EraseChar: true, RepeatChar: true}
	front, back, full := newBuf(w, h), newBuf(w, h), buffer.NewEmptyBuffer()
	var out []byte
	s.Render(front, 0)
	out, _ = buffer.DiffWithOptions(front, back, out[:0], opts)
	frames := int(10 * time.Second / s.Interval())
	sent, repaint := 0, 0
	for f := 1; f <= frames; f++ {
		s.Render(front, time.Duration(f)*s.Interval())
		front.Invalidate()
		out, _ = buffer.DiffWithOptions(front, back, out[:0], opts)
		sent += len(out)
		if f%10 == 0 {
			// A back buffer of another size makes the encoder repaint the
			// whole screen, with the same options.
			full.Resize(cell.Rect{})
			front.IsDirty = true
			out, _ = buffer.DiffWithOptions(front, full, out[:0], opts)
			repaint += len(out) * 10
		}
	}
	return sent / 10, float64(sent) / float64(repaint)
}

// What a scene costs is what it changes. These budgets are a quarter of
// what the same scene would cost repainted every frame — the point of
// building scenes for the diff — and they fail if a change makes a scene
// rewrite the screen.
func TestSceneTraffic(t *testing.T) {
	for _, name := range Names() {
		perSecond, share := traffic(New(name), 120, 40)
		t.Logf("%-10s %6.1f KB/s, %4.1f%% of repainting every frame", name, float64(perSecond)/1024, share*100)
		if share > 0.25 {
			t.Errorf("%s sends %.0f%% of a full repaint every frame", name, share*100)
		}
	}
}

func BenchmarkScenes(b *testing.B) {
	for _, name := range Names() {
		b.Run(name, func(b *testing.B) {
			s := New(name)
			buf := newBuf(120, 40)
			s.Render(buf, 0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.Render(buf, time.Duration(i)*s.Interval())
			}
		})
	}
}

// BenchmarkSceneFrame is a scene's whole frame: drawn, laid under an
// application's cells, and encoded as a diff.
func BenchmarkSceneFrame(b *testing.B) {
	for _, name := range Names() {
		b.Run(name, func(b *testing.B) {
			s := New(name)
			opts := buffer.DiffOptions{TrueColor: true, EraseChar: true, RepeatChar: true}
			scene, front, back := newBuf(120, 40), newBuf(120, 40), newBuf(120, 40)
			app := make([]cell.Cell, 120*40)
			for i := range app {
				app[i] = cell.Cell{Content: ' '}
			}
			for i, r := range "Plain text floating over the backdrop" {
				app[5*120+10+i] = cell.Cell{Content: r}
			}
			out := make([]byte, 0, 1<<16)
			frame := func(i int) {
				s.Render(scene, time.Duration(i)*s.Interval())
				copy(front.Content, app)
				terminal.ComposeBackdrop(front.Content, scene.Content)
				front.Invalidate()
				out, _ = buffer.DiffWithOptions(front, back, out[:0], opts)
			}
			for i := 0; i < 400; i++ { // warm the style cache
				frame(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			bytes := 0
			for i := 0; i < b.N; i++ {
				frame(400 + i)
				bytes += len(out)
			}
			b.ReportMetric(float64(bytes)/float64(b.N), "B/frame")
		})
	}
}

func ExampleNew() {
	for _, name := range Names() {
		fmt.Println(name, New(name).Interval())
	}
	// Output:
	// aurora 50ms
	// city 33.333333ms
	// starfield 33.333333ms
	// synthwave 41.666666ms
}

// A still scene asks for no frames and draws the same picture whatever the
// time: the moment it was frozen at.
func TestStillFreezesAScene(t *testing.T) {
	s := Still(Aurora(), 5*time.Second)
	if s.Interval() != 0 {
		t.Fatalf("a still scene asks for frames every %v", s.Interval())
	}
	a, b, want := newBuf(60, 20), newBuf(60, 20), newBuf(60, 20)
	s.Render(a, 0)
	s.Render(b, time.Hour)
	Aurora().Render(want, 5*time.Second)
	for i := range a.Content {
		if a.Content[i] != want.Content[i] || b.Content[i] != want.Content[i] {
			t.Fatalf("cell %d is not the frozen moment", i)
		}
	}
}

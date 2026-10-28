package backdrop

import (
	"math"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
)

// sweep is the scene written out in docs/backdrop-art.md, section 10. It is
// here so the guide's example is known to compile, to allocate nothing and
// to be as cheap as the guide says it is.
type sweep struct{}

func (sweep) Interval() time.Duration { return time.Second / 15 }

func (sweep) Render(dst *buffer.Buffer, t time.Duration) {
	w, h := int(dst.Area.Width), int(dst.Area.Height)
	// Where the band is: one pass down the screen every four seconds.
	band := math.Mod(t.Seconds()/4, 1)*float64(h+8) - 4
	for y := 0; y < h; y++ {
		d := math.Abs(float64(y) - band)
		// Quantised to eight steps: a row changes only when the band
		// crosses a step, not on every frame.
		k := math.Floor(math.Max(0, 1-d/4)*8) / 8
		bg := cell.NewColorRGB(uint8(8+k*30), uint8(10+k*40), uint8(24+k*60))
		for x := 0; x < w; x++ {
			dst.Content[y*w+x] = cell.Cell{Content: ' ', Style: cell.Style{Bg: bg}}
		}
	}
}

func TestTheGuidesExampleScene(t *testing.T) {
	b := newBuf(120, 40)
	at := time.Duration(0)
	if n := testing.AllocsPerRun(50, func() {
		at += sweep{}.Interval()
		sweep{}.Render(b, at)
	}); n != 0 {
		t.Fatalf("%v allocations a frame", n)
	}
	perSecond, share := traffic(sweep{}, 120, 40)
	t.Logf("sweep: %.1f KB/s, %.1f%% of repainting every frame", float64(perSecond)/1024, share*100)
	// A flat scene repaints almost for free — one colour and a run per row —
	// so its share of a repaint says little; what it sends is the measure.
	if perSecond > 10*1024 {
		t.Fatalf("the guide's example sends %.1f KB a second", float64(perSecond)/1024)
	}
}

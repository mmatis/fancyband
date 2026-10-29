package ui

import (
	"context"
	"time"

	"github.com/thebanri/limoni/core/engine"

	"fancyband/internal/grid"
)

// tickTime is how long each step of the animated filters on tiles lasts
// (see tiles.Filter).
const tickTime = 120 * time.Millisecond

func nextTick() engine.Cmd {
	return func(ctx context.Context) engine.Msg {
		select {
		case <-time.After(tickTime):
			return animTick{}
		case <-ctx.Done():
			return nil
		}
	}
}

// hasAnimation reports whether the term shows any animated tile, so
// ticks with nothing to animate cost no redraw.
func (m *Model) hasAnimation(g *grid.Grid) bool {
	if m.sheet == nil {
		return false
	}
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			if c := g.Cell(x, y); c.Tile && m.sheet.Animated(c.Pict) {
				return true
			}
		}
	}
	return false
}

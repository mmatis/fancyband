package ui

import (
	"context"
	"time"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/engine"

	"fancyband/internal/grid"
	"fancyband/internal/tiles"
)

// tickTime is how long each step of the lava wave lasts. A full swing
// from red to yellow and back takes 2*(tiles.ShiftSteps-1) steps.
const tickTime = 120 * time.Millisecond

// Palette slots for the ends of the lava colors (z-color.h).
const (
	colorRed    = 4
	colorYellow = 11
)

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

// lavaStep returns how far toward yellow the lava at x, y is at tick, from
// 0 to tiles.ShiftSteps-1. The value swings up and down, and the swing is
// offset along each diagonal, so the color ripples across a pool.
func lavaStep(tick, x, y int) int {
	period := 2 * (tiles.ShiftSteps - 1)
	p := (tick + x + y) % period
	if p < 0 {
		p += period
	}
	if p >= tiles.ShiftSteps {
		p = period - p
	}
	return p
}

// textLava reports whether a text cell is lava: a red '#'. The engine
// draws lava that way in every light, and nothing else on the map.
func textLava(c grid.Cell) bool {
	return c.Rune == '#' && c.Attr.Color() == colorRed && !c.Tile && !c.Covered
}

// lavaColor returns the color for lava at a step: the palette's red at 0,
// moving to its yellow at the last step.
func (p palette) lavaColor(step int) cell.Color {
	t := float64(step) / float64(tiles.ShiftSteps-1)
	r0, g0, b0 := p.color(colorRed).RGB()
	r1, g1, b1 := p.color(colorYellow).RGB()
	mix := func(a, b uint8) uint8 { return uint8(float64(a) + t*(float64(b)-float64(a)) + 0.5) }
	return cell.NewColorRGB(mix(r0, r1), mix(g0, g1), mix(b0, b1))
}

// hasLava reports whether the term shows any lava, so ticks with nothing
// to animate cost no redraw.
func (m *Model) hasLava(g *grid.Grid) bool {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			c := g.Cell(x, y)
			if textLava(c) || (c.Tile && m.sheet != nil && m.sheet.IsLava(c.Pict)) {
				return true
			}
		}
	}
	return false
}

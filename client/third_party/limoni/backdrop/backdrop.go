// Package backdrop holds animated scenes to draw behind a Limoni application:
//
//	limoni.Run(app, limoni.WithBackdrop(backdrop.Aurora()))
//	limoni.RunProgram(ctx, m, limoni.WithProgramBackdrop(backdrop.City()))
//
// Besides the four scenes, Image makes a wallpaper of a picture, and Art a
// scene of ASCII art — still, frame by frame, or drifting — read from a
// plain text file; docs/backdrop-art.md shows how to make and animate one.
//
// Every scene is made of ordinary cells — background colours, and a glyph
// here and there where the application leaves a cell blank — so it works in
// any terminal with 256 colours or more, with no image protocol. How the
// application's cells are laid over a scene is described on
// terminal.Backdrop.
//
// # What a scene costs
//
// The terminal is sent the cell diff, as for any frame, so a scene costs what
// it changes, not what it covers. The scenes are built around that:
//
//   - Everything that does not move is drawn once per window size and copied.
//   - Colours that drift are quantised, so a cell changes when it crosses a
//     step rather than on every frame.
//   - Wide areas are background colour behind a space, which the encoder
//     sends as one colour and a run.
//   - Each scene is a pure function of time and window size, so a frame drawn
//     twice is the same frame, and drawing it costs no allocation.
//
// BenchmarkScenes and TestSceneTraffic measure the bytes each scene sends a
// second through the real encoder; see the numbers on each constructor.
//
// # Readability
//
// The scenes are dark, because the text over them is usually drawn in the
// terminal's default foreground, which is light on most terminals. Dim
// darkens any scene further. Users can turn backdrops off in every
// application with LIMONI_BACKDROP=off.
package backdrop

import (
	"math"
	"time"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"
)

// Names lists the scenes New knows, in the order a demo would show them.
func Names() []string { return []string{"aurora", "city", "starfield", "synthwave"} }

// New returns the scene with the given name, or nil if there is none.
func New(name string) terminal.Backdrop {
	switch name {
	case "aurora":
		return Aurora()
	case "city":
		return City()
	case "starfield":
		return Starfield()
	case "synthwave":
		return Synthwave()
	}
	return nil
}

// Still freezes a scene at the moment at: a picture instead of an
// animation. Its Interval is zero, so nothing ticks for it; it is drawn along
// with the application, and costs nothing while the application is idle.
func Still(scene terminal.Backdrop, at time.Duration) terminal.Backdrop {
	return &still{scene: scene, at: at}
}

type still struct {
	scene terminal.Backdrop
	at    time.Duration
}

func (s *still) Interval() time.Duration { return 0 }

func (s *still) Render(dst *buffer.Buffer, _ time.Duration) { s.scene.Render(dst, s.at) }

// Dim darkens a scene: every colour is scaled by k, from 0 (black) to 1
// (unchanged). Use it where text sits over the brighter parts of a scene.
func Dim(scene terminal.Backdrop, k float64) terminal.Backdrop {
	return Fade(scene, cell.NewColorRGB(0, 0, 0), k)
}

// Fade shows a scene at opacity k over the colour base, from 0 (only base)
// to 1 (the scene unchanged). Given the terminal's own background colour,
// it is the scene seen through a window of that colour: the lower k, the
// quieter the scene behind the text. A base that is not an RGB colour is
// taken as black.
func Fade(scene terminal.Backdrop, base cell.Color, k float64) terminal.Backdrop {
	k = math.Max(0, math.Min(1, k))
	f := &faded{scene: scene, k: uint32(k*256 + 0.5)}
	if base.Type() == cell.ColorRGB {
		r, g, b := base.RGB()
		f.base = [3]uint32{uint32(r), uint32(g), uint32(b)}
	}
	return f
}

type faded struct {
	scene terminal.Backdrop
	base  [3]uint32
	k     uint32 // out of 256
}

func (f *faded) Interval() time.Duration { return f.scene.Interval() }

func (f *faded) Render(dst *buffer.Buffer, t time.Duration) {
	f.scene.Render(dst, t)
	for i := range dst.Content {
		s := &dst.Content[i].Style
		s.Fg = f.fade(s.Fg)
		s.Bg = f.fade(s.Bg)
	}
}

func (f *faded) fade(c cell.Color) cell.Color {
	if c.Type() != cell.ColorRGB {
		return c
	}
	r, g, b := c.RGB()
	return cell.NewColorRGB(f.mix(f.base[0], r), f.mix(f.base[1], g), f.mix(f.base[2], b))
}

// mix is base + (v-base)·k in fixed point, rounded down like a scale.
func (f *faded) mix(base uint32, v uint8) uint8 {
	return uint8((base*(256-f.k) + uint32(v)*f.k) >> 8)
}

// ── colour ───────────────────────────────────────────────────────────────

type rgb struct{ r, g, b float64 }

func (c rgb) color() cell.Color {
	return cell.NewColorRGB(clamp8(c.r), clamp8(c.g), clamp8(c.b))
}

func (c rgb) mix(o rgb, t float64) rgb {
	return rgb{c.r + (o.r-c.r)*t, c.g + (o.g-c.g)*t, c.b + (o.b-c.b)*t}
}

func (c rgb) scale(k float64) rgb { return rgb{c.r * k, c.g * k, c.b * k} }

func clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(v + 0.5)
}

func toRGB(c cell.Color) rgb {
	if c.Type() != cell.ColorRGB {
		return rgb{}
	}
	r, g, b := c.RGB()
	return rgb{float64(r), float64(g), float64(b)}
}

// quantize rounds v in [0, 1] down to one of steps levels. A colour driven
// by a quantised value changes only when the value crosses a level, which is
// what keeps a slowly drifting scene from rewriting every cell every frame.
func quantize(v float64, steps int) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return math.Floor(v*float64(steps)) / float64(steps)
}

// clamp01 clamps v to [0, 1]. math.Max and math.Min handle NaN and signed
// zeros, and cost a fifth of a frame in the scenes that call this per cell.
func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

// ── determinism ──────────────────────────────────────────────────────────

// hash mixes two integers into a well-spread 32-bit value (the finaliser of
// MurmurHash3). Scenes draw their events from it rather than from a random
// source, so a scene is a function of time and nothing else.
func hash(a, b int) uint32 {
	h := uint32(a)*0x9E3779B1 ^ uint32(b)*0x85EBCA77
	h ^= h >> 16
	h *= 0x85EBCA6B
	h ^= h >> 13
	h *= 0xC2B2AE35
	h ^= h >> 16
	return h
}

// unit is hash scaled to [0, 1).
func unit(a, b int) float64 { return float64(hash(a, b)) / (1 << 32) }

// noise is smooth value noise on a grid of unit cells, in [0, 1).
func noise(x, y float64, seed int) float64 {
	x0, y0 := math.Floor(x), math.Floor(y)
	fx, fy := x-x0, y-y0
	fx = fx * fx * (3 - 2*fx)
	fy = fy * fy * (3 - 2*fy)
	ix, iy := int(x0), int(y0)
	at := func(dx, dy int) float64 { return unit((ix+dx)*7919+seed, (iy+dy)*104729-seed) }
	top := at(0, 0) + (at(1, 0)-at(0, 0))*fx
	bot := at(0, 1) + (at(1, 1)-at(0, 1))*fx
	return top + (bot-top)*fy
}

// frames converts t to a frame count at the given rate.
func frames(t time.Duration, fps int) int { return int(t * time.Duration(fps) / time.Second) }

// grid is a scene's size and the cells it keeps from one frame to the next.
type grid struct {
	w, h int
}

// resized reports whether dst has a different size from the last frame, and
// records the new one.
func (g *grid) resized(dst *buffer.Buffer) bool {
	w, h := int(dst.Area.Width), int(dst.Area.Height)
	if w == g.w && h == g.h {
		return false
	}
	g.w, g.h = w, h
	return true
}

func bgCell(c rgb) cell.Cell {
	return cell.Cell{Content: ' ', Style: cell.Style{Bg: c.color()}}
}

func glyphCell(r rune, fg, bg rgb) cell.Cell {
	return cell.Cell{Content: r, Style: cell.Style{Fg: fg.color(), Bg: bg.color()}}
}

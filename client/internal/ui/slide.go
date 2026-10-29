package ui

import (
	"context"
	"image/color"
	"time"

	"github.com/thebanri/limoni/core/engine"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
)

// Smooth movement: when the engine says a creature stepped from grid to
// grid (a MOVES event), the client slides its tile along the way instead
// of letting it jump. The engine has already drawn the creature where it
// ends, so while it slides, that cell shows only its terrain, and the
// creature's tile, with see-through parts, moves over the map. Kitty
// moves it to the pixel; other image protocols move it a cell at a time.
// A key, a click, or a native screen ends every slide at once, so
// sliding never slows the game down. Slides share their clock with
// scrolling the map (see scroll.go).

const (
	// slideTime is how long a slide takes, however many steps it has.
	slideTime = 120 * time.Millisecond
	// slideFrame is how often a slide is drawn.
	slideFrame = time.Second / 60
	// slideTrack numbers the sliding pictures for Limoni, which keeps one
	// Kitty placement for each.
	slideTrack = 0x46420000
)

// slide is one creature sliding: its tile, and the cells, on the main
// term, of each grid it passes, from where it started.
type slide struct {
	tile proto.PictCell
	path [][2]int
}

// motionTick asks for the next frame of the slides and the scroll.
type motionTick struct{}

// sliding reports whether smooth movement is on. It is unless the
// settings turn it off.
func (m *Model) sliding() bool { return m.settings.Slide == nil || *m.settings.Slide }

// startSlides starts a slide for each move in a MOVES event that can be
// shown, ending any slides still going. It returns whether to redraw.
func (m *Model) startSlides(d proto.EventData) bool {
	had := len(m.slides) > 0
	m.slides = nil
	g := m.terms.Terms[0]
	if d.Busy || !m.sliding() || m.sheet == nil || m.terms.TileSet == nil || len(m.stack) > 0 || g == nil {
		return had
	}
	for _, mv := range d.Moves {
		if len(mv.Path) < 2 {
			continue
		}
		end := mv.Path[len(mv.Path)-1]
		if end[0] < 0 || end[1] < 0 || end[0] >= g.Width || end[1] >= g.Height {
			continue
		}
		// The creature's tile stands over the terrain's; a cell showing
		// only terrain has no creature to slide.
		c := g.Cell(end[0], end[1])
		if !c.Tile || (c.Pict.FgRow == c.Pict.BgRow && c.Pict.FgCol == c.Pict.BgCol) {
			continue
		}
		tile := proto.PictCell{FgRow: c.Pict.FgRow, FgCol: c.Pict.FgCol, BgRow: c.Pict.FgRow, BgCol: c.Pict.FgCol}
		m.slides = append(m.slides, slide{tile: tile, path: mv.Path})
	}
	if len(m.slides) == 0 {
		return had
	}
	m.slideAt = m.now()
	m.startMotion = !m.motionTicking
	return true
}

// endMotion ends every slide and any scroll at once. It returns whether
// any was going.
func (m *Model) endMotion() bool {
	had := len(m.slides) > 0 || m.scroll != nil || m.pendingScroll != nil
	m.slides, m.scroll, m.pendingScroll = nil, nil, nil
	return had
}

// motionTickCmd waits for the next frame of the slides and the scroll.
func motionTickCmd() engine.Cmd {
	return func(ctx context.Context) engine.Msg {
		select {
		case <-time.After(slideFrame):
			return motionTick{}
		case <-ctx.Done():
			return nil
		}
	}
}

// handleMotionTick draws the next frame of the slides and the scroll,
// and ends each when its time is up. A scroll's time starts with its
// first frame, since the whole map picture may take a moment to send.
func (m *Model) handleMotionTick() engine.UpdateResult {
	now, active, changed := m.now(), false, false
	if len(m.slides) > 0 {
		changed = true
		if now.Sub(m.slideAt) >= slideTime {
			m.slides = nil
		} else {
			active = true
		}
	}
	if s := m.scroll; s != nil {
		changed = true
		switch {
		case !s.started:
			s.started, s.at = true, now
			active = true
		case now.Sub(s.at) >= scrollTime:
			m.scroll = nil
		default:
			active = true
		}
	}
	if !active {
		m.motionTicking = false
		return engine.UpdateResult{Redraw: changed}
	}
	return engine.UpdateResult{Redraw: true, Commands: []engine.Cmd{motionTickCmd()}}
}

// slideEnds returns the cells where sliding creatures end, which show
// only their terrain until the slides end.
func (m *Model) slideEnds() map[[2]int]bool {
	if len(m.slides) == 0 {
		return nil
	}
	ends := map[[2]int]bool{}
	for _, s := range m.slides {
		ends[s.path[len(s.path)-1]] = true
	}
	return ends
}

// ease turns a share of the time, 0 to 1, into a share of the way: slow
// to start and to stop.
func ease(t float64) float64 {
	t = min(max(t, 0), 1)
	return t * t * (3 - 2*t)
}

// at returns where along its path a slide is, in cells, a share t of the
// way through its time.
func (s slide) at(t float64) (x, y float64) {
	steps := len(s.path) - 1
	pos := ease(t) * float64(steps)
	i := min(int(pos), steps-1)
	f := pos - float64(i)
	a, b := s.path[i], s.path[i+1]
	return float64(a[0]) + f*float64(b[0]-a[0]), float64(a[1]) + f*float64(b[1]-a[1])
}

// slidePictures returns the pictures of the sliding creatures, each a
// tile with see-through parts over the map, shifted from where it
// started to where it is now.
func (m *Model) slidePictures(cw, ch int) []terminal.ImageRegion {
	if len(m.slides) == 0 || m.sheet == nil {
		return nil
	}
	t := float64(m.now().Sub(m.slideAt)) / float64(slideTime)
	main := m.layout.Main
	// While the map scrolls, the creatures scroll with it.
	ox, oy, clip := m.scrollOffset()
	var regions []terminal.ImageRegion
	for i, s := range m.slides {
		x, y := s.at(t)
		start := s.path[0]
		regions = append(regions, terminal.ImageRegion{
			Area:        rect(Box{main.X + start[0], main.Y + start[1], cw, ch}),
			Img:         m.sheet.Sprite(s.tile.FgRow, s.tile.FgCol, color.RGBA{}),
			ZIndex:      1,
			Transparent: true,
			ShiftX:      x - float64(start[0]) + ox,
			ShiftY:      y - float64(start[1]) + oy,
			Track:       slideTrack + uint32(i),
			Clip:        clip,
		})
	}
	return regions
}

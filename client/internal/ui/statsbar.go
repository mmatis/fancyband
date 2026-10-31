package ui

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
)

// The game's colors the stats bar uses, from z-color.h.
const (
	colorLightDark  = 8
	colorLightGreen = 13
	colorLightBlue  = 14
)

// statsName is the stats bar's name in the settings' Classic list.
const statsName = "stats"

// nativeStats reports whether the player wants the native stats bar and
// status line. The layout shows them only when the window has room.
func (m *Model) nativeStats() bool { return !slices.Contains(m.settings.Classic, statsName) }

// relayout lays the screen out again for a window of width by height,
// fits the terms to it, and tells the engine whether to draw its own
// sidebar and status line.
func (m *Model) relayout(width, height int) {
	m.layout = m.computeLayout(width, height, m.inGame)
	m.sendResizes()
	m.syncPanels()
}

// computeLayout lays the screen out for a window of width by height.
// The native stats bar and status line show only during a game, not on
// the splash screen, in character creation, or at death.
func (m *Model) computeLayout(width, height int, inGame bool) Layout {
	return Compute(width, height, m.cfg.Subwindows, m.nativeStats() && inGame, m.settings.StatsRight)
}

// setInGame notes that a game started or ended, and shows or hides the
// stats bar and status line.
func (m *Model) setInGame(on bool) {
	if m.inGame == on {
		return
	}
	m.inGame = on
	m.relayout(m.layout.Width, m.layout.Height)
	if on {
		// The character's own filters, now that there is a character.
		m.reloadEdits()
	}
}

// syncPanels asks the engine to hide its sidebar and status line when
// the layout shows the native ones, and to draw them otherwise. The
// engine refuses while one of its menus is open; the next WAIT tries
// again.
func (m *Model) syncPanels() {
	if m.sess == nil || m.layout.TooSmall {
		return
	}
	// What the game's layout would be, so the engine's own sidebar stays
	// hidden when the game starts.
	native := m.computeLayout(m.layout.Width, m.layout.Height, true).Native()
	if m.panelsSent && m.panelsNative == native {
		return
	}
	m.panelsSent, m.panelsNative = true, native
	m.command("PANELS", map[string]any{"sidebar": !native, "status_line": !native}, func(r proto.EventData) {
		if !r.OK {
			m.log.Printf("PANELS refused: %s", r.Error)
			m.panelsSent = false
		}
	})
}

// handleSnapshot keeps the status part of a SNAPSHOT. It returns whether
// the screen needs redrawing.
func (m *Model) handleSnapshot(s proto.Snapshot) bool {
	d, err := s.Parse()
	if err != nil {
		m.log.Printf("bad SNAPSHOT: %v", err)
		return false
	}
	if d.Status == nil {
		return false
	}
	m.status = d.Status
	return m.layout.Native()
}

// IDs for the stats bar and status line in Limoni's accessibility tree.
const (
	statsBarID   = "stats-bar"
	statusLineID = "status-line"
)

// statText formats a stat as the game does: 3 to 18, then 18/01 to
// 18/220, shown as 18/*** at the top.
func statText(v int) string {
	switch bonus := v - 18; {
	case bonus >= 220:
		return "18/***"
	case bonus >= 100:
		return fmt.Sprintf("18/%03d", bonus)
	case bonus > 0:
		return fmt.Sprintf("18/%02d", bonus)
	default:
		return fmt.Sprint(v)
	}
}

// statsRow is one row of the stats bar. A blank row is only spacing,
// and goes first when the bar is short.
type statsRow struct {
	blank bool
	draw  func(f *terminal.Frame, x, y, w int)
}

// drawStats draws the stats bar into box: who the character is, level,
// experience, gold, gear, stats, armor, the HP and mana gauges, the
// tracked monster's health, speed, and depth, as the game's sidebar
// does. With sprite, the gear shows as tiles.
func drawStats(f *terminal.Frame, box Box, st *proto.StatusData, pal palette, sprite func(row, col uint8, bg color.RGBA) image.Image) {
	if box.Empty() || st == nil {
		return
	}
	f.RegisterAccessibility(accessibility.Node{ID: statsBarID, Label: "Stats", Bounds: rect(box)})
	style := func(color int) cell.Style { return cell.NewStyle().WithFg(pal.color(color)) }
	label := style(colorWhite)
	// pair puts a label on the left and a value on the right.
	pair := func(name string, value string, color int) statsRow {
		return statsRow{draw: func(f *terminal.Frame, x, y, w int) {
			f.TextFit(x, y, w, name, label)
			vw := min(limoni.StringWidth(value), w-len(name)-1)
			f.TextFit(x+w-vw, y, vw, value, style(color))
		}}
	}
	text := func(s string, color int) statsRow {
		return statsRow{draw: func(f *terminal.Frame, x, y, w int) { f.TextFit(x, y, w, s, style(color)) }}
	}
	blank := statsRow{blank: true}

	rows := []statsRow{text(st.Name, colorWhite)}
	who := strings.TrimSpace(st.Race + " " + st.Class)
	rows = append(rows, text(who, colorLightBlue), text(st.Title, colorLightBlue), blank)

	levelName, levelColor := "Level", colorLightGreen
	if st.Level < st.MaxLevel {
		levelColor = colorYellow
	}
	rows = append(rows, pair(levelName, fmt.Sprint(st.Level), levelColor))
	expColor := colorLightGreen
	if st.Exp < st.MaxExp {
		expColor = colorYellow
	}
	if st.NextExp >= 0 {
		rows = append(rows, pair("Next level", fmt.Sprint(st.NextExp), expColor))
	} else {
		rows = append(rows, pair("Experience", fmt.Sprint(st.Exp), expColor))
	}
	rows = append(rows, pair("Gold", fmt.Sprint(st.Gold), colorLightGreen))
	rows = append(rows, equippyRows(st.Equippy, style, sprite, box.W-2)...)
	rows = append(rows, blank)

	for _, s := range st.Stats {
		name, color := s.Name, colorLightGreen
		if s.Drained {
			// The game marks a drained stat with a lowercase name.
			name, color = s.Name[:1]+strings.ToLower(s.Name[1:]), colorYellow
		}
		if s.Maxed {
			name += "!"
		}
		rows = append(rows, pair(name, statText(s.Value), color))
	}
	rows = append(rows, blank, pair("Armor", fmt.Sprint(st.AC), colorLightGreen))
	rows = append(rows, statsRow{draw: func(f *terminal.Frame, x, y, w int) {
		drawGauge(f, x, y, w, hpGaugeID, "HP", st.HP, st.MHP, hpStyle(st.HP, st.MHP))
	}})
	if st.Magic {
		rows = append(rows, statsRow{draw: func(f *terminal.Frame, x, y, w int) {
			drawGauge(f, x, y, w, spGaugeID, "SP", st.SP, st.MSP, manaStyle)
		}})
	}
	if h := st.Health; h != nil {
		// The monster the character fights, set apart from the
		// character's own gauges: its name, then its health as a gauge
		// in the game's color for it, empty when the player cannot tell.
		name := h.Name
		if name == "" {
			name = "Unseen foe"
		}
		rows = append(rows, blank, text(name, colorWhite), statsRow{draw: func(f *terminal.Frame, x, y, w int) {
			pct, nums := max(h.Percent, 0), fmt.Sprintf(" %d%%", h.Percent)
			if h.Percent < 0 {
				nums = " ?"
			}
			drawGaugeText(f, x, y, w, foeGaugeID, "Foe", pct, 100, style(h.Color), nums)
		}})
	}
	rows = append(rows, blank)
	if st.SpeedText != "" {
		rows = append(rows, text(st.SpeedText, st.SpeedColor))
	}
	rows = append(rows, text(st.DepthText, colorWhite))

	// A short bar drops its spacing first, then its last rows.
	if len(rows) > box.H {
		rows = slices.DeleteFunc(rows, func(r statsRow) bool { return r.blank })
	}
	for i, r := range rows {
		if i >= box.H {
			break
		}
		if !r.blank {
			r.draw(f, box.X+1, box.Y+i, box.W-2)
		}
	}
}

// Each gear tile in the stats bar takes two cells, as a tile does on the
// map at size 1.
const gearTileCells = 2

// equippyRows draws the gear, one slot after another, as the game's
// sidebar does: as tiles when sprite is set and the engine sent them,
// over as many rows as they need, or else as letters on one row. The
// tiles' see-through parts stay see-through, so the terminal's own
// background shows through them, translucent or not.
func equippyRows(gear []*proto.Glyph, style func(int) cell.Style, sprite func(row, col uint8, bg color.RGBA) image.Image, w int) []statsRow {
	tiles := sprite != nil
	for _, g := range gear {
		if g != nil && g.Tile == nil {
			tiles = false
		}
	}
	if !tiles {
		return []statsRow{{draw: func(f *terminal.Frame, x, y, w int) {
			for i, g := range gear {
				if i >= w {
					break
				}
				if g != nil && limoni.StringWidth(g.Char) == 1 {
					f.Text(x+i, y, g.Char, style(g.Color))
				}
			}
		}}}
	}
	// Split the slots evenly over the rows they need.
	perRow := max(w/gearTileCells, 1)
	n := (len(gear) + perRow - 1) / perRow
	if n > 0 {
		perRow = (len(gear) + n - 1) / n
	}
	var rows []statsRow
	for start := 0; start < len(gear); start += perRow {
		part := gear[start:min(start+perRow, len(gear))]
		rows = append(rows, statsRow{draw: func(f *terminal.Frame, x, y, w int) {
			for i, g := range part {
				at := Box{x + i*gearTileCells, y, gearTileCells, 1}
				if g == nil || at.X+at.W > x+w {
					continue
				}
				for cx := at.X; cx < at.X+at.W; cx++ {
					f.Buffer.SetCell(uint16(cx), uint16(y), cell.Cell{Content: cell.RuneImage})
				}
				img := sprite(g.Tile.Row, g.Tile.Col, color.RGBA{})
				f.RegisterImageRegion(terminal.ImageRegion{Area: rect(at), Img: img, Transparent: true})
			}
		}})
	}
	return rows
}

// drawStatusLine draws the status line into box: each item the game
// shows, such as "Fed 89 %" or "Poisoned", in its color, with a space
// between items. Items that do not fit are left off.
func drawStatusLine(f *terminal.Frame, box Box, st *proto.StatusData, pal palette) {
	if box.Empty() || st == nil {
		return
	}
	var plain []string
	for _, sp := range st.Line {
		plain = append(plain, sp.Text)
	}
	f.RegisterAccessibility(accessibility.Node{ID: statusLineID, Label: "Status", Value: strings.Join(plain, " "), Bounds: rect(box)})
	x := box.X + 1
	for _, sp := range st.Line {
		w := limoni.StringWidth(sp.Text)
		if x+w > box.X+box.W {
			break
		}
		f.Text(x, box.Y, sp.Text, cell.NewStyle().WithFg(pal.color(sp.Color)))
		x += w + 1
	}
}


package ui

import (
	"strings"

	"fancyband/internal/proto"
)

// visibleID names the monster and item lists in the accessibility tree.
const visibleID = "visible"

// openVisible opens the monster or item list, for the screen request id. The
// list sits at the right of the screen, as the game's own does, so the map
// stays in view.
func (m *Model) openVisible(id int, view string) {
	kind, title := "items", "Visible items"
	if view == "monsters" {
		kind, title = "monsters", "Visible monsters"
	}
	s := newListScreen(visibleID, title, []listItem{{Label: "Reading…", Dim: true}})
	s.cancel = m.replyClose(id)
	s.choose = func(int) { s.cancel() }
	s.place = func(area Box, w, h int) Box {
		w, h = min(w, area.W), min(h, area.H)
		return Box{area.X + area.W - w, area.Y, w, h}
	}
	byExp := false
	fill := func() {
		args := map[string]any{"kind": kind, "by_exp": byExp}
		m.query("VISIBLE", args, func(a proto.EventData) {
			s.items = visibleItems(a.Visible)
			s.state.Select(0)
			s.settle(1)
		})
	}
	if kind == "monsters" {
		s.keys = []listKey{{Key: 'x', Label: "Sort by experience", Do: func(int) {
			byExp = !byExp
			s.keys[0].Label = "Sort by experience"
			if byExp {
				s.keys[0].Label = "Sort by nearest"
			}
			fill()
		}}}
	}
	m.push(s)
	fill()
}

// visibleItems makes the list's rows. A thing shows its tile with tiles, and
// its letter without them.
func visibleItems(rows []proto.VisibleRow) []listItem {
	items := make([]listItem, 0, len(rows))
	for _, r := range rows {
		if r.Heading != "" {
			items = append(items, listItem{Label: r.Heading, Header: true})
			continue
		}
		color := r.Line
		// The game pads a count, to line its names up.
		it := listItem{Label: strings.TrimSpace(r.Name), Hint: r.Where, LabelColor: &color}
		if r.Tile != nil {
			it.Tile = &proto.PictCell{FgRow: r.Tile.Row, FgCol: r.Tile.Col}
		} else if g := []rune(r.Char); len(g) > 0 {
			it.Icon, it.IconColor = g[0], r.Color
		}
		items = append(items, it)
	}
	return items
}

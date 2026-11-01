package ui

import (
	"fmt"
	"strings"

	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
)

// The knowledge screen: the sections of the game's knowledge menu as
// tabs on the list screen, each entry with its letter or tile, grouped
// as the game groups them, and its description in the detail panel.

const knowledgeID = "knowledge"

// knowledgeTab is one tab of the knowledge screen: one section of the
// knowledge menu, or for Stores, one section per store.
type knowledgeTab struct {
	title    string
	sections []string
	// titles are the stores' names, for their headings.
	titles []string
}

// knowledgeRow is what a row of the knowledge screen shows: an entry of
// a section, or a heading.
type knowledgeRow struct {
	section string
	entry   *proto.KnowledgeEntry
}

// knowledgeScreen is the knowledge screen.
type knowledgeScreen struct {
	*listScreen
	m    *Model
	kt   []knowledgeTab
	rows [][]knowledgeRow
	// items are each tab's list rows, nil until its sections load.
	tabItems [][]listItem
	// asked marks the entries whose descriptions were asked for, and
	// loading the tabs whose sections are being read.
	asked   map[[2]int]bool
	loading map[int]bool
}

// openKnowledge opens the knowledge screen on the section view, if
// given, for the screen request id. With id 0, as from the death menu,
// it answers no request.
func (m *Model) openKnowledge(id int, view string) {
	m.query("KNOWLEDGE", nil, func(a proto.EventData) {
		var tabs []knowledgeTab
		stores := knowledgeTab{title: "Stores"}
		for _, s := range a.Sections {
			switch {
			case strings.HasPrefix(s.Name, "store:"):
				stores.sections = append(stores.sections, s.Name)
				stores.titles = append(stores.titles, s.Title)
				if len(stores.sections) == 1 {
					tabs = append(tabs, knowledgeTab{})
				}
			case s.Known:
				tabs = append(tabs, knowledgeTab{title: s.Title, sections: []string{s.Name}})
			}
		}
		for i := range tabs {
			if tabs[i].title == "" {
				tabs[i] = stores
			}
		}
		if len(tabs) == 0 {
			if id != 0 {
				m.send(proto.NewReply(id, map[string]any{"classic": true}))
			}
			return
		}
		m.push(newKnowledgeScreen(m, id, tabs, view))
	})
}

func newKnowledgeScreen(m *Model, id int, tabs []knowledgeTab, view string) *knowledgeScreen {
	k := &knowledgeScreen{m: m, kt: tabs, asked: map[[2]int]bool{}, loading: map[int]bool{}}
	k.rows = make([][]knowledgeRow, len(tabs))
	k.tabItems = make([][]listItem, len(tabs))
	k.listScreen = newListScreen(knowledgeID, "Knowledge", nil)
	for i, t := range tabs {
		k.tabs = append(k.tabs, t.title)
		for _, s := range t.sections {
			if s == view {
				k.tab = i
			}
		}
	}
	k.onTab = func(t int) { k.show(t) }
	k.detail = true
	k.searchable = true
	k.width, k.height = 100, 32
	k.cancel = func() {
		m.pop()
		if id != 0 {
			m.send(proto.NewReply(id, map[string]any{}))
		}
	}
	k.choose = func(item int) { k.act(id, item) }
	k.keys = []listKey{{Key: 't', Label: "Tile Editor", Do: func(item int) { k.editTile(item) }}}
	k.show(k.tab)
	return k
}

// show fills the list for tab t, loading its sections the first time.
func (k *knowledgeScreen) show(t int) {
	if k.tabItems[t] != nil {
		k.items = k.tabItems[t]
		return
	}
	k.items = []listItem{{Label: "Reading…", Dim: true}}
	tab := k.kt[t]
	if tab.sections[0] == "equipment" {
		k.rows[t] = []knowledgeRow{{section: "equipment"}}
		k.tabItems[t] = []listItem{{
			Label:  "Compare your equipment",
			Detail: proto.PlainText(colorWhite, k.compareHelp()),
		}}
		k.items = k.tabItems[t]
		return
	}
	if k.loading[t] {
		return
	}
	k.loading[t] = true
	lists := make([][]proto.KnowledgeEntry, len(tab.sections))
	left := len(tab.sections)
	for i, sec := range tab.sections {
		i := i
		k.m.query("KNOWLEDGE", map[string]any{"section": sec}, func(a proto.EventData) {
			lists[i] = a.List
			if left--; left == 0 {
				k.fill(t, lists)
			}
		})
	}
}

// fill makes tab t's rows from its sections' entries, with a heading
// for each group.
func (k *knowledgeScreen) fill(t int, lists [][]proto.KnowledgeEntry) {
	tab := k.kt[t]
	var rows []knowledgeRow
	var items []listItem
	for i, list := range lists {
		group := ""
		if len(tab.titles) > i {
			if len(list) == 0 {
				continue
			}
			rows = append(rows, knowledgeRow{})
			items = append(items, listItem{Label: tab.titles[i], Header: true})
		}
		for j := range list {
			e := &list[j]
			if e.Group != "" && e.Group != group {
				group = e.Group
				rows = append(rows, knowledgeRow{})
				items = append(items, listItem{Label: group, Header: true})
			}
			rows = append(rows, knowledgeRow{section: tab.sections[i], entry: e})
			items = append(items, knowledgeItem(tab.sections[i], *e))
		}
	}
	if len(items) == 0 {
		items = []listItem{{Label: "Nothing known yet", Dim: true}}
		rows = []knowledgeRow{{}}
	}
	k.rows[t], k.tabItems[t] = rows, items
	if k.tab == t {
		k.items = items
		k.state.Select(0)
		k.settle(1)
	}
}

// knowledgeItem is the list row for an entry of section.
func knowledgeItem(section string, e proto.KnowledgeEntry) listItem {
	it := listItem{Label: e.Name, Hint: e.Note, Dim: e.Dim}
	if e.Tile != nil {
		it.Tile = &proto.PictCell{FgRow: e.Tile.Row, FgCol: e.Tile.Col}
	}
	if r := []rune(e.Char); len(r) > 0 {
		it.Icon, it.IconColor = r[0], e.Color
	}
	switch section {
	case "history":
		it.Hint = fmt.Sprintf("%d ft", e.Depth)
		it.Detail = proto.StyledText{
			{{Text: e.Name, Color: colorWhite}},
			nil,
			{{Text: fmt.Sprintf("Turn %d, at %d feet, character level %d.", e.Turn, e.Depth, e.Level), Color: colorLightBlue}},
		}
	case "scores":
		if e.Current {
			it.Icon, it.IconColor = '>', colorLightGreen
		}
		it.Detail = proto.StyledText{{{Text: e.Name, Color: colorWhite}}, nil}
		for _, l := range e.Lines {
			it.Detail = append(it.Detail, []proto.Span{{Text: l, Color: colorWhite}})
		}
		it.Detail = append(it.Detail, nil, []proto.Span{{Text: "Score " + e.Note, Color: colorLightGreen}})
	}
	return it
}

// draw asks for the selected entry's description the first time it is
// shown, then draws the list.
func (k *knowledgeScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	k.describe()
	k.listScreen.draw(f, b, env)
}

// describe asks for the selected entry's description, once.
func (k *knowledgeScreen) describe() {
	t, i := k.tab, k.selected()
	if i < 0 || i >= len(k.rows[t]) || k.asked[[2]int{t, i}] {
		return
	}
	row := k.rows[t][i]
	if row.entry == nil || row.section == "history" || row.section == "scores" {
		return
	}
	k.asked[[2]int{t, i}] = true
	k.m.query("RECALL", map[string]any{"section": row.section, "id": row.entry.ID}, func(a proto.EventData) {
		if a.Title == nil || i >= len(k.tabItems[t]) {
			return
		}
		text := proto.StyledText{{{Text: capitalize(*a.Title), Color: colorYellow}}, nil}
		k.tabItems[t][i].Detail = append(text, a.Description...)
	})
}

// act runs Enter on a row: the equipment comparison opens, Fancyband's
// own or, with its switch off, the classic screen.
func (k *knowledgeScreen) act(id, item int) {
	if item < 0 || item >= len(k.rows[k.tab]) || k.rows[k.tab][item].section != "equipment" {
		return
	}
	if k.m.nativeCompare() {
		k.m.openCompare()
		return
	}
	if id == 0 {
		k.m.notify("The equipment comparison is only on the classic screen, which is closed now.")
		return
	}
	k.m.pop()
	k.m.send(proto.NewReply(id, map[string]any{"classic": "equipment"}))
}

// compareHelp describes the equipment comparison's row.
func (k *knowledgeScreen) compareHelp() string {
	if k.m.nativeCompare() {
		return "Compare what you wear and carry, what lies at your feet and at home, and what the stores sell, " +
			"by their resistances, abilities, hindrances, and modifiers. Press Enter to open it."
	}
	return "The game's own screen compares the equipment you carry, wear, and have at home. Press Enter to open it; Escape there comes back here."
}

// editTile opens the Tile Editor on the selected entry.
func (k *knowledgeScreen) editTile(item int) {
	if item < 0 || item >= len(k.rows[k.tab]) {
		return
	}
	e := k.rows[k.tab][item].entry
	if e == nil || e.Visual == nil {
		k.m.notify("That has no tile to change.")
		return
	}
	k.m.openTileEditorAt(e.Visual.Kind, e.Visual.ID)
}

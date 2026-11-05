package ui

import (
	"fmt"

	"fancyband/internal/proto"
)

// commandMenuID names the command menu's groups in Limoni's
// accessibility tree, for tests; a list opened from them is named by
// commandListID.
const commandMenuID = "command-menu"

// commandListID names the list depth lists deep in the command menu.
func commandListID(depth int) string { return fmt.Sprintf("%s-%d", commandMenuID, depth) }

// commandMenuStep is how far each list opened from another sits below
// and right of it.
const commandMenuStep = 2

// openCommandMenu opens the command menu, which Enter gives, for the
// request id: the game's groups of commands, as COMMANDS lists them.
// Choosing a command sends it back for the game to run.
func (m *Model) openCommandMenu(id int) {
	// Without the list, the game's own menu does the job.
	classic := m.classicElse(id)
	m.queryElse("COMMANDS", nil, func(a proto.EventData) {
		if a.Commands == nil {
			classic()
			return
		}
		menu := a.Commands
		var entries []menuEntry
		for i, l := range menu.Lists {
			// As ui-context.c, the groups are the lists before the first
			// submenu.
			if l.Level != 0 {
				break
			}
			i, l := i, l
			entries = append(entries, menuEntry{label: l.Name, hint: "›", do: func() {
				m.pushCommandList(id, menu, i, l.Name, 1)
			}})
		}
		groups := newMenu(commandMenuID, "Commands", entries)
		groups.cancel = func() { m.chooseCommand(id, -1, -1) }
		groups.width = 30
		m.push(groups)
	}, classic)
}

// pushCommandList opens list from menu, titled title, depth lists deep.
func (m *Model) pushCommandList(id int, menu *proto.CommandMenu, list int, title string, depth int) {
	l := menu.Lists[list]
	entries := make([]menuEntry, len(l.Commands))
	for i, c := range l.Commands {
		i, c := i, c
		e := menuEntry{label: c.Name, hint: c.Key}
		switch {
		case c.Submenu != nil && *c.Submenu >= 0 && *c.Submenu < len(menu.Lists):
			e.label += " ›"
			e.do = func() { m.pushCommandList(id, menu, *c.Submenu, c.Name, depth+1) }
		case c.Submenu != nil:
			// A submenu the game could not find, as an entry that does
			// nothing in its own menu.
			e.do = func() {}
		default:
			e.do = func() { m.chooseCommand(id, list, i) }
		}
		entries[i] = e
	}
	s := newMenu(commandListID(depth), title, entries)
	s.cancel = m.pop
	s.width = 34
	for _, e := range entries {
		s.width = max(s.width, len([]rune(e.label))+len([]rune(e.hint))+8)
	}
	// Each list sits a little below and right of the one it came from,
	// as the game's own do.
	s.place = func(area Box, w, h int) Box {
		x := area.X + (area.W-w)/2 + depth*commandMenuStep*2
		y := area.Y + (area.H-h)/2 + depth*commandMenuStep
		return Box{max(min(x, area.X+area.W-w), area.X), max(min(y, area.Y+area.H-h), area.Y), w, h}
	}
	m.push(s)
}

// chooseCommand closes the command menu and sends the command chosen,
// by its list and its index there, or, with list -1, none.
func (m *Model) chooseCommand(id, list, command int) {
	m.closeAll()
	reply := map[string]any{}
	if list >= 0 {
		reply = map[string]any{"list": list, "command": command}
	}
	m.send(proto.NewReply(id, reply))
}

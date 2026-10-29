package ui

import (
	"fmt"

	"fancyband/internal/proto"
	"fancyband/internal/tiles"
)

// IDs of the menus in Limoni's accessibility tree, which tests use.
const (
	gameMenuID  = "game-menu"
	startMenuID = "start-menu"
	deathMenuID = "death-menu"
	saveListID  = "save-list"
	borgAskID   = "borg-warning"
)

// Palette slots for menu text (COLOUR_WHITE and COLOUR_L_DARK).
const (
	colorWhite = 1
	colorGray  = 13
)

// menuEntry is one choice in a menu.
type menuEntry struct {
	label string
	hint  string
	do    func()
}

// newMenu builds a list screen of entries.
func newMenu(id, title string, entries []menuEntry) *listScreen {
	items := make([]listItem, len(entries))
	for i, e := range entries {
		items[i] = listItem{Label: e.label, Hint: e.hint}
	}
	s := newListScreen(id, title, items)
	s.choose = func(i int) { entries[i].do() }
	s.clickChooses = true
	return s
}

// push opens a screen on top of the others.
func (m *Model) push(s screen) { m.stack = append(m.stack, s) }

// pop closes the top screen.
func (m *Model) pop() {
	if n := len(m.stack); n > 0 {
		m.stack = m.stack[:n-1]
	}
}

// closeAll closes every native screen.
func (m *Model) closeAll() { m.stack = nil }

// openGameMenu opens the menu Escape gives at the command prompt.
func (m *Model) openGameMenu() {
	borg := "Off"
	switch {
	case m.borgStopping:
		borg = "Stopping"
	case m.borgOn:
		borg = "On"
	}
	entries := []menuEntry{
		{label: "Resume", do: m.closeAll},
		{label: "Save", do: m.saveGame},
		{label: "Load a character", do: func() { m.openSaveList(false) }},
	}
	if !m.borgMissing {
		entries = append(entries, menuEntry{label: "Borg", hint: borg, do: m.toggleBorg})
	}
	entries = append(entries, menuEntry{label: "Save & Quit", do: m.quit})
	menu := newMenu(gameMenuID, "Fancyband", entries)
	menu.cancel = m.closeAll
	menu.width = 34
	m.push(menu)
}

// saveGame saves with the SAVE command and closes the menu.
func (m *Model) saveGame() {
	m.closeAll()
	m.command("SAVE", nil, func(r proto.EventData) {
		if r.OK {
			m.notify("Game saved.")
		} else {
			m.notify("Cannot save: " + r.Error)
		}
	})
}

// quit asks the engine to save and exit; the client exits on its BYE.
func (m *Model) quit() {
	m.closeAll()
	m.next = nil
	m.requestQuit()
}

// switchTo saves the game, lets the engine exit, and starts a new one on
// l.
func (m *Model) switchTo(l Launch) {
	m.closeAll()
	m.next = &l
	m.requestQuit()
}

// openSaveList lists the saved characters to load, after asking the
// engine for them. In the death menu, choosing one only records it: the
// engine exits after the death screen, and the client starts the next
// engine then.
func (m *Model) openSaveList(afterDeath bool) {
	m.command("SAVEFILES", nil, func(r proto.EventData) {
		choose := m.switchTo
		if afterDeath {
			choose = m.leaveDeath
		}
		entries := []menuEntry{{label: "New character", do: func() { choose(Launch{New: true}) }}}
		details := []proto.StyledText{proto.PlainText(colorWhite, "Make a new character. Its name names its savefile.")}
		pictures := []*proto.PictCell{nil}
		for _, sv := range m.saves {
			if sv.Dead || sv.Current {
				continue
			}
			sv := sv
			entries = append(entries, menuEntry{label: sv.Name, do: func() { choose(Launch{Name: sv.Name}) }})
			details = append(details, proto.PlainText(colorWhite, sv.Description))
			pictures = append(pictures, m.playerTile(sv.Race, sv.Class))
		}
		list := newMenu(saveListID, "Load a character", entries)
		for i := range list.items {
			list.items[i].Detail = details[i]
			list.items[i].Tile = pictures[i]
		}
		// A list to look through: one click selects, a double click or
		// Load loads.
		list.clickChooses = false
		list.detail = true
		list.searchable = true
		list.cancel = m.pop
		list.buttons = []listButton{
			{Label: "Load", Do: func(i int) {
				if i >= 0 {
					entries[i].do()
				}
			}},
			{Label: "Cancel", Do: func(int) { m.pop() }},
		}
		m.push(list)
	})
}

// playerTile returns the current tile set's tile for a character of race
// and class, or nil in text mode or when the set has none.
func (m *Model) playerTile(race, class string) *proto.PictCell {
	if m.sheet == nil || m.terms.TileSet == nil || race == "" {
		return nil
	}
	c, ok := tiles.PlayerTile(*m.terms.TileSet, race, class)
	if !ok {
		return nil
	}
	return &c
}

// toggleBorg turns the borg on, after the warning the first time for
// this character, or off.
func (m *Model) toggleBorg() {
	if m.borgStopping {
		m.notify("The borg is still stopping.")
		return
	}
	if m.borgOn {
		m.closeAll()
		m.borgStopping = true
		m.borgCommand(false)
		return
	}
	if m.settings.Warned(m.saveName) {
		m.closeAll()
		m.borgCommand(true)
		return
	}
	ask := newMenu(borgAskID, "The borg", []menuEntry{
		{label: "Turn on the borg", do: func() {
			m.settings.SetWarned(m.saveName)
			m.saveSettings()
			m.closeAll()
			m.borgCommand(true)
		}},
		{label: "Cancel", do: m.pop},
	})
	ask.intro = proto.PlainText(colorWhite, "The borg is Angband's automatic player. It plays this character "+
		"until you press Escape or F10.\n\nUsing it marks the character as having used the borg, "+
		"which keeps it off the high score list for good.")
	ask.cancel = m.pop
	ask.width = 60
	ask.selectItem(1)
	m.push(ask)
}

func (m *Model) borgCommand(on bool) {
	m.command("BORG", map[string]any{"on": on}, func(r proto.EventData) {
		if r.OK {
			return
		}
		if !on {
			m.borgStopping = false
		}
		if r.Error == "this engine was built without the borg" {
			m.borgMissing = true
		}
		m.notify("Cannot switch the borg: " + r.Error)
	})
}

// openStartMenu opens the menu over the splash screen.
func (m *Model) openStartMenu() {
	target := m.continueTarget()
	var entries []menuEntry
	if target != nil {
		t := *target
		entries = append(entries, menuEntry{label: "Continue", hint: t.Name, do: func() {
			if t.Current {
				m.closeAll()
				m.pressOn()
			} else {
				m.switchTo(Launch{Name: t.Name})
			}
		}})
	}
	entries = append(entries, menuEntry{label: "New character", do: func() { m.switchTo(Launch{New: true}) }})
	if m.livingSaves(target) > 0 {
		entries = append(entries, menuEntry{label: "Load a character", do: func() { m.openSaveList(false) }})
	}
	entries = append(entries, menuEntry{label: "Quit", do: m.quit})
	menu := newMenu(startMenuID, "Fancyband", entries)
	if target != nil {
		menu.note = target.Description
		if m.launch.first() && hasNew(m.cfg.EngineArgs) {
			// -n on the command line picks New character.
			menu.selectItem(1)
		}
	}
	menu.width = max(34, len(menu.note)+4)
	m.push(menu)
}

// continueTarget picks the character Continue loads: the one the player
// named with -u, if it has a save, else the last one played, else the
// engine's own, if it is alive.
func (m *Model) continueTarget() *proto.SaveData {
	var current, last *proto.SaveData
	for i := range m.saves {
		sv := &m.saves[i]
		if sv.Dead {
			continue
		}
		if sv.Current {
			current = sv
		}
		if sv.Name == m.settings.LastCharacter {
			last = sv
		}
	}
	if m.launch.first() && hasName(m.cfg.EngineArgs) && current != nil || last == nil {
		return current
	}
	return last
}

// livingSaves counts the living characters other than skip.
func (m *Model) livingSaves(skip *proto.SaveData) int {
	n := 0
	for _, sv := range m.saves {
		if !sv.Dead && (skip == nil || sv.Name != skip.Name) {
			n++
		}
	}
	return n
}

// pressOn sends the key that clears the splash screen.
func (m *Model) pressOn() { m.sendKey(proto.Key{Code: proto.KeyEscape}) }

// openDeathMenu opens the menu for the death screen the engine handed
// over with request id.
func (m *Model) openDeathMenu(id int) {
	entries := []menuEntry{
		{label: "New character", do: func() { m.leaveDeath(Launch{New: true}) }},
		{label: "Load a character", do: func() { m.openSaveList(true) }},
		{label: "Look back at this character", hint: "classic", do: func() {
			m.closeAll()
			m.next = &Launch{Menu: true}
			m.send(proto.NewReply(id, map[string]any{"classic": true}))
		}},
		{label: "Quit", do: func() {
			m.closeAll()
			m.next = nil
			m.send(proto.NewReply(id, map[string]any{}))
		}},
	}
	menu := newMenu(deathMenuID, "You have died", entries)
	menu.note = "The classic screens show the character, messages, and items."
	menu.width = len(menu.note) + 4
	m.deathID = id
	m.push(menu)
}

// leaveDeath closes the death screen; the engine saves the death and
// exits, and the client then starts a new engine on l.
func (m *Model) leaveDeath(l Launch) {
	m.closeAll()
	m.next = &l
	m.send(proto.NewReply(m.deathID, map[string]any{}))
}

// notify shows a short message over the game for a few seconds.
func (m *Model) notify(text string) {
	m.notice = text
	m.noticeSeq++
	m.log.Print(text)
}

// noticeText returns the notice drawn over the game.
func noticeText(s string) string { return fmt.Sprintf(" %s ", s) }

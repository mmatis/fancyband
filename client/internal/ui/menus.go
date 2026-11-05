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

// Palette slots for menu text (COLOUR_WHITE and COLOUR_L_DARK), and
// yellow (COLOUR_YELLOW) for the stats bar.
const (
	colorWhite  = 1
	colorYellow = 11
	colorGray   = 13
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

// screenAnimated reports whether an open screen moves on its own, as a
// filter's preview or the splash screen's fire does, so animation ticks
// redraw it.
func (m *Model) screenAnimated() bool {
	for _, s := range m.stack {
		if a, ok := s.(interface{ animated() bool }); ok && a.animated() {
			return true
		}
	}
	return false
}

// stepScreens moves the open screens that animate by steps, such as the
// splash screen's fire, on by one.
func (m *Model) stepScreens() {
	for _, s := range m.stack {
		if st, ok := s.(interface{ step() }); ok {
			st.step()
		}
	}
}

// pop closes the top screen.
func (m *Model) pop() {
	if n := len(m.stack); n > 0 {
		m.stack = m.stack[:n-1]
	}
}

// closeAll closes every native screen.
func (m *Model) closeAll() {
	m.stack = nil
	m.store, m.birth = nil, nil
	m.marker = nil
}

// dev reports whether the client runs in developer mode (-dev), which
// adds the Tile Editor for the defaults Fancyband ships.
func (m *Model) dev() bool { return m.cfg.DefaultsDir != "" }

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
		{label: "Options", do: func() { m.openOptions(0) }},
		{label: "Tile Editor", hint: "F2", do: m.openTileEditor},
	}
	if m.dev() {
		entries = append(entries, menuEntry{label: "Tile Editor (Global)", do: func() { m.openTileEditorOn(tiles.LayerGlobal) }})
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
	entries = append(entries, menuEntry{label: "Options", do: func() { m.openOptions(0) }})
	entries = append(entries, menuEntry{label: "Tile Editor (Global)", do: func() { m.openTileEditorOn(tiles.LayerGlobal) }})
	if m.dev() {
		entries = append(entries, menuEntry{label: "Tile Editor (Defaults)", do: func() { m.openTileEditorOn(tiles.LayerDefaults) }})
	}
	entries = append(entries, menuEntry{label: "Quit", do: m.quit})
	menu := newMenu(startMenuID, "Fancyband", entries)
	if m.nativeSplash() {
		splash := newSplash(m)
		m.push(splash)
		menu.place = splash.menuBox
		m.startIntro = splash.intro
	}
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
// over with request id, beside a tombstone, or a crown for a winner,
// unless the classic death screen is chosen.
func (m *Model) openDeathMenu(id int) {
	m.deathID = id
	// Without a tombstone, the menu still opens.
	open := func(tomb *proto.TombData) {
		if m.deathID == id && len(m.stack) == 0 {
			m.pushDeathMenu(id, tomb)
		}
	}
	m.queryElse("TOMB", nil, func(a proto.EventData) { open(a.Tomb) }, func() { open(nil) })
}

// deathTitle is the death menu's title.
func deathTitle(t *proto.TombData) string {
	switch {
	case t != nil && t.Winner:
		return "You have won"
	case t != nil && t.Retired:
		return "You have retired"
	}
	return "You have died"
}

// pushDeathMenu opens the death menu, beside tomb's stone or crown if
// it is set.
func (m *Model) pushDeathMenu(id int, tomb *proto.TombData) {
	var entries []menuEntry
	// As the game's own death menu's New Game: start again from this
	// character's savefile, so character creation offers a quick start
	// like it, with the numeral at the end of its name, if it has one,
	// counted up ("Tester II" becomes "Tester III").
	if m.saveName != "" {
		name := m.saveName
		if m.status != nil && m.status.Name != "" {
			name = m.status.Name
		}
		save := m.saveName
		entries = append(entries, menuEntry{label: "New character like " + name, do: func() { m.leaveDeath(Launch{Name: save}) }})
	}
	entries = append(entries, []menuEntry{
		{label: "New character", do: func() { m.leaveDeath(Launch{New: true}) }},
		{label: "Load a character", do: func() { m.openSaveList(true) }},
		{label: "Look at the items", do: func() { m.openInventory(id, "inven", true) }},
		{label: "Character sheet", do: func() { m.openCharacter(0) }},
		{label: "Messages", do: func() { m.openMessages(0) }},
		{label: "Knowledge", do: func() { m.openKnowledge(0, "") }},
		{label: "Hall of fame", do: func() { m.openKnowledge(0, "scores") }},
		{label: "Quit", do: func() {
			m.closeAll()
			m.next = nil
			m.send(proto.NewReply(id, map[string]any{}))
		}},
	}...)
	menu := newMenu(deathMenuID, deathTitle(tomb), entries)
	menu.width = 0
	for _, e := range entries {
		menu.width = max(menu.width, len([]rune(e.label))+len(e.hint)+8)
	}
	if tomb != nil {
		stone := &tombScreen{tomb: *tomb, menu: menu}
		m.push(stone)
		menu.place = stone.menuBox
	}
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

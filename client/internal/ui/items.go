package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/thebanri/limoni/core/driver"

	"fancyband/internal/proto"
)

// IDs of the item and spell screens in Limoni's accessibility tree, which
// tests use.
const (
	inventoryID  = "inventory"
	actionsID    = "item-actions"
	itemPromptID = "item-prompt"
	spellsID     = "spells"
)

// itemLetter returns the key the game gives an item in its lists: a
// letter in the pack, the equipment, and the floor, and a digit in the
// quiver.
func itemLetter(r proto.ItemRef) rune {
	switch {
	case r.Inven != nil:
		return 'a' + rune(*r.Inven)
	case r.Equip != nil:
		return 'a' + rune(*r.Equip)
	case r.Quiver != nil:
		return '0' + rune(*r.Quiver)
	case r.Floor != nil:
		return 'a' + rune(*r.Floor)
	}
	return 0
}

// weightText shows a weight in tenths of a pound, as the game does.
func weightText(tenths int) string {
	return fmt.Sprintf("%d.%d lb", tenths/10, tenths%10)
}

// itemRow makes a list row for an item: its letter, its tile or symbol,
// its name, and its weight.
func itemRow(it proto.Item) listItem {
	row := listItem{
		Label: fmt.Sprintf("%c) %s", itemLetter(it.Ref), it.Name),
		Hint:  weightText(it.Weight * max(it.Number, 1)),
	}
	if it.Tile != nil {
		row.Tile = &proto.PictCell{FgRow: it.Tile.Row, FgCol: it.Tile.Col}
	} else if r := []rune(it.Char); len(r) > 0 {
		row.Icon, row.IconColor = r[0], it.Color
	}
	return row
}

// recallInto asks the engine to describe something, with RECALL's args,
// and puts the description in *row once it comes.
func (m *Model) recallInto(row *listItem, args map[string]any) {
	m.query("RECALL", args, func(a proto.EventData) {
		if a.Title == nil {
			return
		}
		row.Detail = append(proto.StyledText{{{Text: *a.Title, Color: colorYellow}}, nil}, a.Description...)
	})
}

// letterKey returns the letter or digit a key press types, or 0 for any
// other key.
func letterKey(ev driver.KeyEvent) rune {
	if ev.Type != driver.KeyRune || ev.Ctrl || ev.Alt {
		return 0
	}
	if unicode.IsLetter(ev.Ch) || unicode.IsDigit(ev.Ch) {
		return ev.Ch
	}
	return 0
}

// inventoryTabs are the inventory screen's tabs, in the order of the
// views the engine names.
var inventoryTabs = []struct{ name, view string }{
	{"Pack", "inven"},
	{"Equipment", "equip"},
	{"Quiver", "quiver"},
}

// openInventory opens the inventory screen on view ("inven", "equip", or
// "quiver"), for the screen request id. With looking set, from the
// death menu, it only shows the items and answers no request.
func (m *Model) openInventory(id int, view string, looking bool) {
	m.command("INVENTORY", nil, func(r proto.EventData) {
		inv, ok := m.answers[r.ID]
		delete(m.answers, r.ID)
		if !r.OK || !ok {
			m.log.Printf("INVENTORY refused: %s", r.Error)
			if !looking {
				m.send(proto.NewReply(id, map[string]any{"classic": true}))
			}
			return
		}
		m.showInventory(id, view, looking, inv)
	})
}

func (m *Model) showInventory(id int, view string, looking bool, inv proto.EventData) {
	// rows and items hold each tab's rows and the item on each, nil for
	// an empty equipment slot.
	rows := make([][]listItem, len(inventoryTabs))
	items := make([][]*proto.Item, len(inventoryTabs))
	add := func(t int, it *proto.Item, row listItem) {
		rows[t] = append(rows[t], row)
		items[t] = append(items[t], it)
	}
	for i := range inv.Inventory {
		add(0, &inv.Inventory[i], itemRow(inv.Inventory[i]))
	}
	for slot, name := range inv.Slots {
		var worn *proto.Item
		for i := range inv.Equipment {
			if e := inv.Equipment[i].Ref.Equip; e != nil && *e == slot {
				worn = &inv.Equipment[i]
			}
		}
		if worn == nil {
			add(1, nil, listItem{Label: fmt.Sprintf("%c) (nothing)", 'a'+rune(slot)), Hint: name, Dim: true})
			continue
		}
		row := itemRow(*worn)
		row.Hint = name + "  " + row.Hint
		add(1, worn, row)
	}
	for i := range inv.Quiver {
		add(2, &inv.Quiver[i], itemRow(inv.Quiver[i]))
	}
	for t := range rows {
		for i, it := range items[t] {
			if it != nil {
				m.recallInto(&rows[t][i], map[string]any{"item": it.Ref.Map()})
			}
		}
	}

	tab := 0
	for t, it := range inventoryTabs {
		if it.view == view {
			tab = t
		}
	}
	s := newListScreen(inventoryID, "Inventory", rows[tab])
	for _, it := range inventoryTabs {
		s.tabs = append(s.tabs, it.name)
	}
	s.tab = tab
	s.onTab = func(t int) { s.items = rows[t] }
	s.detail = true
	s.height = 24
	spare := weightText(abs(inv.Remaining)) + " to spare"
	if inv.Remaining < 0 {
		spare = weightText(-inv.Remaining) + " too heavy"
	}
	s.note = fmt.Sprintf("Burden %s (%s)", weightText(inv.Burden), spare)
	close := func() {
		m.pop()
		if !looking {
			m.send(proto.NewReply(id, map[string]any{}))
		}
	}
	s.cancel = close
	if !looking {
		s.hints = []string{"Enter act on it", "/ pack or equipment", "| quiver"}
		s.choose = func(i int) {
			if it := items[s.tab][i]; it != nil {
				m.openActions(id, it)
			}
		}
	} else {
		s.hints = []string{"/ pack or equipment", "| quiver"}
	}
	s.pick = func(ev driver.KeyEvent) bool {
		switch ev.Ch {
		case '/':
			if ev.Type == driver.KeyRune {
				s.setTab(1 - min(s.tab, 1))
				return true
			}
		case '|':
			if ev.Type == driver.KeyRune {
				s.setTab(2)
				return true
			}
		}
		r := letterKey(ev)
		if r == 0 {
			return false
		}
		for i, it := range items[s.tab] {
			if it != nil && itemLetter(it.Ref) == r {
				s.selectItem(i)
				if s.choose != nil {
					s.choose(i)
				}
				return true
			}
		}
		return true
	}
	m.push(s)
}

// openActions opens the menu of what can be done with an item from the
// inventory screen, for its request id: the actions the game's own menu
// offers, each on the key the game gives it.
func (m *Model) openActions(id int, it *proto.Item) {
	if len(it.Actions) == 0 {
		return
	}
	entries := make([]menuEntry, len(it.Actions))
	for i, a := range it.Actions {
		a := a
		entries[i] = menuEntry{label: a.Label, hint: a.Key, do: func() { m.itemAction(id, *it, a) }}
	}
	menu := newMenu(actionsID, it.Name, entries)
	for i, a := range it.Actions {
		menu.items[i].Dim = !a.Valid
	}
	menu.cancel = m.pop
	menu.pick = func(ev driver.KeyEvent) bool {
		if ev.Type != driver.KeyRune || ev.Ctrl || ev.Alt {
			return false
		}
		for i, a := range it.Actions {
			if a.Key == string(ev.Ch) {
				entries[i].do()
				return true
			}
		}
		return false
	}
	m.push(menu)
}

// itemAction does an action on an item: it closes the inventory screen,
// whose request was id, with the action's command. Browsing a book shows
// its spells over the inventory instead.
func (m *Model) itemAction(id int, it proto.Item, a proto.Action) {
	verb := strings.ToLower(a.Label)
	if !a.Valid {
		m.notify("You cannot " + verb + " that now.")
		return
	}
	if a.Cmd == "BROWSE_SPELL" {
		m.pop()
		m.openBook(it)
		return
	}
	arg := a.Arg
	if arg == "" {
		arg = "item"
	}
	args := map[string]any{arg: it.Ref.Map()}
	if a.Quantity > 0 {
		args["quantity"] = a.Quantity
	}
	m.nextID++
	cid := m.nextID
	m.pending[cid] = func(r proto.EventData) {
		if !r.OK && r.Error != "cancelled" {
			m.notify("Cannot " + verb + ": " + r.Error)
		}
	}
	m.closeAll()
	m.send(proto.NewReply(id, map[string]any{"then": map[string]any{"id": cid, "cmd": a.Cmd, "args": args}}))
}

// openBook shows a book's spells, from the inventory screen.
func (m *Model) openBook(it proto.Item) {
	m.query("SPELLS", map[string]any{"item": it.Ref.Map()}, func(a proto.EventData) {
		s := m.spellList(it.Name, a.Spells)
		s.cancel = m.pop
		m.push(s)
	})
}

// spellList builds a list of spells, each with its level, mana, fail
// rate, and the game's note on it, and its description. Spells that
// cannot be chosen are dimmed. A letter picks a spell, as in the game's
// own menu.
func (m *Model) spellList(title string, spells []proto.SpellData) *listScreen {
	items := make([]listItem, len(spells))
	for i, sp := range spells {
		items[i] = listItem{
			Label: fmt.Sprintf("%c) %s", 'a'+rune(i), sp.Name),
			Hint:  fmt.Sprintf("%2d %4d %3d%%  %-12s", sp.Level, sp.Mana, sp.Fail, sp.Comment),
			Dim:   !sp.Usable,
		}
		m.recallInto(&items[i], map[string]any{"spell": sp.Value})
	}
	s := newListScreen(spellsID, title, items)
	s.detail = true
	s.note = "Level, mana, fail rate, and notes"
	s.pick = func(ev driver.KeyEvent) bool {
		r := letterKey(ev)
		if r < 'a' || r >= 'a'+rune(len(items)) {
			return false
		}
		i := int(r - 'a')
		s.selectItem(i)
		if s.choose != nil {
			s.choose(i)
		}
		return true
	}
	return s
}

// openSpellPrompt answers a spell or spell_from_book prompt with a list
// of the spells it offers. Browsing a book shows its spells and chooses
// none.
func (m *Model) openSpellPrompt(d proto.RequestData) bool {
	spells, err := d.SpellChoices()
	if err != nil {
		m.log.Printf("bad spell choices: %v", err)
		return false
	}
	noun := d.Noun
	if noun == "" {
		noun = "spell"
	}
	browse := d.Verb == "browse"
	title := capitalize(d.Verb) + " which " + noun + "?"
	if browse {
		title = capitalize(d.Book)
	}
	s := m.spellList(title, spells)
	s.cancel = func() {
		m.pop()
		m.send(proto.NewReply(d.ID, nil))
	}
	if !browse {
		s.choose = func(i int) {
			if !spells[i].Usable {
				m.notify("You cannot " + d.Verb + " that " + noun + ".")
				return
			}
			m.pop()
			m.send(proto.NewReply(d.ID, map[string]any{"value": spells[i].Value}))
		}
		s.buttons = []listButton{
			{Label: capitalize(d.Verb), Do: func(i int) {
				if i >= 0 {
					s.choose(i)
				}
			}},
			{Label: "Cancel", Do: func(int) { s.cancel() }},
		}
	}
	m.push(s)
	return true
}

// capitalize makes the first letter of s upper case.
func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// itemWheres are the lists an item prompt's choices come from, as the
// tabs it shows them on.
var itemWheres = []struct {
	name string
	in   func(proto.ItemRef) bool
}{
	{"Pack", func(r proto.ItemRef) bool { return r.Inven != nil }},
	{"Equipment", func(r proto.ItemRef) bool { return r.Equip != nil }},
	{"Quiver", func(r proto.ItemRef) bool { return r.Quiver != nil }},
	{"Floor", func(r proto.ItemRef) bool { return r.Floor != nil }},
}

// openItemPrompt answers an item prompt with a list of the items that
// fit, a tab for each place they are in. A letter picks an item in the
// tab shown, and a digit the item its inscription tags with it, as in
// the game's own menu.
func (m *Model) openItemPrompt(d proto.RequestData) bool {
	choices, err := d.Items()
	if err != nil {
		m.log.Printf("bad item choices: %v", err)
		return false
	}
	var names []string
	var groups [][]proto.Item
	var rows [][]listItem
	for _, w := range itemWheres {
		var g []proto.Item
		for _, it := range choices {
			if w.in(it.Ref) {
				g = append(g, it)
			}
		}
		if len(g) == 0 {
			continue
		}
		r := make([]listItem, len(g))
		for i, it := range g {
			r[i] = itemRow(it)
		}
		names = append(names, w.name)
		groups = append(groups, g)
		rows = append(rows, r)
	}
	for t := range rows {
		for i, it := range groups[t] {
			m.recallInto(&rows[t][i], map[string]any{"item": it.Ref.Map()})
		}
	}
	title := strings.TrimSpace(d.Text)
	if title == "" {
		title = "Which item?"
	}
	var first []listItem
	if len(rows) > 0 {
		first = rows[0]
	}
	s := newListScreen(itemPromptID, title, first)
	if len(names) > 1 {
		s.tabs = names
		s.onTab = func(t int) { s.items = rows[t] }
	}
	s.detail = true
	s.height = 24
	tab := func(name string) int {
		for t, n := range names {
			if n == name {
				return t
			}
		}
		return -1
	}
	reply := func(it proto.Item) {
		m.pop()
		m.send(proto.NewReply(d.ID, map[string]any{"item": it.Ref.Map()}))
	}
	s.choose = func(i int) {
		if len(groups) > 0 {
			reply(groups[s.tab][i])
		}
	}
	s.cancel = func() {
		m.pop()
		m.send(proto.NewReply(d.ID, nil))
	}
	s.buttons = []listButton{
		{Label: "Choose", Do: func(i int) {
			if i >= 0 {
				s.choose(i)
			}
		}},
		{Label: "Cancel", Do: func(int) { s.cancel() }},
	}
	s.pick = func(ev driver.KeyEvent) bool {
		if ev.Type == driver.KeyRune && !ev.Ctrl && !ev.Alt {
			switch ev.Ch {
			case '/':
				// Between the pack and the equipment, as the game's menu
				// does.
				if t := tab("Pack"); t >= 0 && s.tab != t {
					s.setTab(t)
				} else if t := tab("Equipment"); t >= 0 {
					s.setTab(t)
				}
				return true
			case '|':
				if t := tab("Quiver"); t >= 0 {
					s.setTab(t)
				}
				return true
			case '-':
				if t := tab("Floor"); t >= 0 {
					s.setTab(t)
				}
				return true
			}
		}
		r := letterKey(ev)
		if r == 0 || len(groups) == 0 {
			return false
		}
		if unicode.IsDigit(r) {
			// An inscription's tag picks its item wherever it is.
			for _, g := range groups {
				for _, it := range g {
					if strings.ContainsRune(it.Tags, r) {
						reply(it)
						return true
					}
				}
			}
		}
		for _, it := range groups[s.tab] {
			if itemLetter(it.Ref) == r {
				reply(it)
				return true
			}
		}
		return true
	}
	m.push(s)
	return true
}

// openPrompt answers a prompt the client claimed with a native screen,
// and returns false for a kind it does not draw.
func (m *Model) openPrompt(d proto.RequestData) bool {
	switch d.Prompt {
	case "item":
		return m.openItemPrompt(d)
	case "spell", "spell_from_book":
		return m.openSpellPrompt(d)
	case "check":
		m.openCheck(d)
		return true
	case "quantity":
		m.openQuantity(d)
		return true
	case "direction":
		m.openDirection(d)
		return true
	case "aim":
		m.openAim(d)
		return true
	}
	return false
}

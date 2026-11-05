package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"unicode"

	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
)

// The character sheet, the message history, and help: each a pager the
// engine hands over with its screen request, or that the death menu
// opens to look back.

const (
	characterID = "character"
	messagesID  = "messages"
	helpID      = "help"
	renameID    = "rename"
)

// replyClose returns a function that closes the top screen and, for a
// screen request id other than 0, answers it.
func (m *Model) replyClose(id int) func() {
	return func() {
		m.pop()
		if id != 0 {
			m.send(proto.NewReply(id, map[string]any{}))
		}
	}
}

// ------------------------------------------------------------------------
// The character sheet

// sheetTabs are the character sheet's tabs.
var sheetTabs = []string{"Summary", "Skills", "Abilities"}

// openCharacter opens the character sheet for the screen request id, or
// with id 0 only to look.
func (m *Model) openCharacter(id int) {
	m.queryElse("CHARACTER", nil, func(a proto.EventData) {
		if a.Character == nil {
			if id != 0 {
				m.send(proto.NewReply(id, map[string]any{"classic": true}))
			}
			return
		}
		sheet := a.Character
		// With tiles, each slot's column of the abilities table is two
		// cells wide, as wide as a tile.
		tiles := m.sheet != nil
		p := &pagerScreen{id: characterID, title: sheet.Name, tabs: sheetTabs, close: m.replyClose(id)}
		p.onTab = func(t int) {
			p.text, p.head = sheetText(sheet, t, tiles)
			p.center = t == 2
		}
		p.keys = []listKey{{Key: 'f', Label: "save to a file", Do: func(int) { m.dumpCharacter(sheet.Name) }}}
		if !sheet.ForceName {
			p.keys = append([]listKey{{Key: 'c', Label: "change name", Do: func(int) {
				m.renameCharacter(sheet.Name, func(name string) {
					sheet.Name, p.title = name, name
					p.text, p.head = sheetText(sheet, p.tab, tiles)
				})
			}}}, p.keys...)
		}
		p.text, p.head = sheetText(sheet, 0, tiles)
		gear := append(sheet.Equippy[:len(sheet.Equippy):len(sheet.Equippy)], sheet.Player)
		p.overlay = func(f *terminal.Frame, line []proto.Span, row Box, env *drawEnv) {
			if p.tab == 2 {
				slotTiles(f, spansText(line), row, sheet.SlotColumn, gear, env)
			}
		}
		m.push(p)
	}, m.classicElse(id))
}

// sheetText is the text of tab t of the character sheet, and the
// headings that stay put above it. With tiles, the row of the
// equipment's letters, which is blank, is left out of the abilities
// table's headings.
func sheetText(c *proto.CharacterSheet, t int, tiles bool) (proto.StyledText, proto.StyledText) {
	panel := func(i int) []proto.PanelLine {
		if i < len(c.Panels) {
			return c.Panels[i]
		}
		return nil
	}
	var text proto.StyledText
	heading := func(s string) {
		if len(text) > 0 {
			text = append(text, nil)
		}
		text = append(text, []proto.Span{{Text: s, Color: colorYellow}})
	}
	switch t {
	case 0:
		text = append(text, sideBySide(panel(0), panel(1))...)
		heading("Stats")
		text = append(text, statsTable(c.Stats)...)
		heading("Progress")
		text = append(text, sideBySide(panel(2), nil)...)
		heading("History")
		text = append(text, proto.PlainText(colorWhite, c.History)...)
	case 1:
		text = append(text, sideBySide(panel(3), panel(4))...)
	default:
		slots := len(c.Equippy) + 1
		head := c.AbilitiesHead
		if tiles && len(head) > 1 && strings.TrimSpace(spansText(head[0])) == "" {
			// The tiles go on the letters' row; the row above, the
			// equipment's letters, is blank with tiles.
			head = head[1:]
		}
		return spreadSlots(c.Abilities, c.SlotColumn, slots), spreadSlots(head, c.SlotColumn, slots)
	}
	return text, nil
}

// The abilities table's spacing: slotGap cells between the labels and
// the slots, and slotWidth cells for each slot, with its mark or letter
// in the cell after the first. A tile covers that cell and the next.
const (
	slotGap   = 2
	slotWidth = 3
)

// slotX is where slot i's mark is in a row spread by spreadSlots, from
// the start of the row.
func slotX(start, i int) int { return start + slotGap + i*slotWidth + 1 }

// spreadSlots gives the n slot columns from column start on more room:
// a gap after the labels, and slotWidth cells for each slot. It changes
// only the table's rows, whose label ends with ':' or which have no
// label, as the heading rows; a part's name stays as it is.
func spreadSlots(text proto.StyledText, start, n int) proto.StyledText {
	type cellText struct {
		r     rune
		color int
	}
	out := make(proto.StyledText, len(text))
	for i, line := range text {
		var cells []cellText
		for _, sp := range line {
			for _, r := range sp.Text {
				cells = append(cells, cellText{r, sp.Color})
			}
		}
		if len(cells) <= start || cells[start-1].r != ':' && cells[start-1].r != ' ' {
			out[i] = line
			continue
		}
		spread := append([]cellText{}, cells[:start]...)
		for g := 0; g < slotGap; g++ {
			spread = append(spread, cellText{' ', cells[start-1].color})
		}
		for s := 0; s < n; s++ {
			c := cellText{' ', colorWhite}
			if start+s < len(cells) {
				c = cells[start+s]
			}
			spread = append(spread, cellText{' ', c.color}, c)
			for k := 2; k < slotWidth; k++ {
				spread = append(spread, cellText{' ', c.color})
			}
		}
		if start+n < len(cells) {
			spread = append(spread, cells[start+n:]...)
		}
		// Back into spans, one for each run of a color.
		var spans []proto.Span
		for _, c := range spread {
			if k := len(spans); k > 0 && spans[k-1].Color == c.color {
				spans[k-1].Text += string(c.r)
			} else {
				spans = append(spans, proto.Span{Text: string(c.r), Color: c.color})
			}
		}
		out[i] = spans
	}
	return out
}

// panelWidth is the width of a panel's column on the sheet.
const panelWidth = 34

// panelSpans is one line of a panel: the label in white and the value in
// its color, padded to width.
func panelSpans(l proto.PanelLine, width int) []proto.Span {
	if l.Label == "" {
		return []proto.Span{{Text: strings.Repeat(" ", width), Color: colorWhite}}
	}
	label := fmt.Sprintf("%-15s", l.Label)
	value := fmt.Sprintf("%*s", width-len(label)-2, l.Value)
	return []proto.Span{{Text: label, Color: colorWhite}, {Text: value + "  ", Color: l.Color}}
}

// sideBySide lays two panels out side by side.
func sideBySide(left, right []proto.PanelLine) proto.StyledText {
	var text proto.StyledText
	for i := 0; i < max(len(left), len(right)); i++ {
		var line []proto.Span
		if i < len(left) {
			line = append(line, panelSpans(left[i], panelWidth)...)
		} else {
			line = append(line, proto.Span{Text: strings.Repeat(" ", panelWidth), Color: colorWhite})
		}
		if i < len(right) {
			line = append(line, proto.Span{Text: "  ", Color: colorWhite})
			line = append(line, panelSpans(right[i], panelWidth)...)
		}
		text = append(text, line)
	}
	return text
}

// statsTable is the stats table, as the game's sheet shows it: the
// stat's own value, the race's, class's, and equipment's bonuses, the
// best it can be, and its value now when drained.
func statsTable(stats []proto.SheetStat) proto.StyledText {
	text := proto.StyledText{{{Text: "        Self   RB   CB   EB    Best", Color: colorWhite}}}
	for _, s := range stats {
		name := s.Name
		if s.Maxed {
			name += "!"
		}
		line := []proto.Span{
			{Text: fmt.Sprintf("%-5s", name), Color: colorWhite},
			{Text: fmt.Sprintf("%7s", s.Self), Color: colorLightGreen},
			{Text: fmt.Sprintf("%+5d%+5d%+5d", s.Race, s.Class, s.Equip), Color: colorLightBlue},
			{Text: fmt.Sprintf("%8s", s.Best), Color: colorLightGreen},
		}
		if s.Current != "" {
			line = append(line, proto.Span{Text: fmt.Sprintf("%8s", s.Current), Color: colorYellow})
		}
		text = append(text, line)
	}
	return text
}

// slotLetters are the letters the game's sheet heads the equipment
// slots' columns with, as its menus letter the equipment: a to z
// without h, j, k, and l, which move in menus; then @ for the character.
func slotLetters(n int) string {
	var b strings.Builder
	for r := 'a'; r <= 'z' && b.Len() < n; r++ {
		if !strings.ContainsRune("hjkl", r) {
			b.WriteRune(r)
		}
	}
	return b.String() + "@"
}

// slotTiles draws, with tiles, the tile of the item worn in each slot
// over that slot's letter where text, drawn in row, holds the slots'
// letters as spreadSlots spreads them from column start, and the
// character's tile over the @ after them: gear is the slots, then the
// character. A slot with nothing in it is blank. Each tile sits on the
// screen's own background.
func slotTiles(f *terminal.Frame, text string, row Box, start int, gear []*proto.Glyph, env *drawEnv) {
	if env.sprite == nil || len(gear) < 2 {
		return
	}
	runes := []rune(text)
	for i, r := range slotLetters(len(gear) - 1) {
		if x := slotX(start, i); x >= len(runes) || runes[x] != r {
			return
		}
	}
	r, g, b := screenBg.Bg.RGB()
	bg := color.RGBA{r, g, b, 255}
	for i, gl := range gear {
		at := Box{row.X + slotX(start, i), row.Y, 2, 1}
		if at.X+at.W > row.X+row.W {
			break
		}
		if gl == nil || gl.Tile == nil {
			if gl == nil {
				f.Text(at.X, at.Y, "  ", screenText.WithBg(screenBg.Bg))
			}
			continue
		}
		for x := at.X; x < at.X+at.W; x++ {
			f.Buffer.SetCell(uint16(x), uint16(at.Y), cell.Cell{Content: cell.RuneImage})
		}
		env.register(f, terminal.ImageRegion{Area: rect(at), Img: env.picture(env.sprite(gl.Tile.Row, gl.Tile.Col, bg), at)})
	}
}

// renameCharacter asks for a new name, and sets it with RENAME.
func (m *Model) renameCharacter(name string, done func(string)) {
	d := &textDialog{id: renameID, title: "Change name", rows: 1, field: textField{max: nameMax}}
	d.field.set(name)
	d.done = func(text string, ok bool) {
		m.pop()
		text = strings.TrimSpace(text)
		if !ok || text == "" || text == name {
			return
		}
		m.command("RENAME", map[string]any{"name": text}, func(r proto.EventData) {
			if !r.OK {
				m.notify("Cannot change the name: " + r.Error)
				return
			}
			done(text)
		})
	}
	m.push(d)
}

// dumpCharacter saves a character dump, as the game's own sheet does,
// to NAME.txt in the user folder.
func (m *Model) dumpCharacter(name string) {
	file := safeName(name) + ".txt"
	m.command("DUMP_CHARACTER", map[string]any{"file": file}, func(r proto.EventData) {
		if !r.OK {
			m.notify("Cannot save the character: " + r.Error)
			return
		}
		m.notify("Saved the character to " + file + " in the game's user folder.")
	})
}

// safeName makes a name safe for a file name, as the game's
// player_safe_name does: letters and digits stay, the rest become _.
func safeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "PLAYER"
	}
	return b.String()
}

// ------------------------------------------------------------------------
// The message history

// openMessages opens the message history for the screen request id, or
// with id 0 only to look.
func (m *Model) openMessages(id int) {
	m.queryElse("MESSAGES", nil, func(a proto.EventData) {
		msgs := a.Messages
		// The kinds, the commonest first, for the filter.
		count := map[string]int{}
		for _, msg := range msgs {
			count[msg.Kind]++
		}
		kinds := make([]string, 0, len(count))
		for k := range count {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool {
			if count[kinds[i]] != count[kinds[j]] {
				return count[kinds[i]] > count[kinds[j]]
			}
			return kinds[i] < kinds[j]
		})
		kinds = append([]string{""}, kinds...)

		p := &pagerScreen{id: messagesID, title: "Messages", searchable: true, atEnd: true, close: m.replyClose(id)}
		kind := 0
		fill := func() {
			p.text = nil
			for _, msg := range msgs {
				if kinds[kind] != "" && msg.Kind != kinds[kind] {
					continue
				}
				line := []proto.Span{{Text: msg.Text, Color: msg.Color}}
				if msg.Count > 1 {
					line = append(line, proto.Span{Text: fmt.Sprintf(" <%dx>", msg.Count), Color: msg.Color})
				}
				p.text = append(p.text, line)
			}
			if kinds[kind] == "" {
				p.note = fmt.Sprintf("All %d messages", len(msgs))
			} else {
				p.note = fmt.Sprintf("Only %s messages: %d of %d", kindName(kinds[kind]), len(p.text), len(msgs))
			}
			p.atEnd = true
		}
		step := func(d int) {
			kind = (kind + d + len(kinds)) % len(kinds)
			fill()
		}
		p.keys = []listKey{
			{Key: 'f', Label: "filter by kind", Do: func(int) { step(1) }},
			{Key: 'F', Label: "back", Do: func(int) { step(-1) }},
		}
		fill()
		m.push(p)
	}, m.classicElse(id))
}

// kindName makes a message kind such as "MON_HIT" readable: "mon hit".
func kindName(kind string) string {
	return strings.ToLower(strings.ReplaceAll(kind, "_", " "))
}

// ------------------------------------------------------------------------
// Help

// openHelp opens a help file, or the index with file "", for the screen
// request id. A link opens another file over it, and Escape goes back.
func (m *Model) openHelp(id int, file string) {
	args := map[string]any{}
	if file != "" {
		args["file"] = file
	}
	m.queryElse("HELP", args, func(a proto.EventData) {
		if a.Page == nil {
			if id != 0 {
				m.send(proto.NewReply(id, map[string]any{"classic": true}))
			} else {
				m.notify("No help file " + file)
			}
			return
		}
		p := &pagerScreen{id: helpID, title: "Help: " + a.File, searchable: true, close: m.replyClose(id), width: 84}
		p.text = helpText(a.Page)
		links := map[rune]string{}
		for _, l := range a.Links {
			r := []rune(l.Key)
			if len(r) == 0 {
				continue
			}
			links[r[0]] = l.File
			label := l.File
			// The line that offers the link names it, as "(a) Available
			// commands".
			for _, line := range p.text {
				t := spansText(line)
				if i := strings.Index(t, "("+l.Key+") "); i >= 0 {
					label = strings.TrimSpace(t[i+len(l.Key)+3:])
				}
			}
			file := l.File
			p.keys = append(p.keys, listKey{Key: r[0], Label: label, Do: func(int) { m.openHelp(0, file) }})
		}
		p.clickLine = func(line int) {
			t := spansText(p.text[line])
			for key, file := range links {
				if strings.Contains(t, "("+string(key)+")") {
					m.openHelp(0, file)
					return
				}
			}
		}
		m.push(p)
	}, m.classicElse(id))
}

// helpText is a help file's text without the paragraph on the classic
// help's keys, which the native screen does not use.
func helpText(page proto.StyledText) proto.StyledText {
	var out proto.StyledText
	skip := false
	for _, line := range page {
		t := spansText(line)
		if strings.HasPrefix(t, "Browser/help commands") {
			skip = true
		}
		if skip {
			if strings.TrimSpace(t) == "" {
				skip = false
			}
			continue
		}
		out = append(out, line)
	}
	// Drop blank lines at the end.
	for len(out) > 0 && strings.TrimSpace(spansText(out[len(out)-1])) == "" {
		out = out[:len(out)-1]
	}
	return out
}

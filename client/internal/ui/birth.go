package ui

import (
	"fmt"
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/widgets"

	"fancyband/internal/proto"
)

// IDs of the birth screen and its parts in Limoni's accessibility tree,
// which tests use.
const (
	birthID        = "birth"
	birthRacesID   = "birth-races"
	birthClassesID = "birth-classes"
	historyID      = "history"
)

// The longest name and history the game keeps.
const (
	nameMax    = 31
	historyMax = 239
)

// birthStep is one step of making a character.
type birthStep int

const (
	stepQuick birthStep = iota
	stepRace
	stepClass
	stepStats
	stepName
	stepHistory
	stepBegin
)

var birthStepNames = []string{"Quick start", "Race", "Class", "Stats", "Name", "History", "Begin"}

// birthScreen makes a character, a step at a time: race, class, stats,
// name, and history, then a last look before the game begins. A
// character like the last one can be made at once. The engine runs each
// birth command as the screen sends it, and the screen closes once the
// character is made.
type birthScreen struct {
	m *Model
	// id is the birth screen's request.
	id   int
	info proto.BirthInfo
	step birthStep
	// reached is the furthest step taken, which the step tabs go back
	// from.
	reached birthStep
	// races and classes are the lists of the first two steps.
	races, classes *listScreen
	// stat is the stat row selected on the stats step.
	stat int
	name textField
	// busy is set while a command runs, which takes no more keys.
	busy     bool
	hotspots []hotspot
}

// openBirth opens the birth screen for the screen request id. If the
// engine cannot describe the character, the classic menus open instead.
func (m *Model) openBirth(id int) {
	classic := m.classicElse(id)
	m.queryElse("BIRTH", nil, func(a proto.EventData) {
		if a.Birth == nil {
			classic()
			return
		}
		s := &birthScreen{m: m, id: id, step: stepRace, name: textField{max: nameMax}}
		s.fill(*a.Birth)
		if s.info.Quickstart {
			s.step = stepQuick
		}
		s.reached = s.step
		m.birth = s
		m.push(s)
	}, classic)
}

// fill shows a BIRTH answer.
func (s *birthScreen) fill(info proto.BirthInfo) {
	s.info = info
	s.name.set(info.Name)
	choices := func(id, title string, cs []proto.BirthChoice, chosen int, choose func(int)) *listScreen {
		items := make([]listItem, len(cs))
		for i, c := range cs {
			items[i] = listItem{Label: c.Name, Detail: s.m.withManual(id, c.Name, c.Help)}
		}
		l := newListScreen(id, title, items)
		l.detail = true
		l.selectItem(chosen)
		l.choose = choose
		// As the later steps have, a button that takes the choice and
		// goes on.
		l.buttons = []listButton{{Label: "Next", Do: func(i int) {
			if i >= 0 && !s.busy {
				choose(i)
			}
		}}}
		return l
	}
	s.races = choices(birthRacesID, "Choose a race", info.Races, info.Race, func(i int) {
		s.do("CHOOSE_RACE", map[string]any{"choice": i}, func() { s.goTo(stepClass) })
	})
	s.classes = choices(birthClassesID, "Choose a class", info.Classes, info.Class, func(i int) {
		s.do("CHOOSE_CLASS", map[string]any{"choice": i}, func() { s.goTo(stepStats) })
	})
}

// goTo moves to a step.
func (s *birthScreen) goTo(step birthStep) {
	if step == stepName && s.info.ForceName {
		// The name was given when the game started.
		if s.step > step {
			step = stepStats
		} else {
			step = stepHistory
		}
	}
	s.step = step
	s.reached = max(s.reached, step)
}

// back goes back a step, or offers to quit from the first.
func (s *birthScreen) back() {
	switch s.step {
	case stepQuick, stepRace:
		s.m.ask("Quit without making a character?", func(yes bool) {
			if yes {
				s.m.quit()
			}
		})
	case stepClass:
		s.goTo(stepRace)
	default:
		s.goTo(s.step - 1)
	}
}

// do sends a birth command, and once it has run asks for the character
// again and runs then.
func (s *birthScreen) do(cmd string, args map[string]any, then func()) {
	if s.busy {
		return
	}
	s.busy = true
	s.m.command(cmd, args, func(r proto.EventData) {
		if !r.OK {
			s.busy = false
			s.m.notify("Cannot do that: " + r.Error)
			return
		}
		s.refresh(then)
	})
}

// refresh asks for the character again, then runs then.
func (s *birthScreen) refresh(then func()) {
	finish := func() {
		s.busy = false
		if then != nil {
			then()
		}
	}
	s.m.queryElse("BIRTH", nil, func(a proto.EventData) {
		if a.Birth != nil {
			s.fill(*a.Birth)
		}
		finish()
	}, finish)
}

// begin makes the character, and the game begins.
func (s *birthScreen) begin() {
	s.busy = true
	s.m.command("ACCEPT_CHARACTER", nil, func(r proto.EventData) {
		s.busy = false
		if !r.OK {
			s.m.notify("Cannot make the character: " + r.Error)
			return
		}
		s.m.pop()
		s.m.birth = nil
	})
}

// startOver starts again from a reset character, as the game's own menus
// do, with the quick start when there is one.
func (s *birthScreen) startOver() {
	s.do("BIRTH_RESET", nil, func() {
		s.step, s.reached = stepRace, stepRace
		if s.info.Quickstart {
			s.step, s.reached = stepQuick, stepQuick
		}
	})
}

// newOne leaves the quick start to make a character from the start.
func (s *birthScreen) newOne() {
	s.do("BIRTH_RESET", nil, func() { s.step, s.reached = stepRace, stepRace })
}

// chooseName takes the typed name, asking first if a savefile has it.
func (s *birthScreen) chooseName() {
	name := strings.TrimSpace(s.name.String())
	if name == "" {
		s.m.notify("Your character needs a name.")
		return
	}
	set := func() {
		s.do("NAME_CHOICE", map[string]any{"name": name}, func() { s.goTo(stepHistory) })
	}
	s.busy = true
	s.m.queryElse("BIRTH", map[string]any{"name": name}, func(a proto.EventData) {
		s.busy = false
		if a.Birth != nil && a.Birth.NameUsed {
			s.m.ask("A savefile for that name exists. Overwrite it?", func(yes bool) {
				if yes {
					set()
				}
			})
			return
		}
		set()
	}, func() {
		s.busy = false
		set()
	})
}

// editHistory opens the history to change it.
func (s *birthScreen) editHistory() {
	d := &textDialog{id: historyID, title: "Character history", rows: 5, field: textField{max: historyMax}}
	d.field.set(s.info.History)
	d.done = func(text string, ok bool) {
		s.m.pop()
		if ok && text != s.info.History {
			s.do("HISTORY_CHOICE", map[string]any{"history": text}, nil)
		}
	}
	s.m.push(d)
}

// stat buying and the roller

func (s *birthScreen) buyStat(i int) {
	if s.info.Rolled {
		s.m.notify("Rolled stats cannot be bought; choose Point-based.")
		return
	}
	s.do("BUY_STAT", map[string]any{"choice": i}, nil)
}

func (s *birthScreen) sellStat(i int) {
	if !s.info.Rolled {
		s.do("SELL_STAT", map[string]any{"choice": i}, nil)
	}
}

func (s *birthScreen) roll() { s.do("ROLL_STATS", nil, nil) }

func (s *birthScreen) previousRoll() {
	if s.info.Rolled {
		s.do("PREV_STATS", nil, nil)
	}
}

func (s *birthScreen) pointBased() {
	s.do("RESET_STATS", map[string]any{"choice": 1}, nil)
}

func (s *birthScreen) key(ev driver.KeyEvent) {
	if s.busy {
		return
	}
	is := func(r rune) bool { return ev.Type == driver.KeyRune && !ev.Ctrl && !ev.Alt && ev.Ch == r }
	if s.step == stepName {
		switch {
		case ev.Type == driver.KeyEnter:
			s.chooseName()
		case ev.Type == driver.KeyEsc:
			s.back()
		default:
			s.name.key(ev)
		}
		return
	}
	if is('=') {
		// The birth options, as '=' in the game's menus.
		s.m.openOptions(0)
		return
	}
	switch s.step {
	case stepQuick:
		switch {
		case is('y'), ev.Type == driver.KeyEnter:
			s.begin()
		case is('c') && !s.info.ForceName:
			s.goTo(stepName)
		case is('n'):
			s.newOne()
		case ev.Type == driver.KeyEsc:
			s.back()
		}
	case stepRace, stepClass:
		l := s.races
		if s.step == stepClass {
			l = s.classes
		}
		if ev.Type == driver.KeyEsc {
			s.back()
			return
		}
		l.key(ev)
	case stepStats:
		switch {
		case ev.Type == driver.KeyArrowUp, is('k'), is('8'):
			s.stat = (s.stat + len(s.info.Stats) - 1) % len(s.info.Stats)
		case ev.Type == driver.KeyArrowDown, is('j'), is('2'):
			s.stat = (s.stat + 1) % len(s.info.Stats)
		case ev.Type == driver.KeyArrowRight, is('+'), is('6'):
			s.buyStat(s.stat)
		case ev.Type == driver.KeyArrowLeft, is('-'), is('4'):
			s.sellStat(s.stat)
		case is('r'):
			s.roll()
		case is('p'):
			s.previousRoll()
		case is('b'):
			s.pointBased()
		case ev.Type == driver.KeyEnter:
			s.goTo(stepName)
		case ev.Type == driver.KeyEsc:
			s.back()
		}
	case stepHistory:
		switch {
		case is('e'):
			s.editHistory()
		case ev.Type == driver.KeyEnter:
			s.goTo(stepBegin)
		case ev.Type == driver.KeyEsc:
			s.back()
		}
	case stepBegin:
		switch {
		case ev.Type == driver.KeyEnter, is('y'):
			s.begin()
		case is('s'):
			s.startOver()
		case ev.Type == driver.KeyEsc:
			s.back()
		}
	}
}

// box fills the window, less a margin, up to a comfortable size.
func (s *birthScreen) box(area Box) Box {
	w, h := min(area.W, 100), min(area.H, 34)
	return Box{area.X + (area.W-w)/2, area.Y + (area.H-h)/2, w, h}
}

func (s *birthScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	s.hotspots = s.hotspots[:0]
	fillBox(f, b, screenBg)
	f.RenderWidget(widgets.NewBlock().Rounded().WithTitle(" New character ").
		WithTitleStyle(screenTitle).WithBorderStyle(screenBorder), rect(b))
	f.RegisterAccessibility(accessibility.Node{ID: birthID, Role: accessibility.RoleDialog, Label: birthStepNames[s.step], Bounds: rect(b)})
	inner := Box{b.X + 1, b.Y + 1, b.W - 2, b.H - 2}

	// The steps along the top; a click goes back to one already taken.
	x, y := inner.X+1, inner.Y
	for st := stepRace; st <= stepBegin; st++ {
		if st == stepName && s.info.ForceName {
			continue
		}
		label := " " + birthStepNames[st] + " "
		style := screenDim
		switch {
		case st == s.step:
			style = screenSelected
		case st <= s.reached && s.step != stepQuick:
			style = screenText
		}
		w := limoni.StringWidth(label)
		if x+w > inner.X+inner.W {
			break
		}
		f.Text(x, y, label, style)
		if st <= s.reached && s.step != stepQuick {
			st := st
			s.hotspot(f, Box{x, y, w, 1}, func() { s.goTo(st) })
		}
		x += w + 1
	}
	body := Box{inner.X, inner.Y + 2, inner.W, inner.H - 3}
	keys := ""
	switch s.step {
	case stepQuick:
		keys = s.drawQuick(f, body)
	case stepRace:
		s.races.draw(f, body, env)
		keys = "↑↓ choose  Enter Accept & Next  = birth options  Esc quit"
	case stepClass:
		s.classes.draw(f, body, env)
		keys = "↑↓ choose  Enter Accept & Next  = birth options  Esc back"
	case stepStats:
		keys = s.drawStats(f, body)
	case stepName:
		keys = s.drawName(f, body)
	case stepHistory:
		keys = s.drawHistory(f, body, env)
	case stepBegin:
		keys = s.drawBegin(f, body, env)
	}
	if s.busy {
		keys = "…"
	}
	f.TextFit(inner.X+1, inner.Y+inner.H-1, inner.W-2, keys, screenDim)
}

// hotspot makes part of the screen clickable.
func (s *birthScreen) hotspot(f *terminal.Frame, b Box, do func()) {
	act := func() {
		if !s.busy {
			do()
		}
	}
	s.hotspots = append(s.hotspots, hotspot{box: b, do: act})
	f.RegisterClickHandler(rect(b), func(driver.MouseEvent) { act() })
}

// buttons draws buttons in a row from x, y, and returns where the row
// ends.
func (s *birthScreen) buttons(f *terminal.Frame, x, y int, bs ...listButton) int {
	for _, bt := range bs {
		w := limoni.StringWidth(bt.Label) + 4
		at := Box{x, y, w, 1}
		do := bt.Do
		f.RenderWidget(widgets.Button{Label: bt.Label, Style: screenTitle.WithBg(screenBg.Bg),
			FocusedStyle: screenTitle.WithBg(screenBg.Bg), OnPress: func() { do(0) }}, rect(at))
		s.hotspot(f, at, func() { do(0) })
		x += w + 1
	}
	return x
}

// summary is the character as made so far: race, class, and what the
// game rolled for it.
func (s *birthScreen) summary() []string {
	i := s.info
	race, class := "", ""
	if i.Race >= 0 && i.Race < len(i.Races) {
		race = i.Races[i.Race].Name
	}
	if i.Class >= 0 && i.Class < len(i.Classes) {
		class = i.Classes[i.Class].Name
	}
	return []string{
		fmt.Sprintf("%s, a %s %s", i.Name, race, class),
		fmt.Sprintf("Hit points %d", i.HP),
		fmt.Sprintf("Age %d   Height %d'%d\"   Weight %d lb", i.Age, i.Height/12, i.Height%12, i.Weight),
	}
}

func (s *birthScreen) drawQuick(f *terminal.Frame, b Box) string {
	lines := append([]string{"Make a character like your last one:", ""}, s.summary()...)
	for n, line := range lines {
		f.TextFit(b.X+2, b.Y+n, b.W-4, line, screenText)
	}
	y := b.Y + len(lines) + 1
	s.drawStatTable(f, Box{b.X + 2, y, b.W - 4, len(s.info.Stats) + 1}, false)
	y += len(s.info.Stats) + 2
	bs := []listButton{{Label: "Use as is", Do: func(int) { s.begin() }}}
	if !s.info.ForceName {
		bs = append(bs, listButton{Label: "Change the name", Do: func(int) { s.goTo(stepName) }})
	}
	bs = append(bs, listButton{Label: "Make a new one", Do: func(int) { s.newOne() }})
	s.buttons(f, b.X+2, y, bs...)
	if s.info.ForceName {
		return "y use as is  n make a new one  = birth options  Esc quit"
	}
	return "y use as is  c change the name  n make a new one  = birth options  Esc quit"
}

// drawStatTable draws the stats, with the points each cost when they
// are point-based, and with edit the selected row and its buttons.
func (s *birthScreen) drawStatTable(f *terminal.Frame, b Box, edit bool) {
	points := !s.info.Rolled
	head := fmt.Sprintf("%-5s%6s%6s%6s%8s", "", "Self", "Race", "Class", "Best")
	if points && edit {
		head += fmt.Sprintf("%8s%6s", "Spent", "Next")
	}
	f.TextFit(b.X, b.Y, b.W, " "+head, screenDim)
	for i, st := range s.info.Stats {
		y := b.Y + 1 + i
		if y >= b.Y+b.H {
			break
		}
		line := fmt.Sprintf("%-5s%6s%6s%6s%8s", st.Name, statText(st.Self),
			fmt.Sprintf("%+d", st.Race), fmt.Sprintf("%+d", st.Class), statText(st.Best))
		if points && edit {
			next := "max"
			if st.Cost != nil {
				next = fmt.Sprint(*st.Cost)
			}
			line += fmt.Sprintf("%8d%6s", st.Spent, next)
		}
		style := screenText
		if edit && i == s.stat {
			style = screenSelected
		}
		w := limoni.StringWidth(line) + 2
		f.TextFit(b.X, y, b.W, " "+line+" ", style)
		f.RegisterAccessibility(accessibility.Node{Role: accessibility.RoleListItem, Label: st.Name, Value: statText(st.Self), Bounds: rect(Box{b.X, y, w, 1})})
		if !edit {
			continue
		}
		i := i
		s.hotspot(f, Box{b.X, y, w, 1}, func() { s.stat = i })
		if points {
			s.buttons(f, b.X+w+1, y,
				listButton{Label: "-", Do: func(int) { s.stat = i; s.sellStat(i) }},
				listButton{Label: "+", Do: func(int) { s.stat = i; s.buyStat(i) }})
		}
	}
}

func (s *birthScreen) drawStats(f *terminal.Frame, b Box) string {
	s.stat = min(s.stat, len(s.info.Stats)-1)
	s.drawStatTable(f, Box{b.X + 2, b.Y, b.W - 4, len(s.info.Stats) + 1}, true)
	y := b.Y + len(s.info.Stats) + 2
	if s.info.Rolled {
		f.TextFit(b.X+2, y, b.W-4, "The roller made these stats.", screenText)
	} else {
		f.TextFit(b.X+2, y, b.W-4, fmt.Sprintf("Points left: %d", s.info.Points), screenText)
	}
	for n, line := range s.summary()[1:] {
		f.TextFit(b.X+2, y+2+n, b.W-4, line, screenText)
	}
	y += 5
	bs := []listButton{{Label: "Roll", Do: func(int) { s.roll() }}}
	if s.info.Rolled {
		bs = append(bs, listButton{Label: "Previous roll", Do: func(int) { s.previousRoll() }},
			listButton{Label: "Point-based", Do: func(int) { s.pointBased() }})
	} else {
		bs = append(bs, listButton{Label: "Suggested", Do: func(int) { s.pointBased() }})
	}
	bs = append(bs, listButton{Label: "Next", Do: func(int) { s.goTo(stepName) }})
	s.buttons(f, b.X+2, y, bs...)
	if s.info.Rolled {
		return "r roll again  p previous roll  b point-based  Enter next  Esc back"
	}
	return "↑↓ stat  ←→ less or more  r roll  b suggested  Enter next  Esc back"
}

func (s *birthScreen) drawName(f *terminal.Frame, b Box) string {
	f.TextFit(b.X+2, b.Y, b.W-4, "What is your character's name?", screenText)
	s.name.draw(f, Box{b.X + 2, b.Y + 2, min(nameMax+1, b.W-4), 1}, "Name")
	s.buttons(f, b.X+2, b.Y+4, listButton{Label: "Next", Do: func(int) { s.chooseName() }})
	return "Type the name  Enter next  Esc back"
}

func (s *birthScreen) drawHistory(f *terminal.Frame, b Box, env *drawEnv) string {
	f.TextFit(b.X+2, b.Y, b.W-4, "Your character's history:", screenText)
	text := proto.PlainText(colorWhite, s.info.History)
	h := styledHeight(text, b.W-4)
	f.RenderWidget(&styledView{text: text, pal: env.pal}, rect(Box{b.X + 2, b.Y + 2, b.W - 4, h}))
	s.buttons(f, b.X+2, b.Y+3+h,
		listButton{Label: "Edit", Do: func(int) { s.editHistory() }},
		listButton{Label: "Next", Do: func(int) { s.goTo(stepBegin) }})
	return "e edit  Enter next  Esc back"
}

func (s *birthScreen) drawBegin(f *terminal.Frame, b Box, env *drawEnv) string {
	lines := s.summary()
	for n, line := range lines {
		f.TextFit(b.X+2, b.Y+n, b.W-4, line, screenText)
	}
	y := b.Y + len(lines) + 1
	s.drawStatTable(f, Box{b.X + 2, y, b.W - 4, len(s.info.Stats) + 1}, false)
	y += len(s.info.Stats) + 2
	text := proto.PlainText(colorWhite, s.info.History)
	h := min(styledHeight(text, b.W-4), max(b.Y+b.H-y-3, 0))
	f.RenderWidget(&styledView{text: text, pal: env.pal}, rect(Box{b.X + 2, y, b.W - 4, h}))
	s.buttons(f, b.X+2, y+h+1,
		listButton{Label: "Begin", Do: func(int) { s.begin() }},
		listButton{Label: "Start over", Do: func(int) { s.startOver() }})
	return "Enter begin  s start over  Esc back"
}

// controls returns the parts a click acts on, as last drawn.
func (s *birthScreen) controls() []hotspot {
	out := append([]hotspot(nil), s.hotspots...)
	switch s.step {
	case stepRace:
		out = append(out, s.races.hotspots...)
	case stepClass:
		out = append(out, s.classes.hotspots...)
	}
	return out
}

func (s *birthScreen) click(x, y int) {
	if s.busy {
		return
	}
	for _, h := range s.controls() {
		if x >= h.box.X && x < h.box.X+h.box.W && y >= h.box.Y && y < h.box.Y+h.box.H {
			h.do()
			return
		}
	}
}

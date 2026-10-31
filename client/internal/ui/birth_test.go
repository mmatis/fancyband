package ui

import (
	"fmt"
	"testing"

	"github.com/thebanri/limoni/core/driver"

	"fancyband/internal/proto"
)

// fakeBirth is a character being made, with point-based stats.
func fakeBirth() proto.BirthInfo {
	cost := 1
	info := proto.BirthInfo{Name: "Suggested", History: "Born somewhere.", Points: 5, HP: 20, Gold: 600}
	for _, r := range []string{"Human", "Dwarf"} {
		info.Races = append(info.Races, proto.BirthChoice{Name: r, Help: proto.PlainText(colorWhite, r+" help")})
	}
	for _, c := range []string{"Warrior", "Mage"} {
		info.Classes = append(info.Classes, proto.BirthChoice{Name: c, Help: proto.PlainText(colorWhite, c+" help")})
	}
	for _, st := range []string{"Str", "Int", "Wis", "Dex", "Con"} {
		info.Stats = append(info.Stats, proto.BirthStat{Name: st, Self: 10, Best: 10, Cost: &cost})
	}
	return info
}

// sent checks the screen sent a command and waits for it.
func sent(t *testing.T, s *birthScreen) {
	t.Helper()
	if !s.busy {
		t.Fatal("the screen sent no command")
	}
}

// TestBirthScreenSteps goes through every step with the keys: race,
// class, stats, name, history, and begin, and back.
func TestBirthScreenSteps(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	s := &birthScreen{m: m, id: 3, step: stepRace, name: textField{max: nameMax}}
	s.fill(fakeBirth())
	m.birth = s
	m.push(s)
	step := func(want birthStep) {
		t.Helper()
		if s.step != want {
			t.Fatalf("on step %s, want %s", birthStepNames[s.step], birthStepNames[want])
		}
	}
	ran := func(info proto.BirthInfo) {
		t.Helper()
		sent(t, s)
		answer(t, m, true, proto.EventData{})
		answer(t, m, true, proto.EventData{Event: "BIRTH", Birth: &info})
	}

	// Choosing a race moves on once the engine has run it.
	s.key(keyOf(driver.KeyArrowDown))
	s.key(keyOf(driver.KeyEnter))
	step(stepRace)
	dwarf := fakeBirth()
	dwarf.Race = 1
	ran(dwarf)
	step(stepClass)
	if s.races.selected() != 1 {
		t.Errorf("the race list selects %d, want the dwarf", s.races.selected())
	}
	s.key(keyOf(driver.KeyEsc))
	step(stepRace)
	s.reached = stepClass
	s.goTo(stepClass)
	s.key(keyOf(driver.KeyEnter))
	ran(fakeBirth())
	step(stepStats)

	// The arrows buy and sell; r rolls.
	s.key(keyOf(driver.KeyArrowDown))
	s.key(keyOf(driver.KeyArrowRight))
	ran(fakeBirth())
	if s.stat != 1 {
		t.Errorf("buying moved to stat %d", s.stat)
	}
	s.key(runeKey('r'))
	rolled := fakeBirth()
	rolled.Rolled = true
	ran(rolled)
	s.key(keyOf(driver.KeyArrowRight))
	if s.busy {
		t.Error("rolled stats were bought")
	}
	s.key(keyOf(driver.KeyEnter))
	step(stepName)

	// Typing replaces the suggested name.
	for _, r := range "Kim" {
		s.key(runeKey(r))
	}
	if got := s.name.String(); got != "Kim" {
		t.Fatalf("the name is %q", got)
	}
	s.key(keyOf(driver.KeyEnter))
	answer(t, m, true, proto.EventData{Event: "BIRTH", Birth: &proto.BirthInfo{NameUsed: true}})
	ask := m.stack[len(m.stack)-1].(*listScreen)
	if ask.id != checkID {
		t.Fatal("no question about overwriting the savefile")
	}
	ask.key(runeKey('y'))
	named := fakeBirth()
	named.Name = "Kim"
	ran(named)
	step(stepHistory)

	// e edits the history.
	s.key(runeKey('e'))
	d, ok := m.stack[len(m.stack)-1].(*textDialog)
	if !ok {
		t.Fatal("e opened no history dialog")
	}
	d.key(keyOf(driver.KeyEnd))
	for _, r := range " Then left." {
		d.key(runeKey(r))
	}
	d.key(keyOf(driver.KeyEnter))
	ran(named)
	s.key(keyOf(driver.KeyEnter))
	step(stepBegin)

	// Enter makes the character, and the screen closes.
	s.key(keyOf(driver.KeyEnter))
	answer(t, m, true, proto.EventData{})
	if len(m.stack) != 0 || m.birth != nil {
		t.Error("the birth screen is still open")
	}
}

// TestBirthScreenQuickStart uses the quick start's keys, and skips the
// name when the game fixed it.
func TestBirthScreenQuickStart(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	s := &birthScreen{m: m, id: 3, step: stepQuick, name: textField{max: nameMax}}
	info := fakeBirth()
	info.Quickstart, info.ForceName = true, true
	s.fill(info)
	m.push(s)

	s.key(runeKey('c'))
	if s.step != stepQuick {
		t.Error("c changed a name the game fixed")
	}
	s.key(runeKey('n'))
	answer(t, m, true, proto.EventData{})
	answer(t, m, true, proto.EventData{Event: "BIRTH", Birth: &info})
	if s.step != stepRace {
		t.Fatalf("n went to step %s", birthStepNames[s.step])
	}
	s.goTo(stepStats)
	s.key(keyOf(driver.KeyEnter))
	if s.step != stepHistory {
		t.Errorf("after the stats with a fixed name, step %s", birthStepNames[s.step])
	}
	s.key(keyOf(driver.KeyEsc))
	if s.step != stepStats {
		t.Errorf("back from the history with a fixed name, step %s", birthStepNames[s.step])
	}
}

func TestTextField(t *testing.T) {
	f := textField{max: 5}
	f.set("abc")
	f.key(keyOf(driver.KeyArrowLeft))
	f.key(runeKey('X'))
	if f.String() != "abXc" {
		t.Errorf("inserting: %q", f.String())
	}
	f.key(keyOf(driver.KeyBackspace))
	f.key(keyOf(driver.KeyDelete))
	if f.String() != "ab" {
		t.Errorf("deleting: %q", f.String())
	}
	for _, r := range "wxyz" {
		f.key(runeKey(r))
	}
	if f.String() != "abwxy" {
		t.Errorf("past the limit: %q", f.String())
	}
	f.set("old")
	f.key(keyOf(driver.KeyBackspace))
	if f.String() != "" {
		t.Errorf("Backspace on the given text left %q", f.String())
	}

	// Rows break at spaces, and the cell after the text counts.
	f = textField{max: 100}
	f.set("one two three four")
	if got := f.rows(10); fmt.Sprint(got) != "[0 8 14]" {
		t.Errorf("rows of 10: %v", got)
	}
	f.set("abcdefghij")
	if got := f.rows(10); fmt.Sprint(got) != "[0 10]" {
		t.Errorf("a full row: %v", got)
	}
}

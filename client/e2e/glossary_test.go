package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// TestGlossary reads GLOSSARY before a character exists, at the splash
// screen: every race, class, stat, skill, ability, element,
// curse, and status effect the game defines is there, each with
// text and a name, and only races, classes, stats, and skills use the
// manual's descriptions.
func TestGlossary(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := start(t, dargs(d, "-n"))
	a := g.answerOf(1, "GLOSSARY", nil)
	if len(a.Terms) == 0 {
		t.Fatal("GLOSSARY gave no terms")
	}
	byID := map[string]proto.GlossaryTerm{}
	kinds := map[string]int{}
	for _, term := range a.Terms {
		if _, dup := byID[term.ID]; dup {
			t.Errorf("term %s given twice", term.ID)
		}
		byID[term.ID] = term
		kinds[term.Kind]++
		if term.Title == "" || len(term.Text) == 0 || len(term.Names) == 0 {
			t.Errorf("term %s: title %q, %d lines, %d names", term.ID, term.Title, len(term.Text), len(term.Names))
		}
	}
	if path := os.Getenv("GLOSSARY_DUMP"); path != "" {
		var b strings.Builder
		for _, term := range a.Terms {
			var names []string
			for _, n := range term.Names {
				l := ""
				if n.Label {
					l = "*"
				}
				names = append(names, n.Name+l)
			}
			fmt.Fprintf(&b, "%s %q manual=%v names=%q\n  %s\n", term.ID, term.Title, term.Manual, names, glossText(term.Text))
		}
		for _, s := range a.Series {
			fmt.Fprintf(&b, "series %q: %v\n", s.Lead, s.Items)
		}
		os.WriteFile(path, []byte(b.String()), 0o644)
	}
	// The game defines these many of each; elements are those the
	// manual describes.
	want := map[string]int{"race": 11, "class": 9, "stat": 5, "skill": 10, "curse": 27}
	for kind, n := range want {
		if kinds[kind] != n {
			t.Errorf("%d %s terms, want %d", kinds[kind], kind, n)
		}
	}
	if kinds["ability"] < 44 || kinds["element"] < 15 || kinds["timed"] < 50 {
		t.Errorf("terms by kind %v: want 44+ abilities, 15+ elements, 50+ status effects", kinds)
	}
	// Races, classes, stats, and skills have only the manual's text;
	// everything else has its data file's one line.
	for _, id := range []string{"race:Hobbit", "class:Mage", "stat:STR", "skill:SAVE"} {
		if !byID[id].Manual {
			t.Errorf("%s has no manual text: %v", id, glossText(byID[id].Text))
		}
	}
	for _, id := range []string{"ability:KNOW_MUSHROOM", "ability:HOLD_LIFE", "ability:RES_FIRE", "element:NEXUS",
		"curse:vulnerability", "timed:STUN", "timed:FOOD"} {
		if term, ok := byID[id]; !ok || term.Manual {
			t.Errorf("%s: present %v, manual %v, want the data file's line", id, ok, term.Manual)
		}
	}
	if text := glossText(byID["ability:HOLD_LIFE"].Text); len(text) > 80 {
		t.Errorf("Hold Life's text is long: %q", text)
	}
	if text := glossText(byID["element:FIRE"].Text); !strings.Contains(text, "Fire Resistance: You resist fire.") {
		t.Errorf("fire's text %q", text)
	}
	if text := glossText(byID["ability:KNOW_MUSHROOM"].Text); text != "You easily recognize mushrooms." {
		t.Errorf("Mushroom Lore's text %q", text)
	}
	if !strings.Contains(glossText(byID["race:Hobbit"].Text), "mushrooms") {
		t.Errorf("Hobbit's text %q", glossText(byID["race:Hobbit"].Text))
	}
	hasName := func(id, name string, label bool) {
		for _, n := range byID[id].Names {
			if n.Name == name {
				if n.Label != label {
					t.Errorf("%s's name %q: label %v, want %v", id, name, n.Label, label)
				}
				return
			}
		}
		t.Errorf("%s has no name %q: %v", id, name, byID[id].Names)
	}
	hasName("ability:HOLD_LIFE", "Hold Life", false)
	hasName("ability:HOLD_LIFE", "hold life", false)
	hasName("ability:FREE_ACT", "Prevents paralysis", false)
	hasName("timed:AFRAID", "Afraid", true)
	hasName("element:FIRE", "fire", true)
	hasName("element:NEXUS", "nexus", false)
	hasName("stat:STR", "STR", false)
	hasName("skill:SAVE", "Save", true)
	// The comparison's column for fire shows the element.
	hasName("element:FIRE", "Fire", true)
	leads := map[string]map[string]string{}
	for _, s := range a.Series {
		leads[s.Lead] = map[string]string{}
		for _, it := range s.Items {
			leads[s.Lead][it.Name] = it.ID
		}
	}
	if leads["Provides resistance to"]["fire"] != "ability:RES_FIRE" ||
		leads["Provides protection from"]["fear"] != "ability:PROT_FEAR" ||
		leads["Sustains"]["strength"] != "ability:SUST_STR" {
		t.Errorf("series %v", leads)
	}
}

// glossText is styled text as plain text, lines joined by newlines.
func glossText(text proto.StyledText) string {
	var lines []string
	for _, line := range text {
		var b strings.Builder
		for _, sp := range line {
			b.WriteString(sp.Text)
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

package e2e

import (
	"strings"
	"testing"
	"unicode/utf8"

	"fancyband/internal/enginetest"
	"fancyband/internal/proto"
)

// TestCompare reads COMPARE for a new warrior in town: every property
// has a column, the character's row and each item's have a letter and a
// color per column, the worn items come first, the stores' wares are
// there too, and RECALL describes an item from a store by its reference.
func TestCompare(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events"})
	g.createCharacter()

	c := g.answerOf(1, "COMPARE", nil).Compare
	if c == nil {
		t.Fatal("COMPARE gave nothing")
	}
	n := len(c.Props)
	cats := map[string]int{}
	for _, p := range c.Props {
		cats[p.Category]++
		if utf8.RuneCountInString(p.Code) != 2 || p.Name == "" || strings.HasSuffix(p.Name, " ") {
			t.Errorf("property %+v", p)
		}
	}
	for _, cat := range []string{"resistances", "abilities", "hindrances", "modifiers", "stat_modifiers"} {
		if cats[cat] == 0 {
			t.Errorf("no %s among %d properties", cat, n)
		}
	}
	if utf8.RuneCountInString(c.Player.Text) != n || len(c.Player.Colors) != n {
		t.Errorf("player row %q with %d colors, want %d", c.Player.Text, len(c.Player.Colors), n)
	}
	sources := map[string]int{}
	for i, it := range c.Items {
		sources[it.Source]++
		if utf8.RuneCountInString(it.Text) != n || len(it.Colors) != n || len(it.Values) != n {
			t.Fatalf("item %d %q: %d letters, %d colors, %d values; want %d", i, it.Name, utf8.RuneCountInString(it.Text), len(it.Colors), len(it.Values), n)
		}
		if it.Ref.Compare == nil || *it.Ref.Compare != i || it.Name == "" || it.Quality == "" {
			t.Errorf("item %d: %+v", i, it)
		}
		if i > 0 && it.Source == "e" && c.Items[i-1].Source != "e" && c.Items[i-1].Slot >= it.Slot {
			t.Errorf("worn item %q out of order", it.Name)
		}
	}
	if sources["e"] == 0 || sources["s"] == 0 {
		t.Errorf("sources %v, want worn items and the stores' wares", sources)
	}

	var store int
	for i, it := range c.Items {
		if it.Source == "s" {
			store = i
			break
		}
	}
	r := g.answerOf(2, "RECALL", map[string]any{"item": proto.ItemRef{Compare: &store}})
	if r.Title == nil || !strings.EqualFold(*r.Title, c.Items[store].Name) || len(r.Description) == 0 {
		t.Errorf("RECALL of store item %q: title %v, %d lines", c.Items[store].Name, r.Title, len(r.Description))
	}
	if r := g.answerOf(3, "RECALL", map[string]any{"item": map[string]any{"compare": len(c.Items)}}); r.Title != nil {
		t.Errorf("RECALL past the list gave %q", *r.Title)
	}
}

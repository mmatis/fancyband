package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// styledLines returns styled text as plain lines.
func styledLines(text proto.StyledText) []string {
	var out []string
	for _, line := range text {
		var b strings.Builder
		for _, sp := range line {
			b.WriteString(sp.Text)
		}
		out = append(out, b.String())
	}
	return out
}

// knowledgeEntry returns the entry of a KNOWLEDGE section whose name
// contains name.
func knowledgeEntry(t *testing.T, k proto.EventData, name string) proto.KnowledgeEntry {
	t.Helper()
	for _, e := range k.List {
		if strings.Contains(strings.ToLower(e.Name), strings.ToLower(name)) {
			return e
		}
	}
	t.Fatalf("%s has no %q: %+v", k.Section, name, k.List)
	return proto.KnowledgeEntry{}
}

// TestKnowledgeCharacterMessagesHelp kills a monster with a new
// character, then checks KNOWLEDGE, CHARACTER, MESSAGES, and HELP against
// what the game knows and against the classic screens, and opens each
// screen by its key.
func TestKnowledgeCharacterMessagesHelp(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:knowledge", "screen:character", "screen:messages", "screen:help"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" }
	g.createCharacter()
	g.autoMore(1)

	// Summon a shrieker mushroom patch, which never moves, and kill it
	// with the debug command that hits everything in view.
	g.debug('n', [2]string{"Summon which monster", "shrieker mushroom patch"})
	g.waitEvent("WAIT")
	g.key(proto.Ctrl('a'))
	g.until("the debug prompt", func(gr *grid.Grid) bool { return gr.Contains("Debug Command:") })
	g.key('H')

	// The sections, each known or not.
	secs := g.answerOf(2, "KNOWLEDGE", nil).Sections
	names := map[string]proto.KnowledgeSection{}
	for _, s := range secs {
		names[s.Name] = s
	}
	for _, n := range []string{"objects", "runes", "artifacts", "egos", "monsters", "features", "traps", "shapes", "store:0", "scores", "history", "equipment"} {
		if _, ok := names[n]; !ok {
			t.Errorf("no section %q in %+v", n, secs)
		}
	}
	if !names["objects"].Known || names["artifacts"].Known || names["objects"].Title != "Objects" {
		t.Errorf("sections %+v", secs)
	}

	// The patch is in the monster list, with one kill, and its own
	// letter and the Tile Editor's entry.
	var mons proto.EventData
	g.afterWaits("the patch killed", func() bool {
		mons = g.answerOf(len(g.results)+100, "KNOWLEDGE", map[string]any{"section": "monsters"})
		for _, e := range mons.List {
			if strings.Contains(e.Name, "shrieker") && e.Note == "1" {
				return true
			}
		}
		return false
	})
	patch := knowledgeEntry(t, mons, "shrieker mushroom patch")
	if patch.Char != "," || patch.Group == "" || patch.Visual == nil || patch.Visual.Kind != "monster" || patch.Visual.ID != patch.ID {
		t.Errorf("the patch's entry %+v", patch)
	}
	rc := g.answerOf(3, "RECALL", map[string]any{"section": "monsters", "id": patch.ID})
	if rc.Title == nil || !strings.EqualFold(*rc.Title, "shrieker mushroom patch") || len(rc.Description) == 0 {
		t.Errorf("the patch's recall %+v", rc)
	}

	// Objects: what the character carries is known, flavors not yet
	// tried are dim, and each describes itself.
	objs := g.answerOf(4, "KNOWLEDGE", map[string]any{"section": "objects"})
	if len(objs.List) < 10 {
		t.Fatalf("only %d objects", len(objs.List))
	}
	dim := 0
	for _, e := range objs.List {
		if e.Dim {
			dim++
		}
		if e.Char == "" || e.Visual == nil {
			t.Errorf("object without a letter or visual: %+v", e)
		}
	}
	if dim == 0 || dim == len(objs.List) {
		t.Errorf("%d of %d objects dim", dim, len(objs.List))
	}
	first := objs.List[0]
	if rc := g.answerOf(5, "RECALL", map[string]any{"section": "objects", "id": first.ID}); rc.Title == nil {
		t.Errorf("recall of %s: %+v", first.Name, rc)
	}
	// An artifact the character has never seen cannot be recalled.
	if rc := g.answerOf(6, "RECALL", map[string]any{"section": "artifacts", "id": 1}); rc.Title != nil {
		t.Errorf("recall of an unknown artifact: %q", *rc.Title)
	}

	// Features and traps list every kind, each with a description.
	for i, sec := range []string{"features", "traps"} {
		k := g.answerOf(10+i, "KNOWLEDGE", map[string]any{"section": sec})
		if len(k.List) < 5 {
			t.Fatalf("%s: %+v", sec, k.List)
		}
		if rc := g.answerOf(20+i, "RECALL", map[string]any{"section": sec, "id": k.List[0].ID}); rc.Title == nil {
			t.Errorf("recall of %s %s: %+v", sec, k.List[0].Name, rc)
		}
	}

	// Every section answers, and each entry of a known section describes
	// itself.
	for i, sec := range secs {
		k := g.answerOf(200+i, "KNOWLEDGE", map[string]any{"section": sec.Name})
		if k.Section != sec.Name {
			t.Errorf("asked for %s, got %q", sec.Name, k.Section)
		}
		switch {
		case sec.Name == "equipment", sec.Name == "history", sec.Name == "scores", strings.HasPrefix(sec.Name, "store:"):
		case sec.Known && len(k.List) == 0:
			t.Errorf("%s is known but empty", sec.Name)
		case !sec.Known && len(k.List) > 0:
			t.Errorf("%s is unknown but lists %+v", sec.Name, k.List)
		case len(k.List) > 0:
			e := k.List[len(k.List)-1]
			if rc := g.answerOf(300+i, "RECALL", map[string]any{"section": sec.Name, "id": e.ID}); rc.Title == nil {
				t.Errorf("recall of %s %s: %+v", sec.Name, e.Name, rc)
			}
		}
	}

	// The General Store's stock, with prices, and each describes itself.
	store := g.answerOf(30, "KNOWLEDGE", map[string]any{"section": "store:0"})
	if len(store.List) == 0 || !strings.HasSuffix(store.List[0].Note, " gold") {
		t.Fatalf("store:0 %+v", store.List)
	}
	if rc := g.answerOf(31, "RECALL", map[string]any{"section": "store:0", "id": 0}); rc.Title == nil {
		t.Errorf("recall of %s: %+v", store.List[0].Name, rc)
	}

	// The history starts with the character's birth, and the hall of
	// fame has the character's place.
	hist := g.answerOf(32, "KNOWLEDGE", map[string]any{"section": "history"})
	if len(hist.List) == 0 || !strings.HasPrefix(hist.List[0].Name, "Began the quest") || hist.List[0].Level != 1 {
		t.Errorf("history %+v", hist.List)
	}
	scores := g.answerOf(33, "KNOWLEDGE", map[string]any{"section": "scores"})
	current := 0
	for _, s := range scores.List {
		if s.Current {
			current++
			if !strings.Contains(s.Name, "level 1") || len(s.Lines) != 2 || !strings.Contains(s.Lines[0], "nobody (yet!)") {
				t.Errorf("the character's score %+v", s)
			}
		}
	}
	if current != 1 {
		t.Errorf("%d current scores in %+v", current, scores.List)
	}

	// The character sheet.
	ch := g.answerOf(40, "CHARACTER", nil).Character
	if ch == nil {
		t.Fatal("no character sheet")
	}
	if len(ch.Panels) != 5 || len(ch.Stats) != 5 || ch.History == "" || len(ch.Abilities) < 10 || len(ch.Equippy) != 12 {
		t.Fatalf("sheet %+v", ch)
	}
	if ch.Player == nil || ch.Player.Char != "@" {
		t.Errorf("the character's own glyph %+v", ch.Player)
	}
	if l := ch.Panels[0][0]; l.Label != "Name" || l.Value != ch.Name {
		t.Errorf("first line %+v, name %q", l, ch.Name)
	}
	// The abilities table: its headings, then every part in one column.
	if head := styledLines(ch.AbilitiesHead); len(head) != 2 || head[1] != "      abcdefgimnop@" || ch.SlotColumn != 6 {
		t.Errorf("abilities head %q, slots at %d", head, ch.SlotColumn)
	}
	abilities := strings.Join(styledLines(ch.Abilities), "\n")
	for _, want := range []string{"Stats", "  STR:", "Resistances", " Acid:", "Abilities", "Hindrances", "Modifiers"} {
		if !strings.Contains(abilities, want) {
			t.Errorf("abilities lack %q:\n%s", want, abilities)
		}
	}

	// The classic sheet shows the same.
	g.key('C')
	r := g.heldScreen("character")
	g.send(proto.NewReply(r.ID, map[string]any{"classic": true}))
	g.waitText("Max Exp")
	for _, p := range ch.Panels {
		for _, l := range p {
			if l.Label != "" && l.Value != "" && !g.screen().Contains(l.Value) {
				t.Errorf("classic sheet lacks %s %q", l.Label, l.Value)
			}
		}
	}
	for _, s := range ch.Stats {
		if !g.screen().Contains(s.Best) {
			t.Errorf("classic sheet lacks %s %q", s.Name, s.Best)
		}
	}
	// Its second page has the same rows, side by side, but may leave
	// some out to fit; each row it shows matches.
	g.key('h')
	g.waitText("abcdefgimnop@")
	found := 0
	resists := false
	for _, line := range styledLines(ch.Abilities) {
		if line == "Resistances" {
			resists = true
		}
		label, _, ok := strings.Cut(line, ":")
		if !resists || !ok {
			continue
		}
		if g.screen().Contains(label + ":") {
			found++
			if !g.screen().Contains(line) {
				t.Errorf("classic second page has %q, but not as %q", label, line)
			}
		}
	}
	if found < 20 {
		t.Errorf("only %d rows of the abilities table on the classic page", found)
	}
	g.key(proto.KeyEscape)
	g.waitEvent("WAIT")

	// RENAME changes the name; DUMP_CHARACTER writes a file.
	if res := g.command(41, "RENAME", map[string]any{"name": "Renamed"}); !res.OK {
		t.Errorf("RENAME: %+v", res)
	}
	if ch := g.answerOf(42, "CHARACTER", nil).Character; ch.Name != "Renamed" {
		t.Errorf("name after RENAME %q", ch.Name)
	}
	if res := g.command(43, "DUMP_CHARACTER", map[string]any{"file": "dump.txt"}); !res.OK {
		t.Errorf("DUMP_CHARACTER: %+v", res)
	}
	if res := g.command(44, "DUMP_CHARACTER", map[string]any{"file": "../dump.txt"}); res.OK {
		t.Errorf("DUMP_CHARACTER outside the user folder: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(d.User, "dump.txt")); err != nil {
		t.Errorf("no dump: %v", err)
	}
	// The dump draws the sheet where nobody sees it.
	g.key(proto.Ctrl('f'))
	g.waitText("Looks like")
	if g.screen().Contains("abcdefgimnop@") || g.screen().Contains("Max Exp") {
		t.Errorf("the dump drew on the screen:\n%s", g.screen())
	}

	// Messages: the newest is last, with its kind and color, and the
	// classic history shows it too.
	msgs := g.answerOf(50, "MESSAGES", nil).Messages
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	kinds := 0
	for _, m := range msgs {
		if m.Kind != "" {
			kinds++
		}
	}
	if kinds != len(msgs) {
		t.Errorf("%d of %d messages have a kind", kinds, len(msgs))
	}
	last := msgs[len(msgs)-1]
	g.key(proto.Ctrl('p'))
	r = g.heldScreen("messages")
	g.send(proto.NewReply(r.ID, map[string]any{"classic": true}))
	g.waitText("Message recall")
	if !g.screen().Contains(last.Text) {
		t.Errorf("classic messages lack %q", last.Text)
	}
	g.key(proto.KeyEscape)
	g.waitEvent("WAIT")

	// Help: the index links to the command list, whose heading comes in
	// color, and the classic help shows the same lines.
	idx := g.answerOf(60, "HELP", nil)
	if idx.File != "index.txt" || len(idx.Links) != 2 || idx.Links[0].Key != "a" || idx.Links[0].File != "commands.txt" {
		t.Errorf("help index %+v", idx.Links)
	}
	for _, line := range styledLines(idx.Page) {
		if strings.HasPrefix(line, ".. ") {
			t.Errorf("a directive in the help: %q", line)
		}
	}
	cmds := g.answerOf(61, "HELP", map[string]any{"file": "commands.txt"})
	if len(cmds.Page) < 2 || styledLines(cmds.Page)[1] != "Original Keyset Command Summary" || cmds.Page[1][0].Color == 1 || strings.HasPrefix(styledLines(cmds.Page)[2], "===") {
		t.Errorf("commands.txt starts %+v", cmds.Page[:min(len(cmds.Page), 3)])
	}
	if bad := g.answerOf(62, "HELP", map[string]any{"file": "../../README.md"}); bad.Page != nil {
		t.Errorf("help outside the help folder: %+v", bad.Page)
	}
	g.key('?')
	r = g.heldScreen("help")
	g.send(proto.NewReply(r.ID, map[string]any{"classic": true}))
	g.waitText("Please choose one of the following help files")
	g.key('a')
	g.waitText("Original Keyset Command Summary")
	for _, line := range styledLines(cmds.Page)[3:8] {
		if !g.screen().Contains(strings.TrimRight(line, " ")) {
			t.Errorf("classic help lacks %q", line)
		}
	}
	g.key(proto.KeyEscape)
	g.key(proto.KeyEscape)
	g.waitEvent("WAIT")

	// The knowledge screen opens by its key, and the equipment
	// comparison goes to the classic screen and back.
	g.key('~')
	r = g.heldScreen("knowledge")
	g.send(proto.NewReply(r.ID, map[string]any{"classic": "equipment"}))
	g.waitText("ESC to exit")
	g.key(proto.KeyEscape)
	r = g.heldScreen("knowledge")
	if r.View != "equipment" {
		t.Errorf("knowledge screen after the comparison %+v", r)
	}
	g.send(proto.NewReply(r.ID, map[string]any{}))
	g.waitEvent("WAIT")
}

// TestSymbolKeyOpensKnowledge opens the knowledge screen on the monsters
// with the key that asks for a symbol in the classic game.
func TestSymbolKeyOpensKnowledge(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:knowledge"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" && r.Name == "knowledge" }
	g.createCharacterAs("Warrior")
	g.key('/')
	g.until("the knowledge screen", func(*grid.Grid) bool { return len(g.held) > 0 })
	if v := g.held[0].View; v != "monsters" {
		t.Errorf("view %q, want monsters", v)
	}
	g.s.Close()
	g.waitExit()
	requireCleanSave(t, d)
}

// TestVisibleLists opens the monster and item lists by their keys, and reads
// a monster in VISIBLE with its letter, where it is, and its line's color.
func TestVisibleLists(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "screen:lists"})
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" && r.Name == "lists" }
	g.createCharacter()
	g.autoMore(1)

	// A townsperson may be in view, so only the monster summoned is checked.
	g.debug('n', [2]string{"Summon which monster", "shrieker mushroom patch"})
	g.waitEvent("WAIT")
	rows := g.answerOf(3, "VISIBLE", map[string]any{"kind": "monsters", "by_exp": true}).Visible
	if len(rows) < 2 || !strings.HasPrefix(rows[0].Heading, "You can see ") {
		t.Fatalf("the rows: %+v", rows)
	}
	var m proto.VisibleRow
	for _, r := range rows {
		if strings.Contains(r.Name, "shrieker mushroom patch") {
			m = r
		}
	}
	if m.Char != "," || m.Tile != nil || m.Where == "" {
		t.Errorf("the monster's row: %+v", m)
	}
	if got := g.answerOf(4, "VISIBLE", map[string]any{"kind": "items"}).Visible; len(got) == 0 {
		t.Error("the item list has no rows")
	}

	g.key('[')
	g.until("the monster list", func(*grid.Grid) bool { return len(g.held) > 0 })
	if v := g.held[0].View; v != "monsters" {
		t.Errorf("view %q, want monsters", v)
	}
	g.s.Close()
	g.waitExit()
	requireCleanSave(t, d)
}

// TestMonsterRecallGlyph describes a monster with its letter in the text in
// text mode, and with tiles gives the tile for the screen to draw instead.
func TestMonsterRecallGlyph(t *testing.T) {
	for _, c := range []struct {
		name string
		caps []string
		tile bool
	}{
		{"text", []string{"events", "waits"}, false},
		{"tiles", []string{"events", "waits", "tiles:2"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := enginetest.NewDirs(t)
			g := startWith(t, dargs(d, "-n"), c.caps)
			g.createCharacter()
			g.autoMore(1)
			a := g.answerOf(2, "RECALL", map[string]any{"monster": 1})
			if a.Title == nil || a.Glyph == nil {
				t.Fatalf("recall: %+v", a)
			}
			letter := strings.Contains(strings.Join(styledLines(a.Description), "\n"), "('")
			if a.Glyph.Tile != nil != c.tile || letter == c.tile {
				t.Errorf("tile %v, letter in the text %v", a.Glyph.Tile, letter)
			}
			g.s.Close()
			g.waitExit()
			requireCleanSave(t, d)
		})
	}
}

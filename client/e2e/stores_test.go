package e2e

import (
	"slices"
	"strings"
	"testing"

	"fancyband/internal/enginetest"
	"fancyband/internal/grid"
	"fancyband/internal/proto"
)

// holdScreen makes the game leave screen requests named name open, and
// returns a function that waits for the nth one.
func (g *game) holdScreen(name string) func(n int) proto.RequestData {
	g.hold = func(r proto.RequestData) bool { return r.Prompt == "screen" && r.Name == name }
	return func(n int) proto.RequestData {
		g.t.Helper()
		g.until(name+" screen", func(*grid.Grid) bool { return len(g.held) >= n })
		return g.held[n-1]
	}
}

// birthInfo asks for the BIRTH answer.
func (g *game) birthInfo(id int, args map[string]any) *proto.BirthInfo {
	g.t.Helper()
	b := g.answerOf(id, "BIRTH", args).Birth
	if b == nil {
		g.t.Fatal("BIRTH has no character")
	}
	return b
}

// storeInfo asks for the STORE answer.
func (g *game) storeInfo(id int) *proto.StoreInfo {
	g.t.Helper()
	s := g.answerOf(id, "STORE", nil).Store
	if s == nil {
		g.t.Fatal("STORE has no store")
	}
	return s
}

// ok sends a command and fails unless it runs.
func (g *game) ok(id int, name string, args map[string]any) {
	g.t.Helper()
	if r := g.command(id, name, args); !r.OK {
		g.t.Fatalf("%s: %s", name, r.Error)
	}
}

func choiceIndex(t *testing.T, choices []proto.BirthChoice, name string) int {
	t.Helper()
	for i, c := range choices {
		if c.Name == name {
			return i
		}
	}
	t.Fatalf("no %s in %+v", name, choices)
	return -1
}

// TestBirthScreen makes a character through the native birth screen: the
// engine hands it over in place of the C menus and runs the birth
// commands it sends while it stays open.
func TestBirthScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:birth"})
	birth := g.holdScreen("birth")
	g.waitText("[Press any key to continue]")
	g.key('x')
	birth(1)

	b := g.birthInfo(1, nil)
	if len(b.Races) < 10 || len(b.Classes) < 9 || len(b.Stats) != 5 {
		t.Fatalf("birth choices: %d races, %d classes, %d stats", len(b.Races), len(b.Classes), len(b.Stats))
	}
	if b.Quickstart {
		t.Error("a new savefile offers a quick start")
	}
	if help := plain(b.Races[choiceIndex(t, b.Races, "Dwarf")].Help); !strings.Contains(help, "Infravision") {
		t.Errorf("the dwarf's help: %q", help)
	}
	dwarf, mage := choiceIndex(t, b.Races, "Dwarf"), choiceIndex(t, b.Classes, "Mage")

	// Only birth commands go while the screen is open.
	if r := g.command(2, "WALK", map[string]any{"direction": 4}); r.OK || !strings.Contains(r.Error, "not allowed") {
		t.Errorf("WALK at birth: %+v", r)
	}
	g.ok(3, "CHOOSE_RACE", map[string]any{"choice": dwarf})
	g.ok(4, "CHOOSE_CLASS", map[string]any{"choice": mage})
	b = g.birthInfo(5, nil)
	if b.Race != dwarf || b.Class != mage || b.Rolled {
		t.Fatalf("after choosing: race %d class %d rolled %v", b.Race, b.Class, b.Rolled)
	}
	if !strings.Contains(plain(b.Classes[mage].Help), "Learns arcane magic") {
		t.Errorf("the mage's help: %q", plain(b.Classes[mage].Help))
	}
	intel := b.Stats[1]
	if intel.Name != "Int" || intel.Spent == 0 || intel.Race != -3 {
		t.Errorf("a dwarf mage's intelligence: %+v", intel)
	}

	// Selling back a point of intelligence gives its points back.
	points := b.Points
	g.ok(6, "SELL_STAT", map[string]any{"choice": 1})
	b = g.birthInfo(7, nil)
	if b.Points <= points || b.Stats[1].Self != intel.Self-1 {
		t.Errorf("after selling intelligence: %d points (was %d), %+v", b.Points, points, b.Stats[1])
	}
	g.ok(8, "ROLL_STATS", nil)
	if b = g.birthInfo(9, nil); !b.Rolled || b.Stats[0].Cost != nil {
		t.Errorf("after rolling: rolled %v, %+v", b.Rolled, b.Stats[0])
	}
	g.ok(10, "RESET_STATS", map[string]any{"choice": 1})
	if b = g.birthInfo(11, nil); b.Rolled {
		t.Error("point-based stats are still rolled")
	}

	g.ok(12, "NAME_CHOICE", map[string]any{"name": "Tester"})
	if b = g.birthInfo(13, map[string]any{"name": "Tester"}); b.Name != "Tester" || b.NameUsed {
		t.Errorf("name %q, used %v", b.Name, b.NameUsed)
	}
	g.ok(14, "HISTORY_CHOICE", map[string]any{"history": "Born in a test."})
	if b = g.birthInfo(15, nil); b.History != "Born in a test." {
		t.Errorf("history %q", b.History)
	}

	// Closing the screen without a character asks again.
	g.send(proto.NewReply(birth(1).ID, map[string]any{}))
	birth(2)

	g.ok(16, "ACCEPT_CHARACTER", nil)
	g.waitFor("the town", inTown)
	if !g.screen().Contains("Dwarf") || !g.screen().Contains("Mage") {
		t.Errorf("the sidebar does not show the new character:\n%s", g.screen())
	}
	inv := g.inventory(17)
	packItem(t, inv, "First Spells")
	if g.answerOf(18, "BIRTH", nil).Birth != nil {
		t.Error("BIRTH answers after character creation")
	}
}

// TestBirthScreenClassic answers the birth screen with the classic menus.
func TestBirthScreenClassic(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "screen:birth"})
	g.answer = func(r proto.RequestData) map[string]any {
		if r.Prompt == "screen" && r.Name == "birth" {
			return map[string]any{"classic": true}
		}
		return nil
	}
	g.createCharacter()
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// runeAt returns the character on the screen at x, y, or 0 outside it.
func runeAt(gr *grid.Grid, x, y int) rune {
	if y < 0 || y >= gr.Height || x < 0 {
		return 0
	}
	line := []rune(gr.Line(y))
	if x >= len(line) {
		return 0
	}
	return line[x]
}

// walkIntoStore walks into the shops in turn until the one named want
// hands its screen over, closing the others' screens. It returns the
// store's screen request.
func (g *game) walkIntoStore(want string, nextID *int) proto.RequestData {
	g.t.Helper()
	store := g.holdScreen("store")
	m := g.snap.Map
	var shops [][2]int
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			if m.At(x, y) == 's' {
				shops = append(shops, [2]int{x, y})
			}
		}
	}
	// The General Store's entrance shows as 1 on the screen; try it first.
	if want == "General Store" {
		if hx, hy := heroOnScreen(g.screen()); hx >= 0 && g.snap.Player != nil {
			dx, dy := hx-g.snap.Player.X, hy-g.snap.Player.Y
			slices.SortStableFunc(shops, func(a, b [2]int) int {
				one := func(s [2]int) bool { return runeAt(g.screen(), s[0]+dx, s[1]+dy) == '1' }
				switch {
				case one(a) && !one(b):
					return -1
				case one(b) && !one(a):
					return 1
				}
				return 0
			})
		}
	}
	// The shops entered that are not the one wanted. A walk to one shop
	// may pass through another's door.
	other := map[[2]int]bool{}
	for _, shop := range shops {
		// stuck counts the tries in a row that left the character where it
		// was.
		stuck := 0
		for try := 0; try < 40 && !other[shop]; try++ {
			from, events := len(g.held), len(g.events)
			var x, y int
			if p := g.snap.Player; p != nil {
				x, y = p.X, p.Y
			}
			waited := func(*grid.Grid) bool {
				return len(g.held) > from || slices.ContainsFunc(g.events[events:], func(e proto.EventData) bool {
					return e.Event == "WAIT" && e.Context == "game"
				})
			}
			// Townspeople in the way can stop a walk before its first step.
			// Then step toward the shop, which attacks one in the way, or
			// let a turn pass so they move; on a shop's door that enters
			// the shop.
			cmd, args := "PATHFIND", map[string]any{"point": map[string]any{"x": shop[0], "y": shop[1]}}
			switch stuck % 3 {
			case 1:
				cmd, args = "WALK", map[string]any{"direction": dirs[[2]int{sign(shop[0] - x), sign(shop[1] - y)}]}
			case 2:
				cmd, args = "HOLD", nil
			}
			*nextID++
			if r := g.command(*nextID, cmd, args); !r.OK {
				// The engine is busy; try again once it waits.
				events = len(g.events)
			}
			g.until("a wait or a store", waited)
			if len(g.held) > from {
				req := store(len(g.held))
				at := g.lastEvent("PLAYERMOVED")
				*nextID++
				if s := g.storeInfo(*nextID); s.Name == want {
					return req
				}
				other[[2]int{at.X, at.Y}] = true
				g.send(proto.NewReply(req.ID, map[string]any{}))
				stuck = 0
				continue
			}
			p := g.snap.Player
			if p != nil && p.X == shop[0] && p.Y == shop[1] {
				break
			}
			if p != nil && p.X == x && p.Y == y {
				stuck++
			} else {
				stuck = 0
			}
		}
	}
	g.t.Fatalf("could not reach the %s", want)
	return proto.RequestData{}
}

// TestStoreScreen buys and sells in the General Store through the native
// store screen, which runs the store's commands while it stays open.
func TestStoreScreen(t *testing.T) {
	d := enginetest.NewDirs(t)
	g := startWith(t, dargs(d, "-n"), []string{"events", "waits", "snapshots", "screen:store", "prompt:check"})
	g.createCharacter()
	g.autoMore(1)
	// Townspeople can kill a new character on the walk; with this on,
	// the game asks before death, and the test's answer is no.
	if r := g.command(2, "SET_OPTION", map[string]any{"name": "cheat_live", "value": true}); !r.OK {
		t.Fatalf("cheat_live: %+v", r)
	}
	id := 2
	req := g.walkIntoStore("General Store", &id)

	id++
	s := g.storeInfo(id)
	if s.Home || s.Owner == "" || s.Purse <= 0 || s.Gold <= 0 {
		t.Errorf("store: %+v", s)
	}
	var oil *proto.StoreItem
	for i := range s.Stock {
		if strings.Contains(s.Stock[i].Name, "Flask") {
			oil = &s.Stock[i]
		}
	}
	if oil == nil {
		t.Fatalf("no oil in the General Store: %+v", s.Stock)
	}
	if oil.Max < 2 || len(oil.Prices) != oil.Max || oil.Prices[0] != oil.Price || oil.Prices[1] != 2*oil.Price {
		t.Fatalf("oil: max %d, price %d, prices %v", oil.Max, oil.Price, oil.Prices)
	}
	have := oil.Have

	// Game commands wait until the store screen closes.
	id++
	if r := g.command(id, "WALK", map[string]any{"direction": 4}); r.OK {
		t.Error("WALK went ahead in the store")
	}

	id++
	g.ok(id, "BUY", map[string]any{"item": oil.Ref.Map(), "quantity": 2})
	id++
	after := g.storeInfo(id)
	if after.Gold != s.Gold-oil.Prices[1] {
		t.Errorf("gold %d after buying for %d from %d", after.Gold, oil.Prices[1], s.Gold)
	}
	var bought *proto.StoreItem
	for i := range after.Gear {
		if strings.Contains(after.Gear[i].Name, "Flask") {
			bought = &after.Gear[i]
		}
	}
	if bought == nil || bought.Number != have+2 {
		t.Fatalf("the pack after buying two flasks: %+v", after.Gear)
	}
	if !slices.ContainsFunc(g.events, func(e proto.EventData) bool {
		return e.Event == "MESSAGE" && strings.Contains(e.Text, "You bought")
	}) {
		t.Error("no message for the purchase")
	}

	// The store buys the flask back.
	if bought.Max < 1 || len(bought.Prices) == 0 {
		t.Fatalf("the store will not buy oil: %+v", bought)
	}
	id++
	g.ok(id, "SELL", map[string]any{"item": bought.Ref.Map(), "quantity": 1})
	id++
	if sold := g.storeInfo(id); sold.Gold != after.Gold+bought.Prices[0] {
		t.Errorf("gold %d after selling for %d from %d", sold.Gold, bought.Prices[0], after.Gold)
	}

	// Something the store will not buy says why.
	for _, it := range after.Gear {
		if it.Max == 0 && it.Why == "" {
			t.Errorf("%s cannot be sold, with no reason", it.Name)
		}
	}

	// Closing the screen leaves the store.
	g.send(proto.NewReply(req.ID, map[string]any{}))
	g.afterWaits("leaving the store", func() bool { return g.lastEvent("WAIT").Context == "game" })
	id++
	if g.answerOf(id, "STORE", nil).Store == nil {
		// The character still stands on the entrance.
		t.Error("no store at the entrance after leaving")
	}
}

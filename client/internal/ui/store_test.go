package ui

import (
	"fmt"
	"testing"

	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/proto"
)

// fakeStore is a store with n objects for sale and n carried, each
// costing 4 gold, of which up to 3 can be bought and the store pays 2
// for. The last carried one cannot be sold.
func fakeStore(n int) proto.StoreInfo {
	info := proto.StoreInfo{Name: "General Store", Owner: "Bilbo", Purse: 5000, Gold: 100}
	for i := 0; i < n; i++ {
		i := i
		stock := proto.StoreItem{Max: 3, Price: 4, Prices: []int{4, 8, 12}}
		stock.Ref.Store, stock.Name, stock.Number, stock.Char = &i, fmt.Sprintf("%d Flasks of Oil %02d", 5, i), 5, "!"
		info.Stock = append(info.Stock, stock)
		gear := proto.StoreItem{Max: 1, Prices: []int{2}}
		gear.Ref.Inven, gear.Name, gear.Number, gear.Char = &i, fmt.Sprintf("a Torch %02d", i), 1, "~"
		info.Gear = append(info.Gear, gear)
	}
	info.Gear[n-1].Max, info.Gear[n-1].Prices, info.Gear[n-1].Why = 0, nil, "I do not wish to purchase this item."
	return info
}

// answer runs the command the screen sent last, as if the engine ran it,
// with the given answer to a query.
func answer(t *testing.T, m *Model, ok bool, a proto.EventData) {
	t.Helper()
	id := m.nextID
	done, waiting := m.pending[id]
	if !waiting {
		t.Fatalf("no command %d waits for its result", id)
	}
	delete(m.pending, id)
	a.ID = id
	m.answers[id] = a
	done(proto.EventData{Event: "COMMAND", ID: id, OK: ok})
}

func keyOf(t driver.KeyType) driver.KeyEvent { return driver.KeyEvent{Type: t} }

func runeKey(r rune) driver.KeyEvent { return driver.KeyEvent{Type: driver.KeyRune, Ch: r} }

// TestStoreScreenScrolls scrolls both of the store's lists with the
// wheel, the arrows, and the page keys, and moves between them.
func TestStoreScreenScrolls(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	s := &storeScreen{m: m, id: 7}
	s.fill(fakeStore(60))
	m.store = s
	m.push(s)
	term := testkit.NewTerminal(100, 30)
	env := &drawEnv{pal: newPalette(nil)}
	var b Box
	draw := func() {
		b = s.box(Box{0, 0, 100, 30})
		term.Draw(func(f *terminal.Frame) { s.draw(f, b, env) })
	}
	draw()

	// The wheel scrolls the list under the pointer.
	wheel(t, term, b.X+5, b.Y+6, 1)
	if s.stock.state.Offset == 0 {
		t.Error("the wheel did not scroll the stock")
	}
	wheel(t, term, b.X+b.W-10, b.Y+6, 1)
	if s.gear.state.Offset == 0 {
		t.Error("the wheel did not scroll the gear")
	}

	// The arrows and page keys move the list with the keys.
	draw()
	s.stock.state.Select(0)
	s.key(keyOf(driver.KeyArrowDown))
	s.key(keyOf(driver.KeyPageDown))
	if got := s.stock.selected(); got <= 1 {
		t.Errorf("after Down and Page Down the stock selection is %d", got)
	}
	s.key(keyOf(driver.KeyTab))
	if s.focus != 1 {
		t.Fatal("Tab did not move to the gear")
	}
	s.gear.state.Select(0)
	s.key(keyOf(driver.KeyArrowDown))
	if s.gear.selected() != 1 {
		t.Errorf("Down in the gear selects %d", s.gear.selected())
	}
}

// TestStoreScreenBuySell buys two of an object and sells one, checking
// the dialogs and the commands, and that the screen shows the store
// again after each.
func TestStoreScreenBuySell(t *testing.T) {
	m := New(Config{Width: 100, Height: 30})
	s := &storeScreen{m: m, id: 7}
	s.fill(fakeStore(3))
	m.store = s
	m.push(s)

	// p asks how many, then whether to pay.
	s.key(runeKey('p'))
	q, ok := m.stack[len(m.stack)-1].(*quantityDialog)
	if !ok || q.max != 3 {
		t.Fatalf("no quantity dialog for up to 3: %T", m.stack[len(m.stack)-1])
	}
	q.key(runeKey('2'))
	q.key(keyOf(driver.KeyEnter))
	ask, ok := m.stack[len(m.stack)-1].(*listScreen)
	if !ok || ask.id != checkID || len(ask.intro) == 0 {
		t.Fatalf("no price check: %T", m.stack[len(m.stack)-1])
	}
	if got := ask.intro[0][0].Text; got != "Buy 2 Flasks of Oil 00 for 8 gold?" {
		t.Errorf("price check %q", got)
	}
	ask.key(runeKey('y'))
	if !s.busy || len(m.stack) != 1 {
		t.Fatalf("buying: busy %v, %d screens open", s.busy, len(m.stack))
	}
	s.key(runeKey('p'))
	if len(m.stack) != 1 {
		t.Error("a key went ahead while the purchase ran")
	}
	answer(t, m, true, proto.EventData{})
	after := fakeStore(3)
	after.Gold = 92
	answer(t, m, true, proto.EventData{Event: "STORE", Store: &after})
	if s.busy || s.info.Gold != 92 {
		t.Errorf("after buying: busy %v, gold %d", s.busy, s.info.Gold)
	}

	// d sells the selected carried object; one alone needs no count.
	s.key(runeKey('d'))
	ask = m.stack[len(m.stack)-1].(*listScreen)
	if got := ask.intro[0][0].Text; got != "Sell a Torch 00 for 2 gold?" {
		t.Errorf("sale check %q", got)
	}
	ask.key(runeKey('n'))
	if s.busy || len(m.stack) != 1 {
		t.Error("No still sold the torch")
	}

	// Something the store will not buy says why.
	s.gear.selectItem(2)
	s.key(runeKey('d'))
	if len(m.stack) != 1 || len(s.messages) == 0 || s.messages[len(s.messages)-1] != "I do not wish to purchase this item." {
		t.Errorf("selling what the store will not buy: %d screens, messages %q", len(m.stack), s.messages)
	}

	// Escape leaves the store.
	s.key(keyOf(driver.KeyEsc))
	if len(m.stack) != 0 || m.store != nil {
		t.Error("Escape did not leave the store")
	}
}

func TestCountOf(t *testing.T) {
	for _, c := range []struct {
		name string
		n    int
		want string
	}{
		{"5 Flasks of Oil", 2, "2 Flasks of Oil"},
		{"5 Flasks of Oil", 1, "one of the 5 Flasks of Oil"},
		{"5 Flasks of Oil", 5, "5 Flasks of Oil"},
		{"a Potion of Cure Light Wounds", 1, "a Potion of Cure Light Wounds"},
	} {
		if got := countOf(c.name, c.n); got != c.want {
			t.Errorf("countOf(%q, %d) = %q, want %q", c.name, c.n, got, c.want)
		}
	}
}

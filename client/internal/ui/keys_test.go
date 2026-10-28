package ui

import (
	"testing"

	"github.com/thebanri/limoni/core/driver"

	"fancyband/internal/proto"
)

func TestTranslateKey(t *testing.T) {
	cases := []struct {
		name string
		in   driver.KeyEvent
		want proto.Key
	}{
		{"letter", driver.KeyEvent{Type: driver.KeyRune, Ch: 'g'}, proto.Key{Code: 'g'}},
		{"shifted letter", driver.KeyEvent{Type: driver.KeyRune, Ch: 'G', Shift: true}, proto.Key{Code: 'G'}},
		{"digit", driver.KeyEvent{Type: driver.KeyRune, Ch: '8'}, proto.Key{Code: '8'}},
		{"ctrl+x", driver.KeyEvent{Type: driver.KeyRune, Ch: 'x', Ctrl: true}, proto.Key{Code: 0x18}},
		{"alt+a", driver.KeyEvent{Type: driver.KeyRune, Ch: 'a', Alt: true}, proto.Key{Code: 'a', Mods: proto.ModAlt}},
		{"escape", driver.KeyEvent{Type: driver.KeyEsc}, proto.Key{Code: proto.KeyEscape}},
		{"enter", driver.KeyEvent{Type: driver.KeyEnter}, proto.Key{Code: proto.KeyEnter}},
		{"ctrl+j is enter", driver.KeyEvent{Type: driver.KeyEnter, Ctrl: true}, proto.Key{Code: proto.KeyEnter}},
		{"space", driver.KeyEvent{Type: driver.KeySpace}, proto.Key{Code: ' '}},
		{"backspace", driver.KeyEvent{Type: driver.KeyBackspace}, proto.Key{Code: proto.KeyBackspace}},
		{"shift+tab", driver.KeyEvent{Type: driver.KeyTab, Shift: true}, proto.Key{Code: proto.KeyTab, Mods: proto.ModShift}},
		{"up", driver.KeyEvent{Type: driver.KeyArrowUp}, proto.Key{Code: proto.KeyArrowUp}},
		{"shift+left", driver.KeyEvent{Type: driver.KeyArrowLeft, Shift: true}, proto.Key{Code: proto.KeyArrowLeft, Mods: proto.ModShift}},
		{"ctrl+right", driver.KeyEvent{Type: driver.KeyArrowRight, Ctrl: true}, proto.Key{Code: proto.KeyArrowRight, Mods: proto.ModControl}},
		{"page down", driver.KeyEvent{Type: driver.KeyPageDown}, proto.Key{Code: proto.KeyPageDown}},
		{"f1", driver.KeyEvent{Type: driver.KeyF1}, proto.Key{Code: proto.KeyF1}},
		{"f12", driver.KeyEvent{Type: driver.KeyF12}, proto.Key{Code: proto.KeyF1 + 11}},
	}
	for _, c := range cases {
		got, ok := TranslateKey(c.in)
		if !ok || got != c.want {
			t.Errorf("%s: got %+v %v, want %+v", c.name, got, ok, c.want)
		}
	}
}

func TestQuitKey(t *testing.T) {
	if !isQuitKey(driver.KeyEvent{Type: driver.KeyRune, Ch: 'c', Ctrl: true}) {
		t.Error("Ctrl+C is not the quit key")
	}
	if isQuitKey(driver.KeyEvent{Type: driver.KeyRune, Ch: 'x', Ctrl: true}) {
		t.Error("Ctrl+X is the quit key; it belongs to the game")
	}
}

package ui

import (
	"github.com/thebanri/limoni/core/driver"

	"fancyband/internal/proto"
)

// specialKeys maps Limoni's special keys to Angband key codes.
var specialKeys = map[driver.KeyType]uint32{
	driver.KeyEsc:        proto.KeyEscape,
	driver.KeyEnter:      proto.KeyEnter,
	driver.KeyBackspace:  proto.KeyBackspace,
	driver.KeyTab:        proto.KeyTab,
	driver.KeySpace:      ' ',
	driver.KeyArrowUp:    proto.KeyArrowUp,
	driver.KeyArrowDown:  proto.KeyArrowDown,
	driver.KeyArrowLeft:  proto.KeyArrowLeft,
	driver.KeyArrowRight: proto.KeyArrowRight,
	driver.KeyHome:       proto.KeyHome,
	driver.KeyEnd:        proto.KeyEnd,
	driver.KeyPageUp:     proto.KeyPageUp,
	driver.KeyPageDown:   proto.KeyPageDown,
	driver.KeyInsert:     proto.KeyInsert,
	driver.KeyDelete:     proto.KeyDelete,
	driver.KeyF1:         proto.KeyF1,
	driver.KeyF2:         proto.KeyF1 + 1,
	driver.KeyF3:         proto.KeyF1 + 2,
	driver.KeyF4:         proto.KeyF1 + 3,
	driver.KeyF5:         proto.KeyF1 + 4,
	driver.KeyF6:         proto.KeyF1 + 5,
	driver.KeyF7:         proto.KeyF1 + 6,
	driver.KeyF8:         proto.KeyF1 + 7,
	driver.KeyF9:         proto.KeyF1 + 8,
	driver.KeyF10:        proto.KeyF1 + 9,
	driver.KeyF11:        proto.KeyF1 + 10,
	driver.KeyF12:        proto.KeyF1 + 11,
}

// noModifiers lists keys sent without Ctrl or Shift.
var noModifiers = map[driver.KeyType]bool{
	driver.KeyEsc: true, driver.KeyEnter: true, driver.KeyBackspace: true, driver.KeySpace: true,
}

// isQuitKey reports whether ev is Ctrl+C, which the client turns into
// QUIT so the game saves. The game's own save-and-quit key is Ctrl+X.
func isQuitKey(ev driver.KeyEvent) bool {
	return ev.Type == driver.KeyRune && ev.Ctrl && (ev.Ch == 'c' || ev.Ch == 'C')
}

// TranslateKey turns a Limoni key event into an Angband key press. It
// returns false for keys Angband has no code for.
//
// Limoni reports a number-pad digit as a plain digit, so the keypad bit
// is never set. Angband's default keymaps move on plain digits too, so
// walking works; running with Shift and the number pad does not.
func TranslateKey(ev driver.KeyEvent) (proto.Key, bool) {
	var k proto.Key
	if ev.Alt {
		k.Mods |= proto.ModAlt
	}

	if ev.Type == driver.KeyRune {
		switch {
		case ev.Ctrl && ev.Ch < 0x80:
			k.Code = proto.Ctrl(byte(ev.Ch))
		case ev.Ch == 0:
			return proto.Key{}, false
		default:
			// Shift is already part of the character.
			k.Code = uint32(ev.Ch)
		}
		return k, true
	}

	code, ok := specialKeys[ev.Type]
	if !ok {
		return proto.Key{}, false
	}
	k.Code = code
	// Terminals report Ctrl+J as Enter with Ctrl and Ctrl+H as Backspace;
	// Angband wants those keys plain.
	if noModifiers[ev.Type] {
		return k, true
	}
	if ev.Ctrl {
		k.Mods |= proto.ModControl
	}
	if ev.Shift {
		k.Mods |= proto.ModShift
	}
	return k, true
}

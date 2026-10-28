package proto

// Angband key codes from ui-event.h. Printable keys use their Unicode code
// point; these are the special keys.
const (
	KeyArrowDown  = 0x80
	KeyArrowLeft  = 0x81
	KeyArrowRight = 0x82
	KeyArrowUp    = 0x83
	KeyF1         = 0x84 // F2 to F15 follow in order, up to 0x92
	KeyHelp       = 0x93
	KeyHome       = 0x94
	KeyPageUp     = 0x95
	KeyEnd        = 0x96
	KeyPageDown   = 0x97
	KeyInsert     = 0x98
	KeyPause      = 0x99
	KeyBreak      = 0x9A
	KeyBegin      = 0x9B
	KeyEnter      = 0x9C
	KeyTab        = 0x9D
	KeyDelete     = 0x9E
	KeyBackspace  = 0x9F
	KeyEscape     = 0xE000
)

// Ctrl returns the key code for a control letter, such as Ctrl('X') for
// save and quit. It matches KTRL() in ui-event.h.
func Ctrl(c byte) uint32 {
	return uint32(c & 0x1F)
}

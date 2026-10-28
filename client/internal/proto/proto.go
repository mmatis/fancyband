// Package proto encodes and decodes the frames that fancyband and its
// clients exchange. It mirrors engine/src/net-proto.c; the two share the
// test vectors in proto/vectors.txt. The protocol is written up in
// proto/README.md.
package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Version is the protocol version this package speaks.
const Version = 1

// MaxFrame is the longest frame either side accepts, counting the type byte.
const MaxFrame = 1024 * 1024

// Type identifies a message.
type Type uint8

// Message types. HELLO goes both ways; the others go one way only.
const (
	TypeHello Type = 0x01

	// Server to client.
	TypeTermOpen Type = 0x10
	TypePalette  Type = 0x11
	TypeText     Type = 0x12
	TypeWipe     Type = 0x13
	TypeClear    Type = 0x14
	TypeCursor   Type = 0x15
	TypeFresh    Type = 0x16
	TypeBell     Type = 0x17
	TypeSound    Type = 0x18
	TypeEvent    Type = 0x20
	TypeSnapshot Type = 0x21
	TypeRequest  Type = 0x22
	TypeBye      Type = 0x3F

	// Client to server.
	TypeKey     Type = 0x40
	TypeMouse   Type = 0x41
	TypeResize  Type = 0x42
	TypeCommand Type = 0x50
	TypeReply   Type = 0x51
	TypeQuit    Type = 0x7F
)

var typeNames = map[Type]string{
	TypeHello: "HELLO", TypeTermOpen: "TERM_OPEN", TypePalette: "PALETTE",
	TypeText: "TEXT", TypeWipe: "WIPE", TypeClear: "CLEAR",
	TypeCursor: "CURSOR", TypeFresh: "FRESH", TypeBell: "BELL",
	TypeSound: "SOUND", TypeEvent: "EVENT", TypeSnapshot: "SNAPSHOT",
	TypeRequest: "REQUEST", TypeBye: "BYE", TypeKey: "KEY",
	TypeMouse: "MOUSE", TypeResize: "RESIZE", TypeCommand: "COMMAND",
	TypeReply: "REPLY", TypeQuit: "QUIT",
}

func (t Type) String() string {
	if name, ok := typeNames[t]; ok {
		return name
	}
	return fmt.Sprintf("TYPE_%#02x", uint8(t))
}

// Modifier bits for KEY and MOUSE. They match ui-event.h.
const (
	ModControl = 0x01
	ModShift   = 0x02
	ModAlt     = 0x04
	ModMeta    = 0x08
	ModKeypad  = 0x10
)

// Cursor flag bits.
const (
	CursorVisible = 0x01
	CursorBig     = 0x02
)

// Background modes, packed into the high byte of an attribute.
const (
	BgBlack = 0
	BgSame  = 1
	BgDark  = 2
)

// Attr is a packed Angband attribute: a color, a reverse-video bit and a
// background mode.
type Attr uint16

// Color returns the palette index.
func (a Attr) Color() int { return int(a & 0x7F) }

// Reversed reports whether foreground and background swap.
func (a Attr) Reversed() bool { return a&0x80 != 0 }

// Background returns the background mode, one of BgBlack, BgSame or BgDark.
func (a Attr) Background() int { return int(a >> 8) }

// Frame is one undecoded message.
type Frame struct {
	Type    Type
	Payload []byte
}

// ErrFrameTooLong reports a frame length past MaxFrame or of zero.
var ErrFrameTooLong = errors.New("proto: invalid frame length")

// ReadFrame reads one frame.
func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:4]); err != nil {
		return Frame{}, err
	}
	n := binary.LittleEndian.Uint32(hdr[:4])
	if n < 1 || n > MaxFrame {
		return Frame{}, ErrFrameTooLong
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return Frame{Type: Type(buf[0]), Payload: buf[1:]}, nil
}

// Bytes returns the frame as it goes on the wire.
func (f Frame) Bytes() []byte {
	out := make([]byte, 5, 5+len(f.Payload))
	binary.LittleEndian.PutUint32(out, uint32(len(f.Payload)+1))
	out[4] = byte(f.Type)
	return append(out, f.Payload...)
}

// WriteFrame writes one frame.
func WriteFrame(w io.Writer, f Frame) error {
	_, err := w.Write(f.Bytes())
	return err
}

// Message is a decoded message that can encode itself.
type Message interface {
	Frame() Frame
}

// Decode turns a frame into a message. Unknown types decode to Unknown,
// so a newer server does not break an older client.
func Decode(f Frame) (Message, error) {
	r := &reader{b: f.Payload}
	var m Message
	switch f.Type {
	case TypeHello:
		h := Hello{Version: r.u16(), Name: r.shortStr()}
		for n := int(r.u8()); n > 0 && r.err == nil; n-- {
			h.Terms = append(h.Terms, TermSize{Index: r.u8(), Width: r.u16(), Height: r.u16()})
		}
		for n := int(r.u8()); n > 0 && r.err == nil; n-- {
			h.Caps = append(h.Caps, r.shortStr())
		}
		m = h
	case TypeTermOpen:
		m = TermOpen{Term: r.u8(), Width: r.u16(), Height: r.u16(), Name: r.rest()}
	case TypePalette:
		var p Palette
		for n := int(r.u8()); n > 0 && r.err == nil; n-- {
			p.Colors = append(p.Colors, RGB{r.u8(), r.u8(), r.u8()})
		}
		m = p
	case TypeText:
		m = Text{Term: r.u8(), X: r.u16(), Y: r.u16(), Attr: Attr(r.u16()), Text: r.rest()}
	case TypeWipe:
		m = Wipe{Term: r.u8(), X: r.u16(), Y: r.u16(), N: r.u16()}
	case TypeClear:
		m = Clear{Term: r.u8()}
	case TypeCursor:
		m = Cursor{Term: r.u8(), X: r.u16(), Y: r.u16(), Flags: r.u8()}
	case TypeFresh:
		m = Fresh{Term: r.u8()}
	case TypeBell:
		m = Bell{}
	case TypeBye:
		m = Bye{Reason: r.rest()}
	case TypeKey:
		m = Key{Code: r.u32(), Mods: r.u8()}
	case TypeMouse:
		m = Mouse{Term: r.u8(), X: r.u16(), Y: r.u16(), Button: r.u8(), Mods: r.u8()}
	case TypeResize:
		m = Resize{Term: r.u8(), Width: r.u16(), Height: r.u16()}
	case TypeQuit:
		m = Quit{}
	default:
		return Unknown{Raw: f}, nil
	}
	if r.err != nil {
		return nil, fmt.Errorf("proto: decoding %v: %w", f.Type, r.err)
	}
	return m, nil
}

// TermSize asks for one term in HELLO.
type TermSize struct {
	Index         uint8
	Width, Height uint16
}

// Hello opens the session. The client lists the terms it wants and its
// capabilities; the server lists no terms and answers with TERM_OPEN.
type Hello struct {
	Version uint16
	Name    string
	Terms   []TermSize
	Caps    []string
}

func (m Hello) Frame() Frame {
	var w writer
	w.u16(m.Version)
	w.shortStr(m.Name)
	w.u8(uint8(len(m.Terms)))
	for _, t := range m.Terms {
		w.u8(t.Index)
		w.u16(t.Width)
		w.u16(t.Height)
	}
	w.u8(uint8(len(m.Caps)))
	for _, c := range m.Caps {
		w.shortStr(c)
	}
	return Frame{TypeHello, w.b}
}

// TermOpen announces a term, or its new size after a RESIZE.
type TermOpen struct {
	Term          uint8
	Width, Height uint16
	Name          string
}

func (m TermOpen) Frame() Frame {
	var w writer
	w.u8(m.Term)
	w.u16(m.Width)
	w.u16(m.Height)
	w.b = append(w.b, m.Name...)
	return Frame{TypeTermOpen, w.b}
}

// RGB is one palette color.
type RGB struct{ R, G, B uint8 }

// Palette carries the game's color table.
type Palette struct{ Colors []RGB }

func (m Palette) Frame() Frame {
	var w writer
	w.u8(uint8(len(m.Colors)))
	for _, c := range m.Colors {
		w.u8(c.R)
		w.u8(c.G)
		w.u8(c.B)
	}
	return Frame{TypePalette, w.b}
}

// Text draws a run of characters in one attribute. Each rune fills one
// cell, even a wide one.
type Text struct {
	Term uint8
	X, Y uint16
	Attr Attr
	Text string
}

func (m Text) Frame() Frame {
	var w writer
	w.u8(m.Term)
	w.u16(m.X)
	w.u16(m.Y)
	w.u16(uint16(m.Attr))
	w.b = append(w.b, m.Text...)
	return Frame{TypeText, w.b}
}

// Wipe blanks N cells starting at X, Y.
type Wipe struct {
	Term    uint8
	X, Y, N uint16
}

func (m Wipe) Frame() Frame {
	var w writer
	w.u8(m.Term)
	w.u16(m.X)
	w.u16(m.Y)
	w.u16(m.N)
	return Frame{TypeWipe, w.b}
}

// Clear blanks a whole term.
type Clear struct{ Term uint8 }

func (m Clear) Frame() Frame { return Frame{TypeClear, []byte{m.Term}} }

// Cursor places a term's cursor.
type Cursor struct {
	Term  uint8
	X, Y  uint16
	Flags uint8
}

// Visible reports whether the cursor shows.
func (m Cursor) Visible() bool { return m.Flags&CursorVisible != 0 }

// Big reports whether the cursor covers a double-width tile.
func (m Cursor) Big() bool { return m.Flags&CursorBig != 0 }

func (m Cursor) Frame() Frame {
	var w writer
	w.u8(m.Term)
	w.u16(m.X)
	w.u16(m.Y)
	w.u8(m.Flags)
	return Frame{TypeCursor, w.b}
}

// Fresh ends a frame for a term. Clients repaint only on Fresh.
type Fresh struct{ Term uint8 }

func (m Fresh) Frame() Frame { return Frame{TypeFresh, []byte{m.Term}} }

// Bell asks the client to signal the player.
type Bell struct{}

func (Bell) Frame() Frame { return Frame{TypeBell, nil} }

// Bye is the server's last message.
type Bye struct{ Reason string }

func (m Bye) Frame() Frame { return Frame{TypeBye, []byte(m.Reason)} }

// Key is one key press. Code is an Angband key code from ui-event.h.
type Key struct {
	Code uint32
	Mods uint8
}

func (m Key) Frame() Frame {
	var w writer
	w.u32(m.Code)
	w.u8(m.Mods)
	return Frame{TypeKey, w.b}
}

// Mouse is one mouse click.
type Mouse struct {
	Term   uint8
	X, Y   uint16
	Button uint8
	Mods   uint8
}

func (m Mouse) Frame() Frame {
	var w writer
	w.u8(m.Term)
	w.u16(m.X)
	w.u16(m.Y)
	w.u8(m.Button)
	w.u8(m.Mods)
	return Frame{TypeMouse, w.b}
}

// Resize asks for a new term size.
type Resize struct {
	Term          uint8
	Width, Height uint16
}

func (m Resize) Frame() Frame {
	var w writer
	w.u8(m.Term)
	w.u16(m.Width)
	w.u16(m.Height)
	return Frame{TypeResize, w.b}
}

// Quit asks the server to save and exit.
type Quit struct{}

func (Quit) Frame() Frame { return Frame{TypeQuit, nil} }

// Unknown is a message this package does not decode.
type Unknown struct{ Raw Frame }

func (m Unknown) Frame() Frame { return m.Raw }

var errShort = errors.New("payload too short")

type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return make([]byte, n)
	}
	if len(r.b) < n {
		r.err = errShort
		return make([]byte, n)
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *reader) u8() uint8   { return r.take(1)[0] }
func (r *reader) u16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *reader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }

func (r *reader) shortStr() string {
	n := int(r.u8())
	return string(r.take(n))
}

func (r *reader) rest() string {
	s := string(r.b)
	r.b = nil
	return s
}

type writer struct{ b []byte }

func (w *writer) u8(v uint8)   { w.b = append(w.b, v) }
func (w *writer) u16(v uint16) { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *writer) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }

func (w *writer) shortStr(s string) {
	if len(s) > 255 {
		s = s[:255]
	}
	w.u8(uint8(len(s)))
	w.b = append(w.b, s...)
}

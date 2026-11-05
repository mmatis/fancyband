package proto

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// vectorMessages lists the message each vector in proto/vectors.txt
// stands for. engine/src/tests/net/proto.c builds the same messages.
var vectorMessages = map[string]Message{
	"hello_client": Hello{Version: 1, Name: "fancyband-tui",
		Terms: []TermSize{{0, 80, 24}, {1, 80, 10}}, Caps: []string{"events"}},
	"hello_server":       Hello{Version: 1, Name: "Fancyband 4.2.6"},
	"term_open":          TermOpen{Term: 1, Width: 80, Height: 10, Name: "Term-1"},
	"palette":            Palette{Colors: []RGB{{0, 0, 0}, {255, 128, 1}}},
	"text_ascii":         Text{Term: 0, X: 3, Y: 2, Attr: 0x010F, Text: "@#."},
	"text_utf8":          Text{Term: 2, X: 0, Y: 0, Attr: 0x0281, Text: "é·█\U0001D11E"},
	"text_bad_codepoint": Text{Term: 0, X: 0, Y: 0, Attr: 1, Text: "��"},
	"wipe":               Wipe{Term: 0, X: 10, Y: 23, N: 70},
	"clear":              Clear{Term: 3},
	"cursor":             Cursor{Term: 0, X: 13, Y: 0, Flags: CursorVisible},
	"fresh":              Fresh{Term: 0},
	"bell":               Bell{},
	"bye":                Bye{Reason: "quit"},
	"key_escape":         Key{Code: KeyEscape},
	"key_keypad_8":       Key{Code: '8', Mods: ModKeypad},
	"mouse":              Mouse{Term: 0, X: 40, Y: 12, Button: 1, Mods: ModShift},
	"resize":             Resize{Term: 0, Width: 120, Height: 40},
	"quit":               Quit{},
	"tileset": TileSet{ID: 2, TileW: 16, TileH: 16, CellsW: 2, CellsH: 1,
		Name: "Adam Bolt's tiles", Path: "/lib/tiles/adam-bolt/16x16.png"},
	"pict": Pict{Term: 0, X: 20, Y: 5, Cells: []PictCell{
		{FgRow: 0, FgCol: 6, BgRow: 0, BgCol: 6}, {FgRow: 3, FgCol: 4, BgRow: 0, BgCol: 6}}},
}

func loadVectors(t *testing.T) map[string][]byte {
	t.Helper()
	f, err := os.Open("../../../proto/vectors.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	vectors := map[string][]byte{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, hexBytes, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("bad vector line %q", line)
		}
		b, err := hex.DecodeString(hexBytes)
		if err != nil {
			t.Fatalf("vector %s: %v", name, err)
		}
		vectors[name] = b
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return vectors
}

func TestVectors(t *testing.T) {
	vectors := loadVectors(t)
	if len(vectors) != len(vectorMessages) {
		t.Errorf("vectors.txt has %d vectors, the test knows %d", len(vectors), len(vectorMessages))
	}
	for name, want := range vectors {
		msg, ok := vectorMessages[name]
		if !ok {
			t.Errorf("no message for vector %s", name)
			continue
		}
		if got := msg.Frame().Bytes(); !bytes.Equal(got, want) {
			t.Errorf("%s: encoded\n got %x\nwant %x", name, got, want)
		}

		f, err := ReadFrame(bytes.NewReader(want))
		if err != nil {
			t.Errorf("%s: ReadFrame: %v", name, err)
			continue
		}
		decoded, err := Decode(f)
		if err != nil {
			t.Errorf("%s: Decode: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(decoded, msg) {
			t.Errorf("%s: decoded %#v, want %#v", name, decoded, msg)
		}
	}
}

func TestReadFrameErrors(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, io.EOF},
		{"short header", []byte{1, 0}, io.ErrUnexpectedEOF},
		{"zero length", []byte{0, 0, 0, 0}, ErrFrameTooLong},
		{"too long", []byte{0, 0, 0x20, 0}, ErrFrameTooLong},
		{"short payload", []byte{3, 0, 0, 0, 0x16}, io.ErrUnexpectedEOF},
	}
	for _, c := range cases {
		if _, err := ReadFrame(bytes.NewReader(c.in)); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

func TestDecodeShortPayload(t *testing.T) {
	if _, err := Decode(Frame{Type: TypeKey, Payload: []byte{1, 2}}); err == nil {
		t.Error("decoding a short KEY succeeded")
	}
}

func TestDecodeUnknownType(t *testing.T) {
	f := Frame{Type: 0x33, Payload: []byte{9}}
	m, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := m.(Unknown); !ok || !reflect.DeepEqual(u.Raw, f) {
		t.Errorf("got %#v, want Unknown carrying the frame", m)
	}
}

func TestAttr(t *testing.T) {
	a := Attr(0x0281)
	if a.Color() != 1 || !a.Reversed() || a.Background() != BgDark {
		t.Errorf("Attr(%#x) = color %d, reversed %v, background %d", uint16(a), a.Color(), a.Reversed(), a.Background())
	}
}

func TestCtrl(t *testing.T) {
	if Ctrl('X') != 0x18 || Ctrl('x') != 0x18 {
		t.Errorf("Ctrl('X') = %#x, want 0x18", Ctrl('X'))
	}
}

// TestPictFlip carries a cell's Flip in its FgCol's high bit, leaving the
// column whole.
func TestPictFlip(t *testing.T) {
	want := PictCell{FgRow: 3, FgCol: 127, BgRow: 1, BgCol: 2, Flip: true}
	f := Pict{Cells: []PictCell{want}}.Frame()
	m, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.(Pict).Cells[0]; got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

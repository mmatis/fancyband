package backdrop

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/thebanri/limoni/core/cell"
)

func parse(t *testing.T, text string) *Art {
	t.Helper()
	a, err := ParseArt(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// screen renders a at t into a w×h grid and returns it as lines of text.
func screen(a *Art, w, h int, at time.Duration) []string {
	b := newBuf(w, h)
	a.Render(b, at)
	lines := make([]string, h)
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			sb.WriteString(cell.ClusterText(b.Content[y*w+x].Content))
		}
		lines[y] = sb.String()
	}
	return lines
}

func TestArtIsPlacedWhereAlignSays(t *testing.T) {
	art := "ab\ncd\n"
	cases := []struct {
		align string
		want  []string
	}{
		{"center", []string{"      ", "  ab  ", "  cd  ", "      "}},
		{"top-left", []string{"ab    ", "cd    ", "      ", "      "}},
		{"bottom-right", []string{"      ", "      ", "    ab", "    cd"}},
	}
	for _, tc := range cases {
		got := screen(parse(t, "@align "+tc.align+"\n"+art), 6, 4, 0)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s:\n%q\nwant\n%q", tc.align, got, tc.want)
		}
	}
	got := screen(parse(t, "@align top-left\n@offset 2 1\n"+art), 6, 4, 0)
	if got[1] != "  ab  " {
		t.Errorf("@offset: %q", got)
	}
}

func TestArtFramesFlipAtItsFrameRate(t *testing.T) {
	a := parse(t, "@fps 2\n@align top-left\nA\n@frame\nB\n@frame\nC\n")
	if a.Interval() != 500*time.Millisecond {
		t.Fatalf("interval %v", a.Interval())
	}
	for i, want := range []string{"A", "B", "C", "A"} {
		if got := screen(a, 1, 1, time.Duration(i)*500*time.Millisecond)[0]; got != want {
			t.Errorf("frame %d = %q, want %q", i, got, want)
		}
	}
}

func TestArtScrollsAndWraps(t *testing.T) {
	a := parse(t, "@align top-left\n@scroll 2 0\nX\n")
	if a.Interval() != 500*time.Millisecond {
		t.Fatalf("interval %v", a.Interval())
	}
	// Two cells a second across a 4-wide screen; the art is 1 wide, so it
	// wraps every 5 cells.
	for sec, want := range []string{"X   ", "  X ", "    ", " X  "} {
		if got := screen(a, 4, 1, time.Duration(sec)*time.Second)[0]; got != want {
			t.Errorf("at %ds: %q, want %q", sec, got, want)
		}
	}
}

func TestArtTilesWithGaps(t *testing.T) {
	got := screen(parse(t, "@tile 1 1\n@align top-left\nab\n"), 7, 3, 0)
	want := []string{"ab ab a", "       ", "ab ab a"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q\nwant\n%q", got, want)
	}
}

// Many ASCII pictures are drawn with @; a line is only a directive when it
// is exactly one of the known ones.
func TestArtLinesThatLookLikeDirectivesAreArt(t *testing.T) {
	a := parse(t, "@@@@\n@frames are fun\n @fps 3\n")
	if len(a.frames) != 1 || len(a.frames[0].rows) != 3 || a.fps != 4 {
		t.Fatalf("frames=%d rows=%d fps=%v", len(a.frames), len(a.frames[0].rows), a.fps)
	}
}

func TestArtColours(t *testing.T) {
	a := parse(t, "@color #ff0000 #0000ff\n@align top-left\nx\nx\nx\n")
	b := newBuf(1, 3)
	a.Render(b, 0)
	top, mid, bot := b.Content[0].Style.Fg, b.Content[1].Style.Fg, b.Content[2].Style.Fg
	if top != cell.NewColorRGB(255, 0, 0) || bot != cell.NewColorRGB(0, 0, 255) {
		t.Fatalf("gradient ends: %06x %06x", uint32(top), uint32(bot))
	}
	if r, _, bl := mid.RGB(); r < 100 || bl < 100 {
		t.Fatalf("gradient middle %06x", uint32(mid))
	}
	// Colours from ANSI sequences in the text beat @color, and survive a
	// line break like a terminal's pen does.
	a = parse(t, "@color #00ff00\n@align top-left\n\x1b[38;2;1;2;3mab\ncd\x1b[0me\n\x1b[31;44mf\n")
	b = newBuf(3, 3)
	a.Render(b, 0)
	if c := b.Content[0].Style.Fg; c != cell.NewColorRGB(1, 2, 3) {
		t.Errorf("truecolor: %06x", uint32(c))
	}
	if c := b.Content[3].Style.Fg; c != cell.NewColorRGB(1, 2, 3) {
		t.Errorf("colour carried to the next line: %06x", uint32(c))
	}
	if c := b.Content[5].Style.Fg; c != cell.NewColorRGB(0, 255, 0) {
		t.Errorf("after a reset, @color again: %06x", uint32(c))
	}
	if c := b.Content[6].Style; c.Fg != cell.NewColorRGB(205, 0, 0) || c.Bg != cell.NewColorRGB(0, 0, 238) {
		t.Errorf("palette colours: %+v", c)
	}
}

// A space is transparent, so the terminal's own background shows around
// the picture — unless the text gave it a colour, as block art does.
func TestArtSpacesAreTransparent(t *testing.T) {
	a := parse(t, "@align top-left\na b\n\x1b[44m \x1b[0m\n")
	b := newBuf(3, 2)
	a.Render(b, 0)
	if c := b.Content[1]; c != (cell.Cell{Content: ' '}) {
		t.Errorf("space in the art: %+v", c)
	}
	if c := b.Content[3]; c.Style.Bg != cell.NewColorRGB(0, 0, 238) {
		t.Errorf("coloured space: %+v", c)
	}
}

func TestArtKeepsCellsOneColumnWide(t *testing.T) {
	a := parse(t, "@align top-left\na\tb🍋c\n")
	got := screen(a, 14, 1, 0)[0]
	if want := "a       b  c  "; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestArtErrorsNameTheLine(t *testing.T) {
	for _, text := range []string{"x\n@fps fast\n", "@color red\nx\n", "@align middle\nx\n", "@fps 2\n@frame\n"} {
		_, err := ParseArt(strings.NewReader(text))
		if err == nil {
			t.Errorf("%q parsed", text)
		}
	}
	_, err := ParseArt(strings.NewReader("x\n@fps fast\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error %v does not name line 2", err)
	}
}

func TestArtDoesNotAllocate(t *testing.T) {
	a := parse(t, "@tile 2 1\n@scroll 3 1\n@fps 5\n@color #102030 #405060\n/\\\n\\/\n@frame\n\\/\n/\\\n")
	b := newBuf(120, 40)
	at := time.Duration(0)
	if n := testing.AllocsPerRun(50, func() {
		at += a.Interval()
		a.Render(b, at)
	}); n != 0 {
		t.Fatalf("%v allocations a frame", n)
	}
}

func TestImageCoversTheScreen(t *testing.T) {
	// Left half red, right half blue, 4:1 — wider than the screen, so the
	// sides are cropped and the middle stays split in half.
	img := image.NewRGBA(image.Rect(0, 0, 400, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 400; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 200 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	s := Image(img)
	if s.Interval() != 0 {
		t.Fatal("a picture asks for frames")
	}
	b := newBuf(20, 10)
	s.Render(b, 0)
	for y := 0; y < 10; y++ {
		left, right := b.Content[y*20], b.Content[y*20+19]
		if left.Style.Bg != cell.NewColorRGB(255, 0, 0) || right.Style.Bg != cell.NewColorRGB(0, 0, 255) {
			t.Fatalf("row %d: %+v … %+v", y, left, right)
		}
	}
	if n := testing.AllocsPerRun(20, func() { s.Render(b, time.Second) }); n != 0 {
		t.Fatalf("%v allocations a frame once scaled", n)
	}
}

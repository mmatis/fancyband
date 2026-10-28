package buffer

import (
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/cell"
)

func writeRow(buf *Buffer, y uint16, s string) {
	x := uint16(0)
	for _, r := range s {
		buf.SetCellDirect(x, y, cell.Cell{Content: r})
		x++
	}
}

// Inline output must never contain absolute cursor addressing: the row the
// application starts on moves whenever the terminal scrolls, so a CSI row;colH
// would paint over the user's scrollback.
func TestInlineUsesOnlyRelativeMotion(t *testing.T) {
	area := cell.NewRect(0, 0, 20, 3)
	front, back := NewBuffer(area), NewBuffer(area)
	writeRow(front, 0, "one")
	writeRow(front, 1, "two")
	writeRow(front, 2, "three")

	out, err := DiffInline(front, back, nil, DiffOptions{})
	if err != nil {
		t.Fatalf("DiffInline: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "H") {
		t.Errorf("absolute cursor addressing in inline output: %q", got)
	}
	if strings.Contains(got, "\x1b[?1049") {
		t.Errorf("inline output touched the alternate screen: %q", got)
	}
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(got, want) {
			t.Errorf("row %q missing from %q", want, got)
		}
	}
}

// The cursor has to come back to the first row, or every frame would march
// down the screen.
func TestInlineReturnsToTheFirstRow(t *testing.T) {
	area := cell.NewRect(0, 0, 20, 4)
	front, back := NewBuffer(area), NewBuffer(area)
	writeRow(front, 0, "x")

	out, _ := DiffInline(front, back, nil, DiffOptions{})
	got := string(out)
	if !strings.HasSuffix(got, "\x1b[3A\r") {
		t.Errorf("frame does not end by returning 3 rows up: %q", got)
	}
	if strings.Count(got, "\r\n") != 3 {
		t.Errorf("expected 3 row breaks for a 4-row frame, got %d: %q", strings.Count(got, "\r\n"), got)
	}
}

// A row that shrank must not leave the previous frame's tail on screen.
func TestInlineClearsEachRow(t *testing.T) {
	area := cell.NewRect(0, 0, 20, 2)
	front, back := NewBuffer(area), NewBuffer(area)
	writeRow(front, 0, "short")

	out, _ := DiffInline(front, back, nil, DiffOptions{})
	if strings.Count(string(out), "\x1b[K") != 2 {
		t.Errorf("expected an EL per row, got %q", out)
	}
}

// EL erases using the current background, so a styled cell at the end of a row
// would smear its colour across the rest of the line.
func TestInlineResetsStyleBeforeErasing(t *testing.T) {
	area := cell.NewRect(0, 0, 10, 1)
	front, back := NewBuffer(area), NewBuffer(area)
	front.SetCellDirect(0, 0, cell.Cell{Content: 'A', Style: cell.Style{Bg: cell.Color(1)}})

	out, _ := DiffInline(front, back, nil, DiffOptions{Colors256: true})
	got := string(out)
	idx := strings.Index(got, "\x1b[K")
	if idx < 0 {
		t.Fatalf("no EL emitted: %q", got)
	}
	if !strings.Contains(got[:idx], "\x1b[0m") && !strings.Contains(got[:idx], "\x1b[49m") {
		t.Errorf("style not reset before EL: %q", got)
	}
}

func TestInlineSyncsTheBackBuffer(t *testing.T) {
	area := cell.NewRect(0, 0, 10, 2)
	front, back := NewBuffer(area), NewBuffer(area)
	writeRow(front, 0, "hello")

	if _, err := DiffInline(front, back, nil, DiffOptions{}); err != nil {
		t.Fatalf("DiffInline: %v", err)
	}
	for i := range front.Content {
		if front.Content[i] != back.Content[i] {
			t.Fatalf("back buffer diverged at %d", i)
		}
	}
}

func TestInlineReserveAndRelease(t *testing.T) {
	reserve := string(InlineReserve(nil, 3))
	if reserve != "\n\n\n\x1b[3A\r" {
		t.Errorf("reserve = %q", reserve)
	}
	release := string(InlineRelease(nil, 3))
	if release != "\x1b[3B\r" {
		t.Errorf("release = %q", release)
	}
	if got := string(InlineReserve(nil, 0)); got != "" {
		t.Errorf("reserving zero rows emitted %q", got)
	}
}

func TestInlineRepeatCompressesRuns(t *testing.T) {
	area := cell.NewRect(0, 0, 40, 1)
	front, back := NewBuffer(area), NewBuffer(area)
	for x := uint16(0); x < 40; x++ {
		front.SetCellDirect(x, 0, cell.Cell{Content: '-'})
	}

	out, _ := DiffInline(front, back, nil, DiffOptions{RepeatChar: true})
	if !strings.Contains(string(out), "-\x1b[39b") {
		t.Errorf("expected REP in inline output: %q", out)
	}
}

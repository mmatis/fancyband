package buffer

import (
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/cell"
)

func fill(buf *Buffer, y uint16, from, to uint16, r rune, style cell.Style) {
	for x := from; x < to; x++ {
		buf.SetCellDirect(x, y, cell.Cell{Content: r, Style: style})
	}
}

// The back buffer must end up identical to the front whatever encoding was
// chosen, or the next frame diffs against a lie.
func assertSynced(t *testing.T, front, back *Buffer) {
	t.Helper()
	for i := range front.Content {
		if front.Content[i] != back.Content[i] {
			t.Fatalf("back buffer diverged at index %d: %+v vs %+v", i, back.Content[i], front.Content[i])
		}
	}
}

// A small dirty region takes the sparse path, where ECH is the right tool
// because the cursor is being positioned explicitly anyway.
func TestEraseCharCompressesBlankRuns(t *testing.T) {
	area := cell.NewRect(0, 0, 120, 40)
	front, back := NewBuffer(area), NewBuffer(area)
	fill(back, 3, 0, 40, 'x', cell.Style{})
	fill(front, 3, 0, 40, ' ', cell.Style{})

	out, err := DiffWithOptions(front, back, nil, DiffOptions{EraseChar: true})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "\x1b[40X") {
		t.Errorf("expected ECH for 40 blanks, got %q", got)
	}
	assertSynced(t, front, back)
}

// The full-redraw path streams sequentially, so the cheaper tool for a blank
// tail is EL: three bytes for the rest of the row.
func TestFullRedrawErasesToEndOfLine(t *testing.T) {
	area := cell.NewRect(0, 0, 40, 1)
	front, back := NewBuffer(area), NewBuffer(area)
	fill(back, 0, 0, 40, 'x', cell.Style{})
	fill(front, 0, 0, 40, ' ', cell.Style{})

	out, _ := DiffWithOptions(front, back, nil, DiffOptions{EraseChar: true})
	if !strings.Contains(string(out), "\x1b[K") {
		t.Errorf("expected EL for a blank row, got %q", out)
	}
	if strings.Contains(string(out), "    ") {
		t.Errorf("blank tail written literally: %q", out)
	}
	assertSynced(t, front, back)
}

func TestEraseCharDisabledWritesSpaces(t *testing.T) {
	area := cell.NewRect(0, 0, 40, 1)
	front, back := NewBuffer(area), NewBuffer(area)
	fill(back, 0, 0, 40, 'x', cell.Style{})
	fill(front, 0, 0, 40, ' ', cell.Style{})

	out, _ := DiffWithOptions(front, back, nil, DiffOptions{})
	if strings.Contains(string(out), "X") {
		t.Errorf("ECH emitted while disabled: %q", out)
	}
	if !strings.Contains(string(out), strings.Repeat(" ", 40)) {
		t.Errorf("expected literal spaces, got %q", out)
	}
	assertSynced(t, front, back)
}

func TestRepeatCharCompressesGlyphRuns(t *testing.T) {
	area := cell.NewRect(0, 0, 40, 1)
	front, back := NewBuffer(area), NewBuffer(area)
	fill(front, 0, 0, 40, 'A', cell.Style{})

	out, err := DiffWithOptions(front, back, nil, DiffOptions{RepeatChar: true})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	got := string(out)
	// One literal A, then a repeat of the remaining 39.
	if !strings.Contains(got, "A\x1b[39b") {
		t.Errorf("expected REP for a 40-glyph run, got %q", got)
	}
	if strings.Count(got, "A") != 1 {
		t.Errorf("glyph written %d times, want once: %q", strings.Count(got, "A"), got)
	}
	assertSynced(t, front, back)
}

func TestRepeatCharDisabledWritesEveryGlyph(t *testing.T) {
	area := cell.NewRect(0, 0, 40, 1)
	front, back := NewBuffer(area), NewBuffer(area)
	fill(front, 0, 0, 40, 'A', cell.Style{})

	out, _ := DiffWithOptions(front, back, nil, DiffOptions{})
	if strings.Contains(string(out), "b") {
		t.Errorf("REP emitted while disabled: %q", out)
	}
	if strings.Count(string(out), "A") != 40 {
		t.Errorf("wrote %d glyphs, want 40", strings.Count(string(out), "A"))
	}
	assertSynced(t, front, back)
}

// A run shorter than the break-even length must stay literal, or compression
// would make the stream longer than it was.
func TestShortRunsStayLiteral(t *testing.T) {
	area := cell.NewRect(0, 0, 120, 40)
	front, back := NewBuffer(area), NewBuffer(area)
	fill(front, 3, 0, 3, 'A', cell.Style{})

	out, _ := DiffWithOptions(front, back, nil, DiffOptions{EraseChar: true, RepeatChar: true})
	if strings.Contains(string(out), "\x1b[2b") {
		t.Errorf("compressed a 3-cell run: %q", out)
	}
	if strings.Count(string(out), "A") != 3 {
		t.Errorf("wrote %d glyphs, want 3: %q", strings.Count(string(out), "A"), out)
	}
	assertSynced(t, front, back)
}

// A run may not swallow cells that already match the back buffer, or the diff
// stops being minimal.
func TestRunStopsAtAnUnchangedCell(t *testing.T) {
	area := cell.NewRect(0, 0, 120, 40)
	front, back := NewBuffer(area), NewBuffer(area)
	fill(front, 3, 0, 20, 'A', cell.Style{})
	// Cell 8 is already correct in the back buffer.
	back.SetCellDirect(8, 3, cell.Cell{Content: 'A'})

	out, _ := DiffWithOptions(front, back, nil, DiffOptions{RepeatChar: true})
	if strings.Contains(string(out), "\x1b[19b") {
		t.Errorf("run crossed an unchanged cell: %q", out)
	}
	assertSynced(t, front, back)
}

// Wide glyphs occupy two cells and a repeat would desynchronise the grid.
func TestWideGlyphsAreNotRepeated(t *testing.T) {
	area := cell.NewRect(0, 0, 40, 1)
	front, back := NewBuffer(area), NewBuffer(area)
	for x := uint16(0); x < 40; x += 2 {
		front.SetCellDirect(x, 0, cell.Cell{Content: '日'})
		front.SetCellDirect(x+1, 0, cell.Cell{Content: cell.RuneContinuation})
	}

	out, _ := DiffWithOptions(front, back, nil, DiffOptions{RepeatChar: true})
	if strings.Contains(string(out), "b") {
		t.Errorf("REP emitted for a wide glyph: %q", out)
	}
	assertSynced(t, front, back)
}

func TestCompressionShrinksTheStream(t *testing.T) {
	area := cell.NewRect(0, 0, 120, 40)

	measure := func(opts DiffOptions) int {
		front, back := NewBuffer(area), NewBuffer(area)
		for y := uint16(0); y < 40; y++ {
			fill(front, y, 0, 120, 'A', cell.Style{})
		}
		out, err := DiffWithOptions(front, back, nil, opts)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		assertSynced(t, front, back)
		return len(out)
	}

	plain := measure(DiffOptions{})
	compressed := measure(DiffOptions{EraseChar: true, RepeatChar: true})
	if compressed >= plain {
		t.Errorf("compression did not shrink the stream: %d vs %d bytes", compressed, plain)
	}
	t.Logf("full-screen glyph fill: %d bytes plain, %d compressed", plain, compressed)
}

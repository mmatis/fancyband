package widgets

import (
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
)

// renderBlocks draws each block into one buffer and returns the text snapshot.
func renderBlocks(w, h uint16, blocks ...struct {
	block Block
	area  cell.Rect
}) string {
	buf := buffer.NewBuffer(cell.NewRect(0, 0, w, h))
	for _, b := range blocks {
		b.block.Draw(cell.NewContext(b.area, cell.Style{}), buf)
	}
	return buf.Snapshot()
}

type placed = struct {
	block Block
	area  cell.Rect
}

func TestMergeBoxDrawingUnionOfSegments(t *testing.T) {
	for _, tc := range []struct {
		name               string
		existing, incoming rune
		want               rune
	}{
		{"vertical meets horizontal", '│', '─', '┼'},
		{"corner meets vertical below", '┌', '│', '├'},
		{"top-right meets vertical", '┐', '│', '┤'},
		{"horizontal meets top-left", '─', '┌', '┬'},
		{"horizontal meets bottom-left", '─', '└', '┴'},
		{"rounded corner merges as square", '╭', '│', '├'},
		{"identical is unchanged", '│', '│', '│'},
		{"already a cross", '┼', '─', '┼'},
		{"text is never merged", 'x', '─', '─'},
		{"blank is never merged", ' ', '│', '│'},
		{"heavy is left alone", '┃', '─', '─'},
		{"double is left alone", '═', '│', '│'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cell.MergeBoxDrawing(tc.existing, tc.incoming); got != tc.want {
				t.Errorf("MergeBoxDrawing(%q, %q) = %q, want %q", tc.existing, tc.incoming, got, tc.want)
			}
		})
	}
}

// Two blocks sharing an edge should meet in junctions rather than one border
// painting over the other. This is the visible payoff of a cell grid: the
// previous character is still there to merge with.
func TestAdjacentBlocksShareTheirEdge(t *testing.T) {
	draw := func(merge bool) string {
		b := Block{Borders: BorderAll, BorderSymbols: SymbolsSingle, MergeBorders: merge}
		return renderBlocks(11, 5,
			placed{b, cell.NewRect(0, 0, 6, 5)},
			placed{b, cell.NewRect(5, 0, 6, 5)})
	}

	merged := draw(true)
	plain := draw(false)

	if merged == plain {
		t.Fatal("MergeBorders changed nothing")
	}

	lines := strings.Split(merged, "\n")
	if len(lines) < 5 {
		t.Fatalf("snapshot has %d lines", len(lines))
	}
	// Column 5 is shared. The top row should be a ┬, the middle a │, and the
	// bottom a ┴.
	top := []rune(lines[0])
	middle := []rune(lines[2])
	bottom := []rune(lines[4])
	if top[5] != '┬' {
		t.Errorf("shared top = %q, want ┬\n%s", top[5], merged)
	}
	if middle[5] != '│' {
		t.Errorf("shared middle = %q, want │\n%s", middle[5], merged)
	}
	if bottom[5] != '┴' {
		t.Errorf("shared bottom = %q, want ┴\n%s", bottom[5], merged)
	}

	// Without merging, the second block's left border simply overwrites, so no
	// junction appears anywhere on the seam.
	if strings.ContainsAny(plain, "┬┴┼├┤") {
		t.Errorf("unmerged render grew a junction:\n%s", plain)
	}
}

func TestStackedBlocksShareTheirEdge(t *testing.T) {
	b := Block{Borders: BorderAll, BorderSymbols: SymbolsSingle, MergeBorders: true}
	snapshot := renderBlocks(6, 9,
		placed{b, cell.NewRect(0, 0, 6, 5)},
		placed{b, cell.NewRect(0, 4, 6, 5)})

	lines := strings.Split(snapshot, "\n")
	seam := []rune(lines[4])
	if seam[0] != '├' {
		t.Errorf("left seam = %q, want ├\n%s", seam[0], snapshot)
	}
	if seam[5] != '┤' {
		t.Errorf("right seam = %q, want ┤\n%s", seam[5], snapshot)
	}
}

func BenchmarkBlockDrawMergedBorders(b *testing.B) {
	buf := buffer.NewBuffer(cell.NewRect(0, 0, 80, 24))
	block := Block{Borders: BorderAll, BorderSymbols: SymbolsSingle, MergeBorders: true}
	ctx := cell.NewContext(buf.Area, cell.Style{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		block.Draw(ctx, buf)
	}
}

package buffer

import (
	"testing"
	"unsafe"

	"github.com/thebanri/limoni/core/cell"
)

func TestNewBuffer(t *testing.T) {
	area := cell.NewRect(0, 0, 20, 10)
	buf := NewBuffer(area)

	if buf.Area.Width != 20 || buf.Area.Height != 10 {
		t.Errorf("Buffer alanı hatalı. Beklenen: 20x10, alınan: %dx%d", buf.Area.Width, buf.Area.Height)
	}

	if len(buf.Content) != 200 {
		t.Errorf("İçerik boyutu hatalı. Beklenen: 200, alınan: %d", len(buf.Content))
	}

	// Check every cell starts as a space with the default style
	for i, c := range buf.Content {
		if c.Content != ' ' {
			t.Errorf("İndeks %d varsayılan karakter boşluk olmalıydı, alınan: %q", i, c.Content)
		}
		if c.Style.Fg.Type() != cell.ColorDefault || c.Style.Bg.Type() != cell.ColorDefault {
			t.Errorf("İndeks %d varsayılan stil default olmalıydı", i)
		}
	}
}

func TestBufferGetSet(t *testing.T) {
	area := cell.NewRect(0, 0, 10, 5)
	buf := NewBuffer(area)

	c := cell.Cell{
		Content: 'X',
		Style: cell.Style{
			Fg: cell.NewColorANSI(9),
			Bg: cell.NewColorANSI(0),
		},
	}

	buf.SetCell(2, 2, c)

	got := buf.Get(2, 2)
	if got == nil {
		t.Fatalf("Hücre nil dönmemeliydi")
	}

	if got.Content != 'X' || got.Style.Fg.ANSI() != 9 {
		t.Errorf("Hücre değeri doğru set edilmedi")
	}

	// Out-of-range coordinate test
	if out := buf.Get(10, 5); out != nil {
		t.Errorf("Sınır dışı koordinat nil dönmeliydi")
	}
}

func TestBufferSetString(t *testing.T) {
	area := cell.NewRect(0, 0, 10, 3)
	buf := NewBuffer(area)

	style := cell.Style{Fg: cell.NewColorRGB(255, 0, 0)}
	buf.SetString(2, 1, "Merhaba", style)

	// "Merhaba" is 7 characters. Starting at (2, 1) it writes up to (8, 1).
	expected := "Merhaba"
	for i, r := range expected {
		got := buf.Get(uint16(2+i), 1)
		if got == nil || got.Content != r {
			t.Errorf("Karakter %d eşleşmedi. Beklenen: %c", i, r)
		}
		if got.Style.Fg.Type() != cell.ColorRGB {
			t.Errorf("Karakter %d rengi TrueColor olmalıydı", i)
		}
	}

	// Bounds check (must be cut off, not wrapped)
	buf.Clear()
	buf.SetString(7, 1, "UzunMetin", style) // Starts at (7, 1). Only "Uzu" fits (width 10)

	if buf.Get(9, 1).Content != 'u' {
		t.Errorf("Sınırda kesilme hatalı. (9,1) 'u' olmalı, alınan: %c", buf.Get(9, 1).Content)
	}

	// SetString at an invalid coordinate must not panic
	buf.SetString(20, 20, "Test", style)
}

func TestBufferSetStringWithin(t *testing.T) {
	area := cell.NewRect(0, 0, 30, 5)
	buf := NewBuffer(area)

	style := cell.Style{}
	// Write with maxWidth = 5 starting at x=10
	buf.SetStringWithin(10, 0, "ABCDEFGHIJ", style, 5)

	// Columns 10..14 should be "ABCDE"
	got := ""
	for x := uint16(10); x < 15; x++ {
		got += string(buf.CellAt(x, 0).Content)
	}
	if got != "ABCDE" {
		t.Fatalf("SetStringWithin text = %q; want ABCDE", got)
	}

	// Column 15 must remain blank space (never bled into)
	if cell15 := buf.CellAt(15, 0).Content; cell15 != ' ' {
		t.Fatalf("Column 15 was corrupted with %c; should remain empty space", cell15)
	}
}

func TestBufferResizeAllocation(t *testing.T) {
	area := cell.NewRect(0, 0, 10, 10) // 100 cells
	buf := NewBuffer(area)

	ptrBefore := unsafe.Pointer(&buf.Content[0])

	// Resize to a smaller or equal size
	buf.Resize(cell.NewRect(0, 0, 5, 5)) // 25 cells
	ptrAfter := unsafe.Pointer(&buf.Content[0])

	if ptrBefore != ptrAfter {
		t.Errorf("Kapasite yeterliyken bellek yeniden tahsis edildi (re-allocated)")
	}

	// Resize beyond the capacity
	buf.Resize(cell.NewRect(0, 0, 15, 10)) // 150 cells
	ptrNew := unsafe.Pointer(&buf.Content[0])

	if ptrBefore == ptrNew {
		t.Errorf("Kapasite aşılmasına rağmen bellek adresi değişmedi")
	}
}

func TestBufferTransparentInheritance(t *testing.T) {
	area := cell.NewRect(0, 0, 10, 5)
	buf := NewBuffer(area)

	// 1. Paint the panel background (e.g. a navy Surface)
	surfaceBg := cell.NewColorRGB(25, 28, 36)
	for y := uint16(0); y < 5; y++ {
		for x := uint16(0); x < 10; x++ {
			buf.SetCellDirect(x, y, cell.Cell{Content: ' ', Style: cell.Style{Bg: surfaceBg}})
		}
	}

	// 2. Write text with SetString and no background (Bg=0)
	textFg := cell.NewColorRGB(200, 220, 255)
	buf.SetString(2, 2, "Test", cell.Style{Fg: textFg})

	for i, r := range "Test" {
		c := buf.Get(uint16(2+i), 2)
		if c == nil {
			t.Fatalf("Hücre nil dönmemeliydi")
		}
		if c.Content != r {
			t.Errorf("Karakter hatalı: beklenen %c, alınan %c", r, c.Content)
		}
		if c.Style.Fg != textFg {
			t.Errorf("Yazı rengi hatalı: beklenen %v, alınan %v", textFg, c.Style.Fg)
		}
		if c.Style.Bg != surfaceBg {
			t.Errorf("Şeffaf kalıtım başarısız! Arkaplan korunmalıydı: beklenen %v, alınan %v", surfaceBg, c.Style.Bg)
		}
	}

	// 3. Write a single cell with SetCell and no background
	buf.SetCell(0, 0, cell.Cell{Content: '●', Style: cell.Style{Fg: textFg}})
	thumb := buf.Get(0, 0)
	if thumb.Content != '●' {
		t.Errorf("SetCell karakteri hatalı")
	}
	if thumb.Style.Bg != surfaceBg {
		t.Errorf("SetCell şeffaf kalıtım başarısız! Arkaplan korunmalıydı: beklenen %v, alınan %v", surfaceBg, thumb.Style.Bg)
	}

	// 4. An explicit background must override it
	customBg := cell.NewColorRGB(255, 0, 0)
	buf.SetString(0, 1, "Red", cell.Style{Fg: textFg, Bg: customBg})
	redCell := buf.Get(0, 1)
	if redCell.Style.Bg != customBg {
		t.Errorf("Özel arkaplan ezilemedi: beklenen %v, alınan %v", customBg, redCell.Style.Bg)
	}
}

func TestBufferOrphanWideCharacters(t *testing.T) {
	buf := NewBuffer(cell.NewRect(0, 0, 10, 1))

	// 1. Write emoji at 0, 0
	buf.SetString(0, 0, "🚀", cell.Style{})
	if buf.CellAt(0, 0).Content != '🚀' {
		t.Errorf("Cell 0 want 🚀, got %c", buf.CellAt(0, 0).Content)
	}
	if buf.CellAt(1, 0).Content != cell.RuneContinuation {
		t.Errorf("Cell 1 want RuneContinuation, got U+%04X", buf.CellAt(1, 0).Content)
	}

	// 2. Overwrite continuation cell at (1, 0) with 'A'
	buf.SetString(1, 0, "A", cell.Style{})
	if buf.CellAt(0, 0).Content != ' ' {
		t.Errorf("Cell 0 should be reset to space after continuation overwrite, got %c", buf.CellAt(0, 0).Content)
	}
	if buf.CellAt(1, 0).Content != 'A' {
		t.Errorf("Cell 1 want A, got %c", buf.CellAt(1, 0).Content)
	}

	// 3. Write emoji at 0 again, then overwrite left half at (0, 0) with 'B'
	buf.SetString(0, 0, "🚀", cell.Style{})
	buf.SetString(0, 0, "B", cell.Style{})
	if buf.CellAt(0, 0).Content != 'B' {
		t.Errorf("Cell 0 want B, got %c", buf.CellAt(0, 0).Content)
	}
	if buf.CellAt(1, 0).Content != ' ' {
		t.Errorf("Cell 1 should be reset to space after wide character left-half overwrite, got %c", buf.CellAt(1, 0).Content)
	}

	// 4. Test SetCell with wide character sets continuation
	buf.Clear()
	buf.SetCell(2, 0, cell.Cell{Content: '🔥'})
	if buf.CellAt(2, 0).Content != '🔥' {
		t.Errorf("Cell 2 want 🔥, got %c", buf.CellAt(2, 0).Content)
	}
	if buf.CellAt(3, 0).Content != cell.RuneContinuation {
		t.Errorf("Cell 3 want RuneContinuation after SetCell wide rune, got U+%04X", buf.CellAt(3, 0).Content)
	}
}

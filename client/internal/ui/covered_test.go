package ui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"
	"github.com/thebanri/limoni/graphics"
	"github.com/thebanri/limoni/testkit"

	"fancyband/internal/proto"
)

// coveredGear is six gear tiles, which the stats bar draws from column 1 on
// rows 7 and 8.
func coveredGear() []*proto.Glyph {
	gear := make([]*proto.Glyph, 12)
	for i := 0; i < len(gear); i += 2 {
		gear[i] = &proto.Glyph{Char: "|", Color: 1, Tile: &proto.TileSpot{Row: 1, Col: uint8(i + 1)}}
	}
	return gear
}

// gameWithGear returns a tiled game of 130 by 40 cells with gear on.
func gameWithGear(t *testing.T) (*Model, *sentLog) {
	t.Helper()
	m := tiledMapModel(t, 130, 40)
	var sent sentLog
	m.sendHook = sent.hook
	m.status = &proto.StatusData{Name: "T", Equippy: coveredGear()}
	m.setInGame(true)
	return m, &sent
}

// picturesOnPictureCells fails if a picture is placed on a cell a window
// drew over, and returns how many pictures there are.
func picturesOnPictureCells(t *testing.T, term *testkit.Terminal) int {
	t.Helper()
	regions := term.Frame().ImageRegionsSnapshot()
	for _, r := range regions {
		for y := r.Area.Y; y < r.Area.Y+r.Area.Height; y++ {
			for x := r.Area.X; x < r.Area.X+r.Area.Width; x++ {
				if c := term.Frame().Buffer.CellAt(x, y).Content; c != cell.RuneImage {
					t.Errorf("picture %+v is under %q at %d, %d", r.Area, string(c), x, y)
				}
			}
		}
	}
	return len(regions)
}

// TestGearLeavesWithWindow takes the gear's tiles away under a window that
// covers them, and keeps them under one that does not.
func TestGearLeavesWithWindow(t *testing.T) {
	m, sent := gameWithGear(t)
	term := testkit.NewTerminal(130, 40)
	term.Draw(m.View)
	if n := picturesOnPictureCells(t, term); n != 6 {
		t.Fatalf("%d pictures with no window, want the 6 gear tiles", n)
	}

	narrow := newListScreen("x", "x", []listItem{{Label: "row"}})
	narrow.width, narrow.height = 60, 20
	m.push(narrow)
	term.Draw(m.View)
	if n := picturesOnPictureCells(t, term); n != 6 {
		t.Errorf("%d pictures beside a narrow window, want 6", n)
	}
	m.pop()

	openFakeMap(t, m, sent, fakeLevel(198, 66, false))
	term.Draw(m.View)
	if n := picturesOnPictureCells(t, term); n != 0 {
		t.Errorf("%d pictures under the text map, want none", n)
	}
	m.pop()

	term.Draw(m.View)
	if n := picturesOnPictureCells(t, term); n != 6 {
		t.Errorf("%d pictures after the window closed, want 6", n)
	}
}

// TestGearUnderDescription takes the gear's tiles away under a description
// box that opens over them, from a name at the top of the stats bar.
func TestGearUnderDescription(t *testing.T) {
	m, _ := gameWithGear(t)
	text := strings.Repeat("word ", 80)
	m.gloss = &glossary{terms: map[string]*proto.GlossaryTerm{"race:Human": {
		ID: "race:Human", Title: "Human", Text: proto.PlainText(1, text)}}}
	m.desc = &descBox{id: "race:Human", anchor: Box{1, 1, 5, 1}, hover: true, under: m.topScreen()}
	term := testkit.NewTerminal(130, 40)
	term.Draw(m.View)
	if m.desc.box.Y > 7 || m.desc.box.Y+m.desc.box.H <= 8 {
		t.Fatalf("the box %+v does not reach the gear", m.desc.box)
	}
	if n := picturesOnPictureCells(t, term); n != 0 {
		t.Errorf("%d pictures under the description box, want none", n)
	}
}

// TestListTilesUnderDialog takes a list's tiles away under a dialog drawn
// over the list, and keeps those beside it.
func TestListTilesUnderDialog(t *testing.T) {
	m, _ := gameWithGear(t)
	var items []listItem
	for i := 0; i < 12; i++ {
		items = append(items, listItem{Label: "row", Tile: &proto.PictCell{FgRow: 1, FgCol: uint8(i)}})
	}
	list := newListScreen("x", "x", items)
	list.width, list.height = 100, 14
	m.push(list)
	term := testkit.NewTerminal(130, 40)
	term.Draw(m.View)
	if n := picturesOnPictureCells(t, term); n != 6+12 {
		t.Fatalf("%d pictures with the list alone, want 18", n)
	}
	dialog := newListScreen("y", "y", []listItem{{Label: "a dialog"}})
	dialog.width, dialog.height = 110, 10
	m.push(dialog)
	term.Draw(m.View)
	if n := picturesOnPictureCells(t, term); n >= 6+12 {
		t.Errorf("%d pictures under the dialog, want fewer than 18", n)
	}
}

// TestKittyDeletesCoveredGear checks what reaches the terminal with Kitty's
// protocol, where a picture floats over the text: the gear's placements go
// when a window opens over them, and come back when it closes.
func TestKittyDeletesCoveredGear(t *testing.T) {
	t.Setenv("LIMONI_PROBE", "0")
	io := driver.NewMemoryTerminalIO(nil, 130, 40)
	term, err := terminal.New(driver.NewPortableBackend(io))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	caps := term.Capabilities()
	caps.GraphicsProto = graphics.ProtocolKitty
	term.SetCapabilities(caps)

	m, sent := gameWithGear(t)
	frame := func() string {
		t.Helper()
		before := len(io.Output())
		if err := term.Draw(m.View); err != nil {
			t.Fatal(err)
		}
		return string(io.Output()[before:])
	}
	if out := frame(); strings.Count(out, "a=p,") != 6 {
		t.Fatalf("placed %d tiles, want 6", strings.Count(out, "a=p,"))
	}
	openFakeMap(t, m, sent, fakeLevel(198, 66, false))
	if out := frame(); !strings.Contains(out, "a=d") {
		t.Errorf("the gear's placements stayed under the window")
	}
	m.pop()
	if out := frame(); strings.Count(out, "a=p,") != 6 {
		t.Errorf("placed %d tiles after the window closed, want 6", strings.Count(out, "a=p,"))
	}
}

// TestPlaceUncovered cuts a picture around a window over its middle: four
// parts, with the right pixels, and the same picture values on the next
// frame, so nothing is sent again.
func TestPlaceUncovered(t *testing.T) {
	// Ten cells by four, two pixels a cell, each pixel naming its cell.
	img := image.NewRGBA(image.Rect(0, 0, 20, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 20; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x / 2), uint8(y / 2), 0, 255})
		}
	}
	m := New(Config{Width: 40, Height: 10})
	draw := func() []terminal.ImageRegion {
		buf := buffer.NewBuffer(cell.NewRect(0, 0, 40, 10))
		f := terminal.NewFrame(buf, terminal.NewFocusManager())
		for y := 2; y < 6; y++ {
			for x := 5; x < 15; x++ {
				buf.SetCell(uint16(x), uint16(y), cell.Cell{Content: cell.RuneImage})
			}
		}
		m.holding = true
		m.place(f, terminal.ImageRegion{Area: cell.NewRect(5, 2, 10, 4), Img: img, Transparent: true, ZIndex: 3})
		if len(f.ImageRegionsSnapshot()) != 0 {
			t.Fatal("place did not hold the picture back")
		}
		// A window over columns 8 to 10 of rows 3 and 4.
		for y := 3; y < 5; y++ {
			for x := 8; x < 11; x++ {
				buf.SetCell(uint16(x), uint16(y), cell.Cell{Content: ' '})
			}
		}
		m.placeHeld(f)
		return f.ImageRegionsSnapshot()
	}
	got := draw()
	want := map[cell.Rect][4]int{
		cell.NewRect(5, 2, 10, 1): {0, 0, 20, 2},
		cell.NewRect(5, 3, 3, 2):  {0, 1, 6, 4},
		cell.NewRect(11, 3, 4, 2): {6, 1, 8, 4},
		cell.NewRect(5, 5, 10, 1): {0, 3, 20, 2},
	}
	if len(got) != len(want) {
		t.Fatalf("%d parts: %+v", len(got), got)
	}
	for _, r := range got {
		w, ok := want[r.Area]
		if !ok {
			t.Errorf("a part at %+v", r.Area)
			continue
		}
		if b := r.Img.Bounds(); b.Dx() != w[2] || b.Dy() != w[3] {
			t.Errorf("part %+v is %dx%d pixels, want %dx%d", r.Area, b.Dx(), b.Dy(), w[2], w[3])
		}
		if !r.Transparent || r.ZIndex != 3 {
			t.Errorf("part %+v lost its flags: %+v", r.Area, r)
		}
		pr, pg, _, _ := r.Img.At(r.Img.Bounds().Min.X, r.Img.Bounds().Min.Y).RGBA()
		if int(pr>>8) != w[0] || int(pg>>8) != w[1] {
			t.Errorf("part %+v starts with cell %d, %d, want %d, %d", r.Area, pr>>8, pg>>8, w[0], w[1])
		}
	}
	again := draw()
	for i := range got {
		if got[i].Img != again[i].Img {
			t.Errorf("part %+v is a new picture on the next frame", got[i].Area)
		}
	}
}

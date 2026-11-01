package ui

import (
	"fmt"
	"strings"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/cell"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/core/terminal"

	"fancyband/internal/proto"
)

// tombID names the tombstone's or crown's engraving in Limoni's
// accessibility tree, for tests.
const tombID = "tomb"

// Sizes of the tombstone, in cells: the engraving's width, as the game's
// own stone has it, and the stone's width with its edges.
const (
	tombTextWidth = 31
	tombWidth     = tombTextWidth + 4
)

// The death screen's colors.
var (
	tombGround   = limoni.RGB(14, 14, 20)
	tombStone    = limoni.RGB(150, 150, 158)
	tombLight    = limoni.RGB(178, 178, 186)
	tombDark     = limoni.RGB(96, 96, 104)
	tombCarved   = limoni.RGB(52, 52, 60)
	tombEarth    = limoni.RGB(62, 46, 32)
	tombEarthTop = limoni.RGB(40, 64, 30)
	crownText    = limoni.RGB(230, 200, 120)
)

// tombScreen is the death screen behind the death menu: a tombstone
// engraved as the game's own, or a crown for a winner. The death menu
// sits beside it, as a screen of its own.
type tombScreen struct {
	tomb proto.TombData
	// menu is the death menu, which the picture leaves room for.
	menu *listScreen
}

// tombLines is what the stone says, as display_exit_screen() in
// ui-death.c engraves it.
func tombLines(t proto.TombData) []string {
	lines := []string{"R I P", "", t.Name, "the", t.Title, "", t.Class,
		fmt.Sprintf("Level: %d", t.Level),
		fmt.Sprintf("Exp: %d", t.Exp),
		fmt.Sprintf("AU: %d", t.Gold)}
	if t.Retired {
		lines = append(lines, fmt.Sprintf("Retired on Level %d", t.Depth))
	} else {
		lines = append(lines, fmt.Sprintf("Killed on Level %d", t.Depth))
		lines = append(lines, wrapWords("by "+t.DiedFrom+".", tombTextWidth)...)
	}
	return append(lines, "", "on "+t.Date)
}

// crownLines is what shows under a winner's crown.
func crownLines(t proto.TombData) []string {
	return []string{"All Hail the Mighty Champion!", "",
		t.Name, "the " + t.Title, "",
		fmt.Sprintf("%s, level %d", t.Class, t.Level),
		fmt.Sprintf("Exp: %d   AU: %d", t.Exp, t.Gold), "",
		"Veni, Vidi, Vici!", "I came, I saw, I conquered!", "",
		"on " + t.Date}
}

// wrapWords breaks s into lines of at most w runes, at spaces.
func wrapWords(s string, w int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= w:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	return append(lines, line)
}

// tombArch is how far in from each side the stone's top rows start.
var tombArch = []int{9, 5, 3, 2, 1, 1}

// crownArt is the crown, two pixels a cell: G gold, g its shade, R ruby,
// B sapphire, E emerald, W pearl.
var crownArt = []string{
	"             W             ",
	"    W       GGG       W    ",
	"   GGG       G       GGG   ",
	"    G       GGG       G    ",
	"    GG     GGGGG     GG    ",
	"    GGG   GGGRGGG   GGG    ",
	"    GGGG GGGGGGGGG GGGG    ",
	"    GGGGGGGGGGGGGGGGGGG    ",
	"    GGGGGGGGGGGGGGGGGGG    ",
	"   ggggggggggggggggggggg   ",
	"   ggBgWgRgWgEgWgRgWgBgg   ",
	"   ggggggggggggggggggggg   ",
	"   GGGGGGGGGGGGGGGGGGGGG   ",
	"   ggggggggggggggggggggg   ",
}

var crownColors = map[byte]cell.Color{
	'G': limoni.RGB(232, 188, 64),
	'g': limoni.RGB(168, 122, 34),
	'R': limoni.RGB(200, 32, 44),
	'B': limoni.RGB(44, 84, 210),
	'E': limoni.RGB(34, 162, 74),
	'W': limoni.RGB(242, 240, 228),
}

// size is the stone's or the crown's picture with its text, in cells.
func (s *tombScreen) size() (int, int) {
	if s.tomb.Winner {
		w := len(crownArt[0])
		for _, l := range crownLines(s.tomb) {
			w = max(w, len([]rune(l)))
		}
		return w, (len(crownArt)+1)/2 + 1 + len(crownLines(s.tomb))
	}
	// The arch, the lines with a row above and below, and two rows of
	// ground.
	return tombWidth + 4, len(tombLines(s.tomb)) + 3 + 2
}

// gap is the room between the picture and the menu.
const tombGap = 3

// placement returns where the picture goes in area, beside a menu w
// wide, and where the menu goes.
func (s *tombScreen) placement(area Box, w, h int) (Box, Box) {
	pw, ph := s.size()
	total := pw + tombGap + w
	left := area.X + max((area.W-total)/2, 0)
	pic := Box{left, area.Y + max((area.H-ph)/2, 0), pw, min(ph, area.H)}
	menuX := min(left+pw+tombGap, area.X+area.W-w)
	return pic, Box{max(menuX, area.X), area.Y + max((area.H-h)/2, 0), w, h}
}

// menuBox places the death menu beside the picture.
func (s *tombScreen) menuBox(area Box, w, h int) Box {
	_, menu := s.placement(area, w, h)
	return menu
}

func (s *tombScreen) key(driver.KeyEvent) {}
func (s *tombScreen) click(int, int)      {}
func (s *tombScreen) box(area Box) Box    { return area }

func (s *tombScreen) draw(f *terminal.Frame, b Box, env *drawEnv) {
	ground := cell.NewStyle().WithBg(tombGround)
	for y := b.Y; y < b.Y+b.H; y++ {
		for x := b.X; x < b.X+b.W; x++ {
			f.Buffer.SetCell(uint16(x), uint16(y), cell.Cell{Content: ' ', Style: ground})
		}
	}
	// Room for the menu beside the picture.
	menuW := 0
	if s.menu != nil {
		menuW = s.menu.box(b).W
	}
	pic, _ := s.placement(b, menuW, 0)
	var lines []string
	if s.tomb.Winner {
		lines = s.drawCrown(f, pic)
	} else {
		lines = s.drawStone(f, pic)
	}
	f.RegisterAccessibility(accessibility.Node{ID: tombID, Role: accessibility.RoleImage,
		Label: strings.Join(lines, "\n"), Bounds: rect(pic)})
}

// put sets the cell at x, y, if it is inside b.
func put(f *terminal.Frame, b Box, x, y int, r rune, st cell.Style) {
	if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
		f.Buffer.SetCell(uint16(x), uint16(y), cell.Cell{Content: r, Style: st})
	}
}

// drawStone draws the tombstone in pic and returns its engraving.
func (s *tombScreen) drawStone(f *terminal.Frame, pic Box) []string {
	lines := tombLines(s.tomb)
	// The stone stands on the ground two cells in from each side of pic.
	left := pic.X + 2
	h := len(lines) + 3
	top := pic.Y
	// The engraved spans, with a cell either side, by row, which the
	// speckles stay off.
	carved := func(col, row int) bool {
		i := row - 2
		if i < 0 || i >= len(lines) || lines[i] == "" {
			return false
		}
		w := len([]rune(lines[i]))
		from := (tombWidth - w) / 2
		return col >= from-1 && col <= from+w
	}
	for row := 0; row < h; row++ {
		in := 0
		if row < len(tombArch) {
			in = tombArch[row]
		}
		y := top + row
		for col := in; col < tombWidth-in; col++ {
			x := left + col
			st := cell.NewStyle().WithFg(tombLight).WithBg(tombStone)
			r := ' '
			switch {
			case col == in:
				// The lit edge, fading into the dark.
				r, st = '▒', cell.NewStyle().WithFg(tombLight).WithBg(tombGround)
			case col == tombWidth-in-1:
				// The edge in shadow.
				r, st = '▓', cell.NewStyle().WithFg(tombDark).WithBg(tombGround)
			case col == tombWidth-in-2:
				r, st = '▓', cell.NewStyle().WithFg(tombDark).WithBg(tombStone)
			case speckle(col, row) && !carved(col, row):
				r = '░'
			}
			put(f, pic, x, y, r, st)
		}
	}
	// The engraving, centered on the face, a row under the arch's top.
	for i, l := range lines {
		y := top + 2 + i
		if l == "" {
			continue
		}
		st := cell.NewStyle().WithFg(tombCarved).WithBg(tombStone)
		if i == 0 || l == s.tomb.Name {
			st = st.Bold()
		}
		w := len([]rune(l))
		x := left + (tombWidth-w)/2
		for j, r := range []rune(l) {
			put(f, pic, x+j, y, r, st)
		}
	}
	// The ground: grass, then earth, wider than the stone.
	gy := top + h
	for x := pic.X; x < pic.X+pic.W; x++ {
		put(f, pic, x, gy, '▀', cell.NewStyle().WithFg(tombEarthTop).WithBg(tombEarth))
		put(f, pic, x, gy+1, '▒', cell.NewStyle().WithFg(tombEarth).WithBg(tombGround))
	}
	return lines
}

// speckle picks a few cells of the stone's face to roughen.
func speckle(x, y int) bool {
	h := uint32(x*73856093) ^ uint32(y*19349663)
	return h%11 == 0
}

// drawCrown draws the crown in pic, with the winner's lines under it, and
// returns the lines.
func (s *tombScreen) drawCrown(f *terminal.Frame, pic Box) []string {
	cw := len(crownArt[0])
	left := pic.X + (pic.W-cw)/2
	for row := 0; row < len(crownArt); row += 2 {
		y := pic.Y + row/2
		for x := 0; x < cw; x++ {
			up := crownArt[row][x]
			down := byte(' ')
			if row+1 < len(crownArt) {
				down = crownArt[row+1][x]
			}
			if up == ' ' && down == ' ' {
				continue
			}
			st := cell.NewStyle().WithFg(tombGround).WithBg(tombGround)
			if c, ok := crownColors[up]; ok {
				st = st.WithFg(c)
			}
			if c, ok := crownColors[down]; ok {
				st = st.WithBg(c)
			}
			put(f, pic, left+x, y, '▀', st)
		}
	}
	lines := crownLines(s.tomb)
	y := pic.Y + (len(crownArt)+1)/2 + 1
	for i, l := range lines {
		st := cell.NewStyle().WithFg(screenText.Fg).WithBg(tombGround)
		if i == 0 || l == s.tomb.Name {
			st = cell.NewStyle().WithFg(crownText).WithBg(tombGround).Bold()
		}
		w := len([]rune(l))
		for j, r := range []rune(l) {
			put(f, pic, pic.X+(pic.W-w)/2+j, y+i, r, st)
		}
	}
	return lines
}

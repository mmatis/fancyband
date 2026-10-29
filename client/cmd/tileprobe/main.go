// Command tileprobe shows one picture several ways, each on a numbered
// line, to find out which image protocol features a terminal supports.
// Run it in the terminal you want to test and note which lines show a
// picture. It writes raw escape codes and does not use Limoni.
//
// With -slide it tests whether the terminal can slide a picture smoothly
// between cells with Kitty's pixel offsets, for easing moves on the map.
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// tile is a 16x16 test picture: a yellow square with a red border.
func tile() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := color.RGBA{230, 200, 40, 255}
			if x < 2 || y < 2 || x > 13 || y > 13 {
				c = color.RGBA{220, 40, 40, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func kitty(ctrl string, payload []byte) string {
	if payload == nil {
		return "\x1b_G" + ctrl + "\x1b\\"
	}
	return "\x1b_G" + ctrl + ";" + base64.StdEncoding.EncodeToString(payload) + "\x1b\\"
}

// sixel is a solid red block, 16 pixels wide and 12 tall.
const sixel = "\x1bPq\"1;1;16;12#0;2;90;20;20#0!16~-!16~\x1b\\"

func main() {
	slideMode := flag.Bool("slide", false, "test sliding a picture between cells with pixel offsets")
	flag.Parse()
	if *slideMode {
		slide()
		return
	}
	png := tile()
	out := os.Stdout
	line := func(n int, what, seq string) {
		fmt.Fprintf(out, "%d. %-58s [", n, what)
		fmt.Fprint(out, seq)
		fmt.Fprint(out, "  ]\r\n\r\n")
	}
	fmt.Fprint(out, "tileprobe: each numbered line should end with a picture between the brackets.\r\n\r\n")
	line(1, "Kitty: send and show at once (a=T)", kitty("a=T,f=100,c=2,r=1", png))
	line(2, "Kitty: send, then place by id, z=-1, C=1, q=2 (Limoni)",
		kitty("q=2,f=100,a=t,t=d,i=7001,s=16,v=16", png)+kitty("a=p,q=2,C=1,i=7001,c=2,r=1,z=-1", nil))
	line(3, "Kitty: same, z=1 (over the text)",
		kitty("q=2,f=100,a=t,t=d,i=7002,s=16,v=16", png)+kitty("a=p,q=2,C=1,i=7002,c=2,r=1,z=1", nil))
	line(4, "Kitty: send, then place by id, no C, q or z",
		kitty("f=100,a=t,t=d,i=7003", png)+kitty("a=p,i=7003,c=2,r=1", nil))
	line(5, "Kitty: line 2 on a black cell background",
		"\x1b[40m  \x1b[2D"+kitty("q=2,f=100,a=t,t=d,i=7004,s=16,v=16", png)+kitty("a=p,q=2,C=1,i=7004,c=2,r=1,z=-1", nil)+"\x1b[0m")
	line(6, "Sixel: a red block", sixel)
	fmt.Fprint(out, "Done. Lines 1 to 5 test Kitty's protocol; line 6 tests Sixel.\r\n")
}

// sprite is a 16x16 test picture with see-through corners: a yellow disc
// with a red rim, so a slide shows whether the text under it shows
// through.
func sprite() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			d := math.Hypot(float64(x)-7.5, float64(y)-7.5)
			switch {
			case d < 5.5:
				img.Set(x, y, color.RGBA{240, 210, 40, 255})
			case d < 7.5:
				img.Set(x, y, color.RGBA{220, 40, 40, 255})
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// slide moves a picture back and forth three ways, for about four
// seconds each, and asks what the eye saw. The picture covers two cells
// by one, as a map tile does at size 1.
func slide() {
	out := os.Stdout
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		fmt.Fprintln(out, "tileprobe: cannot read the window size:", err)
		return
	}
	cellW, cellH := int(ws.Xpixel)/int(ws.Col), int(ws.Ypixel)/int(ws.Row)
	guessed := cellW == 0 || cellH == 0
	if guessed {
		cellW, cellH = 10, 20
	}

	const id, pid = 7101, 1
	at := func(row, col int) string { return fmt.Sprintf("\x1b[%d;%dH", row, col) }
	// place puts the picture with its top left corner px, py pixels from
	// the top left of cell (row, col), both counted from 1.
	place := func(row, col, px, py int) string {
		col += px / cellW
		row += py / cellH
		return at(row, col) + kitty(fmt.Sprintf("a=p,q=2,C=1,i=%d,p=%d,c=2,r=1,z=1,X=%d,Y=%d", id, pid, px%cellW, py%cellH), nil)
	}
	// ease turns a share of the way, 0 to 1, into an eased share.
	ease := func(t float64) float64 { return t * t * (3 - 2*t) }
	run := func(title string, frame func(t float64) string) {
		fmt.Fprint(out, "\x1b[2J"+at(1, 1)+title+"\r\n")
		// A track of dots under the picture, ten cells wide and three
		// rows tall, so a see-through picture shows them.
		for r := 0; r < 3; r++ {
			fmt.Fprint(out, at(4+r, 5)+"..........")
		}
		start := time.Now()
		for time.Since(start) < 4*time.Second {
			// Out and back once a second, as eased moves would be.
			p := math.Mod(time.Since(start).Seconds(), 1)
			t := p * 2
			if t > 1 {
				t = 2 - t
			}
			fmt.Fprint(out, frame(ease(t)))
			time.Sleep(time.Second / 60)
		}
		fmt.Fprint(out, kitty(fmt.Sprintf("a=d,d=i,i=%d", id), nil))
	}

	fmt.Fprint(out, "\x1b[?25l"+kitty(fmt.Sprintf("q=2,f=100,a=t,t=d,i=%d,s=16,v=16", id), sprite()))
	run("1 of 3. Across, sliding pixel by pixel over six cells.", func(t float64) string {
		return place(4, 5, int(t*float64(6*cellW)+0.5), 0)
	})
	run("2 of 3. Down, sliding pixel by pixel over two rows.", func(t float64) string {
		return place(4, 7, 0, int(t*float64(2*cellH)+0.5))
	})
	run("3 of 3. Across in whole cells, for comparison: this one should jump.", func(t float64) string {
		return place(4, 5, int(t*6+0.5)*cellW, 0)
	})
	fmt.Fprint(out, kitty(fmt.Sprintf("a=d,d=I,i=%d", id), nil)+"\x1b[?25h\x1b[2J"+at(1, 1))
	if guessed {
		fmt.Fprint(out, "The terminal did not give its cell size in pixels; guessed 10x20, so the slides may be off.\r\n")
	} else {
		fmt.Fprintf(out, "Cells are %dx%d pixels.\r\n", cellW, cellH)
	}
	fmt.Fprint(out, "Did tests 1 and 2 glide smoothly, while test 3 jumped a cell at a time?\r\n"+
		"Did the dots show through the disc's corners?\r\n")
}

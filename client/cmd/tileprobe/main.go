// Command tileprobe shows one picture several ways, each on a numbered
// line, to find out which image protocol features a terminal supports.
// Run it in the terminal you want to test and note which lines show a
// picture. It writes raw escape codes and does not use Limoni.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
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

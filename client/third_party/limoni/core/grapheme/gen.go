//go:build ignore

// gen.go builds tables.go from the Unicode Character Database.
//
//	go run gen.go -version 17.0.0            # downloads from unicode.org
//	go run gen.go -version 17.0.0 -dir ucd/  # uses local copies
//
// The tables are generated rather than written by hand because hand-written
// ranges are what this package replaces: they drift from the standard, and
// nobody notices until an emoji measures wrong.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	gcbOther = iota
	gcbCR
	gcbLF
	gcbControl
	gcbExtend
	gcbZWJ
	gcbRI
	gcbPrepend
	gcbSpacingMark
	gcbL
	gcbV
	gcbT
	gcbLV
	gcbLVT
)

var gcbNames = map[string]uint16{
	"CR": gcbCR, "LF": gcbLF, "Control": gcbControl, "Extend": gcbExtend, "ZWJ": gcbZWJ,
	"Regional_Indicator": gcbRI, "Prepend": gcbPrepend, "SpacingMark": gcbSpacingMark,
	"L": gcbL, "V": gcbV, "T": gcbT, "LV": gcbLV, "LVT": gcbLVT,
}

// Bit layout of a property word; mirrored by the constants in grapheme.go.
const (
	maskGCB      = 0x000F
	bitExtPict   = 1 << 4
	shiftInCB    = 5
	bitEmojiPres = 1 << 7
	bitEmoji     = 1 << 8
	shiftWidth   = 9
	widthZero    = 1
	widthWide    = 2
)

func main() {
	version := flag.String("version", "17.0.0", "Unicode version")
	dir := flag.String("dir", "", "directory holding the UCD files; downloads when empty")
	out := flag.String("out", "tables.go", "output file")
	flag.Parse()

	const size = 0x110000
	props := make([]uint16, size)
	// Default width is one column; the zero and wide classes are applied below.

	read := func(name string) []byte {
		if *dir != "" {
			data, err := os.ReadFile(filepath.Join(*dir, filepath.Base(name)))
			if err != nil {
				log.Fatal(err)
			}
			return data
		}
		url := "https://www.unicode.org/Public/" + *version + "/" + name
		resp, err := http.Get(url)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("%s: %s", url, resp.Status)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		return data
	}

	each := func(data []byte, fn func(lo, hi int, fields []string)) {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := scanner.Text()
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = line[:i]
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			parts := strings.Split(line, ";")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			span := strings.SplitN(parts[0], "..", 2)
			lo, err := strconv.ParseInt(span[0], 16, 32)
			if err != nil {
				log.Fatalf("bad line %q", line)
			}
			hi := lo
			if len(span) == 2 {
				if hi, err = strconv.ParseInt(span[1], 16, 32); err != nil {
					log.Fatalf("bad line %q", line)
				}
			}
			fn(int(lo), int(hi), parts[1:])
		}
	}

	each(read("ucd/auxiliary/GraphemeBreakProperty.txt"), func(lo, hi int, f []string) {
		value, ok := gcbNames[f[0]]
		if !ok {
			log.Fatalf("unknown Grapheme_Cluster_Break value %q", f[0])
		}
		for r := lo; r <= hi; r++ {
			props[r] = props[r]&^maskGCB | value
		}
	})

	each(read("ucd/emoji/emoji-data.txt"), func(lo, hi int, f []string) {
		var bit uint16
		switch f[0] {
		case "Extended_Pictographic":
			bit = bitExtPict
		case "Emoji_Presentation":
			bit = bitEmojiPres
		case "Emoji":
			bit = bitEmoji
		default:
			return
		}
		for r := lo; r <= hi; r++ {
			props[r] |= bit
		}
	})

	each(read("ucd/DerivedCoreProperties.txt"), func(lo, hi int, f []string) {
		if len(f) < 2 || f[0] != "InCB" {
			return
		}
		var value uint16
		switch f[1] {
		case "Consonant":
			value = 1
		case "Extend":
			value = 2
		case "Linker":
			value = 3
		default:
			log.Fatalf("unknown InCB value %q", f[1])
		}
		for r := lo; r <= hi; r++ {
			props[r] |= value << shiftInCB
		}
	})

	wide := make([]bool, size)
	eaw := read("ucd/EastAsianWidth.txt")
	// @missing lines give the default for unassigned code points: whole CJK
	// blocks default to Wide, so a character added to them later still
	// measures correctly. They are comments, so each() skips them; apply them
	// first and let the explicit entries override.
	for _, line := range strings.Split(string(eaw), "\n") {
		const prefix = "# @missing:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, prefix)), ";")
		if len(parts) != 2 {
			continue
		}
		value := strings.TrimSpace(parts[1])
		span := strings.SplitN(strings.TrimSpace(parts[0]), "..", 2)
		lo, _ := strconv.ParseInt(span[0], 16, 32)
		hi, _ := strconv.ParseInt(span[1], 16, 32)
		for r := lo; r <= hi; r++ {
			wide[r] = value == "W" || value == "F"
		}
	}
	each(eaw, func(lo, hi int, f []string) {
		for r := lo; r <= hi; r++ {
			wide[r] = f[0] == "W" || f[0] == "F"
		}
	})

	// Nonspacing and enclosing marks and format characters take no column,
	// whatever their East Asian Width. This is the classification wcwidth
	// uses. Grapheme_Cluster_Break=Extend is not a substitute: it includes the
	// halfwidth katakana voiced marks (U+FF9E, U+FF9F), which are spacing.
	zero := make([]bool, size)
	each(read("ucd/extracted/DerivedGeneralCategory.txt"), func(lo, hi int, f []string) {
		if f[0] == "Mn" || f[0] == "Me" || f[0] == "Cf" {
			for r := lo; r <= hi; r++ {
				zero[r] = true
			}
		}
	})

	for r := 0; r < size; r++ {
		p := props[r]
		gcb := p & maskGCB
		var class uint16
		switch {
		case gcb == gcbCR || gcb == gcbLF || gcb == gcbControl:
			class = widthZero
		case zero[r]:
			class = widthZero
		case gcb == gcbV || gcb == gcbT:
			// Hangul medial vowels and final consonants join the preceding
			// syllable rather than occupying a column of their own.
			class = widthZero
		case wide[r] || p&bitEmojiPres != 0:
			class = widthWide
		}
		props[r] = p | class<<shiftWidth
	}

	type span struct {
		lo, hi int
		props  uint16
	}
	var spans []span
	for r := 0; r < size; {
		start := r
		for r < size && props[r] == props[start] {
			r++
		}
		if props[start] != 0 {
			spans = append(spans, span{start, r - 1, props[start]})
		}
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by go run gen.go -version %s; DO NOT EDIT.\n\n", *version)
	b.WriteString("package grapheme\n\n")
	fmt.Fprintf(&b, "// UnicodeVersion is the version of the Unicode Character Database these\n// tables were generated from.\nconst UnicodeVersion = %q\n\n", *version)
	b.WriteString("// propTable holds every code point range whose properties are not all\n// zero, sorted. A code point outside every range is Other, not pictographic,\n// with no conjunct role, and one column wide.\n")
	b.WriteString("var propTable = [...]propRange{\n")
	for _, s := range spans {
		fmt.Fprintf(&b, "\t{0x%04X, 0x%04X, 0x%04X},\n", s.lo, s.hi, s.props)
	}
	b.WriteString("}\n")

	src, err := format.Source(b.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d ranges to %s", len(spans), *out)
}

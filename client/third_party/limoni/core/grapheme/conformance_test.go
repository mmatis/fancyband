package grapheme

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestUnicodeConformance runs every case in the Unicode Consortium's
// GraphemeBreakTest.txt for the same version the tables were generated from.
// Tests written by hand prove the author's understanding of the rules; this
// file proves the rules.
func TestUnicodeConformance(t *testing.T) {
	file, err := os.Open("testdata/GraphemeBreakTest.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	cases, failures := 0, 0
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = text[:i]
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		cases++

		var input strings.Builder
		var want []string
		var current strings.Builder
		for _, field := range strings.Fields(text) {
			switch field {
			case "÷":
				if current.Len() > 0 {
					want = append(want, current.String())
					current.Reset()
				}
			case "×":
			default:
				cp, err := strconv.ParseUint(field, 16, 32)
				if err != nil {
					t.Fatalf("line %d: bad code point %q", line, field)
				}
				current.WriteRune(rune(cp))
				input.WriteRune(rune(cp))
			}
		}

		var got []string
		for s := input.String(); s != ""; {
			var cluster string
			cluster, _, s = Next(s)
			got = append(got, cluster)
		}
		if !equal(got, want) {
			failures++
			if failures <= 10 {
				t.Errorf("line %d: %s\n  want %q\n  got  %q", line, scanner.Text(), want, got)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if cases < 700 {
		t.Fatalf("only %d cases read; the test file is not the one expected", cases)
	}
	if failures > 0 {
		t.Fatalf("%d of %d Unicode conformance cases failed", failures, cases)
	}
	t.Logf("all %d Unicode %s conformance cases pass", cases, UnicodeVersion)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

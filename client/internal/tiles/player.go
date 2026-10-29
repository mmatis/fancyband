package tiles

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fancyband/internal/proto"
)

// PlayerTile returns the tile a tile set gives a character of race and
// class, as the engine would pick it: by reading the set's preference
// files, with their "?:" conditions on $RACE and $CLASS, and taking the
// last "monster:<player>" line that applies. The set is found in the
// list.txt beside its folder, by the TILESET's id.
func PlayerTile(ts proto.TileSet, race, class string) (proto.PictCell, bool) {
	dir := filepath.Dir(ts.Path)
	pref, err := prefName(filepath.Join(filepath.Dir(dir), "list.txt"), int(ts.ID))
	if err != nil {
		return proto.PictCell{}, false
	}
	e := &prefEval{vars: map[string]string{"RACE": race, "CLASS": class, "SYS": "net"}}
	e.file(filepath.Join(dir, pref), 0)
	return e.tile, e.found
}

// prefName finds the preference file of tile set id in list.txt.
func prefName(list string, id int) (string, error) {
	f, err := os.Open(list)
	if err != nil {
		return "", err
	}
	defer f.Close()
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if !ok {
			continue
		}
		switch key {
		case "name":
			n, _, _ := strings.Cut(val, ":")
			in = n == strconv.Itoa(id)
		case "pref":
			if in {
				return val, nil
			}
		}
	}
	return "", fmt.Errorf("tiles: no tile set %d in %s", id, list)
}

// prefEval reads preference files for the player's tile.
type prefEval struct {
	vars  map[string]string
	tile  proto.PictCell
	found bool
}

// file reads one preference file. As in the engine, a "?:" line that is
// false skips the lines after it, up to the next "?:" line, and "%:"
// includes another file from the same folder.
func (e *prefEval) file(path string, depth int) {
	if depth > 8 {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	skip := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "?:"):
			skip = evalPrefExpr(line[2:], e.vars) == "0"
		case skip:
		case strings.HasPrefix(line, "%:"):
			e.file(filepath.Join(filepath.Dir(path), strings.TrimSpace(line[2:])), depth+1)
		case strings.HasPrefix(line, "monster:<player>:"):
			fields := strings.Split(strings.Fields(line)[0], ":")
			if len(fields) < 4 {
				continue
			}
			a, err1 := strconv.ParseUint(fields[2], 0, 8)
			c, err2 := strconv.ParseUint(fields[3], 0, 8)
			if err1 != nil || err2 != nil || a&0x80 == 0 {
				continue
			}
			row, col := uint8(a&0x7F), uint8(c&0x7F)
			e.tile = proto.PictCell{FgRow: row, FgCol: col, BgRow: row, BgCol: col}
			e.found = true
		}
	}
}

// evalPrefExpr evaluates a preference file expression, such as
// "[AND [EQU $CLASS Warrior] [EQU $RACE Elf] ]", as
// process_pref_file_expr() in ui-prefs.c does. "0" is false.
func evalPrefExpr(s string, vars map[string]string) string {
	toks := strings.Fields(strings.NewReplacer("[", " [ ", "]", " ] ").Replace(s))
	v, _ := evalTokens(toks, vars)
	return v
}

// evalTokens evaluates the expression at the start of toks and returns
// its value and the tokens after it.
func evalTokens(toks []string, vars map[string]string) (string, []string) {
	if len(toks) == 0 {
		return "", nil
	}
	t, rest := toks[0], toks[1:]
	if t != "[" {
		if strings.HasPrefix(t, "$") {
			if v, ok := vars[t[1:]]; ok {
				return v, rest
			}
			// The engine's value for a variable it does not know.
			return "?o?o?", rest
		}
		return t, rest
	}
	if len(rest) == 0 {
		return "?x?x?", nil
	}
	op, rest := rest[0], rest[1:]
	var args []string
	for len(rest) > 0 && rest[0] != "]" {
		var a string
		a, rest = evalTokens(rest, vars)
		args = append(args, a)
	}
	if len(rest) == 0 {
		return "?x?x?", nil
	}
	rest = rest[1:]
	truth := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	switch op {
	case "IOR":
		for _, a := range args {
			if a != "0" {
				return "1", rest
			}
		}
		return "0", rest
	case "AND":
		for _, a := range args {
			if a == "0" {
				return "0", rest
			}
		}
		return "1", rest
	case "NOT":
		for _, a := range args {
			if a != "0" {
				return "0", rest
			}
		}
		return "1", rest
	case "EQU", "LEQ", "GEQ":
		ok := true
		for i := 1; i < len(args); i++ {
			switch op {
			case "EQU":
				ok = ok && args[i-1] == args[i]
			case "LEQ":
				ok = ok && args[i-1] < args[i]
			case "GEQ":
				ok = ok && args[i-1] > args[i]
			}
		}
		return truth(ok), rest
	}
	return "?o?o?", rest
}

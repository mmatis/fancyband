package tiles

import (
	"os"
	"path/filepath"
	"testing"

	"fancyband/internal/proto"
)

func TestEvalPrefExpr(t *testing.T) {
	vars := map[string]string{"RACE": "Elf", "CLASS": "Warrior"}
	cases := map[string]string{
		"1": "1",
		"[AND [EQU $CLASS Warrior] [EQU $RACE Elf] ]":      "1",
		"[AND [EQU $CLASS Warrior] [EQU $RACE Human] ]":    "0",
		"[IOR [EQU $RACE Human] [EQU $RACE Elf]]":          "1",
		"[NOT [EQU $RACE Elf]]":                            "0",
		"[AND [EQU $CLASS Warrior] [EQU $GENDER Female] ]": "0",
	}
	for expr, want := range cases {
		if got := evalPrefExpr(expr, vars); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}

// TestPlayerTile reads the real tile sets: Adam Bolt's set gives each
// race and class its own tile, and its rules sit in an included file.
func TestPlayerTile(t *testing.T) {
	lib, _ := filepath.Abs("../../../engine/lib/tiles")
	if _, err := os.Stat(filepath.Join(lib, "list.txt")); err != nil {
		t.Skip("no engine tiles in this checkout")
	}
	ab := proto.TileSet{ID: 2, Path: filepath.Join(lib, "adam-bolt", "16x16.png")}
	human, ok := PlayerTile(ab, "Human", "Warrior")
	if !ok || human.FgRow != 0x12 || human.FgCol != 0x00 {
		t.Errorf("Adam Bolt's Human Warrior = %+v, %v; want row 0x12, column 0", human, ok)
	}
	elf, _ := PlayerTile(ab, "Elf", "Warrior")
	if elf == human {
		t.Error("an Elf Warrior has the Human Warrior's tile")
	}
	for _, id := range []uint8{1, 3, 4, 5, 6} {
		ts := proto.TileSet{ID: id}
		dirs := map[uint8]string{1: "old", 3: "gervais", 4: "nomad", 5: "shockbolt", 6: "shockbolt"}
		ts.Path = filepath.Join(lib, dirs[id], "sheet.png")
		if _, ok := PlayerTile(ts, "Human", "Mage"); !ok {
			t.Errorf("tile set %d has no tile for a Human Mage", id)
		}
	}
}

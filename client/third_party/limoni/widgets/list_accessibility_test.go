package widgets

import (
	"testing"

	"github.com/thebanri/limoni/core/accessibility"
	"github.com/thebanri/limoni/core/buffer"
	"github.com/thebanri/limoni/core/cell"
)

// An agent or a test can only act on a row it can address. A flat list node
// said "3 of 20" and nothing else, so reaching a particular row took a guess
// at how many times to press down.
func TestListExposesItsVisibleRows(t *testing.T) {
	items := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	state := &ListState{Selected: 3, Offset: 0}
	list := List{ID: "l", Label: "Letters", Items: items, State: state}
	area := cell.NewRect(4, 2, 20, 3)
	buf := buffer.NewBuffer(cell.NewRect(0, 0, 40, 10))
	list.Draw(cell.Context{Area: area}, buf)

	node := list.AccessibilityNode(area, false)
	if node.Label != "Letters" {
		t.Errorf("label = %q, want Letters", node.Label)
	}
	// Three rows fit, and the selection at index 3 scrolls the view to 1..3.
	want := []string{"beta", "gamma", "delta"}
	if len(node.Children) != len(want) {
		t.Fatalf("children = %d, want %d: %+v", len(node.Children), len(want), node.Children)
	}
	for i, row := range node.Children {
		if row.Role != accessibility.RoleListItem || row.Label != want[i] {
			t.Errorf("row %d = %v %q, want list-item %q", i, row.Role, row.Label, want[i])
		}
		if row.Bounds != cell.NewRect(4, 2+uint16(i), 20, 1) {
			t.Errorf("row %d bounds = %v", i, row.Bounds)
		}
		if row.Position != i+2 || row.SetSize != len(items) {
			t.Errorf("row %d position = %d/%d, want %d/%d", i, row.Position, row.SetSize, i+2, len(items))
		}
		selected := row.State&accessibility.StateSelected != 0
		if selected != (want[i] == "delta") {
			t.Errorf("row %q selected = %t", row.Label, selected)
		}
	}
}

func TestListRowsLeaveRoomForTheScrollbar(t *testing.T) {
	list := List{Items: []string{"a", "b", "c", "d"}, Scrollbar: true, State: &ListState{Selected: 0}}
	area := cell.NewRect(0, 0, 10, 2)
	list.Draw(cell.Context{Area: area}, buffer.NewBuffer(area))
	node := list.AccessibilityNode(area, false)
	if len(node.Children) != 2 || node.Children[0].Bounds.Width != 9 {
		t.Errorf("rows = %+v, want 2 rows 9 wide", node.Children)
	}
}

func TestListDefaultsItsLabel(t *testing.T) {
	node := List{Items: []string{"a"}}.AccessibilityNode(cell.NewRect(0, 0, 5, 1), false)
	if node.Label != "List" {
		t.Errorf("label = %q, want List", node.Label)
	}
}

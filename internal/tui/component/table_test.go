package component

import (
	"testing"

	"github.com/Breina/Jenking/internal/tui/theme"
)

func newTestTable(rowCount int) Table {
	th := theme.Default()
	columns := []Column{{Title: "A", Width: 10}}
	var rows []Row
	for i := 0; i < rowCount; i++ {
		rows = append(rows, Row{string(rune('0' + i))})
	}
	tbl := NewTable(th, columns)
	tbl.SetRows(rows)
	return tbl
}

func TestTable_DisabledRows_MoveDown(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{1: true, 2: true})
	tbl.SetCursor(0)

	tbl.MoveDown()

	if got := tbl.Cursor(); got != 3 {
		t.Errorf("MoveDown: cursor = %d, want 3 (should skip disabled rows 1 and 2)", got)
	}
}

func TestTable_DisabledRows_MoveUp(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{2: true, 3: true})
	tbl.SetCursor(4)

	tbl.MoveUp()

	if got := tbl.Cursor(); got != 1 {
		t.Errorf("MoveUp: cursor = %d, want 1 (should skip disabled rows 2 and 3)", got)
	}
}

func TestTable_DisabledRows_SetCursor(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{2: true})

	tbl.SetCursor(2)

	if got := tbl.Cursor(); got != 3 {
		t.Errorf("SetCursor(2): cursor = %d, want 3 (should snap forward past disabled row)", got)
	}
}

func TestTable_DisabledRows_Home(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{0: true})
	tbl.SetCursor(4)

	tbl.Home()

	if got := tbl.Cursor(); got != 1 {
		t.Errorf("Home: cursor = %d, want 1 (should skip disabled row 0)", got)
	}
}

func TestTable_DisabledRows_End(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{4: true})
	tbl.SetCursor(0)

	tbl.End()

	if got := tbl.Cursor(); got != 3 {
		t.Errorf("End: cursor = %d, want 3 (should skip disabled row 4)", got)
	}
}

func TestTable_DisabledRows_AllDisabled(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true})
	tbl.SetCursor(0)

	tbl.MoveDown()

	if got := tbl.Cursor(); got != 0 {
		t.Errorf("MoveDown (all disabled): cursor = %d, want 0 (should stay put)", got)
	}
}

func TestTable_DisabledRows_MoveDown_StaysIfNoneAfter(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{3: true, 4: true})
	tbl.SetCursor(2)

	tbl.MoveDown()

	if got := tbl.Cursor(); got != 2 {
		t.Errorf("MoveDown (none after): cursor = %d, want 2 (should stay at current)", got)
	}
}

func TestTable_IsDisabled(t *testing.T) {
	tbl := newTestTable(5)
	tbl.SetDisabled(map[int]bool{2: true})

	if !tbl.IsDisabled(2) {
		t.Error("IsDisabled(2) = false, want true")
	}
	if tbl.IsDisabled(0) {
		t.Error("IsDisabled(0) = true, want false")
	}

	// Nil map case: new table without SetDisabled called.
	tbl2 := newTestTable(3)
	if tbl2.IsDisabled(0) {
		t.Error("IsDisabled(0) with nil map = true, want false")
	}
}

package component

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// bgColorRe matches a 256-color background SGR parameter (e.g. "48;5;238").
var bgColorRe = regexp.MustCompile(`48;5;\d+`)

// Column defines a table column.
type Column struct {
	Title string
	Width int
}

// Row is a slice of cell values.
type Row []string

// Table renders a selectable, scrollable table.
type Table struct {
	theme    theme.Theme
	columns  []Column
	rows     []Row
	cursor   int
	offset   int
	width    int
	height   int
	disabled map[int]bool
}

// NewTable creates a new table component.
func NewTable(t theme.Theme, columns []Column) Table {
	return Table{
		theme:   t,
		columns: columns,
		width:   80,
		height:  20,
	}
}

// SetTheme updates the theme used for rendering.
func (t *Table) SetTheme(th theme.Theme) {
	t.theme = th
}

// SetRows replaces the table data.
func (t *Table) SetRows(rows []Row) {
	t.rows = rows
	if t.cursor >= len(rows) {
		t.cursor = max(0, len(rows)-1)
	}
}

// SetSize updates the table dimensions and clamps the scroll offset so that
// rows aren't needlessly scrolled out of view (e.g. after a height increase).
func (t *Table) SetSize(w, h int) {
	t.width = w
	t.height = h
	visibleRows := t.height - 1
	if visibleRows < 1 {
		return
	}
	maxOffset := len(t.rows) - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
}

// SetColumnWidth updates the width of the column at index i.
func (t *Table) SetColumnWidth(i, w int) {
	if i >= 0 && i < len(t.columns) {
		t.columns[i].Width = w
	}
}

// ScrollOffset returns the index of the first visible row.
func (t *Table) ScrollOffset() int { return t.offset }

// TotalRows returns the total number of rows in the table.
func (t *Table) TotalRows() int { return len(t.rows) }

// ContentHeight returns the number of visible content rows (excluding header).
func (t *Table) ContentHeight() int { return max(0, t.height-1) }

// SetDisabled stores the set of disabled row indices. Pass nil to clear.
func (t *Table) SetDisabled(indices map[int]bool) {
	t.disabled = indices
}

// IsDisabled returns true if the row at idx is disabled.
func (t *Table) IsDisabled(idx int) bool {
	return t.disabled[idx]
}

// MoveUp moves the cursor up, skipping disabled rows.
func (t *Table) MoveUp() {
	for i := t.cursor - 1; i >= 0; i-- {
		if !t.IsDisabled(i) {
			t.cursor = i
			if t.cursor < t.offset {
				t.offset = t.cursor
			}
			return
		}
	}
}

// MoveDown moves the cursor down, skipping disabled rows.
func (t *Table) MoveDown() {
	for i := t.cursor + 1; i < len(t.rows); i++ {
		if !t.IsDisabled(i) {
			t.cursor = i
			visibleRows := t.height - 1
			if t.cursor >= t.offset+visibleRows {
				t.offset = t.cursor - visibleRows + 1
			}
			return
		}
	}
}

// Home moves the cursor to the first non-disabled row.
func (t *Table) Home() {
	for i := 0; i < len(t.rows); i++ {
		if !t.IsDisabled(i) {
			t.cursor = i
			t.offset = 0
			return
		}
	}
}

// End moves the cursor to the last non-disabled row.
func (t *Table) End() {
	if len(t.rows) == 0 {
		return
	}
	for i := len(t.rows) - 1; i >= 0; i-- {
		if !t.IsDisabled(i) {
			t.cursor = i
			visibleRows := t.height - 1
			if t.cursor >= visibleRows {
				t.offset = t.cursor - visibleRows + 1
			}
			return
		}
	}
}

// PageUp moves the cursor up by one page, snapping to a non-disabled row.
func (t *Table) PageUp() {
	pageSize := t.height - 1
	if pageSize < 1 {
		pageSize = 1
	}
	target := max(0, t.cursor-pageSize)
	// Scan backward from target for a non-disabled row.
	for i := target; i >= 0; i-- {
		if !t.IsDisabled(i) {
			t.cursor = i
			if t.cursor < t.offset {
				t.offset = t.cursor
			}
			return
		}
	}
	// If none found backward, scan forward.
	for i := target + 1; i < len(t.rows); i++ {
		if !t.IsDisabled(i) {
			t.cursor = i
			if t.cursor < t.offset {
				t.offset = t.cursor
			}
			return
		}
	}
}

// PageDown moves the cursor down by one page, snapping to a non-disabled row.
func (t *Table) PageDown() {
	if len(t.rows) == 0 {
		return
	}
	pageSize := t.height - 1
	if pageSize < 1 {
		pageSize = 1
	}
	target := min(len(t.rows)-1, t.cursor+pageSize)
	// Scan forward from target for a non-disabled row.
	for i := target; i < len(t.rows); i++ {
		if !t.IsDisabled(i) {
			t.cursor = i
			visibleRows := t.height - 1
			if t.cursor >= t.offset+visibleRows {
				t.offset = t.cursor - visibleRows + 1
			}
			return
		}
	}
	// If none found forward, scan backward.
	for i := target - 1; i >= 0; i-- {
		if !t.IsDisabled(i) {
			t.cursor = i
			visibleRows := t.height - 1
			if t.cursor >= t.offset+visibleRows {
				t.offset = t.cursor - visibleRows + 1
			}
			return
		}
	}
}

// Cursor returns the current cursor index.
func (t *Table) Cursor() int {
	return t.cursor
}

// SetCursor moves the cursor to the given index, adjusting scroll offset.
// If the target row is disabled, it snaps to the nearest non-disabled row
// (preferring forward, then backward).
func (t *Table) SetCursor(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(t.rows) {
		idx = max(0, len(t.rows)-1)
	}
	// Snap to nearest non-disabled row: prefer forward, then backward.
	if t.IsDisabled(idx) {
		found := false
		for i := idx + 1; i < len(t.rows); i++ {
			if !t.IsDisabled(i) {
				idx = i
				found = true
				break
			}
		}
		if !found {
			for i := idx - 1; i >= 0; i-- {
				if !t.IsDisabled(i) {
					idx = i
					found = true
					break
				}
			}
		}
		if !found {
			return // all rows disabled, stay put
		}
	}
	t.cursor = idx
	visibleRows := t.height - 1
	if t.cursor < t.offset {
		t.offset = t.cursor
	} else if t.cursor >= t.offset+visibleRows {
		t.offset = t.cursor - visibleRows + 1
	}
}

// SelectedRow returns the currently selected row, or nil if empty.
func (t *Table) SelectedRow() Row {
	if len(t.rows) == 0 {
		return nil
	}
	return t.rows[t.cursor]
}

// View renders the table.
func (t Table) View() string {
	if len(t.columns) == 0 {
		return ""
	}

	var b strings.Builder

	// Header row
	var headerCells []string
	for _, col := range t.columns {
		cell := t.theme.Table.Header.Width(col.Width).MaxWidth(col.Width + 2).Inline(true).Render(col.Title)
		headerCells = append(headerCells, cell)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, headerCells...))
	b.WriteByte('\n')

	// Data rows
	visibleRows := t.height - 1
	end := min(t.offset+visibleRows, len(t.rows))
	for i := t.offset; i < end; i++ {
		row := t.rows[i]
		selected := i == t.cursor
		style := t.theme.Table.Row
		if selected {
			style = t.theme.Table.Selected
		}
		var cells []string
		for j, col := range t.columns {
			val := ""
			if j < len(row) {
				val = row[j]
			}
			// Dual-rendered cells (normal\x1fselected) — pick the right variant.
			if idx := strings.IndexByte(val, '\x1f'); idx >= 0 {
				if selected {
					val = val[idx+1:]
				} else {
					val = val[:idx]
				}
			} else if selected {
				// Plain styled cells: recolor for selection.
				val = recolorForSelection(val, t.theme)
			}
			val = truncate(val, col.Width)
			cells = append(cells, style.Width(col.Width).MaxWidth(col.Width+2).Inline(true).Render(val))
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// recolorForSelection adapts pre-styled cell content for the selected row.
// It replaces all ANSI background colors with the selected background and
// swaps dark foreground colors (unreadable on the selected bg) with the
// selected foreground. Distinctive foreground colors (blue bars, amber
// overrun bars, green build numbers, etc.) are preserved.
func recolorForSelection(s string, th theme.Theme) string {
	selBg := colorString(th.Table.Selected.GetBackground())
	selFg := colorString(th.Table.Selected.GetForeground())
	if selBg == "" || selFg == "" {
		// Fallback: strip everything if we can't extract colors.
		return ansi.Strip(s)
	}

	// Replace all 256-color backgrounds with the selected background.
	s = bgColorRe.ReplaceAllString(s, "48;5;"+selBg)

	// Replace dark foregrounds that would be invisible on the selected bg.
	// These are the progress bar's dark text (232) and empty fill (238).
	for _, dark := range []string{"232", "238"} {
		s = strings.ReplaceAll(s, "38;5;"+dark, "38;5;"+selFg)
	}

	// After a full reset (\x1b[0m), re-apply the selected background so
	// subsequent characters don't fall back to the terminal default.
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0;48;5;"+selBg+"m")

	return s
}

// colorString extracts the 256-color number from a lipgloss TerminalColor.
// Returns "" if the color is not a simple 256-color value.
func colorString(c lipgloss.TerminalColor) string {
	if c == nil {
		return ""
	}
	// lipgloss.Color is a string type that implements TerminalColor.
	if s, ok := c.(lipgloss.Color); ok {
		return string(s)
	}
	return ""
}

// truncate shortens s to at most width visible columns, appending … if trimmed.
// Uses ansi.Truncate to correctly handle ANSI escape sequences in styled strings.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return ansi.Truncate(s, width-1, "…")
}

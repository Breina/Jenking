package component

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// Pane is a single tile in a PaneGrid. Render must return exactly `height`
// lines, each at most `width` visible columns wide; the grid pads/truncates
// defensively but panes are expected to respect the bounds.
type Pane interface {
	Title() string
	Render(width, height int) string
}

// ScrollablePane is an optional Pane that also consumes input. A PaneGrid
// routes key events only to the focused pane. Defined for reuse by future
// multi-pane views; the dashboard handles its single zoom control at the view
// level rather than through focus.
type ScrollablePane interface {
	Pane
	Update(tea.Msg) (ScrollablePane, tea.Cmd)
}

// PaneGrid arranges panes in a fixed grid of rows, each row holding one or more
// panes laid out left-to-right. It wraps every pane in a bordered box with the
// title in the top border (k9s style). Rows split the available height; panes
// within a row split that row's width.
type PaneGrid struct {
	theme theme.Theme
	rows  [][]Pane
}

// NewPaneGrid creates an empty grid with the given theme.
func NewPaneGrid(t theme.Theme) *PaneGrid { return &PaneGrid{theme: t} }

// SetTheme updates the theme used for borders and titles.
func (g *PaneGrid) SetTheme(t theme.Theme) { g.theme = t }

// SetRows replaces the grid layout.
func (g *PaneGrid) SetRows(rows [][]Pane) { g.rows = rows }

// View renders the whole grid to exactly `width`x`height`.
func (g *PaneGrid) View(width, height int) string {
	if len(g.rows) == 0 || width < 2 || height < 2 {
		return ""
	}
	rowHeights := distribute(height, len(g.rows))
	rowStrs := make([]string, 0, len(g.rows))
	for ri, row := range g.rows {
		rowStrs = append(rowStrs, g.renderRow(row, width, rowHeights[ri]))
	}
	return strings.Join(rowStrs, "\n")
}

func (g *PaneGrid) renderRow(row []Pane, width, height int) string {
	if len(row) == 0 {
		return strings.Repeat("\n", max(0, height-1))
	}
	colWidths := distribute(width, len(row))
	boxes := make([]string, 0, len(row))
	for ci, p := range row {
		boxes = append(boxes, g.box(p, colWidths[ci], height))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
}

// box renders a single pane inside a bordered box of exactly w×h.
func (g *PaneGrid) box(p Pane, w, h int) string {
	bs := g.theme.PanelBorder
	b := g.theme.Border
	innerW := w - 2
	innerH := h - 2
	if innerW < 1 || innerH < 1 {
		// Too small to draw a box; emit blank lines of the right height.
		return strings.TrimRight(strings.Repeat(strings.Repeat(" ", w)+"\n", h), "\n")
	}

	var sb strings.Builder
	sb.WriteString(g.topBorder(p.Title(), innerW))
	sb.WriteString("\n")

	content := p.Render(innerW, innerH)
	lines := strings.Split(content, "\n")
	for i := 0; i < innerH; i++ {
		var line string
		if i < len(lines) {
			line = padTruncate(lines[i], innerW)
		} else {
			line = strings.Repeat(" ", innerW)
		}
		sb.WriteString(bs.Render(b.Left))
		sb.WriteString(line)
		sb.WriteString(bs.Render(b.Right))
		sb.WriteString("\n")
	}

	sb.WriteString(bs.Render(b.BottomLeft + strings.Repeat(b.Bottom, innerW) + b.BottomRight))
	return sb.String()
}

// topBorder builds the top edge with the title embedded after one filler rune.
// The title is rendered verbatim so callers can embed per-segment ANSI colors
// (e.g. legend-colored titles); only the surrounding fill uses the border style.
func (g *PaneGrid) topBorder(title string, innerW int) string {
	bs := g.theme.PanelBorder
	b := g.theme.Border
	titleText := " " + title + " "
	if lipgloss.Width(titleText) > innerW {
		titleText = padTruncate(titleText, innerW)
	}
	tw := lipgloss.Width(titleText)
	left := 1
	if left > innerW-tw {
		left = 0
	}
	right := innerW - tw - left
	return bs.Render(b.TopLeft+strings.Repeat(b.Top, left)) +
		titleText +
		bs.Render(strings.Repeat(b.Top, right)+b.TopRight)
}

// padTruncate returns s adjusted to exactly w visible columns, preserving ANSI.
func padTruncate(s string, w int) string {
	vw := lipgloss.Width(s)
	if vw > w {
		return ansi.Truncate(s, w, "")
	}
	if vw < w {
		return s + strings.Repeat(" ", w-vw)
	}
	return s
}

// distribute splits total into n parts as evenly as possible, giving the
// remainder to the earliest parts.
func distribute(total, n int) []int {
	if n <= 0 {
		return nil
	}
	base := total / n
	rem := total % n
	out := make([]int, n)
	for i := range out {
		out[i] = base
		if i < rem {
			out[i]++
		}
	}
	return out
}

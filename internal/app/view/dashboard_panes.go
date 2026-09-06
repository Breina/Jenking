package view

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// The dashboard tiles are small value types implementing component.Pane. Each
// captures already-computed data at layout time and builds its ntcharts chart
// (or renders its rows) on demand at the pane's actual size.

// activityPane renders running/queued as lines with throughput stacked bars
// overlaid on the same time axis.
type activityPane struct {
	title       string
	theme       theme.Theme
	now         time.Time
	window      time.Duration
	lines       []lineSeries
	completions []completionEvent
	segStyles   []lipgloss.Style
}

func (p activityPane) Title() string { return p.title }
func (p activityPane) Render(w, h int) string {
	return buildActivityChart(w, h, p.theme, p.now, p.window, p.lines, p.completions, p.segStyles)
}

// hbarPane renders a horizontal bar chart with value labels beside each bar.
type hbarPane struct {
	title string
	theme theme.Theme
	bars  []hbar
	empty string
}

func (p hbarPane) Title() string { return p.title }
func (p hbarPane) Render(w, h int) string {
	if len(p.bars) == 0 {
		return emptyMessage(w, h, p.empty)
	}
	return renderHBars(w, h, p.theme, p.bars)
}

// listPane renders label→value rows, value right-aligned and styled.
type dashRow struct {
	label string
	value string
	style lipgloss.Style
}

type listPane struct {
	title string
	rows  []dashRow
	empty string // message when there are no rows
}

func (p listPane) Title() string { return p.title }

func (p listPane) Render(w, h int) string {
	if len(p.rows) == 0 {
		return emptyMessage(w, h, p.empty)
	}
	lines := make([]string, h)
	for i := range lines {
		lines[i] = strings.Repeat(" ", w)
	}
	n := min(len(p.rows), h)
	for i := 0; i < n; i++ {
		r := p.rows[i]
		valW := lipgloss.Width(r.value)
		labW := w - valW - 1
		if labW < 1 {
			labW = 1
		}
		lab := vtrunc(r.label, labW)
		gap := w - lipgloss.Width(lab) - valW
		if gap < 1 {
			gap = 1
		}
		line := lab + strings.Repeat(" ", gap) + r.style.Render(r.value)
		lines[i] = vpad(vtrunc(line, w), w)
	}
	return strings.Join(lines, "\n")
}

// emptyMessage renders a single-line placeholder padded to w×h.
func emptyMessage(w, h int, msg string) string {
	lines := make([]string, h)
	for i := range lines {
		lines[i] = strings.Repeat(" ", w)
	}
	if h > 0 && msg != "" {
		lines[0] = vpad(vtrunc(msg, w), w)
	}
	return strings.Join(lines, "\n")
}

// vtrunc truncates s to at most w visible columns, preserving ANSI.
func vtrunc(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// vpad right-pads s to exactly w visible columns.
func vpad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// compile-time assertions that panes satisfy component.Pane.
var (
	_ component.Pane = activityPane{}
	_ component.Pane = hbarPane{}
	_ component.Pane = listPane{}
	_ component.Pane = nodeMatrixPane{}
)

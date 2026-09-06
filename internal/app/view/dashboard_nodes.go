package view

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// nodeMatrixPane renders nodes as a grid of colored health cells, tinted by
// controller→agent response time (green healthy → red slow/offline).
type nodeMatrixPane struct {
	title string
	theme theme.Theme
	nodes []jmodel.Node
	empty string
}

func (p nodeMatrixPane) Title() string { return p.title }
func (p nodeMatrixPane) Render(w, h int) string {
	if len(p.nodes) == 0 {
		return emptyMessage(w, h, p.empty)
	}
	return renderNodeMatrix(w, h, p.theme, p.nodes)
}

const nodeCellW = 18

// renderNodeMatrix lays out one colored cell per node, wrapping into rows that
// fit the width, with a blank line between cell rows for breathing room.
func renderNodeMatrix(w, h int, th theme.Theme, nodes []jmodel.Node) string {
	perRow := (w + 1) / (nodeCellW + 1)
	if perRow < 1 {
		perRow = 1
	}
	cells := make([]string, len(nodes))
	for i, n := range nodes {
		cells[i] = nodeCell(th, n)
	}

	var rows []string
	for i := 0; i < len(cells); i += perRow {
		end := i + perRow
		if end > len(cells) {
			end = len(cells)
		}
		if len(rows) > 0 {
			rows = append(rows, "") // blank separator line between cell rows
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, spaced(cells[i:end])...))
	}
	return strings.Join(rows, "\n")
}

// nodeCell renders a node as a 3-line colored box: name, a blank spacer row, and
// the latency (or "offline"). Color reflects response-time health.
func nodeCell(th theme.Theme, n jmodel.Node) string {
	color, metric := nodeHealth(th, n)
	name := vtrunc(shortName(decodeName(n.Name)), nodeCellW-2)
	body := name + "\n\n" + metric
	return lipgloss.NewStyle().
		Width(nodeCellW).
		Height(3).
		Padding(0, 1).
		Align(lipgloss.Left).
		Background(color).
		Foreground(lipgloss.Color("232")).
		Bold(true).
		Render(body)
}

// nodeHealth maps a node to a health color and its latency text. Offline nodes
// are red; otherwise the latency is always printed and colored by threshold.
func nodeHealth(th theme.Theme, n jmodel.Node) (lipgloss.TerminalColor, string) {
	latency := strconv.FormatInt(n.ResponseMs, 10) + "ms"
	switch {
	case n.Offline:
		return th.BuildStatus.Failed.GetForeground(), "offline"
	case n.ResponseMs < 200:
		return th.BuildStatus.Success.GetForeground(), latency
	case n.ResponseMs < 1000:
		return th.BuildStatus.Unstable.GetForeground(), latency
	default:
		return th.BuildStatus.Failed.GetForeground(), latency
	}
}

// spaced inserts a one-column gap between cells for horizontal joining.
func spaced(cells []string) []string {
	if len(cells) == 0 {
		return cells
	}
	out := make([]string, 0, len(cells)*2-1)
	for i, c := range cells {
		if i > 0 {
			out = append(out, " ")
		}
		out = append(out, c)
	}
	return out
}

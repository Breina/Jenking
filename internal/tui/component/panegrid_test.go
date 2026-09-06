package component

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

type fakePane struct {
	title string
	fill  rune
}

func (p fakePane) Title() string { return p.title }
func (p fakePane) Render(width, height int) string {
	line := strings.Repeat(string(p.fill), width)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func assertBox(t *testing.T, s string, width, height int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != height {
		t.Fatalf("got %d lines, want %d", len(lines), height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d width = %d, want %d (%q)", i, w, width, ln)
		}
	}
}

func TestPaneGridDimensions(t *testing.T) {
	g := NewPaneGrid(theme.Default())
	g.SetRows([][]Pane{
		{fakePane{"top", 'a'}},
		{fakePane{"l", 'b'}, fakePane{"r", 'c'}},
	})
	const w, h = 40, 12
	assertBox(t, g.View(w, h), w, h)
}

func TestPaneGridTooSmall(t *testing.T) {
	g := NewPaneGrid(theme.Default())
	g.SetRows([][]Pane{{fakePane{"x", 'a'}}})
	if got := g.View(1, 1); got != "" {
		t.Errorf("want empty for undersized grid, got %q", got)
	}
}

func TestDistribute(t *testing.T) {
	got := distribute(10, 3)
	if len(got) != 3 || got[0] != 4 || got[1] != 3 || got[2] != 3 {
		t.Errorf("distribute(10,3) = %v", got)
	}
	sum := 0
	for _, v := range distribute(41, 4) {
		sum += v
	}
	if sum != 41 {
		t.Errorf("distribute must preserve total, got sum %d", sum)
	}
}

func TestPaneGridTitleInBorder(t *testing.T) {
	g := NewPaneGrid(theme.Default())
	// A composite (potentially colored) title must appear verbatim in the top
	// border and not break the box geometry.
	g.SetRows([][]Pane{{fakePane{"Run / Queue", 'a'}}})
	out := g.View(24, 4)
	if !strings.Contains(out, "Run / Queue") {
		t.Errorf("expected title text in border, got:\n%s", out)
	}
	assertBox(t, out, 24, 4)
}

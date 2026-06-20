package view

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// RenderHelpDialog renders the :help overlay centred on top of bg.
// cmds should be the visible (non-hidden) command list from the registry.
func RenderHelpDialog(t theme.Theme, cmds []command.Command, bg string, width, height int) string {
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })

	accentColor := t.Popup.Title.GetForeground()
	titleStyle := t.Popup.Title.Align(lipgloss.Center)
	nameStyle := lipgloss.NewStyle().Foreground(accentColor)
	footerStyle := lipgloss.NewStyle().Faint(true)

	// Compute column widths.
	maxName, maxAlias := 0, 0
	for _, cmd := range cmds {
		if n := len(cmd.Name); n > maxName {
			maxName = n
		}
		if a := helpAlias(cmd); len(a) > maxAlias {
			maxAlias = len(a)
		}
	}

	var rows []string
	for _, cmd := range cmds {
		alias := helpAlias(cmd)
		namePart := fmt.Sprintf(":%-*s", maxName, cmd.Name)
		aliasPart := fmt.Sprintf("%-*s", maxAlias, alias)
		rows = append(rows, nameStyle.Render(namePart)+"  "+aliasPart+"  "+cmd.Help)
	}

	lines := []string{titleStyle.Render("Commands"), ""}
	lines = append(lines, rows...)
	lines = append(lines, "", footerStyle.Render("any key to close"))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 3).
		Render(content)

	return overlayCenter(bg, box, width, height)
}

// helpAlias returns the first (short) alias for display in :help.
// Plural/singular aliases are appended after the short one, so Aliases[0] is always short.
func helpAlias(cmd command.Command) string {
	if len(cmd.Aliases) > 0 {
		return cmd.Aliases[0]
	}
	return ""
}

// RenderUpdateConfirmDialog renders a centered dialog asking the user to confirm
// an in-place update from currentVersion to latestVersion.
// confirmYes controls which button is pre-selected.
func RenderUpdateConfirmDialog(t theme.Theme, bg string, width, height int, currentVersion, latestVersion string, confirmYes bool) string {
	body := fmt.Sprintf("Update from  %s  →  %s ?",
		lipgloss.NewStyle().Faint(true).Render(currentVersion),
		lipgloss.NewStyle().Bold(true).Render(latestVersion),
	)
	return renderConfirmDialog(t, bg, width, height, "Update Available", body, confirmYes)
}

// RenderUpdatingDialog renders a centered overlay indicating an update is in progress.
func RenderUpdatingDialog(t theme.Theme, bg string, width, height int) string {
	accentColor := t.Popup.Title.GetForeground()
	content := lipgloss.JoinVertical(lipgloss.Center,
		t.Popup.Title.Align(lipgloss.Center).Render("Updating…"),
		"",
		lipgloss.NewStyle().Faint(true).Render("Downloading and replacing binary, please wait."),
	)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 4).
		Render(content)
	return overlayCenter(bg, box, width, height)
}

// renderConfirmDialog renders a k9s-style centered confirmation dialog
// overlaid on top of the background content (tableView).
// confirmYes controls which button is highlighted.
func renderConfirmDialog(t theme.Theme, tableView string, width, height int, title, body string, confirmYes bool) string {
	return overlayCenter(tableView, widget.RenderConfirmBox(t, title, body, confirmYes), width, height)
}

// OverlayPopup centres popup over bg at full terminal dimensions (w×h).
// Used by the app to render view-level popups over the fully assembled screen.
func OverlayPopup(bg, popup string, w, h int) string {
	return overlayCenter(bg, popup, w, h)
}

// overlayCenter places overlay in the center of background (bg), which is
// expected to be width×height characters wide. ANSI escape sequences in the
// background are preserved.
func overlayCenter(bg, overlay string, bgWidth, bgHeight int) string {
	overlayLines := strings.Split(overlay, "\n")
	oh := len(overlayLines)
	ow := 0
	for _, l := range overlayLines {
		if w := visualLen(l); w > ow {
			ow = w
		}
	}

	startRow := (bgHeight - oh) / 2
	startCol := (bgWidth - ow) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	bgLines := strings.Split(bg, "\n")
	// Pad to bgHeight lines
	for len(bgLines) < bgHeight {
		bgLines = append(bgLines, "")
	}

	result := make([]string, bgHeight)
	for i := range result {
		result[i] = bgLines[i]
	}

	for i, ol := range overlayLines {
		row := startRow + i
		if row >= bgHeight {
			break
		}
		left := ansiTakeLeft(result[row], startCol)
		right := ansiTakeRange(result[row], startCol+ow, bgWidth)
		result[row] = left + ol + right
	}

	return strings.Join(result, "\n")
}

// visualLen returns the number of visible terminal columns in s, accounting for
// wide (multi-cell) glyphs and grapheme clusters, and ignoring ANSI escapes.
// It is consistent with lipgloss.Width (both measure via x/ansi).
func visualLen(s string) int {
	return ansi.StringWidth(s)
}

// ansiTakeLeft returns the leftmost n visible columns of s, padding with spaces
// if s is narrower than n. ANSI styling is preserved and reset at the cut.
// A wide glyph straddling the cut is dropped and the gap filled with spaces.
func ansiTakeLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := ansi.Truncate(s, n, "")
	if w := ansi.StringWidth(out); w < n {
		out += strings.Repeat(" ", n-w)
	}
	return out
}

// ansiTakeRange returns visible columns [from, to) of s, preserving ANSI escapes
// and padding to the full (to-from) width.
func ansiTakeRange(s string, from, to int) string {
	if from >= to {
		return ""
	}
	return ansiTakeLeft(ansiTakeRight(s, from), to-from)
}

// ansiTakeRight returns the visible content starting at column skip, replaying
// any SGR styling active at that point so colours aren't lost.
func ansiTakeRight(s string, skip int) string {
	if skip <= 0 {
		return s
	}
	return ansi.TruncateLeft(s, skip, "")
}

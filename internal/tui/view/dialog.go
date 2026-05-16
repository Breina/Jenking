package view

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/theme"
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

// renderConfirmBox builds the styled box for a confirmation dialog.
// It returns the rendered box string without positioning it on any background.
func renderConfirmBox(t theme.Theme, title, body string, confirmYes bool) string {
	titleStyle := t.Popup.Title
	accentColor := t.Popup.Title.GetForeground()

	baseBtn := lipgloss.NewStyle().Bold(true).Padding(0, 2)
	selectedBtn := baseBtn.Background(accentColor).Foreground(t.Popup.Accent.GetForeground())

	// Use raw ANSI underline for mnemonics so it composes with lipgloss
	// background/foreground without nested style issues.
	underline := func(c string) string { return "\033[4m" + c + "\033[24m" }

	var yesLabel, noLabel string
	if confirmYes {
		yesLabel = selectedBtn.Render(underline("Y") + "es")
		noLabel = baseBtn.Render(underline("N") + "o")
	} else {
		yesLabel = baseBtn.Render(underline("Y") + "es")
		noLabel = selectedBtn.Render(underline("N") + "o")
	}

	buttons := lipgloss.JoinHorizontal(lipgloss.Center,
		yesLabel,
		"  ",
		noLabel,
	)

	content := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render(title),
		"",
		body,
		"",
		buttons,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 4).
		Render(content)
}

// renderConfirmDialog renders a k9s-style centered confirmation dialog
// overlaid on top of the background content (tableView).
// confirmYes controls which button is highlighted.
func renderConfirmDialog(t theme.Theme, tableView string, width, height int, title, body string, confirmYes bool) string {
	return overlayCenter(tableView, renderConfirmBox(t, title, body, confirmYes), width, height)
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

// skipEsc advances i past the escape sequence starting at s[i] (which must be '\033').
// Handles CSI (\033[...final), OSC (\033]...BEL or \033\\), and 2-char sequences.
func skipEsc(s string, i int) int {
	i++ // skip ESC itself
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[': // CSI — ends at a byte in 0x40–0x7E (inclusive)
		i++
		for i < len(s) && (s[i] < 0x40 || s[i] > 0x7E) {
			i++
		}
		if i < len(s) {
			i++
		}
	case ']': // OSC — ends at BEL (\a) or ST (\033\\)
		i++
		for i < len(s) {
			if s[i] == '\007' {
				i++
				break
			}
			if s[i] == '\033' && i+1 < len(s) && s[i+1] == '\\' {
				i += 2
				break
			}
			i++
		}
	default: // 2-char sequence (\033c, \033M, …)
		i++
	}
	return i
}

// visualLen returns the number of visible columns in s (ignoring all ANSI escapes).
func visualLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == '\033' {
			i = skipEsc(s, i)
			continue
		}
		if s[i] < 0x20 { // other control chars — skip
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		n++
		i += size
	}
	return n
}

// ansiTakeLeft returns the leftmost n visible columns of s.
// All escape sequences are passed through unchanged; SGR colour state is closed
// with \033[0m if any colour was opened before the cut point.
func ansiTakeLeft(s string, n int) string {
	var out strings.Builder
	col := 0
	i := 0
	openSGR := false
	for i < len(s) && col < n {
		if s[i] == '\033' {
			j := skipEsc(s, i)
			seq := s[i:j]
			out.WriteString(seq)
			// Track SGR open state (CSI sequences only; resets close it).
			if i+1 < len(s) && s[i+1] == '[' {
				if seq == "\033[0m" || seq == "\033[m" {
					openSGR = false
				} else {
					openSGR = true
				}
			}
			i = j
			continue
		}
		if s[i] < 0x20 { // non-escape control chars: pass through, don't count
			out.WriteByte(s[i])
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		out.WriteString(s[i : i+size])
		col++
		i += size
	}
	for col < n {
		out.WriteByte(' ')
		col++
	}
	if openSGR {
		out.WriteString("\033[0m")
	}
	return out.String()
}

// ansiTakeRange returns visible columns [from, to) of s, preserving ANSI escapes.
func ansiTakeRange(s string, from, to int) string {
	if from >= to {
		return ""
	}
	mid := ansiTakeRight(s, from)
	return ansiTakeLeft(mid, to-from)
}

// ansiTakeRight returns the visible content starting at column skip.
// SGR sequences before the cut point are replayed so colours aren't lost;
// non-SGR sequences (OSC 8 hyperlinks, etc.) are discarded before the cut.
func ansiTakeRight(s string, skip int) string {
	var sgrPending strings.Builder // SGR sequences before cut point
	var out strings.Builder
	col := 0
	i := 0
	past := false
	for i < len(s) {
		if s[i] == '\033' {
			j := skipEsc(s, i)
			seq := s[i:j]
			if !past {
				// Only replay SGR sequences (CSI ending in 'm') — skip OSC etc.
				if i+1 < len(s) && s[i+1] == '[' {
					sgrPending.WriteString(seq)
				}
			} else {
				out.WriteString(seq)
			}
			i = j
			continue
		}
		if s[i] < 0x20 {
			if past {
				out.WriteByte(s[i])
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if col >= skip {
			if !past {
				past = true
				out.WriteString(sgrPending.String())
			}
			out.WriteString(s[i : i+size])
		}
		col++
		i += size
	}
	return out.String()
}

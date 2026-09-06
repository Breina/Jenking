package view

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

// renderConfirmDialog renders a k9s-style centered confirmation dialog
// overlaid on top of the background content (tableView).
// confirmYes controls which button is highlighted.
func renderConfirmDialog(t theme.Theme, tableView string, width, height int, title, body string, confirmYes bool) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	baseBtn := lipgloss.NewStyle().Bold(true).Padding(0, 2)
	selectedBtn := baseBtn.Background(lipgloss.Color("214")).Foreground(lipgloss.Color("0"))

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

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 4).
		Render(content)

	return overlayCenter(tableView, box, width, height)
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
		right := ansiTakeRight(result[row], startCol+ow)
		result[row] = left + ol + right
	}

	return strings.Join(result, "\n")
}

// visualLen returns the number of visible columns in s (ignoring ANSI escapes).
func visualLen(s string) int {
	n := 0
	inEsc := false
	for i := 0; i < len(s); {
		if inEsc {
			if s[i] == 'm' {
				inEsc = false
			}
			i++
			continue
		}
		if i+1 < len(s) && s[i] == '\033' && s[i+1] == '[' {
			inEsc = true
			i += 2
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		_ = r
		n++
		i += size
	}
	return n
}

// ansiTakeLeft returns the leftmost n visible columns of s, preserving ANSI
// escape sequences and appending a reset if any escape was open.
func ansiTakeLeft(s string, n int) string {
	var out strings.Builder
	col := 0
	i := 0
	openEsc := false
	for i < len(s) && col < n {
		// Consume any CSI sequences without counting columns
		if i+1 < len(s) && s[i] == '\033' && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++ // include 'm'
			}
			seq := s[i:j]
			out.WriteString(seq)
			openEsc = seq != "\033[0m" && seq != "\033[m"
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		_ = r
		out.WriteString(s[i : i+size])
		col++
		i += size
	}
	// Pad with spaces if we didn't reach n columns
	for col < n {
		out.WriteByte(' ')
		col++
	}
	if openEsc {
		out.WriteString("\033[0m")
	}
	return out.String()
}

// ansiTakeRight returns the visible content starting at column skip, preserving
// ANSI escapes. Escapes before skip are replayed so colours aren't lost.
func ansiTakeRight(s string, skip int) string {
	var pending strings.Builder // escape sequences before cut point
	var out strings.Builder
	col := 0
	i := 0
	past := false
	for i < len(s) {
		if i+1 < len(s) && s[i] == '\033' && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++
			}
			seq := s[i:j]
			if !past {
				pending.WriteString(seq)
			} else {
				out.WriteString(seq)
			}
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		_ = r
		if col >= skip {
			if !past {
				past = true
				out.WriteString(pending.String())
			}
			out.WriteString(s[i : i+size])
		}
		col++
		i += size
	}
	return out.String()
}

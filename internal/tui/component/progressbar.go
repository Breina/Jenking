package component

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// ProgressBar renders a styled progress bar string of exact visible width.
type ProgressBar struct {
	theme theme.Theme
}

// NewProgressBar creates a new progress bar with the given theme.
func NewProgressBar(t theme.Theme) ProgressBar {
	return ProgressBar{theme: t}
}

// SetTheme updates the theme used for rendering.
func (pb *ProgressBar) SetTheme(t theme.Theme) {
	pb.theme = t
}

// blockChars maps 0-7 eighths to Unicode block elements for sub-character granularity.
// Index 0 = empty (no fractional block), 1 = ▏ (1/8), ..., 7 = ▉ (7/8).
// A fully filled character is represented by █ directly.
var blockChars = [8]rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}

// Render returns a styled progress bar string of exactly `width` visible characters.
// When estimated is 0, returns a "● Running" fallback.
func (pb ProgressBar) Render(width int, elapsed, estimated time.Duration) string {
	if width <= 0 {
		return ""
	}
	if estimated <= 0 {
		return pb.renderFallback(width)
	}
	return pb.renderBar(width, elapsed, estimated, "")
}

// RenderWithText returns a styled progress bar with time-remaining text centered inside.
func (pb ProgressBar) RenderWithText(width int, elapsed, estimated time.Duration) string {
	if width <= 0 {
		return ""
	}
	if estimated <= 0 {
		return pb.renderFallback(width)
	}
	text := pb.timeText(elapsed, estimated)
	return pb.renderBar(width, elapsed, estimated, text)
}

// RenderWithTextTall returns a 3-line tall progress bar with text centered on the middle line.
// Top and bottom lines show a solid filled/empty bar without text.
func (pb ProgressBar) RenderWithTextTall(width int, elapsed, estimated time.Duration) string {
	if width <= 0 {
		return ""
	}
	middleLine := pb.RenderWithText(width, elapsed, estimated)
	topLine := pb.Render(width, elapsed, estimated)
	// Top and bottom are identical plain bars
	return topLine + "\n" + middleLine + "\n" + topLine
}

// renderFallback shows "● Running" when no estimate is available, padded to width.
func (pb ProgressBar) renderFallback(width int) string {
	text := "● Running"
	if lipgloss.Width(text) > width {
		text = "●"
	}
	padded := text + strings.Repeat(" ", max(0, width-lipgloss.Width(text)))
	return pb.theme.ProgressBar.Filled.Render(padded)
}

// timeText produces the overlay text for the bar.
func (pb ProgressBar) timeText(elapsed, estimated time.Duration) string {
	remaining := estimated - elapsed
	if remaining < 0 {
		over := elapsed - estimated
		return "+" + formatBarDuration(over)
	}
	return "~" + formatBarDuration(remaining)
}

// DualRenderWithText returns both normal and selected renderings of an inline
// progress bar, joined by \x1f. The table uses this to pick the right variant.
func (pb ProgressBar) DualRenderWithText(width int, elapsed, estimated time.Duration) string {
	if width <= 0 {
		return ""
	}
	if estimated <= 0 {
		fb := pb.renderFallback(width)
		return fb + "\x1f" + fb
	}
	normal := pb.RenderWithText(width, elapsed, estimated)
	sel := pb.renderBarInner(width, elapsed, estimated, pb.timeText(elapsed, estimated), true)
	return normal + "\x1f" + sel
}

// renderBar renders the actual bar with optional centered text.
func (pb ProgressBar) renderBar(width int, elapsed, estimated time.Duration, text string) string {
	return pb.renderBarInner(width, elapsed, estimated, text, false)
}

func (pb ProgressBar) renderBarInner(width int, elapsed, estimated time.Duration, text string, selected bool) string {
	ratio := float64(elapsed) / float64(estimated)
	overrun := ratio > 1.0
	if ratio > 1.0 {
		ratio = 1.0
	}

	var filledStyle, emptyStyle, filledTextStyle lipgloss.Style
	if selected {
		filledStyle = pb.theme.ProgressBar.SelFilled
		emptyStyle = pb.theme.ProgressBar.SelEmpty
		filledTextStyle = pb.theme.ProgressBar.SelFilledText
		if overrun {
			filledStyle = pb.theme.ProgressBar.SelOverrun
			filledTextStyle = pb.theme.ProgressBar.SelOverrunText
		}
	} else {
		filledStyle = pb.theme.ProgressBar.Filled
		emptyStyle = pb.theme.ProgressBar.Empty
		filledTextStyle = pb.theme.ProgressBar.FilledText
		if overrun {
			filledStyle = pb.theme.ProgressBar.Overrun
			filledTextStyle = pb.theme.ProgressBar.OverrunText
		}
	}

	// Calculate filled width in eighths for sub-character precision.
	totalEighths := width * 8
	filledEighths := int(ratio * float64(totalEighths))
	if filledEighths > totalEighths {
		filledEighths = totalEighths
	}
	fullBlocks := filledEighths / 8
	fractionalEighths := filledEighths % 8

	var emptyTextStyle lipgloss.Style
	if selected {
		emptyTextStyle = pb.theme.ProgressBar.SelEmptyText
	} else {
		emptyTextStyle = pb.theme.ProgressBar.EmptyText
	}

	if text == "" {
		return pb.renderPlainBar(width, fullBlocks, fractionalEighths, filledStyle, emptyStyle)
	}
	return pb.renderTextBar(width, fullBlocks, fractionalEighths, filledStyle, emptyStyle, filledTextStyle, emptyTextStyle, text)
}

// renderPlainBar renders a bar without text overlay.
func (pb ProgressBar) renderPlainBar(width, fullBlocks, fractionalEighths int, filledStyle, emptyStyle lipgloss.Style) string {
	var b strings.Builder

	// Filled portion
	if fullBlocks > 0 {
		b.WriteString(filledStyle.Render(strings.Repeat("█", fullBlocks)))
	}

	// Fractional block
	if fractionalEighths > 0 && fullBlocks < width {
		b.WriteString(filledStyle.Render(string(blockChars[fractionalEighths])))
		fullBlocks++ // account for the fractional character position
	}

	// Empty portion
	emptyCount := width - fullBlocks
	if emptyCount > 0 {
		b.WriteString(emptyStyle.Render(strings.Repeat(" ", emptyCount)))
	}

	return b.String()
}

// renderTextBar renders a bar with centered text overlay.
// Characters on the filled portion use filledTextStyle, characters on empty use emptyText style.
func (pb ProgressBar) renderTextBar(width, fullBlocks, fractionalEighths int, filledStyle, emptyStyle, filledTextStyle, emptyTextStyle lipgloss.Style, text string) string {
	textWidth := lipgloss.Width(text)
	if textWidth > width {
		// Text too wide, truncate
		text = text[:width]
		textWidth = width
	}

	// Center the text
	textStart := (width - textWidth) / 2

	// Build a character-by-character representation
	// Create the full bar content as a slice of runes
	bar := make([]rune, width)
	for i := range bar {
		if i < fullBlocks {
			bar[i] = '█'
		} else if i == fullBlocks && fractionalEighths > 0 {
			bar[i] = blockChars[fractionalEighths]
		} else {
			bar[i] = ' '
		}
	}

	// The boundary between filled and empty (in character positions)
	filledEnd := fullBlocks
	if fractionalEighths > 0 {
		filledEnd++
	}

	// Now render: before-text filled, text-on-filled, text-on-empty, after-text empty
	var b strings.Builder

	// Region before text
	if textStart > 0 {
		beforeFilled := min(textStart, filledEnd)
		if beforeFilled > 0 {
			b.WriteString(filledStyle.Render(string(bar[:beforeFilled])))
		}
		beforeEmpty := textStart - beforeFilled
		if beforeEmpty > 0 {
			b.WriteString(emptyStyle.Render(string(bar[beforeFilled:textStart])))
		}
	}

	// Text region — split at the filled boundary
	textEnd := textStart + textWidth
	textRunes := []rune(text)

	filledTextEnd := min(filledEnd, textEnd) - textStart
	if filledTextEnd < 0 {
		filledTextEnd = 0
	}
	if filledTextEnd > textWidth {
		filledTextEnd = textWidth
	}

	if filledTextEnd > 0 {
		b.WriteString(filledTextStyle.Render(string(textRunes[:filledTextEnd])))
	}
	if filledTextEnd < textWidth {
		b.WriteString(emptyTextStyle.Render(string(textRunes[filledTextEnd:])))
	}

	// Region after text
	afterStart := textEnd
	if afterStart < filledEnd {
		afterFilled := min(filledEnd, width) - afterStart
		if afterFilled > 0 {
			b.WriteString(filledStyle.Render(string(bar[afterStart : afterStart+afterFilled])))
		}
		afterStart += afterFilled
	}
	if afterStart < width {
		b.WriteString(emptyStyle.Render(string(bar[afterStart:width])))
	}

	return b.String()
}

// RenderComplete renders a fully filled bar with centered text in the given style.
func (pb ProgressBar) RenderComplete(width int, text string, style lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	textWidth := lipgloss.Width(text)
	if textWidth > width {
		text = text[:width]
		textWidth = width
	}
	textStart := (width - textWidth) / 2
	padLeft := textStart
	padRight := width - textStart - textWidth

	fgColor := style.GetForeground()
	blockStyle := lipgloss.NewStyle().Foreground(fgColor).Background(fgColor)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(fgColor)
	var b strings.Builder
	if padLeft > 0 {
		b.WriteString(blockStyle.Render(strings.Repeat("█", padLeft)))
	}
	b.WriteString(textStyle.Render(text))
	if padRight > 0 {
		b.WriteString(blockStyle.Render(strings.Repeat("█", padRight)))
	}
	return b.String()
}

// RenderCompleteTall renders a 3-line tall fully filled bar with centered text on the middle line.
func (pb ProgressBar) RenderCompleteTall(width int, text string, style lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	fgColor := style.GetForeground()
	blockStyle := lipgloss.NewStyle().Foreground(fgColor).Background(fgColor)
	solidLine := blockStyle.Render(strings.Repeat("█", width))
	middleLine := pb.RenderComplete(width, text, style)
	return solidLine + "\n" + middleLine + "\n" + solidLine
}

// RenderPendingTall renders a 3-line tall bar filled with a uniform shade
// character, with centered text on the middle line. Uses the same colors as
// the unfilled progress bar portion for a subtle "waiting" appearance.
func (pb ProgressBar) RenderPendingTall(width int, text string) string {
	if width <= 0 {
		return ""
	}

	barStyle := pb.theme.ProgressBar.Empty
	textStyle := pb.theme.ProgressBar.EmptyText

	solidLine := barStyle.Render(strings.Repeat(" ", width))

	textWidth := lipgloss.Width(text)
	if textWidth > width {
		text = text[:width]
		textWidth = width
	}
	textStart := (width - textWidth) / 2

	var mid strings.Builder
	if textStart > 0 {
		mid.WriteString(barStyle.Render(strings.Repeat(" ", textStart)))
	}
	mid.WriteString(textStyle.Render(text))
	if afterStart := textStart + textWidth; afterStart < width {
		mid.WriteString(barStyle.Render(strings.Repeat(" ", width-afterStart)))
	}

	return solidLine + "\n" + mid.String() + "\n" + solidLine
}

// formatBarDuration formats duration for display inside progress bars.
// Compact format: "1m07s", "45s", "1h04m".
func formatBarDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

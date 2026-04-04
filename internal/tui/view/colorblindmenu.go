package view

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// ColorblindMenu is a self-contained modal list for selecting a colorblindness type.
// It follows the same overlay pattern as renderConfirmDialog.
type ColorblindMenu struct {
	options []theme.ColorblindnessType
	cursor  int
	active  bool
	theme   theme.Theme
}

// NewColorblindMenu returns a new ColorblindMenu positioned at current.
func NewColorblindMenu(t theme.Theme, current theme.ColorblindnessType) ColorblindMenu {
	options := theme.AllColorblindnessTypes
	cursor := 0
	for i, o := range options {
		if o == current {
			cursor = i
			break
		}
	}
	return ColorblindMenu{
		options: options,
		cursor:  cursor,
		active:  true,
		theme:   theme.Theme(t),
	}
}

// IsActive reports whether the menu is open.
func (m ColorblindMenu) IsActive() bool { return m.active }

// Update handles key input.
//
// Returns: (updated menu, previewType, chosen, close)
//   - previewType: the type at the current cursor position, always returned on
//     up/down/enter so the caller can show a live preview.
//   - chosen: non-nil only on Enter (the confirmed selection).
//   - close: true on Enter or Esc.
func (m ColorblindMenu) Update(msg tea.KeyMsg) (ColorblindMenu, theme.ColorblindnessType, *theme.ColorblindnessType, bool) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.options[m.cursor], nil, false

	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
		return m, m.options[m.cursor], nil, false

	case "enter":
		chosen := m.options[m.cursor]
		m.active = false
		return m, chosen, &chosen, true

	case "esc", "q":
		m.active = false
		return m, m.options[m.cursor], nil, true
	}

	return m, m.options[m.cursor], nil, false
}

// Render overlays the menu on top of bg (the already-rendered screen).
func (m ColorblindMenu) Render(bg string, width, height int) string {
	titleStyle := m.theme.Popup.Title
	hintStyle := m.theme.Popup.Hint
	normalStyle := m.theme.Popup.Normal
	// Selected style: no padding — explicit spaces are used for the prefix so
	// that both selected and normal rows have the same left-side visual width.
	selectedStyle := lipgloss.NewStyle().
		Foreground(m.theme.Table.Selected.GetForeground()).
		Background(m.theme.Table.Selected.GetBackground()).
		Bold(true)

	var rows []string
	rows = append(rows, "") // top padding
	for i, opt := range m.options {
		label := formatOptionLabel(opt)
		if i == m.cursor {
			// "▶  " = arrow (1 col) + 2 spaces = 3 cols, same as "   " below.
			rows = append(rows, selectedStyle.Render("▶  "+label))
		} else {
			rows = append(rows, normalStyle.Render("   "+label))
		}
	}
	rows = append(rows, "") // bottom padding

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Colorblindness"),
		strings.Join(rows, "\n"),
		hintStyle.Render("↑↓ move  Enter select  Esc cancel"),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Popup.Title.GetForeground()).
		Padding(0, 2).
		Render(content)

	return overlayCenter(bg, box, width, height)
}

// formatOptionLabel returns a human-readable label for a ColorblindnessType.
func formatOptionLabel(t theme.ColorblindnessType) string {
	switch t {
	case theme.ColorblindnessNone:
		return "None"
	case theme.ColorblindnessDeuteranopia:
		return "Deuteranopia  (red-green, M-cone)"
	case theme.ColorblindnessProtanopia:
		return "Protanopia    (red-green, L-cone)"
	case theme.ColorblindnessTritanopia:
		return "Tritanopia    (blue-yellow)"
	case theme.ColorblindnessAchromatopsia:
		return "Achromatopsia (no colour)"
	default:
		return string(t)
	}
}

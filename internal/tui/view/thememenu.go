package view

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// ThemeMenu is a self-contained modal list for selecting a colour theme.
// It follows the same overlay pattern as ColorblindMenu.
type ThemeMenu struct {
	options   []theme.ThemeInfo
	cursor    int
	active    bool
	theme     theme.Theme
	sponsored bool // true when the user has a valid sponsor key
}

// NewThemeMenu returns a new ThemeMenu positioned at current.
// sponsored controls whether the Royal theme lock icon is shown.
func NewThemeMenu(t theme.Theme, current theme.ThemeID, sponsored bool) ThemeMenu {
	options := theme.AllThemes
	cursor := 0
	for i, o := range options {
		if o.ID == current {
			cursor = i
			break
		}
	}
	return ThemeMenu{
		options:   options,
		cursor:    cursor,
		active:    true,
		theme:     t,
		sponsored: sponsored,
	}
}

// IsActive reports whether the menu is open.
func (m ThemeMenu) IsActive() bool { return m.active }

// Update handles key input.
//
// Returns: (updated menu, previewID, chosen, close)
//   - previewID: the ThemeID at the current cursor position.
//   - chosen: non-nil only on Enter (the confirmed selection).
//   - close: true on Enter or Esc.
func (m ThemeMenu) Update(msg tea.KeyMsg) (ThemeMenu, theme.ThemeID, *theme.ThemeID, bool) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.options[m.cursor].ID, nil, false

	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
		return m, m.options[m.cursor].ID, nil, false

	case "enter":
		chosen := m.options[m.cursor].ID
		m.active = false
		return m, chosen, &chosen, true

	case "esc", "q":
		m.active = false
		return m, m.options[m.cursor].ID, nil, true
	}

	return m, m.options[m.cursor].ID, nil, false
}

// Render overlays the menu on top of bg (the already-rendered screen).
func (m ThemeMenu) Render(bg string, width, height int) string {
	titleStyle := m.theme.Popup.Title
	hintStyle := m.theme.Popup.Hint
	normalStyle := m.theme.Popup.Normal
	descStyle := m.theme.Popup.Desc
	selectedStyle := lipgloss.NewStyle().
		Foreground(m.theme.Table.Selected.GetForeground()).
		Background(m.theme.Table.Selected.GetBackground()).
		Bold(true)

	// Build display names (Royal gets a lock icon when not sponsored).
	displayNames := make([]string, len(m.options))
	for i, o := range m.options {
		displayNames[i] = o.Name
		if o.ID == theme.ThemeRoyal && !m.sponsored {
			displayNames[i] += " 🔒"
		}
	}

	// Find the widest display name for alignment (visual width, not bytes).
	maxName := 0
	for _, n := range displayNames {
		if w := lipgloss.Width(n); w > maxName {
			maxName = w
		}
	}

	var rows []string
	rows = append(rows, "") // top padding
	for i, opt := range m.options {
		pad := strings.Repeat(" ", maxName-lipgloss.Width(displayNames[i]))
		padded := displayNames[i] + pad
		if i == m.cursor {
			line := selectedStyle.Render("▶  "+padded) + "  " + descStyle.Render(opt.Desc)
			rows = append(rows, line)
		} else {
			line := normalStyle.Render("   "+padded) + "  " + descStyle.Render(opt.Desc)
			rows = append(rows, line)
		}
	}
	rows = append(rows, "") // bottom padding

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Theme"),
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

package view

import (
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/theme"
)

const GitHubSponsorsURL = "https://github.com/sponsors/breina"

// PaywallResult is the outcome of a Royal paywall interaction.
type PaywallResult int

const (
	PaywallResultSponsor PaywallResult = iota // open GitHub Sponsors page
	PaywallResultDegrade                      // use Royal without crown
	PaywallResultCancel                       // cancel — revert theme
)

// RoyalPaywall is a modal popup shown when the user selects the locked Royal theme.
type RoyalPaywall struct {
	cursor int
	active bool
	theme  theme.Theme
}

var paywallOptions = []struct {
	label string
	desc  string
}{
	{"Become a GitHub sponsor", "opens browser"},
	{"Hand in your crown", ""},
	{"Cancel", ""},
}

// NewRoyalPaywall creates the paywall modal.
func NewRoyalPaywall(t theme.Theme) RoyalPaywall {
	return RoyalPaywall{cursor: 2, active: true, theme: t} // default to Cancel
}

// IsActive reports whether the popup is open.
func (p RoyalPaywall) IsActive() bool { return p.active }

// Update handles key input.
// Returns (updated, result, close). result is non-nil only when an action is confirmed.
func (p RoyalPaywall) Update(msg tea.KeyMsg) (RoyalPaywall, *PaywallResult, bool) {
	switch msg.String() {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil, false

	case "down", "j":
		if p.cursor < len(paywallOptions)-1 {
			p.cursor++
		}
		return p, nil, false

	case "enter":
		p.active = false
		result := PaywallResult(p.cursor)
		return p, &result, true

	case "esc", "q":
		p.active = false
		result := PaywallResultCancel
		return p, &result, true
	}

	return p, nil, false
}

// Render overlays the paywall on top of bg.
func (p RoyalPaywall) Render(bg string, width, height int) string {
	t := p.theme
	titleStyle := t.Popup.Title
	hintStyle := t.Popup.Hint
	normalStyle := t.Popup.Normal
	descStyle := t.Popup.Desc
	selectedStyle := lipgloss.NewStyle().
		Foreground(t.Table.Selected.GetForeground()).
		Background(t.Table.Selected.GetBackground()).
		Bold(true)

	body := normalStyle.Render("This theme is normally reserved for GitHub Sponsors.\n\nHowever, I will grant you access as well.\nThe only price: your crown.")

	var rows []string
	rows = append(rows, "")
	for i, opt := range paywallOptions {
		var line string
		label := opt.label
		if i == p.cursor {
			if opt.desc != "" {
				line = selectedStyle.Render("▶  "+label) + "  " + descStyle.Render(opt.desc)
			} else {
				line = selectedStyle.Render("▶  " + label)
			}
		} else {
			if opt.desc != "" {
				line = normalStyle.Render("   "+label) + "  " + descStyle.Render(opt.desc)
			} else {
				line = normalStyle.Render("   " + label)
			}
		}
		rows = append(rows, line)
	}
	rows = append(rows, "")

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Royal Theme — Locked 🔒"),
		"",
		body,
		lipgloss.JoinVertical(lipgloss.Left, rows...),
		hintStyle.Render("↑↓ move  Enter select  Esc cancel"),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Popup.Title.GetForeground()).
		Padding(0, 2).
		Render(content)

	return overlayCenter(bg, box, width, height)
}

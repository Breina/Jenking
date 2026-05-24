package widget

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// ConfirmDialog is a small yes/no popup state machine. The pattern
// (left/right/h/l toggle, y confirms, enter confirms when yes is focused, any
// other key cancels) is reused across cancel/trigger/context flows.
//
// Embed as a value field. Open() flips it open with the safer "No" default;
// Update() drives transitions; View() renders. The owner supplies the box
// title + body so the dialog stays decoupled from any particular action.
type ConfirmDialog struct {
	open bool
	yes  bool
}

func (d *ConfirmDialog) Open()        { d.open = true; d.yes = false }
func (d *ConfirmDialog) Close()       { d.open = false }
func (d *ConfirmDialog) IsOpen() bool { return d.open }

// Update consumes a key event. Returns confirmed=true exactly when the user
// accepted (y, or enter while yes-focused). The dialog auto-closes on any
// terminal transition — caller does not need to call Close().
func (d *ConfirmDialog) Update(msg tea.KeyMsg) (confirmed bool) {
	if !d.open {
		return false
	}
	switch msg.String() {
	case "left", "right", "h", "l":
		d.yes = !d.yes
		return false
	case "y":
		d.open = false
		return true
	case "enter":
		d.open = false
		return d.yes
	default:
		d.open = false
		return false
	}
}

// View renders the dialog body, or "" when closed.
func (d *ConfirmDialog) View(t theme.Theme, title, body string) string {
	if !d.open {
		return ""
	}
	return RenderConfirmBox(t, title, body, d.yes)
}

// RenderConfirmBox builds the styled box for a confirmation dialog. It returns
// the rendered box string without positioning it on any background.
func RenderConfirmBox(t theme.Theme, title, body string, confirmYes bool) string {
	titleStyle := t.Popup.Title
	accentColor := t.Popup.Title.GetForeground()

	baseBtn := lipgloss.NewStyle().Bold(true).Padding(0, 2)
	selectedBtn := baseBtn.Background(accentColor).Foreground(t.Popup.Accent.GetForeground())

	// Raw ANSI underline for mnemonics so it composes with lipgloss
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

package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// confirmDialog is a small yes/no popup state machine. The pattern (left/right/h/l
// toggle, y confirms, enter confirms when yes is focused, any other key cancels)
// was independently re-implemented in cancelBehavior, triggerMixin, contextview,
// and joblist before this extraction.
//
// Embed as a value field. Open() flips it open with the safer "No" default; Update()
// drives transitions; View() renders. The owner supplies the box title + body so the
// dialog stays decoupled from any particular action.
type confirmDialog struct {
	open bool
	yes  bool
}

func (d *confirmDialog) Open()        { d.open = true; d.yes = false }
func (d *confirmDialog) Close()       { d.open = false }
func (d *confirmDialog) IsOpen() bool { return d.open }

// Update consumes a key event. Returns confirmed=true exactly when the user
// accepted (y, or enter while yes-focused). The dialog auto-closes on any
// terminal transition — caller does not need to call Close().
func (d *confirmDialog) Update(msg tea.KeyMsg) (confirmed bool) {
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
func (d *confirmDialog) View(t theme.Theme, title, body string) string {
	if !d.open {
		return ""
	}
	return renderConfirmBox(t, title, body, d.yes)
}

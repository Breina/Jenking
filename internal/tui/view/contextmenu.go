package view

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// ContextMenuEvent describes what action the user triggered in the context menu.
type ContextMenuEvent int

const (
	ContextMenuNoEvent  ContextMenuEvent = iota
	ContextMenuSwitch                    // switch to Name
	ContextMenuDelete                    // delete Name (already confirmed)
	ContextMenuOpenAdd                   // open add-context dialog
	ContextMenuClose                     // menu dismissed
)

// ContextMenuResult is returned by Update.
type ContextMenuResult struct {
	Event ContextMenuEvent
	Name  string
}

// contextConnState is the probed connection state for a context.
type contextConnState int

const (
	connUnknown contextConnState = iota
	connOK
	connFailed
)

type contextMenuMode int

const (
	contextMenuModeMain          contextMenuMode = iota
	contextMenuModeConfirmDelete                 // Y/N sub-dialog
)

// ContextMenu is a self-contained modal list for switching Jenkins environments.
type ContextMenu struct {
	contexts         []config.ContextConfig
	cursor           int
	active           bool
	theme            theme.Theme
	connStatus       map[string]contextConnState
	mode             contextMenuMode
	deleteConfirmYes bool
	errorMsg         string // transient — shown when a switch is blocked
}

// NewContextMenu returns a new ContextMenu positioned at current.
func NewContextMenu(t theme.Theme, contexts []config.ContextConfig, current string) ContextMenu {
	cursor := 0
	for i, c := range contexts {
		if c.Name == current {
			cursor = i
			break
		}
	}
	return ContextMenu{
		contexts:   contexts,
		cursor:     cursor,
		active:     true,
		theme:      t,
		connStatus: make(map[string]contextConnState),
	}
}

// IsActive reports whether the menu is open.
func (m ContextMenu) IsActive() bool { return m.active }

// SetConnStatus records a connection probe result for name.
func (m *ContextMenu) SetConnStatus(name string, ok bool) {
	if ok {
		m.connStatus[name] = connOK
	} else {
		m.connStatus[name] = connFailed
	}
}

// SetContexts replaces the displayed context list (after add/delete).
func (m *ContextMenu) SetContexts(contexts []config.ContextConfig, current string) {
	m.contexts = contexts
	m.mode = contextMenuModeMain
	m.errorMsg = ""
	m.cursor = 0
	for i, c := range contexts {
		if c.Name == current {
			m.cursor = i
			break
		}
	}
}

// Update handles key input.
func (m ContextMenu) Update(msg tea.KeyMsg) (ContextMenu, ContextMenuResult) {
	switch m.mode {
	case contextMenuModeConfirmDelete:
		return m.updateConfirmDelete(msg)
	default:
		return m.updateMain(msg)
	}
}

func (m ContextMenu) updateMain(msg tea.KeyMsg) (ContextMenu, ContextMenuResult) {
	m.errorMsg = ""
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.contexts)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.contexts) == 0 {
			break
		}
		selected := m.contexts[m.cursor]
		if m.connStatus[selected.Name] == connFailed {
			m.errorMsg = "cannot switch: connection failed"
			break
		}
		m.active = false
		return m, ContextMenuResult{Event: ContextMenuSwitch, Name: selected.Name}
	case "delete", "ctrl+d":
		if len(m.contexts) > 0 {
			m.mode = contextMenuModeConfirmDelete
			m.deleteConfirmYes = false
		}
	case "ctrl+n", "insert":
		return m, ContextMenuResult{Event: ContextMenuOpenAdd}
	case "esc", "q":
		m.active = false
		return m, ContextMenuResult{Event: ContextMenuClose}
	}
	return m, ContextMenuResult{}
}

func (m ContextMenu) updateConfirmDelete(msg tea.KeyMsg) (ContextMenu, ContextMenuResult) {
	switch msg.String() {
	case "left", "right", "h", "l":
		m.deleteConfirmYes = !m.deleteConfirmYes
	case "y":
		name := m.contexts[m.cursor].Name
		m.mode = contextMenuModeMain
		return m, ContextMenuResult{Event: ContextMenuDelete, Name: name}
	case "enter":
		if m.deleteConfirmYes {
			name := m.contexts[m.cursor].Name
			m.mode = contextMenuModeMain
			return m, ContextMenuResult{Event: ContextMenuDelete, Name: name}
		}
		m.mode = contextMenuModeMain
	default:
		m.mode = contextMenuModeMain
	}
	return m, ContextMenuResult{}
}

// Render overlays the menu on top of bg.
func (m ContextMenu) Render(bg string, width, height int) string {
	if m.mode == contextMenuModeConfirmDelete {
		return m.renderDeleteConfirm(bg, width, height)
	}
	return m.renderMain(bg, width, height)
}

func (m ContextMenu) renderMain(bg string, width, height int) string {
	titleStyle := m.theme.Popup.Title
	hintStyle := m.theme.Popup.Hint
	normalStyle := m.theme.Popup.Normal
	selectedStyle := lipgloss.NewStyle().
		Foreground(m.theme.Table.Selected.GetForeground()).
		Background(m.theme.Table.Selected.GetBackground()).
		Bold(true)

	accentColor := titleStyle.GetForeground()
	okStyle := m.theme.Header.Connected
	failStyle := m.theme.Header.Disconnected
	unknownStyle := lipgloss.NewStyle().Faint(true)

	// Find the widest name for alignment.
	maxName := 0
	for _, c := range m.contexts {
		if w := lipgloss.Width(c.Name); w > maxName {
			maxName = w
		}
	}

	var rows []string
	rows = append(rows, "") // top padding
	for i, c := range m.contexts {
		var connIndicator string
		switch m.connStatus[c.Name] {
		case connOK:
			connIndicator = okStyle.Render("●")
		case connFailed:
			connIndicator = failStyle.Render("●")
		default:
			connIndicator = unknownStyle.Render("○")
		}

		pad := strings.Repeat(" ", maxName-lipgloss.Width(c.Name))
		padded := c.Name + pad
		urlStr := normalStyle.Faint(true).Render(c.URL)

		if i == m.cursor {
			line := connIndicator + " " + selectedStyle.Render("▶  "+padded) + "  " + urlStr
			rows = append(rows, line)
		} else {
			var nameStr string
			if m.connStatus[c.Name] == connFailed {
				nameStr = normalStyle.Faint(true).Render("   " + padded)
			} else {
				nameStr = normalStyle.Render("   " + padded)
			}
			line := connIndicator + " " + nameStr + "  " + urlStr
			rows = append(rows, line)
		}
	}
	if len(m.contexts) == 0 {
		rows = append(rows, normalStyle.Faint(true).Render("  No contexts configured"))
	}
	rows = append(rows, "") // bottom padding

	var errorLine string
	if m.errorMsg != "" {
		errorLine = m.theme.Header.Disconnected.Render(m.errorMsg) + "\n"
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Context"),
		strings.Join(rows, "\n"),
		errorLine+hintStyle.Render("↑↓ move  Enter select  Del delete  Ctrl+N add  Esc cancel"),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(0, 2).
		Render(content)

	return overlayCenter(bg, box, width, height)
}

func (m ContextMenu) renderDeleteConfirm(bg string, width, height int) string {
	if len(m.contexts) == 0 || m.cursor >= len(m.contexts) {
		return bg
	}
	name := m.contexts[m.cursor].Name
	return renderConfirmDialog(m.theme, bg, width, height,
		"Delete Context",
		fmt.Sprintf("Delete context %q?", name),
		m.deleteConfirmYes,
	)
}

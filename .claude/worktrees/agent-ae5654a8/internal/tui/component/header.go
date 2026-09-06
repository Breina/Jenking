package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

const appVersion = "0.1.0"

var headerArt = `
     ____.              __  |\/\/|              
    |    | ____   ____ |  | |____|____    ____  
    |    |/ __ \ /    \|  |/ /  |/    \  / ___\ 
/\__|    \  ___/|   |  \    <|  |   |  \/ /_/  >
\________|\___  >___|  /__|_ \__|___|  /\___  / 
              \/     \/     \/       \//_____/  
`

// Shortcut is a key/action pair.
type Shortcut struct {
	Key    string
	Action string
}

// Header renders the top panel with connection info and shortcuts.
type Header struct {
	theme          theme.Theme
	url            string
	user           string
	jenkinsVersion string
	connected      bool
	shortcuts      []Shortcut // global, always shown
	viewShortcuts  []Shortcut // context-sensitive, set by app per active view
	width          int
	runningCount   int
	runningKey     string // key letter shown next to count when > 0
}

// NewHeader creates a new header component.
func NewHeader(t theme.Theme, url, user, jenkinsVersion string) Header {
	return Header{
		theme:          t,
		url:            url,
		user:           user,
		jenkinsVersion: jenkinsVersion,
		connected:      true,
		shortcuts: []Shortcut{
			{Key: ":", Action: "command"},
			{Key: "enter", Action: "select"},
			{Key: "esc", Action: "back"},
			{Key: "q", Action: "quit"},
		},
		width: 80,
	}
}

// SetViewShortcuts updates the context-sensitive shortcuts from the active view.
// Called on every render to reflect the current view state.
func (h *Header) SetViewShortcuts(shortcuts []Shortcut) {
	h.viewShortcuts = shortcuts
}

// SetWidth updates the header width.
func (h *Header) SetWidth(w int) {
	h.width = w
}

// SetConnected updates the connection status.
func (h *Header) SetConnected(connected bool) {
	h.connected = connected
}

// SetTheme updates the theme used for rendering.
func (h *Header) SetTheme(t theme.Theme) {
	h.theme = t
}

// SetRunningBuilds updates the running builds count and the key hint shown when > 0.
func (h *Header) SetRunningBuilds(count int, key string) {
	h.runningCount = count
	h.runningKey = key
}

// View renders the header panel.
func (h Header) View() string {
	t := h.theme

	status := t.Header.Connected.Render("● Connected")
	if !h.connected {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("167")).Render("● Disconnected")
	}

	var runningStr string
	if h.runningCount > 0 {
		badge := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(
			fmt.Sprintf("● %d", h.runningCount),
		)
		keyHint := t.StatusBar.Key.Render(fmt.Sprintf("<%s>", h.runningKey))
		runningStr = badge + " " + keyHint
	} else {
		runningStr = t.Header.Value.Faint(true).Render("0")
	}

	infoLines := []string{
		fmt.Sprintf("%s %s", t.Header.Label.Render("       URL:"), t.Header.URL.Render(h.url)),
		fmt.Sprintf("%s %s", t.Header.Label.Render("      User:"), t.Header.Value.Render(h.user)),
		fmt.Sprintf("%s %s", t.Header.Label.Render("   Jenkins:"), t.Header.Value.Render(h.jenkinsVersion)),
		fmt.Sprintf("%s %s", t.Header.Label.Render("   Jenking:"), t.Header.Value.Render(appVersion)),
		fmt.Sprintf("%s %s", t.Header.Label.Render("    Status:"), status),
		fmt.Sprintf("%s %s", t.Header.Label.Render("   Running:"), runningStr),
	}

	infoCol := strings.Join(infoLines, "\n")
	shortcuts := h.renderShortcutColumns()

	left := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().MarginRight(4).Render(infoCol),
		shortcuts,
	)

	crownStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Bold(true)

	headerArt = strings.Replace(headerArt, "|\\/\\/|", crownStyle.Render("|\\/\\/|"), 1)
	headerArt = strings.Replace(headerArt, "|____|", crownStyle.Render("|____|"), 1)

	art := lipgloss.NewStyle().
		Align(lipgloss.Right).
		Render(strings.Trim(headerArt, "\n"))

	leftWidth := lipgloss.Width(left)
	artWidth := lipgloss.Width(art)

	spacerWidth := h.width - 2 - leftWidth - artWidth
	if spacerWidth < 0 {
		spacerWidth = 0
	}

	spacer := strings.Repeat(" ", spacerWidth)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		left,
		spacer,
		art,
	)

	return lipgloss.NewStyle().
		Border(t.Border).
		BorderForeground(lipgloss.Color("62")).
		Width(h.width - 2).
		Render(content)
}

func (h Header) renderShortcutColumns() string {
	t := h.theme
	colSize := 4
	var cols []string

	all := append(h.shortcuts, h.viewShortcuts...) //nolint:gocritic
	for i := 0; i < len(all); i += colSize {
		end := i + colSize
		if end > len(all) {
			end = len(all)
		}

		var lines []string
		for _, sc := range all[i:end] {
			lines = append(lines,
				t.StatusBar.Key.Render(fmt.Sprintf("<%s>", sc.Key))+
					t.StatusBar.Help.Render(" "+sc.Action),
			)
		}
		// Pad short columns to match height
		for len(lines) < colSize {
			lines = append(lines, "")
		}

		col := lipgloss.NewStyle().MarginRight(3).Render(strings.Join(lines, "\n"))
		cols = append(cols, col)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

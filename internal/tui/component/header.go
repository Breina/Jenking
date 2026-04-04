package component

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

const appVersion = "0.1.0"

const headerArt = `
     ____.              __  |\/\/|              
    |    | ____   ____ |  | |____|____    ____  
    |    |/ __ \ /    \|  |/ /  |/    \  / ___\ 
/\__|    \  ___/|   |  \    <|  |   |  \/ /_/  >
\________|\___  >___|  /__|_ \__|___|  /\___  / 
              \/     \/     \/       \//_____/  
`

// headerArtPeasant is shown when the Royal theme is used without a sponsor key.
// The crown glyphs are replaced and the art is not recoloured.
const headerArtPeasant = `
     ____.              __   .__                
    |    | ____   ____ |  | _|__| ____    ____  
    |    |/ __ \ /    \|  |/ /  |/    \  / ___\ 
/\__|    \  ___/|   |  \    <|  |   |  \/ /_/  >
\________|\___  >___|  /__|_ \__|___|  /\___  / 
              \/     \/     \/       \//_____/  
`

// Shortcut is a key/action pair.
type Shortcut struct {
	Key    string
	Action string
	Active bool // when true, action text is highlighted with the search match style
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
	filterMine     bool
	debug          bool
	// debug counters (only used when debug=true)
	dbgRenderMs    int64
	dbgUpdateMs    int64
	dbgCacheTotal  int
	dbgUpdateCount int64
	dbgViewType    string
}

// NewHeader creates a new header component.
func NewHeader(t theme.Theme, url, user, jenkinsVersion string, debug bool) Header {
	return Header{
		theme:          t,
		url:            url,
		user:           user,
		jenkinsVersion: jenkinsVersion,
		connected:      true,
		shortcuts: []Shortcut{
			{Key: ":", Action: "command"},
		},
		width: 80,
		debug: debug,
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

// SetMineFilter updates whether the mine filter is active (shown inline with username).
func (h *Header) SetMineFilter(mine bool) {
	h.filterMine = mine
}

// SetDebugCounters updates the debug overlay values shown in the header.
func (h *Header) SetDebugCounters(renderMs, updateMs int64, cacheTotal int, updateCount int64, viewType string) {
	h.dbgRenderMs = renderMs
	h.dbgUpdateMs = updateMs
	h.dbgCacheTotal = cacheTotal
	h.dbgUpdateCount = updateCount
	h.dbgViewType = viewType
}

// View renders the header panel.
func (h Header) View() string {
	t := h.theme

	status := t.Header.Connected.Render("● Connected")
	if !h.connected {
		status = t.Header.Disconnected.Render("● Disconnected")
	}

	var runningStr string
	if h.runningCount > 0 {
		badge := t.Header.RunningBadge.Render(
			fmt.Sprintf("● %d", h.runningCount),
		)
		keyHint := t.StatusBar.Key.Render(fmt.Sprintf("<%s>", h.runningKey))
		runningStr = badge + " " + keyHint
	} else {
		runningStr = t.Header.Value.Faint(true).Render("0")
	}

	userValue := t.Header.Value.Render(h.user)

	monarchLabel := "   Monarch:"
	if t.Peasant {
		monarchLabel = "   Peasant:"
	}

	infoLines := []string{
		fmt.Sprintf("%s %s", t.Header.Label.Render("       URL:"), t.Header.URL.Render(h.url)),
		fmt.Sprintf("%s %s", t.Header.Label.Render(monarchLabel), userValue),
		fmt.Sprintf("%s %s", t.Header.Label.Render("   Jenkins:"), t.Header.Value.Render(h.jenkinsVersion)),
		fmt.Sprintf("%s %s", t.Header.Label.Render("   Jenking:"), t.Header.Value.Render(appVersion)),
		fmt.Sprintf("%s %s", t.Header.Label.Render("    Status:"), status),
		fmt.Sprintf("%s %s", t.Header.Label.Render("   Running:"), runningStr),
	}
	if h.debug {
		goroutines := runtime.NumGoroutine()
		infoLines = append(infoLines,
			fmt.Sprintf("%s %s", t.Header.Label.Render("Goroutines:"), t.Header.Value.Render(fmt.Sprintf("%d", goroutines))),
			fmt.Sprintf("%s %s", t.Header.Label.Render("    Render:"), t.Header.Value.Render(fmt.Sprintf("%dms", h.dbgRenderMs))),
			fmt.Sprintf("%s %s", t.Header.Label.Render("    Update:"), t.Header.Value.Render(fmt.Sprintf("%dms", h.dbgUpdateMs))),
			fmt.Sprintf("%s %s", t.Header.Label.Render("     Cache:"), t.Header.Value.Render(fmt.Sprintf("%d", h.dbgCacheTotal))),
			fmt.Sprintf("%s %s", t.Header.Label.Render("      Msgs:"), t.Header.Value.Render(fmt.Sprintf("%d", h.dbgUpdateCount))),
			fmt.Sprintf("%s %s", t.Header.Label.Render("      View:"), t.Header.Value.Render(h.dbgViewType)),
		)
	}

	infoCol := strings.Join(infoLines, "\n")
	shortcuts := h.renderShortcutColumns()

	left := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().MarginRight(4).Render(infoCol),
		shortcuts,
	)

	var artStr string
	if h.theme.Peasant {
		artStr = h.theme.Header.Logo.Render(strings.Trim(headerArtPeasant, "\n"))
	} else {
		logo := h.theme.Header.Logo
		crown := h.theme.Header.Crown
		lines := strings.Split(strings.Trim(headerArt, "\n"), "\n")
		styledLines := make([]string, len(lines))
		for i, line := range lines {
			// At most one crown marker per line; style segments individually so
			// Crown's ANSI reset doesn't bleed into the rest of the line.
			if idx := strings.Index(line, "|\\/\\/|"); idx >= 0 {
				styledLines[i] = logo.Render(line[:idx]) + crown.Render("|\\/\\/|") + logo.Render(line[idx+6:])
			} else if idx := strings.Index(line, "|____|"); idx >= 0 {
				styledLines[i] = logo.Render(line[:idx]) + crown.Render("|____|") + logo.Render(line[idx+6:])
			} else {
				styledLines[i] = logo.Render(line)
			}
		}
		artStr = strings.Join(styledLines, "\n")
	}

	art := lipgloss.NewStyle().
		Align(lipgloss.Right).
		Render(artStr)

	leftWidth := lipgloss.Width(left)
	artWidth := lipgloss.Width(art)

	// Hide the art when the terminal is too narrow to fit both panels.
	var spacer string
	var content string
	if leftWidth+artWidth > h.width {
		content = lipgloss.NewStyle().Width(h.width).Render(left)
	} else {
		spacerWidth := h.width - leftWidth - artWidth
		spacer = strings.Repeat(" ", spacerWidth)
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			left,
			spacer,
			art,
		)
	}

	return lipgloss.NewStyle().
		Width(h.width).
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
			keyStyle := t.StatusBar.Key
			actionStyle := t.StatusBar.Help
			if sc.Active {
				activeColor := t.Search.Match.GetBackground()
				keyStyle = lipgloss.NewStyle().Bold(true).Foreground(activeColor)
				actionStyle = lipgloss.NewStyle().Foreground(activeColor)
			}
			lines = append(lines,
				keyStyle.Render(fmt.Sprintf("<%s>", sc.Key))+
					actionStyle.Render(" "+sc.Action),
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

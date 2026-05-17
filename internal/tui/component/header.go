package component

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/version"
)

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

// Shortcut group labels.
const (
	GroupNav    = "Navigate"
	GroupFilter = "Filter"
	GroupView   = "View"
	GroupAction = "Actions"
)

// Shortcut is a key/action pair.
type Shortcut struct {
	Key    string
	Action string
	Active bool   // when true, key+action are highlighted with the active-filter style
	Group  string // GroupNav | GroupFilter | GroupView | GroupAction | ""
}

// Nav returns a Navigate-group shortcut.
func Nav(key, action string) Shortcut {
	return Shortcut{Key: key, Action: action, Group: GroupNav}
}

// Filter returns a Filter-group shortcut. active highlights the entry when the filter is on.
func Filter(key, action string, active bool) Shortcut {
	return Shortcut{Key: key, Action: action, Active: active, Group: GroupFilter}
}

// ViewSC returns a View-group shortcut. active highlights the entry when this view is open.
func ViewSC(key, action string, active bool) Shortcut {
	return Shortcut{Key: key, Action: action, Active: active, Group: GroupView}
}

// Action returns an Actions-group shortcut.
func Action(key, action string) Shortcut {
	return Shortcut{Key: key, Action: action, Group: GroupAction}
}

// Header renders the top panel with connection info and shortcuts.
type Header struct {
	theme          theme.Theme
	url            string
	user           string
	jenkinsVersion string
	connected      bool
	updateVersion  string     // non-empty when a newer version is available
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
			Action(":", "command"),
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

// SetURL updates the server URL shown in the header.
func (h *Header) SetURL(url string) {
	h.url = url
}

// SetUser updates the user shown in the header.
func (h *Header) SetUser(user string) {
	h.user = user
}

// SetUpdateVersion sets the latest available version string.
// Pass an empty string to clear the badge.
func (h *Header) SetUpdateVersion(v string) {
	h.updateVersion = v
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
		h.jenkingVersionLine(t),
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
	infoRendered := lipgloss.NewStyle().MarginRight(4).Render(infoCol)
	shortcuts := h.renderShortcutColumns(h.width - lipgloss.Width(infoRendered))

	left := lipgloss.JoinHorizontal(
		lipgloss.Top,
		infoRendered,
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

func (h Header) jenkingVersionLine(t theme.Theme) string {
	label := t.Header.Label.Render("   Jenking:")
	cur := t.Header.Value.Render(version.App)
	if h.updateVersion == "" {
		return fmt.Sprintf("%s %s", label, cur)
	}
	arrow := t.Header.Value.Faint(true).Render("→")
	badge := t.Header.RunningBadge.Render(h.updateVersion)
	hint := t.StatusBar.Key.Render("<U>")
	return fmt.Sprintf("%s %s %s %s %s", label, cur, arrow, badge, hint)
}

// groupOrder defines the canonical left-to-right display order of shortcut groups.
var groupOrder = map[string]int{
	GroupNav:    0,
	GroupView:   1,
	GroupFilter: 2,
	GroupAction: 3,
	"":          4,
}

// canonicalGroups is the ordered list of groups that get fixed column positions.
var canonicalGroups = []string{GroupNav, GroupView, GroupFilter, GroupAction}

// groupMinWidth is the reserved visual width (chars) for each group's column in
// the fixed layout. Sacrificed when the available header space is too narrow.
var groupMinWidth = map[string]int{
	GroupNav:    21,
	GroupFilter: 14,
	GroupView:   22,
	GroupAction: 16,
}

// maxItemsPerCol is the max number of shortcut items per column before wrapping.
// Header info column is 6 lines tall; 1 line is used for the group label.
const maxItemsPerCol = 5

func (h Header) renderShortcutColumns(available int) string {
	t := h.theme

	// Collect shortcuts by group.
	groupItems := map[string][]Shortcut{}
	all := append(h.shortcuts, h.viewShortcuts...) //nolint:gocritic
	for _, sc := range all {
		groupItems[sc.Group] = append(groupItems[sc.Group], sc)
	}

	type colContent struct {
		content   string
		groupName string
	}

	const colMargin = 3

	// buildChunk renders one column's worth of shortcuts (with label on first chunk).
	buildChunk := func(name string, items []Shortcut, first bool) colContent {
		var lines []string
		if first && name != "" {
			lines = append(lines, t.StatusBar.Help.Faint(true).Render(name))
		}
		for _, sc := range items {
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
		return colContent{content: strings.Join(lines, "\n"), groupName: name}
	}

	appendChunked := func(dst []colContent, name string, items []Shortcut) []colContent {
		for chunkStart := 0; chunkStart < len(items); chunkStart += maxItemsPerCol {
			end := chunkStart + maxItemsPerCol
			if end > len(items) {
				end = len(items)
			}
			dst = append(dst, buildChunk(name, items[chunkStart:end], chunkStart == 0))
		}
		return dst
	}

	// Fixed layout: all canonical groups hold reserved positions even when empty.
	// Total reserved width determines whether we can use the fixed layout.
	fixedTotal := 0
	for _, name := range canonicalGroups {
		fixedTotal += groupMinWidth[name] + colMargin
	}
	useFixed := fixedTotal <= available

	var colContents []colContent

	if useFixed {
		for _, name := range canonicalGroups {
			items := groupItems[name]
			if len(items) == 0 {
				// Empty placeholder keeps the column position reserved.
				colContents = append(colContents, colContent{groupName: name})
				continue
			}
			// First chunk gets the reserved slot; overflow chunks are appended after.
			end := len(items)
			if end > maxItemsPerCol {
				end = maxItemsPerCol
			}
			colContents = append(colContents, buildChunk(name, items[:end], true))
			for chunkStart := maxItemsPerCol; chunkStart < len(items); chunkStart += maxItemsPerCol {
				chunkEnd := chunkStart + maxItemsPerCol
				if chunkEnd > len(items) {
					chunkEnd = len(items)
				}
				colContents = append(colContents, buildChunk(name, items[chunkStart:chunkEnd], false))
			}
		}
		// Ungrouped shortcuts appended compactly after the fixed block.
		colContents = appendChunked(colContents, "", groupItems[""])
	} else {
		// Compact fallback: only render groups that have items, in canonical order.
		type namedGroup struct {
			name  string
			items []Shortcut
		}
		var present []namedGroup
		for name, items := range groupItems {
			if len(items) > 0 {
				present = append(present, namedGroup{name, items})
			}
		}
		sort.SliceStable(present, func(i, j int) bool {
			return groupOrder[present[i].name] < groupOrder[present[j].name]
		})
		for _, g := range present {
			colContents = appendChunked(colContents, g.name, g.items)
		}
	}

	// Render each column, applying the reserved min-width in fixed mode.
	var cols []string
	for _, c := range colContents {
		w := lipgloss.Width(c.content)
		if useFixed {
			if mw := groupMinWidth[c.groupName]; mw > w {
				w = mw
			}
		}
		col := lipgloss.NewStyle().Width(w).MarginRight(colMargin).Render(c.content)
		cols = append(cols, col)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

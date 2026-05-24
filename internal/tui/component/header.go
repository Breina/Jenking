package component

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
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

// Shortcut is a key/action pair. Rank controls position within its Group
// (lower renders first; ties preserve insertion order via stable sort).
// Producers — not consumers — decide rank, so the header column is deterministic
// regardless of who registered the shortcut when.
type Shortcut struct {
	Key    string
	Action string
	Active bool   // when true, key+action are highlighted with the active-filter style
	Group  string // GroupNav | GroupFilter | GroupView | GroupAction | ""
	Rank   int    // intra-group ordering; 0 = unranked, sorts among other 0s in insertion order
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

// ViewSCRanked is ViewSC plus an intra-group Rank for deterministic ordering.
func ViewSCRanked(key, action string, active bool, rank int) Shortcut {
	return Shortcut{Key: key, Action: action, Active: active, Group: GroupView, Rank: rank}
}

// Action returns an Actions-group shortcut.
func Action(key, action string) Shortcut {
	return Shortcut{Key: key, Action: action, Group: GroupAction}
}

// ActionRanked is Action plus an intra-group Rank for deterministic ordering.
func ActionRanked(key, action string, rank int) Shortcut {
	return Shortcut{Key: key, Action: action, Group: GroupAction, Rank: rank}
}

// Header renders the top panel with connection info and shortcuts.
type Header struct {
	theme          theme.Theme
	url            string
	user           string
	jenkinsVersion string
	appVersion     string // current build version, injected by the caller
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

// NewHeader creates a new header component. appVersion is the current build
// version (passed in to keep this package Jenking-agnostic).
func NewHeader(t theme.Theme, url, user, jenkinsVersion, appVersion string, debug bool) Header {
	return Header{
		theme:          t,
		url:            url,
		user:           user,
		jenkinsVersion: jenkinsVersion,
		appVersion:     appVersion,
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
	cur := t.Header.Value.Render(h.appVersion)
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
	GroupView:   23,
	GroupAction: 16,
}

// maxItemsPerCol is the max number of shortcut items per column before wrapping.
// Header info column is 6 lines tall; 1 line is used for the group label.
const maxItemsPerCol = 5

// shortcutColumn is one rendered column of shortcuts plus the group name it
// was reserved for (so the layout phase can apply min-widths).
type shortcutColumn struct {
	content   string
	groupName string
}

const shortcutColMargin = 3

func (h Header) renderShortcutColumns(available int) string {
	groupItems := h.collectShortcutsByGroup()

	fixedTotal := 0
	for _, name := range canonicalGroups {
		fixedTotal += groupMinWidth[name] + shortcutColMargin
	}
	useFixed := fixedTotal <= available

	var cols []shortcutColumn
	if useFixed {
		cols = h.layoutFixed(groupItems)
	} else {
		cols = h.layoutCompact(groupItems)
	}
	return h.assembleColumns(cols, useFixed)
}

// collectShortcutsByGroup groups all shortcuts and stable-sorts each group by Rank.
func (h Header) collectShortcutsByGroup() map[string][]Shortcut {
	groupItems := map[string][]Shortcut{}
	all := append(h.shortcuts, h.viewShortcuts...) //nolint:gocritic
	for _, sc := range all {
		groupItems[sc.Group] = append(groupItems[sc.Group], sc)
	}
	for g, items := range groupItems {
		sort.SliceStable(items, func(i, j int) bool { return items[i].Rank < items[j].Rank })
		groupItems[g] = items
	}
	return groupItems
}

// buildShortcutChunk renders one column's worth of shortcuts (with label on first chunk).
func (h Header) buildShortcutChunk(name string, items []Shortcut, first bool) shortcutColumn {
	t := h.theme
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
	return shortcutColumn{content: strings.Join(lines, "\n"), groupName: name}
}

// appendChunked splits items into maxItemsPerCol-sized columns and appends each.
func (h Header) appendChunked(dst []shortcutColumn, name string, items []Shortcut) []shortcutColumn {
	for chunkStart := 0; chunkStart < len(items); chunkStart += maxItemsPerCol {
		end := chunkStart + maxItemsPerCol
		if end > len(items) {
			end = len(items)
		}
		dst = append(dst, h.buildShortcutChunk(name, items[chunkStart:end], chunkStart == 0))
	}
	return dst
}

// layoutFixed produces columns for the fixed layout (canonical groups always reserve a slot).
func (h Header) layoutFixed(groupItems map[string][]Shortcut) []shortcutColumn {
	var cols []shortcutColumn
	for _, name := range canonicalGroups {
		items := groupItems[name]
		if len(items) == 0 {
			cols = append(cols, shortcutColumn{groupName: name})
			continue
		}
		cols = h.appendChunked(cols, name, items)
	}
	// Ungrouped shortcuts appended compactly after the fixed block.
	cols = h.appendChunked(cols, "", groupItems[""])
	return cols
}

// layoutCompact produces columns for the compact fallback layout (only non-empty groups).
func (h Header) layoutCompact(groupItems map[string][]Shortcut) []shortcutColumn {
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
	var cols []shortcutColumn
	for _, g := range present {
		cols = h.appendChunked(cols, g.name, g.items)
	}
	return cols
}

// assembleColumns renders each column with the reserved min-width when in fixed mode.
func (h Header) assembleColumns(cols []shortcutColumn, useFixed bool) string {
	rendered := make([]string, 0, len(cols))
	for _, c := range cols {
		w := lipgloss.Width(c.content)
		if useFixed {
			if mw := groupMinWidth[c.groupName]; mw > w {
				w = mw
			}
		}
		rendered = append(rendered, lipgloss.NewStyle().Width(w).MarginRight(shortcutColMargin).Render(c.content))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

package view

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// BuildsView is a unified view for listing builds, backed by a BuildDataProvider.
type BuildsView struct {
	theme          theme.Theme
	table          component.Table
	provider       BuildDataProvider
	client         jenkins.JenkinsClient
	store          *cache.Store
	nc             NavigationContext
	filters        Filters
	filteredBuilds []int
	width          int
	height         int
	ctx            context.Context
	cancel         context.CancelFunc
	progressBar    component.ProgressBar
	searchQuery    string
	searchRe       *regexp.Regexp
	flexColIdx     int // index of the flexible (JOB) column, or -1 if none
	fixedColsWidth int // sum of all non-flexible col widths + padding for all cols
	trigger        triggerMixin
	host           behaviorHost
	// lazy fetch tracking
	lastFetchedKey string
}

// NewBuildsView creates a BuildsView backed by the given provider.
// The first column is always a flexible REF column; its content adapts to the NC level.
func NewBuildsView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext, provider BuildDataProvider) *BuildsView {
	ctx, cancel := context.WithCancel(context.Background())
	// REF is always the first column and the only flexible column (flexColIdx=0).
	// All builds views show the same columns — provider fills empty strings when data is unavailable.
	columns := []component.Column{
		{Title: "REF", Width: colRefWidth},
		{Title: "STATUS", Width: colStatusBWidth},
		{Title: "DURATION", Width: colDurationWidth},
		{Title: "STARTED", Width: colStartedWidth},
		{Title: "TRIGGERED BY", Width: colTriggeredByWidth},
	}
	// fixedColsWidth = sum of all (col.Width+2) for fixed columns (REF excluded).
	fixedColsWidth := 0
	for i, col := range columns {
		if i == 0 {
			continue // REF is flexible — exclude its width
		}
		fixedColsWidth += col.Width + 2
	}
	fixedColsWidth += 2 // padding for REF column itself
	bv := &BuildsView{
		theme:          t,
		table:          component.NewTable(t, columns),
		provider:       provider,
		client:         client,
		store:          store,
		nc:             nc,
		ctx:            ctx,
		cancel:         cancel,
		progressBar:    component.NewProgressBar(t),
		flexColIdx:     0,
		fixedColsWidth: fixedColsWidth,
	}
	// Row-aware accessor: returns the currently selected build's NC + Build.
	access := func() (NavigationContext, jenkins.Build, bool) {
		builds := bv.provider.Builds()
		di := bv.dataIndex(bv.table.Cursor())
		if di < 0 || di >= len(builds) {
			return NavigationContext{}, jenkins.Build{}, false
		}
		selected := builds[di]
		return bv.ncForSelected(selected).AtBuild(selected.Number), selected.Build, true
	}
	storeFn := func() *cache.Store { return bv.store }
	bv.trigger = newTriggerMixin(t, client, nc)
	bv.host.Add(newTestReportBehavior(t, client, storeFn, access, pushTo))
	bv.host.Add(newArtifactBehavior(t, client, storeFn, access, pushTo))
	bv.host.Add(newCancelBehavior(t, client, access))
	bv.host.Add(newTriggerBehavior(&bv.trigger).WithShortcutGate(func() bool {
		return bv.provider.Config().CanTrigger
	}))
	return bv
}

func (bv *BuildsView) ApplySearch(pattern string) error {
	bv.searchQuery = pattern
	bv.searchRe = compileSearchRegex(pattern)
	bv.populateTable()
	bv.table.SetCursor(0)
	return nil
}

func (bv *BuildsView) SearchQuery() string {
	return bv.searchQuery
}

func (bv *BuildsView) ActiveFilters() Filters { return bv.filters }

func (bv *BuildsView) ToggleMine() {
	bv.filters.Mine = !bv.filters.Mine
	bv.populateTable()
	bv.table.SetCursor(0)
}

func (bv *BuildsView) ToggleRunning() {
	bv.filters.Running = !bv.filters.Running
	bv.populateTable()
	bv.table.SetCursor(0)
}

func (bv *BuildsView) dataIndex(tableIdx int) int {
	if bv.filteredBuilds != nil && tableIdx >= 0 && tableIdx < len(bv.filteredBuilds) {
		return bv.filteredBuilds[tableIdx]
	}
	return tableIdx
}

func (bv *BuildsView) Init() tea.Cmd {
	cmd := bv.provider.Init()
	bv.populateTable()
	return cmd
}

func (bv *BuildsView) populateTable() {
	builds := bv.provider.Builds()
	bv.filteredBuilds = nil
	var rows []component.Row
	for i, b := range builds {
		buildLabel := fmt.Sprintf("#%d", b.Number)
		if bv.searchRe != nil && !bv.searchRe.MatchString(buildLabel) {
			continue
		}
		if bv.filters.Running && b.Status != jenkins.BuildStatusRunning {
			continue
		}
		if bv.filters.Mine && bv.nc.Username != "" && !matchesUser(b.Build, bv.nc.Username, bv.nc.GitUsernames) {
			continue
		}
		bv.filteredBuilds = append(bv.filteredBuilds, i)
		ref := renderBuildRef(bv.theme, b, bv.nc.Level)
		var statusStr, durationStr string
		if b.Status == jenkins.BuildStatusRunning {
			elapsed := time.Since(b.Timestamp)
			statusStr = renderRunningStatus(bv.theme, bv.progressBar, colStatusBWidth, elapsed, b.EstimatedDuration)
			durationStr = "~" + formatDuration(elapsed)
		} else {
			statusStr = renderStatus(bv.theme, b.Status)
			durationStr = formatDuration(b.Duration)
		}
		row := component.Row{ref, statusStr, durationStr, relativeTime(b.Timestamp), b.Cause}
		rows = append(rows, row)
	}
	bv.table.SetRows(rows)
}

// maybeFetchSelected fires fetch commands for the selected build if not already cached.
// Guards with lastFetchedKey so it only fires once per selection change.
func (bv *BuildsView) maybeFetchSelected() tea.Cmd {
	if bv.store == nil {
		return nil
	}
	builds := bv.provider.Builds()
	di := bv.dataIndex(bv.table.Cursor())
	if di < 0 || di >= len(builds) {
		return nil
	}
	b := builds[di]
	key := fmt.Sprintf("%s:%d", b.JobPath, b.Number)
	if key == bv.lastFetchedKey {
		return nil
	}
	bv.lastFetchedKey = key
	var cmds []tea.Cmd
	if bv.store.TestReports.Get(key) == nil {
		cmds = append(cmds, fetchTestReport(bv.client, bv.store, b.JobPath, b.Number))
	}
	// Use b.Artifacts (tracker state) not store cache: store can be populated without
	// the provider's tracker being updated (e.g. preloadOne not called for this build).
	if b.Artifacts == nil {
		cmds = append(cmds, fetchArtifacts(bv.client, bv.store, b.JobPath, b.Number))
	}
	return tea.Batch(cmds...)
}

func (bv *BuildsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate to provider first; on handled messages repopulate and return.
	if handled, cmds := bv.provider.HandleMsg(msg); handled {
		cursorIdx := bv.table.Cursor()
		bv.populateTable()
		bv.table.SetCursor(cursorIdx)
		return bv, tea.Batch(append(cmds, bv.maybeFetchSelected())...)
	}

	// TriggerBuildResultMsg is intercepted before the host's triggerBehavior
	// can emit OpenTriggeredBuildMsg, because BuildsView pushes its own
	// PendingStageView instead of going through the app-level open flow.
	if tr, ok := msg.(TriggerBuildResultMsg); ok {
		if tr.Err != nil {
			return bv, func() tea.Msg { return ErrorMsg{Err: tr.Err} }
		}
		builds := bv.provider.Builds()
		lastKnown := 0
		if len(builds) > 0 {
			lastKnown = builds[0].Number
		}
		sv := NewPendingStageView(bv.theme, bv.client, bv.store, bv.nc, lastKnown)
		return bv, func() tea.Msg { return PushViewMsg{View: sv} }
	}

	if handled, cmd := bv.host.HandleMsg(msg); handled {
		return bv, cmd
	}

	switch msg := msg.(type) {
	case ThemeChangedMsg:
		bv.theme = msg.Theme
		bv.table.SetTheme(msg.Theme)
		bv.progressBar.SetTheme(msg.Theme)
		bv.host.SetTheme(msg.Theme)
		bv.populateTable()
		return bv, nil

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return bv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		return bv, bv.provider.Refresh()

	case tea.KeyMsg:
		if handled, cmd := bv.host.HandleKey(msg); handled {
			return bv, cmd
		}

		cfg := bv.provider.Config()
		builds := bv.provider.Builds()
		switch msg.String() {
		case "up", "k":
			bv.table.MoveUp()
		case "down", "j":
			bv.table.MoveDown()
		case "pgup":
			bv.table.PageUp()
		case "pgdown":
			bv.table.PageDown()
		case "home":
			bv.table.Home()
		case "end":
			bv.table.End()
		case "enter", "s":
			di := bv.dataIndex(bv.table.Cursor())
			if di >= 0 && di < len(builds) {
				selected := builds[di]
				nc := bv.ncForSelected(selected)
				child := NewStageView(bv.theme, bv.client, bv.store, nc.AtBuild(selected.Number), selected.Build)
				return bv, func() tea.Msg { return PushViewMsg{View: child} }
			}
		case "l":
			di := bv.dataIndex(bv.table.Cursor())
			if di >= 0 && di < len(builds) {
				selected := builds[di]
				nc := bv.ncForSelected(selected)
				child := NewConsoleView(bv.theme, bv.client, nc.AtBuild(selected.Number))
				child.store = bv.store
				return bv, func() tea.Msg { return PushViewMsg{View: child} }
			}
		case "d":
			di := bv.dataIndex(bv.table.Cursor())
			if di >= 0 && di < len(builds) {
				selected := builds[di]
				nc := bv.ncForSelected(selected)
				child := NewDescribeView(bv.theme, bv.client, bv.store, nc.AtBuild(selected.Number), selected.Build)
				return bv, func() tea.Msg { return PushViewMsg{View: child} }
			}
		case "t":
			if cfg.CanTrigger {
				lastKnown := 0
				if len(builds) > 0 {
					lastKnown = builds[0].Number
				}
				return bv, bv.trigger.startTriggerFor(bv.nc, lastKnown)
			}
		case "m":
			bv.ToggleMine()
		case "r":
			bv.ToggleRunning()
		}
	}
	return bv, bv.maybeFetchSelected()
}

func (bv *BuildsView) View() string {
	return bv.table.View()
}

func (bv *BuildsView) PopupView() string {
	return bv.host.PopupView()
}

func (bv *BuildsView) Title() string {
	return decodeName(bv.nc.ProjectName)
}

func (bv *BuildsView) Breadcrumb() BreadcrumbSegment {
	seg := BreadcrumbFor("builds", bv.nc)
	seg.Running = bv.filters.Running
	seg.Mine = bv.filters.Mine
	return seg
}

func (bv *BuildsView) ItemCount() int {
	if bv.filteredBuilds != nil {
		return len(bv.filteredBuilds)
	}
	return len(bv.provider.Builds())
}

func (bv *BuildsView) Commands() []command.Command {
	return nil
}

func (bv *BuildsView) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{
		component.Nav("enter", "stages"),
		component.Nav("esc", "jobs"),
	}
	builds := bv.provider.Builds()
	if len(builds) == 0 {
		sc = append(sc,
			component.Filter("/", "search", false),
			component.Filter("m", "mine", bv.filters.Mine),
			component.Filter("r", "running", bv.filters.Running),
		)
		return sc
	}
	sc = append(sc,
		component.Filter("/", "search", false),
		component.ViewSC("l", "full log", false),
		component.ViewSC("s", "stages", false),
		component.ViewSC("d", "describe", false),
	)
	// T (tests), A (artifacts), x (cancel), t (trigger) come from the host —
	// each behavior advertises its own shortcut, gated on resolvability of
	// the currently selected row.
	sc = bv.host.AppendShortcuts(sc)
	sc = append(sc,
		component.Filter("m", "mine", bv.filters.Mine),
		component.Filter("r", "running", bv.filters.Running),
	)
	return sc
}

func (bv *BuildsView) HasPopup() bool {
	return bv.host.HasPopup()
}

func (bv *BuildsView) Close() error {
	bv.provider.Close()
	if bv.cancel != nil {
		bv.cancel()
	}
	return nil
}

func (bv *BuildsView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	if bv.nc.BranchName != "" || bv.nc.Level == CtxProject {
		pp := bv.nc.ProjectName
		if bv.nc.FolderPath != "" {
			pp = bv.nc.FolderPath + "/" + bv.nc.ProjectName
		}
		return NewJobList(t, c, s, pp, bv.nc.ProjectName, true, bv.nc.Username, bv.nc.GitUsernames)
	}
	childPath := bv.nc.ProjectName
	if bv.nc.FolderPath != "" {
		childPath = bv.nc.FolderPath + "/" + bv.nc.ProjectName
	}
	return folderParentJobList(t, c, s, childPath, bv.nc.Username, bv.nc.GitUsernames)
}

func (bv *BuildsView) SetSize(width, height int) {
	bv.width = width
	bv.height = height
	if bv.flexColIdx >= 0 {
		flex := width - bv.fixedColsWidth
		if flex < 15 {
			flex = 15
		}
		bv.table.SetColumnWidth(bv.flexColIdx, flex)
	}
	bv.table.SetSize(width, height)
	bv.host.SetSize(width, height-6)
}

// NC returns the NavigationContext for this view. Used by app.go to distinguish
// AllBuilds (CtxRoot) from other BuildsView instances.
func (bv *BuildsView) NC() NavigationContext { return bv.nc }

func (bv *BuildsView) ScrollInfo() ScrollInfo {
	return ScrollInfo{Offset: bv.table.ScrollOffset(), TotalLines: bv.table.TotalRows(), ViewHeight: bv.table.ContentHeight()}
}

// ncForSelected returns the NavigationContext to use for navigating into a build.
// For root-level views (AllBuilds), derives the full NC from the build's JobPath.
// For project/branch views, inherits from the view's NC.
func (bv *BuildsView) ncForSelected(selected UnifiedBuild) NavigationContext {
	if bv.nc.Level == CtxRoot && selected.JobPath != "" {
		nc := ncFromJobPath(selected.JobPath)
		nc.Username = bv.nc.Username
		return nc
	}
	if selected.BranchName != "" {
		return bv.nc.AtBranch(selected.BranchName)
	}
	return bv.nc
}

// buildRefPlainText returns the plain-text (no ANSI) build reference for a build.
// Used by the cancel dialog title and as the basis for renderBuildRef.
//
//	CtxBranch         — "#42"
//	CtxProject        — "↳ branch #42"
//	CtxRoot/CtxFolder — "project ↳ branch #42"
func buildRefPlainText(b UnifiedBuild, level ContextLevel) string {
	numStr := fmt.Sprintf("#%d", b.Number)
	switch level {
	case CtxBranch:
		return numStr
	case CtxProject:
		if b.BranchName == "" {
			return numStr
		}
		return refIcon(b.BranchName) + " " + decodeName(b.BranchName) + " " + numStr
	default: // CtxRoot, CtxFolder
		displayName := shortName(decodeName(b.DisplayName))
		if displayName == "" {
			return numStr
		}
		if b.BranchName == "" {
			return displayName + " " + numStr
		}
		return displayName + " " + refIcon(b.BranchName) + " " + decodeName(b.BranchName) + " " + numStr
	}
}

// renderBuildRef renders the REF column cell for a build with ANSI theme colors.
func renderBuildRef(t theme.Theme, b UnifiedBuild, level ContextLevel) string {
	numStr := t.Breadcrumb.BuildNum.Render(fmt.Sprintf("#%d", b.Number))
	switch level {
	case CtxBranch:
		return numStr
	case CtxProject:
		if b.BranchName == "" {
			return numStr
		}
		icon := refIcon(b.BranchName)
		return t.Breadcrumb.Paren.Render(icon) + " " + t.Breadcrumb.Context.Render(decodeName(b.BranchName)) + " " + numStr
	default: // CtxRoot, CtxFolder
		displayName := shortName(decodeName(b.DisplayName))
		if displayName == "" {
			return numStr
		}
		proj := t.Breadcrumb.Context.Render(displayName)
		if b.BranchName == "" {
			return proj + " " + numStr
		}
		icon := refIcon(b.BranchName)
		branch := t.Breadcrumb.Context.Render(decodeName(b.BranchName))
		return proj + " " + t.Breadcrumb.Paren.Render(icon) + " " + branch + " " + numStr
	}
}

// refIcon returns the bare icon character for a branch/MR (no surrounding spaces).
func refIcon(branchName string) string {
	if strings.HasPrefix(branchName, "PR-") || strings.HasPrefix(branchName, "MR-") {
		return "⊕"
	}
	return "↳"
}

// formatDuration formats a duration as "45s", "2m 13s", or "1h 04m".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// relativeTime formats a time as a human-readable relative string.
func relativeTime(t time.Time) string {
	elapsed := time.Since(t)
	switch {
	case elapsed < 30*time.Second:
		return "just now"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	}
}

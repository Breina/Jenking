package view

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// BuildsView is a unified view for listing builds, backed by a BuildDataProvider.
type BuildsView struct {
	BaseView
	table          component.Table
	provider       BuildDataProvider
	filters        Filters
	filteredBuilds []int
	progressBar    component.ProgressBar
	searchQuery    string
	searchRe       *regexp.Regexp
	flexColIdx     int // index of the flexible (JOB) column, or -1 if none
	fixedColsWidth int // sum of all non-flexible col widths + padding for all cols
	trigger        triggerMixin
	host           widget.BehaviorHost
	// queued-build cancel flow (mirrors the host's cancelBehavior, but targets
	// a queue item rather than a running build).
	queueDialog       widget.ConfirmDialog
	queueCancelTarget UnifiedBuild
	// lazy fetch tracking
	lastFetchedKey string
}

// NewBuildsView creates a BuildsView backed by the given provider.
// The first column is always a flexible REF column; its content adapts to the NC level.
func NewBuildsView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, provider BuildDataProvider) *BuildsView {
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
		BaseView:       NewBaseView(t, client, store, nc, nc.Level),
		table:          component.NewTable(t, columns),
		provider:       provider,
		progressBar:    component.NewProgressBar(t),
		flexColIdx:     0,
		fixedColsWidth: fixedColsWidth,
	}
	// Row-aware accessor: returns the currently selected build's NC + Build.
	access := func() (NavigationContext, jmodel.Build, bool) {
		builds := bv.provider.Builds()
		di := bv.dataIndex(bv.table.Cursor())
		if di < 0 || di >= len(builds) {
			return NavigationContext{}, jmodel.Build{}, false
		}
		selected := builds[di]
		if selected.Queued {
			// Queued rows have no build number; build-only behaviors (cancel,
			// stages, log, describe, tests, artifacts, trigger) do not apply.
			return NavigationContext{}, jmodel.Build{}, false
		}
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
	// Navigation shortcuts. Each accessor resolves the selected row + builds
	// its child view. `s` aliases `enter` so the conventional drill-in key
	// produces the same action; only `s` appears as an explicit shortcut.
	bv.host.Add(widget.NewNavBehavior("l", "full log", func() (tea.Cmd, bool) {
		selectedNC, _, ok := access()
		if !ok {
			return nil, false
		}
		child := NewConsoleView(bv.theme, bv.client, selectedNC)
		child.store = bv.store
		return func() tea.Msg { return PushViewMsg{View: child} }, true
	}).WithRank(rankViewFullLog))
	bv.host.Add(widget.NewNavBehavior("s", "stages", func() (tea.Cmd, bool) {
		selectedNC, selected, ok := access()
		if !ok {
			return nil, false
		}
		child := NewStageView(bv.theme, bv.client, bv.store, selectedNC, selected)
		return func() tea.Msg { return PushViewMsg{View: child} }, true
	}).WithAlias("enter").WithRank(rankViewStages))
	bv.host.Add(widget.NewNavBehavior("d", "describe", func() (tea.Cmd, bool) {
		selectedNC, selected, ok := access()
		if !ok {
			return nil, false
		}
		child := NewDescribeView(bv.theme, bv.client, bv.store, selectedNC, selected)
		return func() tea.Msg { return PushViewMsg{View: child} }, true
	}).WithRank(rankViewDescribe))
	return bv
}

func (bv *BuildsView) ApplySearch(pattern string) tea.Cmd {
	bv.searchQuery = pattern
	bv.searchRe = widget.CompileSearchRegex(pattern)
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
		if bv.filters.Running && b.Status != jmodel.BuildStatusRunning {
			continue
		}
		if bv.filters.Mine && bv.nc.Username != "" && !matchesUser(b.Build, bv.nc.Username, bv.nc.GitUsernames) {
			continue
		}
		bv.filteredBuilds = append(bv.filteredBuilds, i)
		ref := renderBuildRef(bv.theme, b, bv.nc.Level)
		if b.Queued {
			row := component.Row{ref, renderQueueStatus(bv.theme, b.QueueState), "—", relativeTime(b.Timestamp), b.Why}
			rows = append(rows, row)
			continue
		}
		var statusStr, durationStr string
		if b.Status == jmodel.BuildStatusRunning {
			elapsed := time.Since(b.Timestamp)
			if isBuildPausedOnInput(bv.store, b.JobPath, b.Number) {
				statusStr = renderStatus(bv.theme, jmodel.BuildStatusPausedInput)
			} else {
				statusStr = renderRunningStatus(bv.theme, bv.progressBar, colStatusBWidth, elapsed, b.EstimatedDuration)
			}
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
	var cmds []tea.Cmd
	// Prefetch the selected build's job SCM URL so "open repo" is ready.
	if c := fetchRepoURL(bv.client, bv.store, b.JobPath); c != nil {
		cmds = append(cmds, c)
	}
	if b.Queued {
		// No build exists yet — nothing else to fetch.
		return tea.Batch(cmds...)
	}
	key := fmt.Sprintf("%s:%d", b.JobPath, b.Number)
	if key == bv.lastFetchedKey {
		return tea.Batch(cmds...)
	}
	bv.lastFetchedKey = key
	if bv.store.TestReports.Get(key) == nil {
		cmds = append(cmds, fetchTestReport(bv.client, bv.store, b.JobPath, b.Number))
	}
	// Use b.Artifacts (tracker state) not store cache: store can be populated without
	// the provider's tracker being updated (e.g. preloadOne not called for this build).
	if b.Artifacts == nil {
		cmds = append(cmds, fetchArtifacts(bv.client, bv.store, b.JobPath, b.Number))
	}
	// For running builds we cheaply check whether the run is paused on a
	// pipeline `input` step so the list can swap the progress bar for a
	// paused badge. Only fires once per selection (cache miss path).
	if b.Status == jmodel.BuildStatusRunning && bv.store.PendingInputs.Get(key) == nil {
		cmds = append(cmds, fetchPendingInputs(bv.client, bv.store, b.JobPath, b.Number))
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
			return bv, func() tea.Msg { return ErrorMsg(msg) }
		}
		return bv, bv.provider.Refresh()

	case tea.KeyMsg:
		if cmd, returned := bv.handleKeyMsg(msg); returned {
			return bv, cmd
		}
	}
	return bv, bv.maybeFetchSelected()
}

// handleKeyMsg processes view-local keys after the behavior host has had its
// chance. Returns (cmd, true) when the key produced a definitive action; the
// caller falls through to maybeFetchSelected when (nil, false) is returned.
func (bv *BuildsView) handleKeyMsg(msg tea.KeyMsg) (tea.Cmd, bool) {
	// Queued-build cancel dialog owns all keys while open.
	if bv.queueDialog.IsOpen() {
		if bv.queueDialog.Update(msg) {
			return bv.cancelQueuedCmd(), true
		}
		return nil, true
	}
	// Queued rows: intercept the queue-specific actions before the behavior
	// host (whose build-only behaviors are inert here anyway).
	if q, ok := bv.selectedQueued(); ok {
		switch msg.String() {
		case "enter", "s":
			return bv.openPendingForQueued(q), true
		case "x":
			bv.queueCancelTarget = q
			bv.queueDialog.Open()
			return nil, true
		}
	}
	if handled, cmd := bv.host.HandleKey(msg); handled {
		return cmd, true
	}
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
	case "o":
		if url := cachedRepoURL(bv.store, bv.selectedJobPath()); url != "" {
			return openURLCmd(url), true
		}
	case "t":
		return bv.startTriggerCmd()
	case "m":
		bv.ToggleMine()
	case "r":
		bv.ToggleRunning()
	}
	return nil, false
}

// startTriggerCmd is the "t" entry point: the triggerBehavior owns the popup
// lifecycle, but the initial keypress with the lastKnown lookup is view-local
// (BuildsView uses the head of its builds list).
func (bv *BuildsView) startTriggerCmd() (tea.Cmd, bool) {
	if !bv.provider.Config().CanTrigger {
		return nil, false
	}
	builds := bv.provider.Builds()
	lastKnown := 0
	if len(builds) > 0 {
		lastKnown = builds[0].Number
	}
	return bv.trigger.startTriggerFor(bv.nc, lastKnown), true
}

// selectedJobPath returns the job path of the selected row, or "".
func (bv *BuildsView) selectedJobPath() string {
	builds := bv.provider.Builds()
	di := bv.dataIndex(bv.table.Cursor())
	if di < 0 || di >= len(builds) {
		return ""
	}
	return builds[di].JobPath
}

// selectedQueued returns the currently selected row iff it is a queued item.
func (bv *BuildsView) selectedQueued() (UnifiedBuild, bool) {
	builds := bv.provider.Builds()
	di := bv.dataIndex(bv.table.Cursor())
	if di < 0 || di >= len(builds) {
		return UnifiedBuild{}, false
	}
	if b := builds[di]; b.Queued {
		return b, true
	}
	return UnifiedBuild{}, false
}

// openPendingForQueued drills into the Pending stage view for a queued item,
// seeded with the queue info so the waiting state shows the reason. This is the
// same view shown right after a manual trigger.
func (bv *BuildsView) openPendingForQueued(q UnifiedBuild) tea.Cmd {
	nc := bv.ncForSelected(q)
	item := jmodel.QueueItem{
		ID:              q.QueueID,
		JobPath:         q.JobPath,
		DisplayName:     q.DisplayName,
		Why:             q.Why,
		InQueueSince:    q.Timestamp,
		Cause:           q.Cause,
		TriggeredBy:     q.TriggeredBy,
		TriggeredByName: q.TriggeredByName,
	}
	switch q.QueueState {
	case "stuck":
		item.Stuck = true
	case "blocked":
		item.Blocked = true
	case "pending":
		item.Pending = true
	default:
		item.Buildable = true
	}
	child := NewPendingStageViewForQueue(bv.theme, bv.client, bv.store, nc, bv.lastKnownBuildFor(q.JobPath), &item)
	return func() tea.Msg { return PushViewMsg{View: child} }
}

// cancelQueuedCmd removes the confirmed queue item. Reuses CancelBuildResultMsg
// so the existing Update handler refreshes the provider on success.
func (bv *BuildsView) cancelQueuedCmd() tea.Cmd {
	id := bv.queueCancelTarget.QueueID
	client := bv.client
	return func() tea.Msg {
		return CancelBuildResultMsg{Err: client.CancelQueueItem(context.Background(), id)}
	}
}

// lastKnownBuildFor returns the highest build number the registry knows for a
// job, used to seed the Pending view's "wait for a newer build" poll.
func (bv *BuildsView) lastKnownBuildFor(jobPath string) int {
	if bv.store == nil || bv.store.Registry == nil {
		return 0
	}
	max := 0
	for _, ub := range bv.store.Registry.Query(buildregistry.Filter{JobPath: jobPath}) {
		if ub.Number > max {
			max = ub.Number
		}
	}
	return max
}

func (bv *BuildsView) View() string {
	return bv.table.View()
}

func (bv *BuildsView) PopupView() string {
	if bv.queueDialog.IsOpen() {
		return bv.queueDialog.View(bv.theme,
			"Cancel Queued Build",
			fmt.Sprintf("Remove %s from the queue?", decodeName(bv.queueCancelTarget.DisplayName)),
		)
	}
	return bv.host.PopupView()
}

func (bv *BuildsView) Title() string {
	return decodeName(bv.nc.ProjectName)
}

func (bv *BuildsView) Breadcrumb() BreadcrumbSegment {
	seg := bv.MakeBreadcrumb("builds")
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
	if _, ok := bv.selectedQueued(); ok {
		sc := []component.Shortcut{
			component.Nav("enter", "job"),
			component.Nav("esc", "jobs"),
		}
		if cachedRepoURL(bv.store, bv.selectedJobPath()) != "" {
			sc = append(sc, component.Nav("o", "open repo"))
		}
		return append(sc,
			component.Action("x", "cancel"),
			component.Filter("/", "search", false),
			component.Filter("m", "mine", bv.filters.Mine),
			component.Filter("r", "running", bv.filters.Running),
		)
	}
	sc := []component.Shortcut{
		component.Nav("enter", "stages"),
		component.Nav("esc", "jobs"),
	}
	if cachedRepoURL(bv.store, bv.selectedJobPath()) != "" {
		sc = append(sc, component.Nav("o", "open repo"))
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
	sc = append(sc, component.Filter("/", "search", false))
	// l (full log), s (stages), d (describe), T (tests), A (artifacts), x
	// (cancel), t (trigger) all come from the host — each behavior advertises
	// its own shortcut, gated on resolvability of the currently selected row.
	sc = bv.host.AppendShortcuts(sc)
	sc = append(sc,
		component.Filter("m", "mine", bv.filters.Mine),
		component.Filter("r", "running", bv.filters.Running),
	)
	return sc
}

func (bv *BuildsView) HasPopup() bool {
	return bv.queueDialog.IsOpen() || bv.host.HasPopup()
}

func (bv *BuildsView) Close() error {
	bv.provider.Close()
	return bv.BaseView.Close()
}

func (bv *BuildsView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
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
	bv.BaseView.SetSize(width, height)
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

func (bv *BuildsView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: bv.table.ScrollOffset(), TotalLines: bv.table.TotalRows(), ViewHeight: bv.table.ContentHeight()}
}

// InspectTarget returns the nc for the :inspect command — build-level for a
// resolvable row, job-level for a queued row (no build number yet).
func (bv *BuildsView) InspectTarget() (NavigationContext, bool) {
	builds := bv.provider.Builds()
	di := bv.dataIndex(bv.table.Cursor())
	if di < 0 || di >= len(builds) {
		return NavigationContext{}, false
	}
	selected := builds[di]
	if selected.Queued {
		return bv.ncForSelected(selected), true
	}
	return bv.ncForSelected(selected).AtBuild(selected.Number), true
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
//
// renderBuildRef renders the REF column cell for a build with ANSI theme colors.
func renderBuildRef(t theme.Theme, b UnifiedBuild, level ContextLevel) string {
	if b.Queued {
		return renderQueuedRef(t, b, level)
	}
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

// renderQueuedRef renders the REF cell for a queued row. There is no build
// number yet, so it identifies the row by job/branch instead. The STATUS column
// carries the "queued" badge, so this only needs to name the target.
func renderQueuedRef(t theme.Theme, b UnifiedBuild, level ContextLevel) string {
	switch level {
	case CtxBranch:
		return t.Breadcrumb.Context.Render("(queued)")
	case CtxProject:
		if b.BranchName == "" {
			return t.Breadcrumb.Context.Render("(queued)")
		}
		icon := refIcon(b.BranchName)
		return t.Breadcrumb.Paren.Render(icon) + " " + t.Breadcrumb.Context.Render(decodeName(b.BranchName))
	default: // CtxRoot, CtxFolder
		displayName := shortName(decodeName(b.DisplayName))
		if displayName == "" {
			return t.Breadcrumb.Context.Render("(queued)")
		}
		proj := t.Breadcrumb.Context.Render(displayName)
		if b.BranchName == "" {
			return proj
		}
		icon := refIcon(b.BranchName)
		return proj + " " + t.Breadcrumb.Paren.Render(icon) + " " + t.Breadcrumb.Context.Render(decodeName(b.BranchName))
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

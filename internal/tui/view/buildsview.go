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
	// dialog state
	confirmCancel  bool
	confirmYes     bool
	confirmBuild   UnifiedBuild
	confirmTrigger bool
	triggerYes     bool
	paramForm      *component.ParamForm
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
		{Title: "TESTS", Width: colTestsWidth},
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
	return &BuildsView{
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
		row := component.Row{ref, statusStr, durationStr, relativeTime(b.Timestamp),
			renderTestBadge(bv.theme, b.TestResult), b.Cause}
		rows = append(rows, row)
	}
	bv.table.SetRows(rows)
}

func (bv *BuildsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate to provider first; on handled messages repopulate and return.
	if handled, cmds := bv.provider.HandleMsg(msg); handled {
		cursorIdx := bv.table.Cursor()
		bv.populateTable()
		bv.table.SetCursor(cursorIdx)
		return bv, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {
	case ThemeChangedMsg:
		bv.theme = msg.Theme
		bv.table.SetTheme(msg.Theme)
		bv.progressBar.SetTheme(msg.Theme)
		if bv.paramForm != nil {
			bv.paramForm.SetTheme(msg.Theme)
		}
		bv.populateTable()
		return bv, nil

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return bv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		return bv, bv.provider.Refresh()

	case JobParamsMsg:
		if msg.Err != nil {
			return bv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		if len(msg.Params) == 0 {
			bv.confirmTrigger = true
			bv.triggerYes = false
			return bv, nil
		}
		form := component.NewParamForm(bv.theme, msg.Params)
		form.SetMaxHeight(bv.height - 6)
		bv.paramForm = &form
		return bv, nil

	case TriggerBuildResultMsg:
		if msg.Err != nil {
			return bv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		builds := bv.provider.Builds()
		lastKnown := 0
		if len(builds) > 0 {
			lastKnown = builds[0].Number
		}
		sv := NewPendingStageView(bv.theme, bv.client, bv.store, bv.nc, lastKnown)
		return bv, func() tea.Msg { return PushViewMsg{View: sv} }

	case tea.KeyMsg:
		if bv.paramForm != nil {
			result := bv.paramForm.Update(msg)
			switch result.Status {
			case component.ParamFormDone:
				bv.paramForm = nil
				return bv, triggerBuild(bv.client, bv.nc, result.Values)
			case component.ParamFormCancelled:
				bv.paramForm = nil
			}
			return bv, nil
		}

		if bv.confirmTrigger {
			switch msg.String() {
			case "left", "right", "h":
				bv.triggerYes = !bv.triggerYes
			case "y":
				bv.confirmTrigger = false
				return bv, triggerBuild(bv.client, bv.nc, nil)
			case "enter":
				if bv.triggerYes {
					bv.confirmTrigger = false
					return bv, triggerBuild(bv.client, bv.nc, nil)
				}
				bv.confirmTrigger = false
			default:
				bv.confirmTrigger = false
			}
			return bv, nil
		}

		if bv.confirmCancel {
			switch msg.String() {
			case "left", "right", "h":
				bv.confirmYes = !bv.confirmYes
			case "y":
				bv.confirmCancel = false
				jobPath, number := bv.confirmBuild.JobPath, bv.confirmBuild.Number
				return bv, func() tea.Msg {
					err := bv.client.CancelBuild(context.Background(), jobPath, number)
					return CancelBuildResultMsg{Err: err}
				}
			case "enter":
				if bv.confirmYes {
					bv.confirmCancel = false
					jobPath, number := bv.confirmBuild.JobPath, bv.confirmBuild.Number
					return bv, func() tea.Msg {
						err := bv.client.CancelBuild(context.Background(), jobPath, number)
						return CancelBuildResultMsg{Err: err}
					}
				}
				bv.confirmCancel = false
			default:
				bv.confirmCancel = false
			}
			return bv, nil
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
		case "enter":
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
				return bv, fetchJobParams(bv.client, bv.nc)
			}
		case "T":
			di := bv.dataIndex(bv.table.Cursor())
			if di >= 0 && di < len(builds) {
				selected := builds[di]
				if selected.TestResult != nil && len(selected.TestResult.Suites) > 0 {
					child := NewTestReportView(bv.theme, *selected.TestResult, bv.nc.AtBuild(selected.Number), selected.Build)
					return bv, func() tea.Msg { return PushViewMsg{View: child} }
				}
			}
		case "x":
			di := bv.dataIndex(bv.table.Cursor())
			if di >= 0 && di < len(builds) {
				if builds[di].Status == jenkins.BuildStatusRunning {
					bv.confirmCancel = true
					bv.confirmYes = false
					bv.confirmBuild = builds[di]
				}
			}
		case "m":
			bv.ToggleMine()
		case "r":
			bv.ToggleRunning()
		}
	}
	return bv, nil
}

func (bv *BuildsView) View() string {
	tableView := bv.table.View()
	if bv.paramForm != nil {
		return overlayCenter(tableView, bv.paramForm.View(), bv.width, bv.height)
	}
	if bv.confirmTrigger {
		return renderConfirmDialog(bv.theme, tableView, bv.width, bv.height,
			"Trigger Build",
			fmt.Sprintf("Start a new build of %s?", decodeName(bv.nc.ProjectName)),
			bv.triggerYes,
		)
	}
	if bv.confirmCancel {
		return renderConfirmDialog(bv.theme, tableView, bv.width, bv.height,
			"Cancel Build",
			fmt.Sprintf("Stop build %s?", renderBuildRef(bv.theme, bv.confirmBuild, bv.nc.Level)),
			bv.confirmYes,
		)
	}
	return tableView
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
	// enter and esc first for stable grid positioning
	sc := []component.Shortcut{
		{Key: "enter", Action: "stages"},
		{Key: "esc", Action: "jobs"},
		{Key: "/", Action: "search"},
		{Key: "m", Action: "mine", Active: bv.filters.Mine},
		{Key: "r", Action: "running", Active: bv.filters.Running},
	}
	builds := bv.provider.Builds()
	if len(builds) == 0 {
		return sc
	}
	cfg := bv.provider.Config()
	sc = append(sc, component.Shortcut{Key: "l", Action: "log"})
	sc = append(sc, component.Shortcut{Key: "d", Action: "describe"})
	if cfg.CanTrigger {
		sc = append(sc, component.Shortcut{Key: "t", Action: "trigger"})
	}
	di := bv.dataIndex(bv.table.Cursor())
	if di >= 0 && di < len(builds) {
		if builds[di].Status == jenkins.BuildStatusRunning {
			sc = append(sc, component.Shortcut{Key: "x", Action: "cancel"})
		}
		if builds[di].TestResult != nil && len(builds[di].TestResult.Suites) > 0 {
			sc = append(sc, component.Shortcut{Key: "T", Action: "test results"})
		}
	}
	return sc
}

func (bv *BuildsView) HasPopup() bool {
	return bv.confirmTrigger || bv.confirmCancel || bv.paramForm != nil
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

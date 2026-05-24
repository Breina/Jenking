package view

import (
	"context"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// stageLogPollMsg carries the result of a periodic poll for updated node logs.
type stageLogPollMsg struct {
	nodes      map[int]*nodeLogState
	nodeIDs    []int // updated ordered node list (from re-fetched stages)
	stageFound bool  // false if stage disappeared from stages list
	running    bool  // true if the stage is still running
	err        error
}

// StageLogView shows the aggregated log output for a pipeline stage,
// combining the console output of all flow graph nodes within the stage.
type StageLogView struct {
	lv           widget.LogViewer
	theme        theme.Theme
	client       jmodel.JenkinsClient
	nc           NavigationContext
	nodeIDs      []int
	nodes        map[int]*nodeLogState
	done         bool
	buildRunning bool
	build        jmodel.Build
	store        *cache.Store
	ctx          context.Context
	cancel       context.CancelFunc
	trigger      triggerMixin
	host         widget.BehaviorHost
	copyLogFlash bool
	copySelFlash bool
	// scopedParent, when set, overrides ParentView to return a fresh MyBuildsView
	// using the stored scope. Set when this view is opened from a scoped view.
	hasScopedParent      bool
	scopedParentScope    NavigationContext
	scopedParentInterval time.Duration
}

// SetScopedParent implements ScopedParentTarget. When set, ESC returns to a
// fresh MyBuildsView for the given scope rather than the default StageView.
func (sl *StageLogView) SetScopedParent(scope NavigationContext, slowInterval time.Duration) {
	sl.hasScopedParent = true
	sl.scopedParentScope = scope
	sl.scopedParentInterval = slowInterval
}

func NewStageLogView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, nodeIDs []int, buildRunning bool) *StageLogView {
	ctx, cancel := context.WithCancel(context.Background())
	sl := &StageLogView{
		lv:           widget.NewLogViewer(t),
		theme:        t,
		client:       client,
		store:        store,
		nc:           nc,
		nodeIDs:      nodeIDs,
		nodes:        make(map[int]*nodeLogState),
		buildRunning: buildRunning,
		ctx:          ctx,
		cancel:       cancel,
		trigger:      newTriggerMixin(t, client, nc),
	}
	return sl
}

// NewStageLogViewWithBuild is like NewStageLogView but also stores the build,
// enabling the full set of view tabs (l/s/d/T/A) and build actions (x/t).
func NewStageLogViewWithBuild(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, nodeIDs []int, buildRunning bool, build jmodel.Build) *StageLogView {
	sl := NewStageLogView(t, client, store, nc, nodeIDs, buildRunning)
	sl.build = build
	addFixedBuildActions(&sl.host, t, client, &sl.nc, &sl.build, &sl.store, &sl.trigger, popSwapTo)
	return sl
}

func (sl *StageLogView) IsBuildRunning() bool { return sl.buildRunning }

func (sl *StageLogView) ApplySearch(pattern string) tea.Cmd {
	return sl.lv.ApplySearch(pattern)
}

func (sl *StageLogView) HandleSearchResult(msg widget.SearchResultMsg) tea.Cmd {
	sl.lv.ApplySearchResult(msg)
	return nil
}

func (sl *StageLogView) SearchQuery() string {
	return sl.lv.SearchQuery()
}

func (sl *StageLogView) HasActiveNavigation() bool { return sl.lv.HasActiveNavigation() }
func (sl *StageLogView) ClearActiveNavigation()    { sl.lv.ClearActiveNavigation() }

func (sl *StageLogView) Init() tea.Cmd {
	slog.Debug("stagelog.Init", "stage", sl.nc.StageName, "nodes", len(sl.nodeIDs), "buildRunning", sl.buildRunning)
	if nodes, ok := restoreNodesFromCache(sl.store, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs); ok {
		sl.nodes = nodes
		text := aggregateNodeLogs(sl.nodeIDs, sl.nodes)
		sl.lv.SetRawLines(widget.SplitLogLines(text))
		if allNodesTerminal(sl.nodes) {
			sl.done = true
			return widget.SelectionCheckCmd()
		}
		return tea.Batch(sl.pollLogs(), widget.SelectionCheckCmd())
	}
	return tea.Batch(sl.fetchAllLogs, widget.SelectionCheckCmd())
}

// fetchAllLogs fetches log text for all nodes (initial load).
func (sl *StageLogView) fetchAllLogs() tea.Msg {
	nodes, err := fetchNodeLogs(sl.ctx, sl.client, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs)
	if err != nil {
		if sl.ctx.Err() != nil {
			return nil
		}
		return StageLogMsg{Err: err}
	}
	text := aggregateNodeLogs(sl.nodeIDs, nodes)
	return StageLogMsg{Text: text, Nodes: nodes}
}

// pollLogs re-fetches stages (to discover new nodes) and incrementally
// fetches new output from any node that still has moreData.
func (sl *StageLogView) pollLogs() tea.Cmd {
	ctx := sl.ctx
	client := sl.client
	jobPath := sl.nc.JobPath()
	buildNumber := sl.nc.Build.Number
	stageName := sl.nc.StageName
	nodesCopy := snapshotNodes(sl.nodes)
	oldNodeIDs := make([]int, len(sl.nodeIDs))
	copy(oldNodeIDs, sl.nodeIDs)

	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}

		result := pollNodeLogs(ctx, client, jobPath, buildNumber, stageName, nodesCopy, oldNodeIDs)
		if result.Nodes == nil {
			return nil // context cancelled
		}
		return stageLogPollMsg{
			nodes:      result.Nodes,
			nodeIDs:    result.NodeIDs,
			stageFound: true,
			running:    result.Running,
		}
	}
}

func (sl *StageLogView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := sl.host.HandleMsg(msg); handled {
		return sl, cmd
	}

	switch msg := msg.(type) {
	case widget.SelectionCheckMsg:
		sl.lv.RecordSelection(msg.Text, msg.LineCount)
		return sl, widget.SelectionCheckCmd()
	case widget.CopyFlashMsg:
		return sl, sl.handleCopyFlash(msg)
	case widget.CopyFlashDoneMsg:
		sl.handleCopyFlashDone(msg)
		return sl, nil
	case ThemeChangedMsg:
		sl.handleThemeChanged(msg)
		return sl, nil
	case ConnectionRestoredMsg:
		return sl, sl.handleConnectionRestored()
	case StageLogMsg:
		return sl, sl.handleStageLogMsg(msg)
	case stageLogPollMsg:
		return sl, sl.handleStageLogPoll(msg)
	case tea.KeyMsg:
		return sl.handleKeyMsg(msg)
	}
	return sl, nil
}

func (sl *StageLogView) handleCopyFlash(msg widget.CopyFlashMsg) tea.Cmd {
	if msg.IsSel {
		sl.copySelFlash = true
	} else {
		sl.copyLogFlash = true
	}
	return widget.CopyFlashTimer(msg.IsSel)
}

func (sl *StageLogView) handleCopyFlashDone(msg widget.CopyFlashDoneMsg) {
	if msg.IsSel {
		sl.copySelFlash = false
	} else {
		sl.copyLogFlash = false
	}
}

func (sl *StageLogView) handleThemeChanged(msg ThemeChangedMsg) {
	sl.theme = msg.Theme
	sl.lv.SetTheme(msg.Theme)
	sl.host.SetTheme(msg.Theme)
}

func (sl *StageLogView) handleConnectionRestored() tea.Cmd {
	// The poll loop self-heals during transient errors once nodes are loaded.
	// Only re-run the initial fetch if it never completed (nodes still empty).
	if !sl.done && len(sl.nodes) == 0 {
		return sl.fetchAllLogs
	}
	return nil
}

func (sl *StageLogView) handleStageLogMsg(msg StageLogMsg) tea.Cmd {
	if msg.Err != nil {
		return func() tea.Msg { return ErrorMsg{Err: msg.Err} }
	}
	if msg.Nodes != nil {
		sl.nodes = msg.Nodes
	}
	persistNodeLogs(sl.store, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs, sl.nodes)
	sl.lv.SetRawLines(widget.SplitLogLines(msg.Text))
	if sl.buildRunning {
		return sl.pollLogs()
	}
	sl.done = true
	return nil
}

func (sl *StageLogView) handleStageLogPoll(msg stageLogPollMsg) tea.Cmd {
	if msg.err != nil {
		if sl.buildRunning {
			return sl.pollLogs()
		}
		return nil
	}
	sl.nodes = msg.nodes
	sl.nodeIDs = msg.nodeIDs
	persistNodeLogs(sl.store, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs, sl.nodes)
	text := aggregateNodeLogs(sl.nodeIDs, sl.nodes)
	sl.lv.SetRawLines(widget.SplitLogLines(text))
	if msg.running {
		sl.buildRunning = true
		return sl.pollLogs()
	}
	sl.buildRunning = false
	sl.done = true
	return nil
}

func (sl *StageLogView) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if handled, cmd := sl.host.HandleKey(msg); handled {
		return sl, cmd
	}
	if handleLogScrollKey(&sl.lv, msg, false) {
		return sl, nil
	}
	switch msg.String() {
	case "l":
		return sl, sl.openConsoleSwap()
	case "s":
		return sl, sl.openStageSwap()
	case "e":
		sl.lv.ToggleHighlightErrors()
	case "f2", "n":
		sl.lv.NextHighlight(true)
	case "N":
		sl.lv.NextHighlight(false)
	case "w", "W":
		sl.lv.ToggleWrap()
	case "d":
		return sl, sl.openDescribeSwap()
	case "t":
		return sl, sl.trigger.startTrigger(sl.build.Number)
	case "c":
		return sl, sl.lv.CopyLogCmd()
	case "C":
		if sl.lv.SelectionInLog() {
			return sl, sl.lv.CopySelectionCmd()
		}
	}
	return sl, nil
}

// handleLogScrollKey processes pure scroll keys against a LogViewer.
// Returns true when the key was consumed. When rightAsCols is false (StageLogView),
// "right" scrolls right by 8 cols; when true (ConsoleView), "right"/"l" both scroll.
// Note: callers that bind "l" to a different action handle it before this returns.
func handleLogScrollKey(lv *widget.LogViewer, msg tea.KeyMsg, rightIsAlsoL bool) bool {
	switch msg.String() {
	case "up", "k":
		lv.ScrollByLines(-1)
	case "down", "j":
		lv.ScrollByLines(1)
	case "pgup":
		lv.ScrollByPages(-1)
	case "pgdown":
		lv.ScrollByPages(1)
	case "g", "home":
		lv.ScrollToTop()
	case "G", "end":
		lv.ScrollToBottom()
	case "left", "h":
		lv.ScrollByCols(-8)
	case "right":
		lv.ScrollByCols(8)
	default:
		if rightIsAlsoL && msg.String() == "l" {
			lv.ScrollByCols(8)
			return true
		}
		return false
	}
	return true
}

func (sl *StageLogView) openConsoleSwap() tea.Cmd {
	nc := sl.nc
	build := sl.build
	store := sl.store
	return func() tea.Msg {
		child := NewConsoleView(sl.theme, sl.client, nc)
		child.build = build
		child.store = store
		return PopSwapViewMsg{View: child}
	}
}

func (sl *StageLogView) openStageSwap() tea.Cmd {
	nc := sl.nc
	build := sl.build
	store := sl.store
	return func() tea.Msg {
		return PopSwapViewMsg{View: NewStageView(sl.theme, sl.client, store, nc, build)}
	}
}

func (sl *StageLogView) openDescribeSwap() tea.Cmd {
	nc := sl.nc
	build := sl.build
	return func() tea.Msg {
		return PopSwapViewMsg{View: NewDescribeView(sl.theme, sl.client, sl.store, nc, build)}
	}
}

func (sl *StageLogView) View() string {
	return sl.lv.RenderVisible(sl.done, "--- end ---")
}

func (sl *StageLogView) PopupView() string {
	return sl.host.PopupView()
}

func (sl *StageLogView) Title() string {
	return sl.nc.StageName
}

func (sl *StageLogView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbFor("stagelog", sl.nc)
}

func (sl *StageLogView) ItemCount() int {
	return sl.lv.ItemCount()
}

func (sl *StageLogView) SetSize(w, h int) {
	sl.lv.SetSize(w, h)
	sl.host.SetSize(w, h-6)
}

func (sl *StageLogView) Commands() []command.Command {
	return nil
}

func (sl *StageLogView) Shortcuts() []component.Shortcut {
	shortcuts := []component.Shortcut{
		component.Nav("esc", "stages"),
		component.Filter("/", "search", false),
		component.Filter("w", "wrap", sl.lv.Wrap()),
		{Key: "c", Action: sl.lv.LogLabel(), Active: sl.copyLogFlash, Group: component.GroupAction, Rank: rankActionCopy},
	}
	shortcuts = append(shortcuts, detailViewTabs("")...)
	shortcuts = sl.host.AppendShortcuts(shortcuts) // adds T, A, x, t
	if sl.lv.SelectionInLog() {
		shortcuts = append(shortcuts, component.Shortcut{Key: "C", Action: sl.lv.SelLabel(), Active: sl.copySelFlash, Group: component.GroupAction, Rank: rankActionCopy})
	}
	shortcuts = append(shortcuts, component.Filter("e", "errors", sl.lv.HighlightErrors()))
	navActive := sl.lv.HasSearch() || sl.lv.ErrorNavActive()
	if navActive {
		posInfo := sl.lv.NavigationPositionInfo()
		nextLabel := "next"
		if posInfo != "" {
			nextLabel = "next " + posInfo
		}
		shortcuts = append(shortcuts, component.Nav("n/N", nextLabel))
	}
	return shortcuts
}

func (sl *StageLogView) HasPopup() bool {
	return sl.host.HasPopup()
}

func (sl *StageLogView) NC() NavigationContext { return sl.nc }

func (sl *StageLogView) Close() error {
	persistNodeLogs(sl.store, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs, sl.nodes)
	if sl.cancel != nil {
		sl.cancel()
	}
	return nil
}

func (sl *StageLogView) Badge() string { return sl.lv.Badge() }

func (sl *StageLogView) ScrollInfo() widget.ScrollInfo { return sl.lv.ScrollInfo() }

func (sl *StageLogView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	if sl.hasScopedParent {
		return NewMyBuildsView(t, c, s, sl.scopedParentScope, sl.scopedParentInterval)
	}
	sv := NewStageView(t, c, s, sl.nc.AtBuild(sl.nc.Build.Number), jmodel.Build{Number: sl.nc.Build.Number})
	sv.selectStageName = sl.nc.StageName
	return sv
}

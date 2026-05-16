package view

import (
	"context"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
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
	lv           LogViewer
	client       jenkins.JenkinsClient
	nc           NavigationContext
	nodeIDs      []int
	nodes        map[int]*nodeLogState
	done         bool
	buildRunning bool
	build        jenkins.Build
	store        *cache.Store
	ctx          context.Context
	cancel       context.CancelFunc
	trigger      triggerMixin
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

func NewStageLogView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext, nodeIDs []int, buildRunning bool) *StageLogView {
	ctx, cancel := context.WithCancel(context.Background())
	return &StageLogView{
		lv:           LogViewer{theme: t},
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
}

// NewStageLogViewWithBuild is like NewStageLogView but also stores the build
// so 'd' (describe) and 't' (trigger) can use it.
func NewStageLogViewWithBuild(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext, nodeIDs []int, buildRunning bool, build jenkins.Build) *StageLogView {
	sl := NewStageLogView(t, client, store, nc, nodeIDs, buildRunning)
	sl.build = build
	return sl
}

func (sl *StageLogView) IsBuildRunning() bool { return sl.buildRunning }

func (sl *StageLogView) ApplySearch(pattern string) error {
	return sl.lv.ApplySearch(pattern)
}

func (sl *StageLogView) SearchQuery() string {
	return sl.lv.SearchQueryWithCount()
}

func (sl *StageLogView) Init() tea.Cmd {
	slog.Debug("stagelog.Init", "stage", sl.nc.StageName, "nodes", len(sl.nodeIDs), "buildRunning", sl.buildRunning)
	if nodes, ok := restoreNodesFromCache(sl.store, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs); ok {
		sl.nodes = nodes
		text := aggregateNodeLogs(sl.nodeIDs, sl.nodes)
		sl.lv.rawLines = splitLines(text)
		sl.lv.recomputeLines()
		if allNodesTerminal(sl.nodes) {
			sl.done = true
			return selectionCheckCmd()
		}
		return tea.Batch(sl.pollLogs(), selectionCheckCmd())
	}
	return tea.Batch(sl.fetchAllLogs, selectionCheckCmd())
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
	if handled, cmd := sl.trigger.handleMsg(msg); handled {
		return sl, cmd
	}

	switch msg := msg.(type) {
	case selectionCheckMsg:
		sl.lv.selectionText = msg.text
		sl.lv.selectionLineCount = msg.lineCount
		sl.lv.selectionInLog = sl.lv.checkSelectionInLog(msg.text)
		return sl, selectionCheckCmd()

	case copyFlashMsg:
		if msg.isSel {
			sl.copySelFlash = true
		} else {
			sl.copyLogFlash = true
		}
		return sl, copyFlashTimer(msg.isSel)

	case copyFlashDoneMsg:
		if msg.isSel {
			sl.copySelFlash = false
		} else {
			sl.copyLogFlash = false
		}
		return sl, nil

	case ThemeChangedMsg:
		sl.lv.theme = msg.Theme
		sl.trigger.setTheme(msg.Theme)
		return sl, nil

	case ConnectionRestoredMsg:
		// The poll loop self-heals during transient errors once nodes are loaded.
		// Only re-run the initial fetch if it never completed (nodes still empty).
		if !sl.done && len(sl.nodes) == 0 {
			return sl, sl.fetchAllLogs
		}
		return sl, nil

	case StageLogMsg:
		if msg.Err != nil {
			return sl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		if msg.Nodes != nil {
			sl.nodes = msg.Nodes
		}
		persistNodeLogs(sl.store, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs, sl.nodes)
		sl.lv.rawLines = splitLines(msg.Text)
		sl.lv.recomputeLines()
		if sl.buildRunning {
			return sl, sl.pollLogs()
		}
		sl.done = true
		return sl, nil

	case stageLogPollMsg:
		if msg.err != nil {
			if sl.buildRunning {
				return sl, sl.pollLogs()
			}
			return sl, nil
		}
		sl.nodes = msg.nodes
		sl.nodeIDs = msg.nodeIDs
		persistNodeLogs(sl.store, sl.nc.JobPath(), sl.nc.Build.Number, sl.nodeIDs, sl.nodes)
		text := aggregateNodeLogs(sl.nodeIDs, sl.nodes)
		sl.lv.rawLines = splitLines(text)
		sl.lv.recomputeLines()
		if msg.running {
			sl.buildRunning = true
			return sl, sl.pollLogs()
		}
		sl.buildRunning = false
		sl.done = true
		return sl, nil

	case tea.KeyMsg:
		if handled, cmd := sl.trigger.handleKey(msg); handled {
			return sl, cmd
		}
		maxOffset := max(0, len(sl.lv.lines)-sl.lv.contentHeight())
		pageSize := max(1, sl.lv.height-1)
		switch msg.String() {
		case "up", "k":
			sl.lv.offset = max(0, sl.lv.offset-1)
		case "down", "j":
			sl.lv.offset = min(maxOffset, sl.lv.offset+1)
		case "pgup":
			sl.lv.offset = max(0, sl.lv.offset-pageSize)
		case "pgdown":
			sl.lv.offset = min(maxOffset, sl.lv.offset+pageSize)
		case "g", "home":
			sl.lv.offset = 0
		case "G", "end":
			sl.lv.offset = maxOffset
		case "left", "h":
			if !sl.lv.wrap {
				sl.lv.hOffset = max(0, sl.lv.hOffset-8)
			}
		case "right", "l":
			if !sl.lv.wrap {
				sl.lv.hOffset += 8
			}
		case "f2", "n":
			sl.lv.nextHighlight(true)
		case "N":
			sl.lv.nextHighlight(false)
		case "w", "W":
			sl.lv.wrap = !sl.lv.wrap
			if sl.lv.wrap {
				sl.lv.hOffset = 0
			}
			sl.lv.recomputeLines()
		case "d":
			build := sl.build
			nc := sl.nc
			return sl, func() tea.Msg {
				return PushViewMsg{View: NewDescribeView(sl.lv.theme, sl.client, sl.store, nc, build)}
			}
		case "t":
			return sl, sl.trigger.startTrigger(sl.build.Number)
		case "c":
			return sl, sl.lv.CopyLogCmd()
		case "C":
			if sl.lv.selectionInLog {
				return sl, sl.lv.CopySelectionCmd()
			}
		}
	}
	return sl, nil
}

func (sl *StageLogView) View() string {
	if sl.lv.height <= 0 {
		return ""
	}

	ch := sl.lv.contentHeight()
	end := min(sl.lv.offset+ch, len(sl.lv.lines))
	visible := sl.lv.lines[sl.lv.offset:end]

	rows := make([]string, 0, sl.lv.height)
	for i, dl := range visible {
		rows = append(rows, sl.lv.renderLineAt(dl, sl.lv.offset+i))
	}

	if sl.done && len(rows) < sl.lv.height {
		rows = append(rows, sl.lv.theme.Log.Dim.Render("--- end ---"))
	}

	for len(rows) < sl.lv.height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

func (sl *StageLogView) PopupView() string {
	return sl.trigger.popupView()
}

func (sl *StageLogView) Title() string {
	return sl.nc.StageName
}

func (sl *StageLogView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbFor("log", sl.nc)
}

func (sl *StageLogView) ItemCount() int {
	return sl.lv.ItemCount()
}

func (sl *StageLogView) SetSize(w, h int) {
	sl.lv.SetSize(w, h)
	sl.trigger.setSize(w, h-6)
}

func (sl *StageLogView) Commands() []command.Command {
	return nil
}

func (sl *StageLogView) Shortcuts() []component.Shortcut {
	wrapLabel := "wrap"
	if sl.lv.wrap {
		wrapLabel = "no wrap"
	}
	// esc first for stable grid positioning
	shortcuts := []component.Shortcut{
		{Key: "esc", Action: "stages"},
		{Key: "/", Action: "search"},
		{Key: "w", Action: wrapLabel},
		{Key: "c", Action: sl.lv.logLabel(), Active: sl.copyLogFlash},
		{Key: "d", Action: "describe"},
		{Key: "t", Action: "trigger"},
		{Key: "g/G", Action: "top/bottom"},
	}
	if sl.lv.selectionInLog {
		shortcuts = append(shortcuts, component.Shortcut{Key: "C", Action: sl.lv.selLabel(), Active: sl.copySelFlash})
	}
	if !sl.lv.wrap {
		shortcuts = append(shortcuts, component.Shortcut{Key: "←/→", Action: "scroll"})
	}
	if sl.lv.searchRe != nil {
		shortcuts = append(shortcuts, component.Shortcut{Key: "n/N", Action: "next/prev match"})
	} else if sl.lv.errCount+sl.lv.warnCount > 0 {
		shortcuts = append(shortcuts, component.Shortcut{Key: "n/N", Action: "next/prev issue"})
	}
	return shortcuts
}

func (sl *StageLogView) HasPopup() bool {
	return sl.trigger.hasPopup()
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

func (sl *StageLogView) ScrollInfo() ScrollInfo { return sl.lv.ScrollInfo() }

func (sl *StageLogView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	if sl.hasScopedParent {
		return NewMyBuildsView(t, c, s, sl.scopedParentScope, sl.scopedParentInterval)
	}
	sv := NewStageView(t, c, s, sl.nc.AtBuild(sl.nc.Build.Number), jenkins.Build{Number: sl.nc.Build.Number})
	sv.selectStageName = sl.nc.StageName
	return sv
}

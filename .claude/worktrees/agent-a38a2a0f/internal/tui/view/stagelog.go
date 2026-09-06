package view

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brecht/jenkins-tui/internal/cache"
	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/command"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
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
	theme        theme.Theme
	client       jenkins.JenkinsClient
	jobPath      string
	buildNumber  int
	stageName    string
	nodeIDs      []int
	nodes        map[int]*nodeLogState
	rawLines     []string
	lines        []displayLine
	offset       int
	width        int
	height       int
	done         bool
	wrap         bool
	showInternal bool
	searchQuery  string
	searchRe     *regexp.Regexp
	buildRunning bool
	store        *cache.Store
	ctx          context.Context
	cancel       context.CancelFunc
	jobName      string // human-readable project name for breadcrumb
	branchName   string // branch name for multibranch projects
}

func NewStageLogView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, jobPath string, buildNumber int, stageName string, nodeIDs []int, buildRunning bool, jobName, branchName string) *StageLogView {
	ctx, cancel := context.WithCancel(context.Background())
	return &StageLogView{
		theme:        t,
		client:       client,
		store:        store,
		jobPath:      jobPath,
		buildNumber:  buildNumber,
		stageName:    stageName,
		nodeIDs:      nodeIDs,
		nodes:        make(map[int]*nodeLogState),
		buildRunning: buildRunning,
		ctx:          ctx,
		cancel:       cancel,
		jobName:      jobName,
		branchName:   branchName,
	}
}

func (sl *StageLogView) ApplySearch(pattern string) error {
	sl.searchQuery = pattern
	sl.searchRe = compileSearchRegex(pattern)
	sl.recomputeLines()
	return nil
}

func (sl *StageLogView) SearchQuery() string {
	return sl.searchQuery
}

func (sl *StageLogView) Init() tea.Cmd {
	slog.Debug("stagelog.Init", "stage", sl.stageName, "nodes", len(sl.nodeIDs), "buildRunning", sl.buildRunning)
	if nodes, ok := restoreNodesFromCache(sl.store, sl.jobPath, sl.buildNumber, sl.nodeIDs); ok {
		sl.nodes = nodes
		text := aggregateNodeLogs(sl.nodeIDs, sl.nodes)
		sl.rawLines = splitLines(text)
		sl.recomputeLines()
		if allNodesTerminal(sl.nodes) {
			sl.done = true
			return nil
		}
		return sl.pollLogs()
	}
	return sl.fetchAllLogs
}

// fetchAllLogs fetches log text for all nodes (initial load).
func (sl *StageLogView) fetchAllLogs() tea.Msg {
	nodes, err := fetchNodeLogs(sl.ctx, sl.client, sl.jobPath, sl.buildNumber, sl.nodeIDs)
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
	jobPath := sl.jobPath
	buildNumber := sl.buildNumber
	stageName := sl.stageName
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
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		sl.theme = msg.Theme
		return sl, nil

	case StageLogMsg:
		if msg.Err != nil {
			return sl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		if msg.Nodes != nil {
			sl.nodes = msg.Nodes
		}
		persistNodeLogs(sl.store, sl.jobPath, sl.buildNumber, sl.nodeIDs, sl.nodes)
		sl.rawLines = splitLines(msg.Text)
		sl.recomputeLines()
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
		persistNodeLogs(sl.store, sl.jobPath, sl.buildNumber, sl.nodeIDs, sl.nodes)
		text := aggregateNodeLogs(sl.nodeIDs, sl.nodes)
		sl.rawLines = splitLines(text)
		sl.recomputeLines()
		if msg.running {
			sl.buildRunning = true
			return sl, sl.pollLogs()
		}
		sl.buildRunning = false
		sl.done = true
		return sl, nil

	case tea.KeyMsg:
		maxOffset := max(0, len(sl.lines)-sl.height)
		pageSize := max(1, sl.height-1)
		switch msg.String() {
		case "up", "k":
			sl.offset = max(0, sl.offset-1)
		case "down", "j":
			sl.offset = min(maxOffset, sl.offset+1)
		case "pgup":
			sl.offset = max(0, sl.offset-pageSize)
		case "pgdown":
			sl.offset = min(maxOffset, sl.offset+pageSize)
		case "g", "home":
			sl.offset = 0
		case "G", "end":
			sl.offset = maxOffset
		case "w", "W":
			sl.wrap = !sl.wrap
			sl.recomputeLines()
		case "p", "P":
			sl.showInternal = !sl.showInternal
			sl.recomputeLines()
		}
	}
	return sl, nil
}

func (sl *StageLogView) recomputeLines() {
	atBottom := len(sl.lines) == 0 || sl.offset >= max(0, len(sl.lines)-sl.height)
	sl.lines = nil
	for _, raw := range sl.rawLines {
		if !sl.showInternal && isInternalLine(raw) {
			continue
		}
		if sl.searchRe != nil && !sl.searchRe.MatchString(raw) {
			continue
		}
		sl.lines = append(sl.lines, sl.toDisplayLines(raw)...)
	}
	newMax := max(0, len(sl.lines)-sl.height)
	if atBottom {
		sl.offset = newMax
	} else {
		sl.offset = min(sl.offset, newMax)
	}
}

func (sl *StageLogView) toDisplayLines(raw string) []displayLine {
	dim := isInternalLine(raw)
	if !sl.wrap || sl.width <= 0 {
		return []displayLine{{text: raw, dim: dim}}
	}
	runes := []rune(raw)
	if len(runes) <= sl.width {
		return []displayLine{{text: raw, dim: dim}}
	}
	var chunks []displayLine
	for len(runes) > 0 {
		end := sl.width
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, displayLine{text: string(runes[:end]), dim: dim})
		runes = runes[end:]
	}
	return chunks
}

func (sl *StageLogView) View() string {
	if sl.height <= 0 {
		return ""
	}

	end := min(sl.offset+sl.height, len(sl.lines))
	visible := sl.lines[sl.offset:end]

	rows := make([]string, 0, sl.height)
	for _, dl := range visible {
		rows = append(rows, sl.renderLine(dl))
	}

	if sl.done && len(rows) < sl.height {
		rows = append(rows, consoleDim.Render("--- end ---"))
	}

	for len(rows) < sl.height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

func (sl *StageLogView) renderLine(dl displayLine) string {
	line := dl.text
	if !sl.wrap && sl.width > 0 {
		runes := []rune(line)
		if len(runes) > sl.width {
			line = string(runes[:sl.width])
		}
	}
	if dl.dim {
		return consoleDim.Render(line)
	}
	if sl.searchRe != nil {
		return highlightMatches(line, sl.searchRe, sl.theme.Search.Match, consoleNormal)
	}
	return highlightURLs(line)
}

func (sl *StageLogView) Title() string {
	return sl.stageName
}

func (sl *StageLogView) Breadcrumb() BreadcrumbSegment {
	ctx := jobRefParts(sl.jobName, sl.branchName)
	ctx = append(ctx,
		component.BreadcrumbPart{Text: fmt.Sprintf("%d", sl.buildNumber), IsBuildNum: true},
		component.BreadcrumbPart{Text: sl.stageName, Separator: ":"},
	)
	return BreadcrumbSegment{ViewType: "log", Context: ctx}
}

func (sl *StageLogView) ItemCount() int {
	return len(sl.lines)
}

func (sl *StageLogView) SetSize(w, h int) {
	needRecompute := sl.wrap && w != sl.width && len(sl.rawLines) > 0
	sl.width = w
	sl.height = h
	if needRecompute {
		sl.recomputeLines()
	} else {
		sl.offset = min(sl.offset, max(0, len(sl.lines)-h))
	}
}

func (sl *StageLogView) Commands() []command.Command {
	return nil
}

func (sl *StageLogView) Shortcuts() []component.Shortcut {
	wrapLabel := "wrap"
	if sl.wrap {
		wrapLabel = "no wrap"
	}
	pipelineLabel := "[Pipeline] show"
	if sl.showInternal {
		pipelineLabel = "[Pipeline] hide"
	}
	return []component.Shortcut{
		{Key: "/", Action: "search"},
		{Key: "w", Action: wrapLabel},
		{Key: "p", Action: pipelineLabel},
		{Key: "g/G", Action: "top/bottom"},
	}
}

func (sl *StageLogView) Close() error {
	persistNodeLogs(sl.store, sl.jobPath, sl.buildNumber, sl.nodeIDs, sl.nodes)
	if sl.cancel != nil {
		sl.cancel()
	}
	return nil
}

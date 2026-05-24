package view

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// previewLogMsg carries log data for the inline preview panel (stage-specific logs).
type previewLogMsg struct {
	stageIdx int // which stage this fetch was for (staleness check)
	nodes    map[int]*nodeLogState
	nodeIDs  []int
	running  bool
	err      error
}

// previewConsoleChunkMsg carries a chunk of the full build console log,
// used as fallback when no stages are available yet.
type previewConsoleChunkMsg struct {
	lines     []string
	nextStart int
	moreData  bool
}

// PreviewPanel manages the inline log preview shown alongside the stage table.
// It handles both stage-specific log previews and the console log fallback
// (shown when no pipeline stages are available yet).
type PreviewPanel struct {
	theme  theme.Theme
	client jmodel.JenkinsClient
	store  *cache.Store
	ctx    context.Context
	nc     NavigationContext

	// Stage-specific log preview state.
	nodeIDs       []int
	nodes         map[int]*nodeLogState
	rawLines      []string
	lines         []widget.DisplayLine
	stageIdx      int    // cursor index being previewed (-1 = none)
	stageNameSnap string // stage name snapshot for polling (avoids needing stages list)
	done          bool   // true when fetch completed (distinguishes loading vs empty)
	width         int
	height        int

	// Console log state for the synthetic Pipeline row (full build log).
	consoleRawLines  []string
	consoleLines     []widget.DisplayLine
	consoleNextStart int  // byte offset for the next progressive fetch
	pipelineConsole  bool // true when preview shows the full console log
	consoleComplete  bool // true when the console fetch has fully completed (moreData=false)

	// Search state (targets preview log only).
	searchRe *regexp.Regexp

	errCount        int
	warnCount       int
	lastVisibleKind widget.LineKind

	// Cached display results per completed stage, keyed by stage index.
	// Avoids re-running regex classification when navigating back to a stage.
	lineCache map[int]previewLineSnapshot
}

// previewLineSnapshot caches the computed display state for a completed stage
// so we can skip expensive regex re-classification when navigating back.
type previewLineSnapshot struct {
	rawLines        []string
	lines           []widget.DisplayLine
	errCount        int
	warnCount       int
	lastVisibleKind widget.LineKind
}

// NewPreviewPanel creates a preview panel for the given build.
func NewPreviewPanel(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store,
	ctx context.Context, nc NavigationContext) *PreviewPanel {
	return &PreviewPanel{
		theme:     t,
		client:    client,
		store:     store,
		ctx:       ctx,
		nc:        nc,
		stageIdx:  -1,
		lineCache: make(map[int]previewLineSnapshot),
	}
}

// SetContext replaces the cancellation context (e.g. after re-init).
func (pp *PreviewPanel) SetContext(ctx context.Context) { pp.ctx = ctx }

// SetTheme updates the rendering theme.
func (pp *PreviewPanel) SetTheme(t theme.Theme) { pp.theme = t }

// SetSearch updates the search filter and recomputes visible lines.
func (pp *PreviewPanel) SetSearch(re *regexp.Regexp) {
	pp.searchRe = re
	pp.recomputeLines()
}

// SetSize sets the preview panel's inner dimensions.
func (pp *PreviewPanel) SetSize(w, h int) {
	pp.width = w
	pp.height = h
}

// SetBuildNumber updates the build number (e.g. when a pending build is found).
func (pp *PreviewPanel) SetBuildNumber(n int) { pp.nc.Build.Number = n }

// StageIdx returns the stage index currently being previewed.
func (pp *PreviewPanel) StageIdx() int { return pp.stageIdx }

// Done returns true when the preview fetch has completed.
func (pp *PreviewPanel) Done() bool { return pp.done }

// HandleMsg processes preview-related messages. Returns (handled, cmd).
func (pp *PreviewPanel) HandleMsg(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case previewLogMsg:
		return true, pp.handlePreviewLog(msg)
	case previewConsoleChunkMsg:
		return true, pp.handleConsoleChunk(msg)
	}
	return false, nil
}

func (pp *PreviewPanel) handlePreviewLog(msg previewLogMsg) tea.Cmd {
	// Discard stale results (cursor moved since fetch started).
	if msg.stageIdx != pp.stageIdx {
		slog.Debug("preview.handlePreviewLog: stale, discarded", "msgIdx", msg.stageIdx, "curIdx", pp.stageIdx)
		return nil
	}
	if msg.err != nil {
		slog.Debug("preview.handlePreviewLog: error", "stageIdx", msg.stageIdx, "err", msg.err)
		pp.done = true
		return nil
	}
	// Guard: don't let a poll result wipe out good cached data.
	// This can happen when pollNodeLogs matches a different stage with the
	// same name (parent vs child) and returns mismatched nodeIDs.
	newText := aggregateNodeLogs(msg.nodeIDs, msg.nodes)
	newRawLines := widget.SplitLogLines(newText)
	if len(newRawLines) == 0 && len(pp.rawLines) > 0 {
		slog.Debug("preview.handlePreviewLog: poll returned empty, keeping cached data",
			"stageIdx", msg.stageIdx, "cachedLines", len(pp.rawLines), "running", msg.running)
		if msg.running {
			return pp.pollLogs()
		}
		pp.done = true
		return nil
	}
	pp.nodes = msg.nodes
	pp.nodeIDs = msg.nodeIDs
	persistNodeLogs(pp.store, pp.nc.JobPath(), pp.nc.Build.Number, pp.nodeIDs, pp.nodes)
	pp.rawLines = newRawLines
	pp.recomputeLines()
	slog.Debug("preview.handlePreviewLog: applied",
		"stageIdx", msg.stageIdx, "lines", len(pp.lines), "running", msg.running)
	if msg.running {
		return pp.pollLogs()
	}
	pp.done = true
	pp.saveLineCache(pp.stageIdx)
	return nil
}

func (pp *PreviewPanel) handleConsoleChunk(msg previewConsoleChunkMsg) tea.Cmd {
	if !pp.pipelineConsole {
		return nil
	}
	pp.appendConsoleLines(msg.lines)
	pp.consoleNextStart = msg.nextStart
	if msg.moreData {
		return pp.FetchConsoleFallback(msg.nextStart, time.Second)
	}
	pp.consoleComplete = true
	return nil
}

// pipelinePreviewIdx is the sentinel stageIdx used when the cursor is on the
// synthetic Pipeline row. The preview panel shows the console log fallback.
const pipelinePreviewIdx = -2

// UpdateForCursor starts fetching logs for the given cursor position if it
// differs from the current preview. Returns a tea.Cmd to start the fetch.
// Pass pipelinePreviewIdx (-2) for the synthetic Pipeline row.
//
// Acts as a state-machine dispatcher: routes to one of three handler states
// — `pipeline-row`, `same-stage-revisit`, or `new-stage`. Each handler
// resolves its own sub-states (cache hits, polling, fetch) internally.
func (pp *PreviewPanel) UpdateForCursor(idx int, stages []jmodel.Stage) tea.Cmd {
	if idx == pipelinePreviewIdx {
		return pp.enterPipelineState()
	}
	pp.pipelineConsole = false

	if idx == pp.stageIdx {
		return pp.refreshSameStageState(idx, stages)
	}
	return pp.enterStageState(idx, stages)
}

// enterPipelineState transitions into the "pipeline console fallback" state.
// Sub-states: already-showing (no-op), cache-hit (reuse without fetch),
// fresh-entry (preserve current tail, kick off console fetch).
func (pp *PreviewPanel) enterPipelineState() tea.Cmd {
	if pp.stageIdx == pipelinePreviewIdx {
		if pp.consoleComplete && len(pp.consoleRawLines) > 0 {
			slog.Debug("preview.UpdateForCursor: Pipeline row (cache hit)")
		}
		return nil // already showing console fallback
	}
	slog.Debug("preview.UpdateForCursor: Pipeline row")

	if pp.consoleComplete && len(pp.consoleRawLines) > 0 {
		pp.stageIdx = pipelinePreviewIdx
		pp.pipelineConsole = true
		pp.stageNameSnap = "Pipeline"
		slog.Debug("preview.UpdateForCursor: Pipeline row (cache hit, restoring)")
		return nil
	}

	// Preserve the current stage's tail (pp.lines) into consoleLines so it shows
	// immediately while we fetch the full console. The tail of stage logs matches
	// the tail of console logs anyway.
	if len(pp.consoleLines) == 0 && len(pp.lines) > 0 {
		pp.consoleLines = pp.lines
		slog.Debug("preview.UpdateForCursor: Pipeline row (preserving stage tail)")
	}

	pp.resetCounters()
	pp.stageIdx = pipelinePreviewIdx
	pp.nodes = make(map[int]*nodeLogState)
	pp.nodeIDs = nil
	pp.rawLines = nil
	pp.lines = nil
	pp.stageNameSnap = "Pipeline"
	pp.done = false
	pp.pipelineConsole = true
	pp.consoleRawLines = nil
	pp.consoleComplete = false
	slog.Debug("preview.UpdateForCursor: Pipeline row (fetching full console)")
	return pp.FetchConsoleFallback(0, 0)
}

// refreshSameStageState handles the cursor landing on the already-previewed
// stage. The only meaningful transition is "NodeIDs just appeared" — every
// other revisit is a no-op.
func (pp *PreviewPanel) refreshSameStageState(idx int, stages []jmodel.Stage) tea.Cmd {
	if !pp.done || len(pp.nodeIDs) > 0 || idx < 0 || idx >= len(stages) || len(stages[idx].NodeIDs) == 0 {
		return nil
	}
	slog.Debug("preview.UpdateForCursor: nodeIDs appeared", "idx", idx, "name", stages[idx].Name)
	return pp.enterStageState(idx, stages)
}

// enterStageState transitions into the "preview a specific stage" state.
// Sub-states resolved by tryRestoreStageFromCache: line-cache-hit (done),
// node-cache-hit-terminal (done), node-cache-hit-running (poll), miss (fetch).
func (pp *PreviewPanel) enterStageState(idx int, stages []jmodel.Stage) tea.Cmd {
	slog.Debug("preview.UpdateForCursor", "from", pp.stageIdx, "to", idx)
	pp.resetCounters()
	pp.stageIdx = idx
	pp.nodes = make(map[int]*nodeLogState)
	pp.rawLines = nil
	pp.lines = nil
	pp.done = false

	if idx < 0 || idx >= len(stages) || len(stages[idx].NodeIDs) == 0 {
		pp.nodeIDs = nil
		pp.stageNameSnap = ""
		pp.done = true
		return nil
	}
	pp.nodeIDs = stages[idx].NodeIDs
	pp.stageNameSnap = stages[idx].Name

	if cmd, ok := pp.tryRestoreStageFromCache(idx); ok {
		return cmd
	}
	slog.Debug("preview.UpdateForCursor: cache miss, fetching",
		"idx", idx, "name", pp.stageNameSnap, "nodeIDs", len(pp.nodeIDs))
	return pp.fetchLogs()
}

// resetCounters clears the err/warn/last-kind tracking used by recomputeLines.
func (pp *PreviewPanel) resetCounters() {
	pp.errCount = 0
	pp.warnCount = 0
	pp.lastVisibleKind = widget.LineKindNormal
}

// tryRestoreStageFromCache attempts the two-tier cache restore shared by
// UpdateForCursor and Restart. Returns (cmd, true) if either cache served
// the request, (nil, false) if the caller must fall through to a fresh fetch.
//
// Tier 1: display-line cache (pre-classified lines, regex-free path).
// Tier 2: per-node log cache — terminal -> done, running -> poll.
func (pp *PreviewPanel) tryRestoreStageFromCache(idx int) (tea.Cmd, bool) {
	if pp.searchRe == nil {
		if snap, ok := pp.lineCache[idx]; ok {
			pp.rawLines = snap.rawLines
			pp.lines = snap.lines
			pp.errCount = snap.errCount
			pp.warnCount = snap.warnCount
			pp.lastVisibleKind = snap.lastVisibleKind
			pp.done = true
			slog.Debug("preview.tryRestoreStageFromCache: line cache hit",
				"idx", idx, "name", pp.stageNameSnap, "lines", len(pp.lines))
			return nil, true
		}
	}

	nodes, ok := restoreNodesFromCache(pp.store, pp.nc.JobPath(), pp.nc.Build.Number, pp.nodeIDs)
	if !ok {
		return nil, false
	}
	pp.nodes = nodes
	text := aggregateNodeLogs(pp.nodeIDs, pp.nodes)
	pp.rawLines = widget.SplitLogLines(text)
	pp.recomputeLines()
	pp.done = allNodesTerminal(pp.nodes)
	slog.Debug("preview.tryRestoreStageFromCache: node cache hit",
		"idx", idx, "name", pp.stageNameSnap, "lines", len(pp.lines), "done", pp.done)
	if pp.done {
		pp.saveLineCache(idx)
		return nil, true
	}
	return pp.pollLogs(), true
}

// Restart force-restarts the preview fetch for the given cursor,
// used when the view is re-revealed after a child view is popped.
func (pp *PreviewPanel) Restart(idx int, stages []jmodel.Stage) tea.Cmd {
	if idx == pipelinePreviewIdx {
		pp.stageIdx = pipelinePreviewIdx
		pp.pipelineConsole = true
		pp.stageNameSnap = "Pipeline"

		// Try to reuse cached console data to avoid a blank-frame flash.
		if pp.consoleComplete && len(pp.consoleRawLines) > 0 {
			slog.Debug("preview.Restart: Pipeline row (cache hit, restoring)")
			return nil
		}
		// Don't clear consoleLines — keep existing tail visible.
		slog.Debug("preview.Restart: Pipeline row (cache miss, fetching)")
		pp.consoleRawLines = nil
		pp.consoleComplete = false
		return pp.FetchConsoleFallback(0, 0)
	}
	pp.pipelineConsole = false
	if idx < 0 || idx >= len(stages) || len(stages[idx].NodeIDs) == 0 {
		return nil
	}
	pp.stageIdx = idx
	pp.nodeIDs = stages[idx].NodeIDs
	pp.stageNameSnap = stages[idx].Name

	if cmd, ok := pp.tryRestoreStageFromCache(idx); ok {
		return cmd
	}
	pp.nodes = make(map[int]*nodeLogState)
	pp.rawLines = nil
	pp.lines = nil
	pp.done = false
	return pp.fetchLogs()
}

// fetchLogs performs the initial log fetch for all nodes of the previewed stage.
//
// The work is a small state machine: (1) populate cache misses, (2) check
// the stage's running status and pick up any newly-added child nodes. Each
// step is delegated to a helper so the closure stays a thin orchestrator.
func (pp *PreviewPanel) fetchLogs() tea.Cmd {
	ctx := pp.ctx
	client := pp.client
	store := pp.store
	jobPath := pp.nc.JobPath()
	buildNumber := pp.nc.Build.Number
	nodeIDs := make([]int, len(pp.nodeIDs))
	copy(nodeIDs, pp.nodeIDs)
	stageIdx := pp.stageIdx
	stageName := pp.stageNameSnap

	return func() tea.Msg {
		nodes, err := loadInitialNodeLogs(ctx, client, store, jobPath, buildNumber, nodeIDs)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return previewLogMsg{stageIdx: stageIdx, err: err}
		}
		newIDs, running := discoverStageNodes(ctx, client, jobPath, buildNumber, stageName, nodeIDs, nodes)
		return previewLogMsg{
			stageIdx: stageIdx,
			nodes:    nodes,
			nodeIDs:  newIDs,
			running:  running,
		}
	}
}

// loadInitialNodeLogs fills `nodes` with a log state for every nodeID, using
// the on-disk cache first and a progressive fetch for misses. Returns the
// populated map, or the first non-cancellation error encountered.
func loadInitialNodeLogs(
	ctx context.Context,
	client jmodel.JenkinsClient,
	store *cache.Store,
	jobPath string,
	buildNumber int,
	nodeIDs []int,
) (map[int]*nodeLogState, error) {
	nodes, allCached := restoreNodesFromCache(store, jobPath, buildNumber, nodeIDs)
	if allCached {
		return nodes, nil
	}
	if nodes == nil {
		nodes = make(map[int]*nodeLogState)
	}
	for _, nodeID := range nodeIDs {
		if _, ok := nodes[nodeID]; ok {
			continue
		}
		nl, err := client.GetNodeLogProgressive(ctx, jobPath, buildNumber, nodeID, 0)
		if err != nil {
			return nodes, err
		}
		nodes[nodeID] = &nodeLogState{
			text:      strings.TrimRight(nl.Text, "\n"),
			nextStart: nl.NextStart,
			moreData:  nl.MoreData,
		}
	}
	return nodes, nil
}

// discoverStageNodes asks Jenkins whether the previewed stage is still
// running and whether new child nodes have appeared since we last looked.
// Returns the (possibly extended) nodeID list and the running flag.
//
// Matching by name + smallest overlapping NodeID set keeps a child preview
// from being promoted to its enclosing parent — see findOwningStage.
func discoverStageNodes(
	ctx context.Context,
	client jmodel.JenkinsClient,
	jobPath string,
	buildNumber int,
	stageName string,
	nodeIDs []int,
	nodes map[int]*nodeLogState,
) ([]int, bool) {
	fetchedStages, err := client.ListStages(ctx, jobPath, buildNumber)
	if err != nil {
		return nodeIDs, false
	}
	idx, ok := findOwningStage(fetchedStages, stageName, nodeIDs)
	if !ok {
		return nodeIDs, false
	}
	s := fetchedStages[idx]
	running := s.Status == jmodel.BuildStatusRunning
	if len(s.NodeIDs) <= len(nodeIDs) {
		return nodeIDs, running
	}
	for _, nid := range s.NodeIDs {
		if _, exists := nodes[nid]; exists {
			continue
		}
		nl, err := client.GetNodeLogProgressive(ctx, jobPath, buildNumber, nid, 0)
		if err != nil {
			continue
		}
		nodes[nid] = &nodeLogState{
			text:      strings.TrimRight(nl.Text, "\n"),
			nextStart: nl.NextStart,
			moreData:  nl.MoreData,
		}
	}
	return s.NodeIDs, running
}

// pollLogs incrementally fetches new log output for the preview panel.
func (pp *PreviewPanel) pollLogs() tea.Cmd {
	ctx := pp.ctx
	client := pp.client
	jobPath := pp.nc.JobPath()
	buildNumber := pp.nc.Build.Number
	stageIdx := pp.stageIdx
	stageName := pp.stageNameSnap
	nodesCopy := snapshotNodes(pp.nodes)
	oldNodeIDs := make([]int, len(pp.nodeIDs))
	copy(oldNodeIDs, pp.nodeIDs)

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
		return previewLogMsg{
			stageIdx: stageIdx,
			nodes:    result.Nodes,
			nodeIDs:  result.NodeIDs,
			running:  result.Running,
		}
	}
}

// recomputeLines rebuilds the preview display lines from raw lines,
// filtering out internal [Pipeline] lines.
// saveLineCache stores the current display state for the given stage index,
// so future navigations to this stage skip regex re-classification.
func (pp *PreviewPanel) saveLineCache(idx int) {
	if idx < 0 || pp.searchRe != nil {
		return // don't cache filtered results
	}
	pp.lineCache[idx] = previewLineSnapshot{
		rawLines:        pp.rawLines,
		lines:           pp.lines,
		errCount:        pp.errCount,
		warnCount:       pp.warnCount,
		lastVisibleKind: pp.lastVisibleKind,
	}
}

func (pp *PreviewPanel) recomputeLines() {
	pp.lines = nil
	pp.errCount = 0
	pp.warnCount = 0
	prevKind := widget.LineKindNormal
	for _, raw := range pp.rawLines {
		if widget.IsInternalLine(raw) {
			continue
		}
		if pp.searchRe != nil && !pp.searchRe.MatchString(raw) {
			prevKind = widget.LineKindNormal
			continue
		}
		kind := widget.ClassifyContentLine(raw)
		if kind != widget.LineKindNormal && kind != prevKind {
			switch kind {
			case widget.LineKindError:
				pp.errCount++
			case widget.LineKindWarning:
				pp.warnCount++
			}
		}
		prevKind = kind
		pp.lines = append(pp.lines, widget.DisplayLine{Text: raw, Src: raw, Dim: false, Kind: kind})
	}
	pp.lastVisibleKind = prevKind
}

// FetchConsoleFallback fetches the full build progressive log for the preview
// panel, used when no pipeline stages are available yet.
func (pp *PreviewPanel) FetchConsoleFallback(start int, delay time.Duration) tea.Cmd {
	ctx := pp.ctx
	client := pp.client
	jobPath := pp.nc.JobPath()
	buildNumber := pp.nc.Build.Number
	return func() tea.Msg {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(delay):
			}
		}
		chunk, err := client.GetProgressiveLog(ctx, jobPath, buildNumber, start)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return nil
		}
		return previewConsoleChunkMsg{
			lines:     widget.SplitLogLines(chunk.Text),
			nextStart: chunk.NextStart,
			moreData:  chunk.MoreData,
		}
	}
}

// appendConsoleLines adds new console lines and rebuilds display lines.
func (pp *PreviewPanel) appendConsoleLines(newLines []string) {
	prevKind := pp.lastVisibleKind
	for _, raw := range newLines {
		pp.consoleRawLines = append(pp.consoleRawLines, raw)
		if !widget.IsInternalLine(raw) {
			kind := widget.ClassifyContentLine(raw)
			if kind != widget.LineKindNormal && kind != prevKind {
				switch kind {
				case widget.LineKindError:
					pp.errCount++
				case widget.LineKindWarning:
					pp.warnCount++
				}
			}
			prevKind = kind
			pp.consoleLines = append(pp.consoleLines, widget.DisplayLine{Text: raw, Src: raw, Dim: false, Kind: kind})
		}
	}
	pp.lastVisibleKind = prevKind
}

// View renders the preview panel content.
func (pp *PreviewPanel) View(stages []jmodel.Stage) string {
	if pp.height <= 0 {
		return ""
	}

	// Pipeline row — show the full build console log.
	if pp.pipelineConsole {
		return pp.renderLogTail(pp.consoleLines)
	}

	// Stage-specific log preview.
	if pp.stageIdx < 0 || pp.stageIdx >= len(stages) {
		slog.Debug("preview.View: stageIdx out of range", "stageIdx", pp.stageIdx, "numStages", len(stages))
		return pp.renderLogTail(nil)
	}
	if len(stages[pp.stageIdx].NodeIDs) == 0 {
		slog.Debug("preview.View: stage has no NodeIDs",
			"stageIdx", pp.stageIdx, "name", stages[pp.stageIdx].Name,
			"ppNodeIDs", len(pp.nodeIDs), "ppLines", len(pp.lines), "ppDone", pp.done)
		return pp.renderLogTail(nil)
	}
	if !pp.done && len(pp.lines) == 0 {
		rows := []string{pp.theme.Log.Dim.Render("loading...")}
		for len(rows) < pp.height {
			rows = append(rows, "")
		}
		return strings.Join(rows, "\n")
	}
	return pp.renderLogTail(pp.lines)
}

// renderLogTail renders the last pp.height lines from the given display lines.
func (pp *PreviewPanel) renderLogTail(lines []widget.DisplayLine) string {
	rows := make([]string, 0, pp.height)

	if len(lines) > 0 {
		start := len(lines) - pp.height
		if start < 0 {
			start = 0
		}
		for _, dl := range lines[start:] {
			line := dl.Text
			if pp.width > 0 {
				line, _ = widget.TruncateToColumns(line, pp.width)
			}
			if dl.Dim {
				rows = append(rows, pp.theme.Log.Dim.Render(line))
			} else if pp.searchRe != nil {
				rows = append(rows, widget.HighlightMatches(line, pp.searchRe, pp.theme.Search.Match, pp.theme.Log.Normal))
			} else {
				switch dl.Kind {
				case widget.LineKindError:
					rows = append(rows, pp.theme.Log.Error.Render(line))
				case widget.LineKindWarning:
					rows = append(rows, pp.theme.Log.Warning.Render(line))
				default:
					rows = append(rows, widget.ApplyOSC8(line, dl.Src, 0, pp.theme))
				}
			}
		}
	}

	for len(rows) < pp.height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

// Breadcrumb returns the breadcrumb segment for the preview panel.
func (pp *PreviewPanel) Breadcrumb(stages []jmodel.Stage, pending bool) BreadcrumbSegment {
	nc := pp.nc
	if pending {
		nc.Build.Number = 0
	}
	ctx := contextParts(nc)
	if !pp.pipelineConsole && pp.stageIdx >= 0 && pp.stageIdx < len(stages) {
		ctx = append(ctx, component.BreadcrumbPart{Text: stages[pp.stageIdx].Name, Separator: ":"})
	}
	return BreadcrumbSegment{ViewType: "log", Context: ctx}
}

// ConsoleSnapshot returns the accumulated console log state so it can be used
// to seed a ConsoleView without a blank-frame flash on navigation.
func (pp *PreviewPanel) ConsoleSnapshot() (rawLines []string, nextStart int, complete bool) {
	return pp.consoleRawLines, pp.consoleNextStart, pp.consoleComplete
}

// ItemCount returns the number of visible lines in the preview panel.
func (pp *PreviewPanel) ItemCount(stages []jmodel.Stage) int {
	if len(stages) == 0 || pp.pipelineConsole {
		return len(pp.consoleLines)
	}
	return len(pp.lines)
}

// Badge returns a styled badge string for error/warning counts, or "" if none.
func (pp *PreviewPanel) Badge() string {
	return widget.RenderErrWarnBadge(pp.theme, pp.warnCount, pp.errCount)
}

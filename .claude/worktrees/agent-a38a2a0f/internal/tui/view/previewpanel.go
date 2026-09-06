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
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
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
	theme       theme.Theme
	client      jenkins.JenkinsClient
	store       *cache.Store
	ctx         context.Context
	jobPath     string
	buildNumber int
	jobName     string
	branchName  string

	// Stage-specific log preview state.
	nodeIDs       []int
	nodes         map[int]*nodeLogState
	rawLines      []string
	lines         []displayLine
	stageIdx      int    // cursor index being previewed (-1 = none)
	stageNameSnap string // stage name snapshot for polling (avoids needing stages list)
	done          bool   // true when fetch completed (distinguishes loading vs empty)
	width         int
	height        int

	// Console log state for the synthetic Pipeline row (full build log).
	consoleRawLines []string
	consoleLines    []displayLine
	pipelineConsole bool // true when preview shows the full console log

	// Search state (targets preview log only).
	searchRe *regexp.Regexp
}

// NewPreviewPanel creates a preview panel for the given build.
func NewPreviewPanel(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store,
	ctx context.Context, jobPath string, buildNumber int, jobName, branchName string) *PreviewPanel {
	return &PreviewPanel{
		theme:       t,
		client:      client,
		store:       store,
		ctx:         ctx,
		jobPath:     jobPath,
		buildNumber: buildNumber,
		jobName:     jobName,
		branchName:  branchName,
		stageIdx:    -1,
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
func (pp *PreviewPanel) SetBuildNumber(n int) { pp.buildNumber = n }

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
	newRawLines := splitLines(newText)
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
	persistNodeLogs(pp.store, pp.jobPath, pp.buildNumber, pp.nodeIDs, pp.nodes)
	pp.rawLines = newRawLines
	pp.recomputeLines()
	slog.Debug("preview.handlePreviewLog: applied",
		"stageIdx", msg.stageIdx, "lines", len(pp.lines), "running", msg.running)
	if msg.running {
		return pp.pollLogs()
	}
	pp.done = true
	return nil
}

func (pp *PreviewPanel) handleConsoleChunk(msg previewConsoleChunkMsg) tea.Cmd {
	if !pp.pipelineConsole {
		return nil
	}
	pp.appendConsoleLines(msg.lines)
	if msg.moreData {
		return pp.FetchConsoleFallback(msg.nextStart, time.Second)
	}
	return nil
}

// pipelinePreviewIdx is the sentinel stageIdx used when the cursor is on the
// synthetic Pipeline row. The preview panel shows the console log fallback.
const pipelinePreviewIdx = -2

// UpdateForCursor starts fetching logs for the given cursor position if it
// differs from the current preview. Returns a tea.Cmd to start the fetch.
// Pass pipelinePreviewIdx (-2) for the synthetic Pipeline row.
func (pp *PreviewPanel) UpdateForCursor(idx int, stages []jenkins.Stage) tea.Cmd {
	// Pipeline row — show console log fallback in the preview.
	if idx == pipelinePreviewIdx {
		if pp.stageIdx == pipelinePreviewIdx {
			return nil // already showing console fallback
		}
		slog.Debug("preview.UpdateForCursor: Pipeline row")
		pp.stageIdx = pipelinePreviewIdx
		pp.nodes = make(map[int]*nodeLogState)
		pp.nodeIDs = nil
		pp.rawLines = nil
		pp.lines = nil
		pp.stageNameSnap = "Pipeline"
		pp.done = false
		pp.pipelineConsole = true
		return pp.FetchConsoleFallback(0, 0)
	}
	pp.pipelineConsole = false

	if idx == pp.stageIdx {
		// Re-evaluate if NodeIDs appeared for a stage that was previously
		// empty (e.g. Jenkins hadn't assigned flow nodes yet on first sight).
		if !pp.done || len(pp.nodeIDs) > 0 || idx < 0 || idx >= len(stages) || len(stages[idx].NodeIDs) == 0 {
			return nil
		}
		slog.Debug("preview.UpdateForCursor: nodeIDs appeared", "idx", idx, "name", stages[idx].Name)
	}
	slog.Debug("preview.UpdateForCursor", "from", pp.stageIdx, "to", idx)
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

	// Try synchronous cache restore to avoid a blank-frame flash.
	if nodes, ok := restoreNodesFromCache(pp.store, pp.jobPath, pp.buildNumber, pp.nodeIDs); ok {
		pp.nodes = nodes
		text := aggregateNodeLogs(pp.nodeIDs, pp.nodes)
		pp.rawLines = splitLines(text)
		pp.recomputeLines()
		pp.done = allNodesTerminal(pp.nodes)
		slog.Debug("preview.UpdateForCursor: cache hit",
			"idx", idx, "name", pp.stageNameSnap,
			"lines", len(pp.lines), "done", pp.done)
		if pp.done {
			return nil
		}
		return pp.pollLogs()
	}
	slog.Debug("preview.UpdateForCursor: cache miss, fetching",
		"idx", idx, "name", pp.stageNameSnap, "nodeIDs", len(pp.nodeIDs))
	return pp.fetchLogs()
}

// Restart force-restarts the preview fetch for the given cursor,
// used when the view is re-revealed after a child view is popped.
func (pp *PreviewPanel) Restart(idx int, stages []jenkins.Stage) tea.Cmd {
	if idx == pipelinePreviewIdx {
		pp.stageIdx = pipelinePreviewIdx
		pp.pipelineConsole = true
		pp.stageNameSnap = "Pipeline"
		pp.consoleRawLines = nil
		pp.consoleLines = nil
		return pp.FetchConsoleFallback(0, 0)
	}
	pp.pipelineConsole = false
	if idx < 0 || idx >= len(stages) || len(stages[idx].NodeIDs) == 0 {
		return nil
	}
	pp.stageIdx = idx
	pp.nodeIDs = stages[idx].NodeIDs
	pp.stageNameSnap = stages[idx].Name

	if nodes, ok := restoreNodesFromCache(pp.store, pp.jobPath, pp.buildNumber, pp.nodeIDs); ok {
		pp.nodes = nodes
		text := aggregateNodeLogs(pp.nodeIDs, pp.nodes)
		pp.rawLines = splitLines(text)
		pp.recomputeLines()
		pp.done = allNodesTerminal(pp.nodes)
		if pp.done {
			return nil
		}
		return pp.pollLogs()
	}
	pp.nodes = make(map[int]*nodeLogState)
	pp.rawLines = nil
	pp.lines = nil
	pp.done = false
	return pp.fetchLogs()
}

// fetchLogs performs the initial log fetch for all nodes of the previewed stage.
func (pp *PreviewPanel) fetchLogs() tea.Cmd {
	ctx := pp.ctx
	client := pp.client
	store := pp.store
	jobPath := pp.jobPath
	buildNumber := pp.buildNumber
	nodeIDs := make([]int, len(pp.nodeIDs))
	copy(nodeIDs, pp.nodeIDs)
	stageIdx := pp.stageIdx

	return func() tea.Msg {
		// Try cache for individual nodes (async path).
		nodes, allCached := restoreNodesFromCache(store, jobPath, buildNumber, nodeIDs)
		if !allCached {
			if nodes == nil {
				nodes = make(map[int]*nodeLogState)
			}
			for _, nodeID := range nodeIDs {
				if _, ok := nodes[nodeID]; ok {
					continue
				}
				nl, err := client.GetNodeLogProgressive(ctx, jobPath, buildNumber, nodeID, 0)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return previewLogMsg{stageIdx: stageIdx, err: err}
				}
				nodes[nodeID] = &nodeLogState{
					text:      strings.TrimRight(nl.Text, "\n"),
					nextStart: nl.NextStart,
					moreData:  nl.MoreData,
				}
			}
		}
		// Check if stage is still running — match by nodeID overlap, not name.
		running := false
		idSet := make(map[int]struct{}, len(nodeIDs))
		for _, id := range nodeIDs {
			idSet[id] = struct{}{}
		}
		fetchedStages, err := client.ListStages(ctx, jobPath, buildNumber)
		if err == nil {
			for _, s := range fetchedStages {
				matched := false
				for _, nid := range s.NodeIDs {
					if _, ok := idSet[nid]; ok {
						matched = true
						break
					}
				}
				if matched {
					running = s.Status == jenkins.BuildStatusRunning
					if len(s.NodeIDs) > len(nodeIDs) {
						nodeIDs = s.NodeIDs
						for _, nid := range s.NodeIDs {
							if _, ok := nodes[nid]; !ok {
								nl, err := client.GetNodeLogProgressive(ctx, jobPath, buildNumber, nid, 0)
								if err == nil {
									nodes[nid] = &nodeLogState{
										text:      strings.TrimRight(nl.Text, "\n"),
										nextStart: nl.NextStart,
										moreData:  nl.MoreData,
									}
								}
							}
						}
					}
					break
				}
			}
		}
		return previewLogMsg{
			stageIdx: stageIdx,
			nodes:    nodes,
			nodeIDs:  nodeIDs,
			running:  running,
		}
	}
}

// pollLogs incrementally fetches new log output for the preview panel.
func (pp *PreviewPanel) pollLogs() tea.Cmd {
	ctx := pp.ctx
	client := pp.client
	jobPath := pp.jobPath
	buildNumber := pp.buildNumber
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
func (pp *PreviewPanel) recomputeLines() {
	pp.lines = nil
	for _, raw := range pp.rawLines {
		if isInternalLine(raw) {
			continue
		}
		if pp.searchRe != nil && !pp.searchRe.MatchString(raw) {
			continue
		}
		pp.lines = append(pp.lines, displayLine{text: raw, dim: false})
	}
}

// FetchConsoleFallback fetches the full build progressive log for the preview
// panel, used when no pipeline stages are available yet.
func (pp *PreviewPanel) FetchConsoleFallback(start int, delay time.Duration) tea.Cmd {
	ctx := pp.ctx
	client := pp.client
	jobPath := pp.jobPath
	buildNumber := pp.buildNumber
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
			lines:     splitLines(chunk.Text),
			nextStart: chunk.NextStart,
			moreData:  chunk.MoreData,
		}
	}
}

// appendConsoleLines adds new console lines and rebuilds display lines.
func (pp *PreviewPanel) appendConsoleLines(newLines []string) {
	for _, raw := range newLines {
		pp.consoleRawLines = append(pp.consoleRawLines, raw)
		if !isInternalLine(raw) {
			pp.consoleLines = append(pp.consoleLines, displayLine{text: raw, dim: false})
		}
	}
}

// View renders the preview panel content.
func (pp *PreviewPanel) View(stages []jenkins.Stage) string {
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
		rows := []string{consoleDim.Render("loading...")}
		for len(rows) < pp.height {
			rows = append(rows, "")
		}
		return strings.Join(rows, "\n")
	}
	return pp.renderLogTail(pp.lines)
}

// renderLogTail renders the last pp.height lines from the given display lines.
func (pp *PreviewPanel) renderLogTail(lines []displayLine) string {
	rows := make([]string, 0, pp.height)

	if len(lines) > 0 {
		start := len(lines) - pp.height
		if start < 0 {
			start = 0
		}
		for _, dl := range lines[start:] {
			line := dl.text
			if pp.width > 0 {
				runes := []rune(line)
				if len(runes) > pp.width {
					line = string(runes[:pp.width])
				}
			}
			if dl.dim {
				rows = append(rows, consoleDim.Render(line))
			} else if pp.searchRe != nil {
				rows = append(rows, highlightMatches(line, pp.searchRe, pp.theme.Search.Match, consoleNormal))
			} else {
				rows = append(rows, highlightURLs(line))
			}
		}
	}

	for len(rows) < pp.height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

// Breadcrumb returns the breadcrumb segment for the preview panel.
func (pp *PreviewPanel) Breadcrumb(stages []jenkins.Stage, pending bool) BreadcrumbSegment {
	ctx := jobRefParts(pp.jobName, pp.branchName)
	if !pending && pp.buildNumber > 0 {
		ctx = append(ctx, component.BreadcrumbPart{Text: fmt.Sprintf("%d", pp.buildNumber), IsBuildNum: true})
	}
	if pp.pipelineConsole {
		ctx = append(ctx, component.BreadcrumbPart{Text: "Pipeline", Separator: ":"})
	} else if pp.stageIdx >= 0 && pp.stageIdx < len(stages) {
		ctx = append(ctx, component.BreadcrumbPart{Text: stages[pp.stageIdx].Name, Separator: ":"})
	}
	return BreadcrumbSegment{ViewType: "log", Context: ctx}
}

// ItemCount returns the number of visible lines in the preview panel.
func (pp *PreviewPanel) ItemCount(stages []jenkins.Stage) int {
	if len(stages) == 0 || pp.pipelineConsole {
		return len(pp.consoleLines)
	}
	return len(pp.lines)
}

package view

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/brecht/jenkins-tui/internal/cache"
	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/command"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

const (
	colStageStatusWidth   = 14
	colStageDurationWidth = 12
	// 3 cols × 2 padding each
	colStageFixedTotal = colStageStatusWidth + colStageDurationWidth + 3*2
)

// stageRefreshMsg carries the result of a periodic stage refresh.
type stageRefreshMsg struct {
	stages       []jenkins.Stage
	build        *jenkins.Build // official build status from Jenkins API
	err          error
	logChunk     string // incremental console log chunk for when-skip detection
	logNextStart int    // byte offset for next progressive fetch
}

// stageProgressTickMsg triggers a re-render for smooth progress bar animation.
type stageProgressTickMsg struct{}

// prevStagesFetchedMsg carries stages from the previous completed build.
type prevStagesFetchedMsg struct {
	stages []jenkins.Stage
	err    error
}

// pendingBuildFoundMsg is sent when the newly triggered build appears.
type pendingBuildFoundMsg struct {
	build jenkins.Build
}

// pendingBuildPollMsg triggers another poll attempt.
type pendingBuildPollMsg struct{}

// whenSkipDetectedMsg carries the result of when-conditional skip detection.
type whenSkipDetectedMsg struct {
	skippedOccs map[string][]bool
}

// StageView shows the pipeline stages of a single build.
type StageView struct {
	theme         theme.Theme
	table         component.Table
	client        jenkins.JenkinsClient
	jobPath       string
	build         jenkins.Build
	stages        []jenkins.Stage
	confirmCancel bool
	confirmYes    bool
	width         int
	height        int
	ctx           context.Context
	cancel        context.CancelFunc
	progressBar   component.ProgressBar

	// Inline log preview panel (stage-specific logs + console fallback).
	preview *PreviewPanel

	// Pending build state: waiting for Jenkins to create the build.
	pending        bool
	lastKnownBuild int

	// autoFollowing is true while the view automatically tracks the active
	// running stage. It is disabled the moment the user manually moves the
	// cursor so that intentional navigation is not overridden.
	autoFollowing bool

	// When-conditional skip detection.
	consoleSkipText      string            // accumulated console log text
	consoleSkipNextStart int               // byte offset for next progressive fetch
	currentSkipOccs      map[string][]bool // latest parsed skip data, reapplied on every stage refresh

	// Search state (query string kept here for SearchQuery(); regex is in preview panel).
	searchQuery string

	// lastRefreshAt is the wall-clock time of the most recent stage data fetch.
	// Used to interpolate smooth elapsed times between 2s refreshes.
	lastRefreshAt time.Time

	// Ghost stages from the previous completed build.
	prevStages      []jenkins.Stage // stages from previous completed build
	prevStageDurSum time.Duration   // sum of depth-0 stage durations
	ghostsValid     bool            // current stages are a prefix of prevStages

	store      *cache.Store
	jobName    string // human-readable project name for breadcrumb
	branchName string // branch name for multibranch projects (empty if not applicable)
}

func NewStageView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, jobPath string, build jenkins.Build, jobName, branchName string) *StageView {
	ctx, cancel := context.WithCancel(context.Background())
	columns := []component.Column{
		{Title: "STAGE", Width: 40},
		{Title: "STATUS", Width: colStageStatusWidth},
		{Title: "DURATION", Width: colStageDurationWidth},
	}
	return &StageView{
		theme:         t,
		table:         component.NewTable(t, columns),
		client:        client,
		store:         store,
		jobPath:       jobPath,
		build:         build,
		ctx:           ctx,
		cancel:        cancel,
		progressBar:   component.NewProgressBar(t),
		preview:       NewPreviewPanel(t, client, store, ctx, jobPath, build.Number, jobName, branchName),
		autoFollowing: true,
		jobName:       jobName,
		branchName:    branchName,
	}
}

// NewPendingStageView creates a StageView that waits for Jenkins to create
// the build. It polls ListBuilds until a build with a number higher than
// lastKnownBuild appears, then switches to normal operation.
func NewPendingStageView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, jobPath string, lastKnownBuild int, jobName, branchName string) *StageView {
	ctx, cancel := context.WithCancel(context.Background())
	columns := []component.Column{
		{Title: "STAGE", Width: 40},
		{Title: "STATUS", Width: colStageStatusWidth},
		{Title: "DURATION", Width: colStageDurationWidth},
	}
	return &StageView{
		theme:          t,
		table:          component.NewTable(t, columns),
		client:         client,
		store:          store,
		jobPath:        jobPath,
		build:          jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()},
		ctx:            ctx,
		cancel:         cancel,
		progressBar:    component.NewProgressBar(t),
		preview:        NewPreviewPanel(t, client, store, ctx, jobPath, 0, jobName, branchName),
		autoFollowing:  true,
		pending:        true,
		lastKnownBuild: lastKnownBuild,
		jobName:        jobName,
		branchName:     branchName,
	}
}

// SetStages pre-populates stages without fetching, used when stages
// are already available (e.g. from the failed-stage lookup).
// If selectIdx >= 0, the cursor is moved to that stage.
func (sv *StageView) SetStages(stages []jenkins.Stage, selectIdx int) {
	sv.stages = stages
	sv.populateTable()
	if selectIdx >= 0 {
		sv.table.SetCursor(sv.tableCursorForStage(selectIdx))
	}
}

func (sv *StageView) ApplySearch(pattern string) error {
	sv.searchQuery = pattern
	sv.preview.SetSearch(compileSearchRegex(pattern))
	return nil
}

func (sv *StageView) SearchQuery() string {
	return sv.searchQuery
}

func (sv *StageView) stageCacheKey() string {
	return fmt.Sprintf("%s:%d", sv.jobPath, sv.build.Number)
}

func (sv *StageView) Init() tea.Cmd {
	slog.Debug("stageview.Init", "job", sv.jobPath, "build", sv.build.Number, "pending", sv.pending, "cachedStages", len(sv.stages))
	if sv.pending {
		return sv.pollForNewBuild()
	}
	if len(sv.stages) > 0 {
		// Re-init (e.g. after child view popped): cancel stale goroutines
		// from the previous session and create a fresh context.
		sv.cancel()
		sv.ctx, sv.cancel = context.WithCancel(context.Background())
		sv.preview.SetContext(sv.ctx)

		cmds := []tea.Cmd{sv.preview.Restart(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
		if sv.build.Status == jenkins.BuildStatusRunning || sv.anyStageRunning() {
			cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick(), sv.fetchPrevStages)
		}
		return tea.Batch(cmds...)
	}
	// Serve cached stages immediately if available.
	if sv.store != nil {
		if e := sv.store.Stages.Get(sv.stageCacheKey()); e != nil {
			sv.stages = e.Value
			sv.populateTable()
			sv.table.SetCursor(sv.initialCursor())
			cmds := []tea.Cmd{sv.fetchStages, sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
			if sv.build.Status == jenkins.BuildStatusRunning || sv.anyStageRunning() {
				cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick(), sv.fetchPrevStages)
			}
			return tea.Batch(cmds...)
		}
	}
	sv.populateTable()
	sv.table.SetCursor(0) // Pipeline row
	cmds := []tea.Cmd{sv.fetchStages}
	// Pipeline row is selected — start streaming full console log in preview.
	cmds = append(cmds, sv.preview.UpdateForCursor(pipelinePreviewIdx, sv.stages))
	if sv.build.Status == jenkins.BuildStatusRunning {
		cmds = append(cmds, sv.fetchPrevStages)
	}
	return tea.Batch(cmds...)
}

func (sv *StageView) fetchStages() tea.Msg {
	stages, err := sv.client.ListStages(sv.ctx, sv.jobPath, sv.build.Number)
	if sv.ctx.Err() != nil {
		return nil
	}
	return StagesMsg{Stages: stages, Err: err}
}

func (sv *StageView) scheduleProgressTick() tea.Cmd {
	ctx := sv.ctx
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(200 * time.Millisecond):
		}
		if ctx.Err() != nil {
			return nil
		}
		return stageProgressTickMsg{}
	}
}

func (sv *StageView) scheduleRefresh() tea.Cmd {
	ctx := sv.ctx
	client := sv.client
	jobPath := sv.jobPath
	buildNumber := sv.build.Number
	logNextStart := sv.consoleSkipNextStart
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
		stages, err := client.ListStages(ctx, jobPath, buildNumber)
		if ctx.Err() != nil {
			return nil
		}
		// Fetch the official build status from Jenkins.
		var build *jenkins.Build
		if detail, berr := client.GetBuild(ctx, jobPath, buildNumber); berr == nil {
			build = &detail.Build
		}
		// Fetch incremental console log for when-skip detection.
		var logChunk string
		var logNext int
		if pl, lerr := client.GetProgressiveLog(ctx, jobPath, buildNumber, logNextStart); lerr == nil {
			logChunk = pl.Text
			logNext = pl.NextStart
		}
		return stageRefreshMsg{stages: stages, build: build, err: err, logChunk: logChunk, logNextStart: logNext}
	}
}

// pollForNewBuild polls ListBuilds for a build newer than lastKnownBuild.
func (sv *StageView) pollForNewBuild() tea.Cmd {
	ctx := sv.ctx
	client := sv.client
	jobPath := sv.jobPath
	lastKnown := sv.lastKnownBuild
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
		builds, err := client.ListBuilds(ctx, jobPath)
		if err != nil || len(builds) == 0 {
			return pendingBuildPollMsg{}
		}
		for _, b := range builds {
			if b.Number > lastKnown {
				return pendingBuildFoundMsg{build: b}
			}
		}
		return pendingBuildPollMsg{}
	}
}

func (sv *StageView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate preview-related messages to the preview panel.
	if handled, cmd := sv.preview.HandleMsg(msg); handled {
		return sv, cmd
	}

	switch msg := msg.(type) {
	case ThemeChangedMsg:
		sv.theme = msg.Theme
		sv.table.SetTheme(msg.Theme)
		sv.progressBar.SetTheme(msg.Theme)
		sv.preview.SetTheme(msg.Theme)
		sv.populateTable()
		return sv, nil

	case pendingBuildPollMsg:
		if sv.pending {
			return sv, sv.pollForNewBuild()
		}

	case pendingBuildFoundMsg:
		sv.pending = false
		sv.build = msg.build
		sv.preview.SetBuildNumber(msg.build.Number)
		// Now start normal operation (fetch stages, Pipeline preview).
		sv.populateTable()
		sv.table.SetCursor(0) // Pipeline row
		cmds := []tea.Cmd{sv.fetchStages, sv.fetchPrevStages}
		cmds = append(cmds, sv.preview.UpdateForCursor(pipelinePreviewIdx, sv.stages))
		return sv, tea.Batch(cmds...)

	case prevStagesFetchedMsg:
		if msg.err != nil {
			slog.Warn("stageview.prevStagesFetchedMsg error", "err", msg.err)
			return sv, nil
		}
		sv.prevStages = msg.stages
		// Compute sum of depth-0 stage durations (avoid double-counting parallel children).
		sv.prevStageDurSum = 0
		for _, s := range sv.prevStages {
			if s.Depth == 0 {
				sv.prevStageDurSum += s.Duration
			}
		}
		sv.computeGhostValidity()
		sv.populateTable()
		return sv, nil

	case StagesMsg:
		if msg.Err != nil {
			slog.Warn("stageview.StagesMsg error", "err", msg.Err)
			return sv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		slog.Debug("stageview.StagesMsg", "count", len(msg.Stages))
		sv.lastRefreshAt = time.Now()
		sv.stages = msg.Stages
		if sv.store != nil {
			sv.store.Stages.Put(sv.stageCacheKey(), msg.Stages)
		}
		jenkins.MarkSkipped(sv.stages, sv.currentSkipOccs)
		sv.populateTable()
		if len(sv.stages) > 0 {
			sv.table.SetCursor(sv.initialCursor())
		}
		cmds := []tea.Cmd{sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
		if sv.build.Status == jenkins.BuildStatusRunning || sv.anyStageRunning() {
			cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick())
		} else {
			// Build is finished — apply when-skip detection.
			cmds = append(cmds, sv.whenSkipCmd())
		}
		return sv, tea.Batch(cmds...)

	case stageProgressTickMsg:
		if sv.build.Status == jenkins.BuildStatusRunning || sv.anyStageRunning() {
			sv.populateTable()
			return sv, sv.scheduleProgressTick()
		}

	case stageRefreshMsg:
		if msg.err != nil {
			slog.Warn("stageview.stageRefreshMsg error", "err", msg.err)
			if sv.build.Status == jenkins.BuildStatusRunning {
				return sv, sv.scheduleRefresh()
			}
			return sv, nil
		}
		var buildStatus jenkins.BuildStatus
		if msg.build != nil {
			buildStatus = msg.build.Status
		}
		slog.Debug("stageview.stageRefreshMsg", "stages", len(msg.stages), "buildStatus", buildStatus)
		hadStages := len(sv.stages) > 0
		cursorIdx := sv.table.Cursor()

		sv.lastRefreshAt = time.Now()
		sv.stages = msg.stages
		if sv.store != nil {
			sv.store.Stages.Put(sv.stageCacheKey(), msg.stages)
		}
		// Accumulate incremental console log and parse for when-skip detection.
		if msg.logChunk != "" {
			sv.consoleSkipText += msg.logChunk
		}
		if msg.logNextStart > sv.consoleSkipNextStart {
			sv.consoleSkipNextStart = msg.logNextStart
		}
		if sv.consoleSkipText != "" {
			sv.currentSkipOccs = jenkins.ParseSkippedStages(sv.consoleSkipText)
		}
		jenkins.MarkSkipped(sv.stages, sv.currentSkipOccs)
		sv.computeGhostValidity()
		sv.populateTable()

		if !hadStages && len(sv.stages) > 0 {
			// Stages just appeared — pick smart initial cursor.
			newCursor := sv.initialCursor()
			slog.Debug("stageview.refresh cursor: stages appeared", "cursor", newCursor)
			sv.table.SetCursor(newCursor)
		} else if sv.autoFollowing {
			// Auto-follow: always track the active running stage.
			// initialCursor finds the first running stage, or the last
			// meaningful stage when the build is done — covers fast stages
			// that complete between two refresh ticks.
			newCursor := sv.initialCursor()
			slog.Debug("stageview.refresh cursor: auto-follow", "from", cursorIdx, "to", newCursor)
			sv.table.SetCursor(newCursor)
		} else {
			sv.table.SetCursor(cursorIdx)
		}

		var cmds []tea.Cmd
		// Update preview for current cursor position.
		if cmd := sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages); cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Use official build status from Jenkins API when available.
		buildFinished := false
		if msg.build != nil {
			sv.build.Status = msg.build.Status
			sv.build.Duration = msg.build.Duration
			buildFinished = msg.build.Status != jenkins.BuildStatusRunning
		}

		// B14: When the build API still reports running but all stages have
		// finished, infer the terminal status from stage data. This updates
		// the progress bar display but does NOT stop refreshing — new stages
		// (e.g. "Declarative: Post Actions") can still appear after existing
		// stages finish. Only the build API is authoritative for "done".
		if !buildFinished && allStagesFinished(sv.stages) {
			inferred := inferBuildStatusFromStages(sv.stages)
			if inferred != jenkins.BuildStatusUnknown {
				slog.Debug("stageview.refresh: inferred build status from stages (still refreshing)", "inferred", inferred)
				sv.build.Status = inferred
				// Don't set buildFinished — keep refreshing until build API confirms.
			}
		}

		// Keep refreshing while stages are running or the build hasn't finalized.
		stagesRunning := sv.anyStageRunning()
		if stagesRunning || !buildFinished {
			cmds = append(cmds, sv.scheduleRefresh())
		} else {
			slog.Debug("stageview.refresh: build finished", "buildStatus", sv.build.Status)
			// Build finished — hide ghost stages and do final detection.
			sv.ghostsValid = false
			sv.populateTable()
			cmds = append(cmds, sv.whenSkipCmd())
		}
		return sv, tea.Batch(cmds...)

	case whenSkipDetectedMsg:
		sv.currentSkipOccs = msg.skippedOccs
		if sv.store != nil {
			sv.store.WhenSkipped.Put(sv.stageCacheKey(), msg.skippedOccs)
		}
		jenkins.MarkSkipped(sv.stages, msg.skippedOccs)
		sv.populateTable()
		return sv, nil

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return sv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		// Don't set status eagerly — let the refresh loop pick up the
		// official status from Jenkins so the progress bar transitions
		// smoothly instead of disappearing and reappearing.
		return sv, sv.scheduleRefresh()

	case tea.KeyMsg:
		if sv.confirmCancel {
			switch msg.String() {
			case "left", "right", "h":
				sv.confirmYes = !sv.confirmYes
			case "y":
				sv.confirmCancel = false
				jobPath, number := sv.jobPath, sv.build.Number
				return sv, func() tea.Msg {
					err := sv.client.CancelBuild(context.Background(), jobPath, number)
					return CancelBuildResultMsg{Err: err}
				}
			case "enter":
				if sv.confirmYes {
					sv.confirmCancel = false
					jobPath, number := sv.jobPath, sv.build.Number
					return sv, func() tea.Msg {
						err := sv.client.CancelBuild(context.Background(), jobPath, number)
						return CancelBuildResultMsg{Err: err}
					}
				}
				sv.confirmCancel = false
			default:
				sv.confirmCancel = false
			}
			return sv, nil
		}

		prevCursor := sv.table.Cursor()
		switch msg.String() {
		case "up", "k":
			sv.table.MoveUp()
		case "down", "j":
			sv.table.MoveDown()
		case "pgup":
			sv.table.PageUp()
		case "pgdown":
			sv.table.PageDown()
		case "home":
			sv.table.Home()
		case "end":
			sv.table.End()
		case "enter":
			if sv.table.IsDisabled(sv.table.Cursor()) {
				return sv, nil
			}
			realIdx := sv.realStageIdx(sv.table.Cursor())
			if realIdx == -1 {
				// Pipeline row — open the full console log view.
				child := NewConsoleView(sv.theme, sv.client, sv.jobPath, sv.build.Number, sv.jobName, sv.branchName)
				return sv, func() tea.Msg { return PushViewMsg{View: child} }
			}
			if realIdx >= 0 && realIdx < len(sv.stages) {
				stage := sv.stages[realIdx]
				if len(stage.NodeIDs) > 0 {
					child := NewStageLogView(sv.theme, sv.client, sv.store, sv.jobPath, sv.build.Number, stage.Name, stage.NodeIDs, sv.build.Status == jenkins.BuildStatusRunning, sv.jobName, sv.branchName)
					return sv, func() tea.Msg { return PushViewMsg{View: child} }
				}
			}
		case "l":
			child := NewConsoleView(sv.theme, sv.client, sv.jobPath, sv.build.Number, sv.jobName, sv.branchName)
			return sv, func() tea.Msg { return PushViewMsg{View: child} }
		case "f":
			if sv.build.Status == jenkins.BuildStatusFailed {
				if idx := sv.findFailedStage(); idx >= 0 {
					stage := sv.stages[idx]
					if len(stage.NodeIDs) > 0 {
						sv.table.SetCursor(sv.tableCursorForStage(idx))
						child := NewStageLogView(sv.theme, sv.client, sv.store, sv.jobPath, sv.build.Number, stage.Name, stage.NodeIDs, sv.build.Status == jenkins.BuildStatusRunning, sv.jobName, sv.branchName)
						return sv, func() tea.Msg { return PushViewMsg{View: child} }
					}
				}
			}
		case "x":
			if sv.build.Status == jenkins.BuildStatusRunning {
				sv.confirmCancel = true
				sv.confirmYes = false
			}
		}
		// If cursor moved by user input, update auto-follow state and preview.
		// Auto-follow only stays on when the user lands on the exact stage
		// that initialCursor() would pick (the deepest running child).
		// Moving to any other stage — even a running parent — disables it.
		if sv.table.Cursor() != prevCursor {
			sv.autoFollowing = sv.table.Cursor() == sv.initialCursor()
			slog.Debug("stageview.keypress: cursor moved",
				"from", prevCursor, "to", sv.table.Cursor(),
				"autoFollowing", sv.autoFollowing)
			return sv, sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)
		}
	}
	return sv, nil
}

// anyStageRunning returns true if any stage is still in progress.
func (sv *StageView) anyStageRunning() bool {
	for _, s := range sv.stages {
		if s.Status == jenkins.BuildStatusRunning {
			return true
		}
	}
	return false
}

// allStagesFinished returns true when stages exist and none are Running or NotBuilt.
func allStagesFinished(stages []jenkins.Stage) bool {
	if len(stages) == 0 {
		return false
	}
	for _, s := range stages {
		if s.Status == jenkins.BuildStatusRunning || s.Status == jenkins.BuildStatusNotBuilt {
			return false
		}
	}
	return true
}

// inferBuildStatusFromStages derives a terminal build status from stage data.
// Precedence: Failed > Aborted > Unstable > Success. If no recognisable
// terminal status is found, it returns BuildStatusUnknown.
func inferBuildStatusFromStages(stages []jenkins.Stage) jenkins.BuildStatus {
	hasFailed := false
	hasAborted := false
	hasUnstable := false
	hasSuccess := false
	for _, s := range stages {
		switch s.Status {
		case jenkins.BuildStatusFailed:
			hasFailed = true
		case jenkins.BuildStatusAborted:
			hasAborted = true
		case jenkins.BuildStatusUnstable:
			hasUnstable = true
		case jenkins.BuildStatusSuccess, jenkins.BuildStatusSkipped:
			hasSuccess = true
		}
	}
	switch {
	case hasFailed:
		return jenkins.BuildStatusFailed
	case hasAborted:
		return jenkins.BuildStatusAborted
	case hasUnstable:
		return jenkins.BuildStatusUnstable
	case hasSuccess:
		return jenkins.BuildStatusSuccess
	default:
		return jenkins.BuildStatusUnknown
	}
}

// initialCursor returns the best table cursor position for a fresh stage list.
// The returned value is in table-cursor space (accounts for the synthetic
// Pipeline row at index 0). Auto-follow always targets a real stage, never
// the Pipeline row.
// Priority: deepest running child (B11), then first failed stage (B13),
// then last non-skipped/non-not-built stage.
func (sv *StageView) initialCursor() int {
	// Find the last running stage — in tree order, a running child appears
	// after its running parent, so "last" gives us the deepest child (B11).
	lastRunning := -1
	for i, s := range sv.stages {
		if s.Status == jenkins.BuildStatusRunning {
			lastRunning = i
		}
	}
	if lastRunning >= 0 {
		slog.Debug("stageview.initialCursor: running stage", "idx", lastRunning, "name", sv.stages[lastRunning].Name)
		return sv.tableCursorForStage(lastRunning)
	}
	// No running stage — prefer the first failed stage (B13).
	for i, s := range sv.stages {
		if s.Status == jenkins.BuildStatusFailed {
			slog.Debug("stageview.initialCursor: first failed stage", "idx", i, "name", s.Name)
			return sv.tableCursorForStage(i)
		}
	}
	// No failed stage — pick the last non-skipped/non-not-built stage.
	for i := len(sv.stages) - 1; i >= 0; i-- {
		s := sv.stages[i].Status
		if s != jenkins.BuildStatusSkipped && s != jenkins.BuildStatusNotBuilt {
			slog.Debug("stageview.initialCursor: last meaningful stage", "idx", i, "name", sv.stages[i].Name, "status", s)
			return sv.tableCursorForStage(i)
		}
	}
	// Fallback: last real stage (or Pipeline row if no stages).
	if len(sv.stages) > 0 {
		return sv.tableCursorForStage(len(sv.stages) - 1)
	}
	return 0
}

// hasPipelineRow returns true when the table has a synthetic Pipeline row at
// index 0. The Pipeline row is always present.
func (sv *StageView) hasPipelineRow() bool {
	return true
}

// realStageIdx maps a table cursor position to an index in sv.stages,
// accounting for the synthetic Pipeline row at index 0. Returns -1 when the
// cursor is on the Pipeline row.
func (sv *StageView) realStageIdx(cursor int) int {
	return cursor - 1
}

// tableCursorForStage maps a sv.stages index to a table cursor position,
// accounting for the synthetic Pipeline row at index 0.
func (sv *StageView) tableCursorForStage(stageIdx int) int {
	return stageIdx + 1
}

// previewIdxForCursor converts a table cursor to the index expected by the
// preview panel: pipelinePreviewIdx for the Pipeline row, or the real stage
// index for normal stages.
func (sv *StageView) previewIdxForCursor(cursor int) int {
	realIdx := sv.realStageIdx(cursor)
	if realIdx == -1 {
		return pipelinePreviewIdx
	}
	return realIdx
}

func (sv *StageView) populateTable() {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// +1 for the synthetic Pipeline row at index 0.
	rows := make([]component.Row, 0, len(sv.stages)+1)

	// Synthetic Pipeline root row — mirrors the build status and duration.
	pipelineDuration := formatDuration(sv.build.Duration)
	if sv.build.Status == jenkins.BuildStatusRunning {
		elapsed := time.Since(sv.build.Timestamp)
		estimate := sv.effectiveEstimate()
		if estimate > 0 {
			pipelineDuration = sv.progressBar.DualRenderWithText(colStageDurationWidth, elapsed, estimate)
		} else {
			pipelineDuration = formatDuration(elapsed)
		}
	}
	rows = append(rows, component.Row{
		"▸ Pipeline",
		renderStatus(sv.theme, sv.build.Status),
		pipelineDuration,
	})

	for i, s := range sv.stages {
		prefix := pipelineTreePrefix(sv.stages, i)
		icon := "▸ "
		if s.Parallel {
			icon = "⇶ "
		}

		// For running stages, interpolate elapsed time for smooth animation.
		durationCell := formatDuration(s.Duration)
		if s.Status == jenkins.BuildStatusRunning {
			stageElapsed := s.Duration
			if !sv.lastRefreshAt.IsZero() {
				stageElapsed += time.Since(sv.lastRefreshAt)
			}
			if sv.ghostsValid && i < len(sv.prevStages) && sv.prevStages[i].Duration > 0 {
				durationCell = sv.progressBar.DualRenderWithText(colStageDurationWidth, stageElapsed, sv.prevStages[i].Duration)
			} else {
				durationCell = formatDuration(stageElapsed)
			}
		}

		rows = append(rows, component.Row{
			prefix + icon + s.Name,
			renderStatus(sv.theme, s.Status),
			durationCell,
		})
	}

	// Ghost stages: show remaining stages from previous build (dimmed).
	ghostStart := len(sv.stages)
	ghostEnd := len(sv.prevStages)
	var disabledIndices map[int]bool
	if sv.ghostsValid && sv.build.Status == jenkins.BuildStatusRunning && ghostStart < ghostEnd {
		disabledIndices = make(map[int]bool)
		for i := ghostStart; i < ghostEnd; i++ {
			ps := sv.prevStages[i]
			prefix := pipelineTreePrefix(sv.prevStages, i)
			icon := "▸ "
			if ps.Parallel {
				icon = "⇶ "
			}
			rows = append(rows, component.Row{
				dimStyle.Render(prefix + icon + ps.Name),
				dimStyle.Render("pending"),
				dimStyle.Render(formatDuration(ps.Duration)),
			})
			// Table row index = 1 (Pipeline row) + i
			disabledIndices[1+i] = true
		}
	}

	sv.table.SetRows(rows)
	sv.table.SetDisabled(disabledIndices)
}

// pipelineTreePrefix wraps buildTreePrefix with an extra root level so all
// stages appear as children of the synthetic Pipeline row.
func pipelineTreePrefix(stages []jenkins.Stage, idx int) string {
	s := stages[idx]

	// Is this the last depth-0 stage (or nested under the last depth-0)?
	lastTopLevel := true
	for j := idx + 1; j < len(stages); j++ {
		if stages[j].Depth == 0 {
			lastTopLevel = false
			break
		}
	}

	var rootConnector string
	if s.Depth == 0 {
		if lastTopLevel {
			rootConnector = "└─"
		} else {
			rootConnector = "├─"
		}
		return rootConnector
	}

	// For nested stages, prepend the Pipeline continuation line.
	var pipelineCont string
	if lastTopLevel {
		pipelineCont = "  " // Pipeline branch ended
	} else {
		pipelineCont = "│ " // Pipeline branch continues
	}
	return pipelineCont + buildTreePrefix(stages, idx)
}

// buildTreePrefix generates tree-drawing characters for a stage.
// Parallel branches use heavy box-drawing (┃, ┣━, ┗━) to visually
// distinguish them from sequential branches (│, ├─, └─).
func buildTreePrefix(stages []jenkins.Stage, idx int) string {
	s := stages[idx]
	if s.Depth == 0 {
		return ""
	}

	// Determine if this stage is the last sibling at its depth.
	isLast := true
	for j := idx + 1; j < len(stages); j++ {
		if stages[j].Depth < s.Depth {
			break
		}
		if stages[j].Depth == s.Depth {
			isLast = false
			break
		}
	}

	// Check if the direct parent stage is a parallel stage.
	parentIsParallel := isParentParallel(stages, idx)

	// Build the prefix from depth 1 to s.Depth.
	var buf strings.Builder
	for d := 1; d < s.Depth; d++ {
		// Check if there's still a sibling at depth d after this row.
		hasMore := false
		for j := idx + 1; j < len(stages); j++ {
			if stages[j].Depth < d {
				break
			}
			if stages[j].Depth == d {
				hasMore = true
				break
			}
		}

		ancestorParallel := isAncestorParentParallel(stages, idx, d)
		if hasMore {
			if ancestorParallel {
				buf.WriteString("┃ ")
			} else {
				buf.WriteString("│ ")
			}
		} else {
			buf.WriteString("  ")
		}
	}

	if parentIsParallel {
		if isLast {
			buf.WriteString("┗━")
		} else {
			buf.WriteString("┣━")
		}
	} else {
		if isLast {
			buf.WriteString("└─")
		} else {
			buf.WriteString("├─")
		}
	}
	return buf.String()
}

// isParentParallel checks if the direct parent stage of stages[idx] is parallel.
func isParentParallel(stages []jenkins.Stage, idx int) bool {
	for j := idx - 1; j >= 0; j-- {
		if stages[j].Depth < stages[idx].Depth {
			return stages[j].Depth == stages[idx].Depth-1 && stages[j].Parallel
		}
	}
	return false
}

// isAncestorParentParallel checks if the ancestor at the given depth
// has a parallel parent.
func isAncestorParentParallel(stages []jenkins.Stage, idx, depth int) bool {
	for j := idx - 1; j >= 0; j-- {
		if stages[j].Depth < depth {
			break
		}
		if stages[j].Depth == depth {
			return isParentParallel(stages, j)
		}
	}
	return false
}

const stageBarHeight = 3

func (sv *StageView) View() string {
	if sv.pending {
		barWidth := sv.width
		if barWidth < 1 {
			barWidth = 1
		}
		bar := sv.progressBar.RenderPendingTall(barWidth, "Pending")
		return bar + "\n" + sv.table.View()
	}
	var content string
	if sv.build.Status == jenkins.BuildStatusRunning || sv.anyStageRunning() {
		elapsed := time.Since(sv.build.Timestamp)
		barWidth := sv.width
		if barWidth < 1 {
			barWidth = 1
		}
		var bar string
		estimate := sv.effectiveEstimate()
		if estimate > 0 {
			bar = sv.progressBar.RenderWithTextTall(barWidth, elapsed, estimate)
		} else {
			bar = sv.progressBar.RenderPendingTall(barWidth, "First run - no estimate")
		}
		content = bar + "\n" + sv.table.View()
	} else if sv.build.Status != jenkins.BuildStatusUnknown {
		bar := sv.renderFinishedBar()
		content = bar + "\n" + sv.table.View()
	} else {
		content = sv.table.View()
	}

	if sv.confirmCancel {
		return renderConfirmDialog(sv.theme, content, sv.width, sv.height,
			"Cancel Build",
			fmt.Sprintf("Stop build #%d?", sv.build.Number),
			sv.confirmYes,
		)
	}
	return content
}

// renderFinishedBar renders a static full-width bar for a completed build.
func (sv *StageView) renderFinishedBar() string {
	barWidth := sv.width
	if barWidth < 1 {
		barWidth = 1
	}
	text := formatDuration(sv.build.Duration)

	var style lipgloss.Style
	switch sv.build.Status {
	case jenkins.BuildStatusSuccess:
		style = sv.theme.BuildStatus.Success
	case jenkins.BuildStatusFailed:
		style = sv.theme.BuildStatus.Failed
	case jenkins.BuildStatusAborted:
		style = sv.theme.BuildStatus.Aborted
	default:
		style = sv.theme.BuildStatus.Running
	}
	return sv.progressBar.RenderCompleteTall(barWidth, text, style)
}

func (sv *StageView) Title() string {
	if sv.pending {
		return "New Build"
	}
	return fmt.Sprintf("Build #%d", sv.build.Number)
}

func (sv *StageView) Breadcrumb() BreadcrumbSegment {
	ctx := jobRefParts(sv.jobName, sv.branchName)
	if sv.pending {
		ctx = append(ctx, BreadcrumbPart{Text: "pending"})
	} else {
		ctx = append(ctx, BreadcrumbPart{Text: fmt.Sprintf("%d", sv.build.Number), IsBuildNum: true})
	}
	return BreadcrumbSegment{ViewType: "stages", Context: ctx}
}

func (sv *StageView) ItemCount() int {
	return len(sv.stages) + 1 // +1 for Pipeline row
}

func (sv *StageView) Commands() []command.Command {
	return nil
}

func (sv *StageView) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{{Key: "/", Action: "search"}}
	realIdx := sv.realStageIdx(sv.table.Cursor())
	if realIdx == -1 {
		// Pipeline row
		sc = append(sc, component.Shortcut{Key: "enter", Action: "full log"})
	} else if realIdx >= 0 && realIdx < len(sv.stages) && len(sv.stages[realIdx].NodeIDs) > 0 {
		sc = append(sc, component.Shortcut{Key: "enter", Action: "stage log"})
	}
	sc = append(sc, component.Shortcut{Key: "l", Action: "full log"})
	if sv.build.Status == jenkins.BuildStatusFailed && sv.findFailedStage() >= 0 {
		sc = append(sc, component.Shortcut{Key: "f", Action: "failed stage"})
	}
	if sv.build.Status == jenkins.BuildStatusRunning && !sv.pending {
		sc = append(sc, component.Shortcut{Key: "x", Action: "cancel"})
	}
	return sc
}

// findFailedStage returns the index of the first failed stage, or -1.
func (sv *StageView) findFailedStage() int {
	for i, s := range sv.stages {
		if s.Status == jenkins.BuildStatusFailed {
			return i
		}
	}
	return -1
}

func (sv *StageView) SetSize(width, height int) {
	sv.width = width
	sv.height = height
	stageWidth := width - colStageFixedTotal
	if stageWidth < 10 {
		stageWidth = 10
	}
	sv.table.SetColumnWidth(0, stageWidth)
	// Reserve lines for the 3-line progress bar + separator.
	tableHeight := height - stageBarHeight - 1
	if tableHeight < 1 {
		tableHeight = 1
	}
	sv.table.SetSize(width, tableHeight)
}

// SetPreviewSize implements PreviewProvider.
func (sv *StageView) SetPreviewSize(width, height int) {
	sv.preview.SetSize(width, height)
}

// whenSkipCmd returns a command to detect when-conditional skipped stages.
// If the result is already cached, it applies the result inline and returns nil.
func (sv *StageView) whenSkipCmd() tea.Cmd {
	if sv.store != nil {
		if e := sv.store.WhenSkipped.Get(sv.stageCacheKey()); e != nil {
			sv.currentSkipOccs = e.Value
			jenkins.MarkSkipped(sv.stages, e.Value)
			sv.populateTable()
			return nil
		}
	}
	return sv.detectWhenSkips()
}

// detectWhenSkips fetches the full console log and parses when-conditional skip lines.
func (sv *StageView) detectWhenSkips() tea.Cmd {
	ctx := sv.ctx
	client := sv.client
	jobPath := sv.jobPath
	buildNumber := sv.build.Number
	return func() tea.Msg {
		text, err := client.GetFullConsoleText(ctx, jobPath, buildNumber)
		if err != nil || ctx.Err() != nil {
			return nil
		}
		return whenSkipDetectedMsg{skippedOccs: jenkins.ParseSkippedStages(text)}
	}
}

// fetchPrevStages fetches stages from the most recent completed build for ghost rendering.
func (sv *StageView) fetchPrevStages() tea.Msg {
	builds, err := sv.client.ListBuilds(sv.ctx, sv.jobPath)
	if err != nil || sv.ctx.Err() != nil {
		return prevStagesFetchedMsg{err: err}
	}
	currentNum := sv.build.Number
	// Find most recent completed build (not current). Prefer success, accept any terminal.
	var prevBuild *jenkins.Build
	for i := range builds {
		b := &builds[i]
		if b.Number == currentNum {
			continue
		}
		if b.Status == jenkins.BuildStatusRunning {
			continue
		}
		if prevBuild == nil || b.Status == jenkins.BuildStatusSuccess {
			prevBuild = b
		}
		if b.Status == jenkins.BuildStatusSuccess {
			break
		}
	}
	if prevBuild == nil {
		return prevStagesFetchedMsg{}
	}
	stages, err := sv.client.ListStages(sv.ctx, sv.jobPath, prevBuild.Number)
	if sv.ctx.Err() != nil {
		return prevStagesFetchedMsg{err: err}
	}
	return prevStagesFetchedMsg{stages: stages, err: err}
}

// computeGhostValidity checks whether current stages are a prefix of prevStages (by name and depth).
// An empty current stage list is a valid prefix (all prev stages shown as ghosts).
func (sv *StageView) computeGhostValidity() {
	if len(sv.prevStages) == 0 {
		sv.ghostsValid = false
		return
	}
	if len(sv.stages) > len(sv.prevStages) {
		sv.ghostsValid = false
		return
	}
	for i, cs := range sv.stages {
		if i >= len(sv.prevStages) || cs.Name != sv.prevStages[i].Name || cs.Depth != sv.prevStages[i].Depth {
			sv.ghostsValid = false
			return
		}
	}
	sv.ghostsValid = true
}

// effectiveEstimate returns the best available build duration estimate.
func (sv *StageView) effectiveEstimate() time.Duration {
	base := sv.build.EstimatedDuration
	if sv.ghostsValid && sv.prevStageDurSum > 0 {
		if base <= 0 {
			base = sv.prevStageDurSum
		} else {
			base = min(sv.prevStageDurSum, base)
		}
	}
	if !sv.ghostsValid {
		return base
	}

	// Floor: for every running stage, the pipeline estimate must be at least
	// elapsed + (prevStageDuration - stageElapsed). This prevents the pipeline
	// bar from showing "done" while a child stage still shows time remaining.
	elapsed := time.Since(sv.build.Timestamp)
	for i, s := range sv.stages {
		if s.Status != jenkins.BuildStatusRunning || i >= len(sv.prevStages) {
			continue
		}
		stageElapsed := s.Duration
		if !sv.lastRefreshAt.IsZero() {
			stageElapsed += time.Since(sv.lastRefreshAt)
		}
		stageRemaining := sv.prevStages[i].Duration - stageElapsed
		if stageRemaining > 0 {
			if floor := elapsed + stageRemaining; floor > base {
				base = floor
			}
		}
	}

	return base
}

// PreviewView implements PreviewProvider.
func (sv *StageView) PreviewView() string {
	return sv.preview.View(sv.stages)
}

// PreviewBreadcrumb implements PreviewProvider.
func (sv *StageView) PreviewBreadcrumb() BreadcrumbSegment {
	return sv.preview.Breadcrumb(sv.stages, sv.pending)
}

// PreviewItemCount implements PreviewProvider.
func (sv *StageView) PreviewItemCount() int {
	return sv.preview.ItemCount(sv.stages)
}

func (sv *StageView) HasPopup() bool {
	return sv.confirmCancel
}

func (sv *StageView) Close() error {
	if sv.cancel != nil {
		sv.cancel()
	}
	return nil
}

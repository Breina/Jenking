package view

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

const (
	colStageStatusWidth   = 14
	colStageDurationWidth = 12
	// 3 cols × 2 padding each
	colStageFixedTotal = colStageStatusWidth + colStageDurationWidth + 3*2
)

// stageRefreshMsg carries the result of a periodic stage refresh.
type stageRefreshMsg struct {
	stages        []jmodel.Stage
	build         *jmodel.Build // official build status from Jenkins API
	pendingInputs []jmodel.PendingInput
	err           error
	logChunk      string // incremental console log chunk for when-skip detection
	logNextStart  int    // byte offset for next progressive fetch
}

// stageProgressTickMsg triggers a re-render for smooth progress bar animation.
type stageProgressTickMsg struct{}

// prevStagesFetchedMsg carries stages from the previous completed build.
type prevStagesFetchedMsg struct {
	stages []jmodel.Stage
	err    error
}

// pendingBuildFoundMsg is sent when the newly triggered build appears.
type pendingBuildFoundMsg struct {
	build jmodel.Build
}

// pendingBuildPollMsg triggers another poll attempt.
type pendingBuildPollMsg struct{}

// whenSkipDetectedMsg carries the result of when-conditional skip detection.
type whenSkipDetectedMsg struct {
	skippedOccs map[string][]bool
}

// buildDetailMsg carries the result of a build detail fetch.
type buildDetailMsg struct {
	build         jmodel.Build
	pendingInputs []jmodel.PendingInput
	err           error
}

// StageView shows the pipeline stages of a single build.
type StageView struct {
	BaseView
	table       component.Table
	build       jmodel.Build
	stages      []jmodel.Stage
	progressBar component.ProgressBar

	// Inline log preview panel (stage-specific logs + console fallback).
	preview *PreviewPanel

	// Pending build state: waiting for Jenkins to create the build.
	pending        bool
	lastKnownBuild int

	// autoFollowing is true while the view automatically tracks the active
	// running stage. It is disabled the moment the user manually moves the
	// cursor so that intentional navigation is not overridden.
	autoFollowing bool

	// selectStageName, when non-empty, causes Init/StagesMsg to place the
	// cursor on the first stage with this name instead of the auto-follow
	// target (used when returning from a stage log view).
	selectStageName string

	// When-conditional skip detection.
	consoleSkipText      string            // accumulated console log text
	consoleSkipNextStart int               // byte offset for next progressive fetch
	currentSkipOccs      map[string][]bool // latest parsed skip data, reapplied on every stage refresh

	// Search state (query string kept here for SearchQuery(); regex is in preview panel).
	searchQuery string

	// lastRefreshAt is the wall-clock time of the most recent stage data fetch.
	// Used to interpolate smooth elapsed times between 2s refreshes.
	lastRefreshAt time.Time

	// stageStartWall records the wall-clock start time of each running stage,
	// keyed by stage index. Set once on the first refresh where the stage is
	// observed running (as time.Now() - s.Duration) and never updated
	// afterwards. Jenkins reports Duration in coarse (~minute) jumps for
	// long-running stages, which caused the progress bar to reset every minute
	// and then jump ahead. Tracking our own start time keeps the bar smooth.
	stageStartWall map[int]time.Time

	// Post-completion finalizer state. Jenkins sometimes reports a terminal
	// build status before it has written the final Duration to the json API
	// or flushed the full console log (where "when"-conditional skip lines
	// live). buildFinishedAt marks the first time we saw the build finish,
	// so the retry loops below can bound themselves by wall-clock time.
	buildFinishedAt    time.Time
	buildDetailRetries int
	whenSkipRetries    int

	// Ghost stages from the previous completed build.
	prevStages  []jmodel.Stage // stages from previous completed build
	ghostsValid bool           // current stages are a prefix of prevStages

	// Cross-cutting concerns (trigger / cancel / test-open / artifact-open)
	// are owned by behaviors registered on host. The trigger machinery lives
	// in triggerMixin; the behavior wrapper delegates host-callable parts.
	trigger triggerMixin
	input   *inputBehavior // pipeline `input` step approval
	host    widget.BehaviorHost

	// pendingInputs is the latest snapshot of input steps awaiting decision.
	// Sourced from BuildDetail.PendingInputs on every detail/refresh tick;
	// drives ApplyPendingInputs (stage rendering) and the Enter shortcut.
	pendingInputs []jmodel.PendingInput
}

func NewStageView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, build jmodel.Build) *StageView {
	columns := []component.Column{
		{Title: "STAGE", Width: 40},
		{Title: "STATUS", Width: colStageStatusWidth},
		{Title: "DURATION", Width: colStageDurationWidth},
	}
	base := NewBaseView(t, client, store, nc, CtxBuild)
	sv := &StageView{
		BaseView:      base,
		table:         component.NewTable(t, columns),
		build:         build,
		progressBar:   component.NewProgressBar(t),
		preview:       NewPreviewPanel(t, client, store, base.ctx, base.nc),
		autoFollowing: true,
		trigger:       newTriggerMixin(t, client, base.nc),
	}
	sv.registerBehaviors()
	return sv
}

// registerBehaviors wires the four fixed-build cross-cutting concerns
// (artifact, test, cancel, trigger) onto this view's host. Called by both
// stage-view constructors so the wiring lives in one place.
func (sv *StageView) registerBehaviors() {
	sv.input = newInputBehavior(sv.theme, sv.client, sv.resolveFocusedInput, sv.dropPendingInput)
	sv.host.Add(sv.input)
	sv.host.Add(inputAbortShortcut{b: sv.input})
	addFixedBuildActions(&sv.host, sv.theme, sv.client, &sv.nc, &sv.build, &sv.store, &sv.trigger, swapTo)
}

// dropPendingInput removes the resolved input from the local snapshot and
// rebuilds stage status, so the paused-input badge disappears immediately
// after a successful proceed/abort instead of waiting for the next refresh
// tick. Persists the trimmed list to the cache too.
func (sv *StageView) dropPendingInput(inputID string) {
	if inputID == "" {
		return
	}
	filtered := make([]jmodel.PendingInput, 0, len(sv.pendingInputs))
	for _, p := range sv.pendingInputs {
		if p.ID != inputID {
			filtered = append(filtered, p)
		}
	}
	sv.pendingInputs = filtered
	sv.recomputeStageStatusForInputs()
	sv.populateTable()
	sv.cachePendingInputs(filtered)
}

// recomputeStageStatusForInputs re-projects pendingInputs onto stage statuses.
// Stages previously flipped to PausedInput must revert to Running when their
// input is gone; ApplyPendingInputs only flips forward, so we reset first.
func (sv *StageView) recomputeStageStatusForInputs() {
	for i := range sv.stages {
		if sv.stages[i].Status == jmodel.BuildStatusPausedInput {
			sv.stages[i].Status = jmodel.BuildStatusRunning
		}
	}
	jmodel.ApplyPendingInputs(sv.stages, sv.pendingInputs)
}

// resolveFocusedInput returns the pending input that matches the currently
// focused stage, if any. Used by inputBehavior to drive Enter handling and
// the Shortcut gate.
func (sv *StageView) resolveFocusedInput() (jmodel.PendingInput, NavigationContext, int, bool) {
	if len(sv.pendingInputs) == 0 {
		return jmodel.PendingInput{}, NavigationContext{}, 0, false
	}
	realIdx := sv.realStageIdx(sv.table.Cursor())
	if realIdx < 0 || realIdx >= len(sv.stages) {
		return jmodel.PendingInput{}, NavigationContext{}, 0, false
	}
	if sv.stages[realIdx].Status != jmodel.BuildStatusPausedInput {
		return jmodel.PendingInput{}, NavigationContext{}, 0, false
	}
	pi := sv.pickPendingInputForStage(realIdx)
	if pi == nil {
		return jmodel.PendingInput{}, NavigationContext{}, 0, false
	}
	return *pi, sv.nc, sv.build.Number, true
}

// pickPendingInputForStage picks which pending input belongs to the focused
// stage. Jenkins does not expose a flow-node id on the InputAction, so for
// the single-input case we return that one input. With multiple parallel
// inputs we pick by index of the paused stage among paused stages — best
// effort until we can correlate node ids properly.
func (sv *StageView) pickPendingInputForStage(stageIdx int) *jmodel.PendingInput {
	if len(sv.pendingInputs) == 1 {
		return &sv.pendingInputs[0]
	}
	pausedOrder := 0
	for i := 0; i < stageIdx; i++ {
		if sv.stages[i].Status == jmodel.BuildStatusPausedInput {
			pausedOrder++
		}
	}
	if pausedOrder >= len(sv.pendingInputs) {
		return &sv.pendingInputs[0]
	}
	return &sv.pendingInputs[pausedOrder]
}

// NewPendingStageView creates a StageView that waits for Jenkins to create
// the build. It polls ListBuilds until a build with a number higher than
// lastKnownBuild appears, then switches to normal operation.
func NewPendingStageView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, lastKnownBuild int) *StageView {
	columns := []component.Column{
		{Title: "STAGE", Width: 40},
		{Title: "STATUS", Width: colStageStatusWidth},
		{Title: "DURATION", Width: colStageDurationWidth},
	}
	pendingNC := nc
	pendingNC.Build = NavBuildRef{}
	base := NewBaseView(t, client, store, pendingNC, CtxBuild)
	sv := &StageView{
		BaseView:       base,
		table:          component.NewTable(t, columns),
		build:          jmodel.Build{Status: jmodel.BuildStatusRunning, Timestamp: time.Now()},
		progressBar:    component.NewProgressBar(t),
		preview:        NewPreviewPanel(t, client, store, base.ctx, base.nc),
		autoFollowing:  true,
		pending:        true,
		lastKnownBuild: lastKnownBuild,
		trigger:        newTriggerMixin(t, client, base.nc),
	}
	sv.registerBehaviors()
	return sv
}

// SetStages pre-populates stages without fetching, used when stages
// are already available (e.g. from the failed-stage lookup).
// If selectIdx >= 0, the cursor is moved to that stage.
func (sv *StageView) SetStages(stages []jmodel.Stage, selectIdx int) {
	sv.stages = stages
	sv.populateTable()
	if selectIdx >= 0 {
		sv.table.SetCursor(sv.tableCursorForStage(selectIdx))
	}
}

func (sv *StageView) ApplySearch(pattern string) tea.Cmd {
	sv.searchQuery = pattern
	sv.preview.SetSearch(widget.CompileSearchRegex(pattern))
	return nil
}

func (sv *StageView) SearchQuery() string {
	return sv.searchQuery
}

func (sv *StageView) stageCacheKey() string {
	return fmt.Sprintf("%s:%d", sv.nc.JobPath(), sv.build.Number)
}

// setBuild updates the build and caches it for later restoration.
func (sv *StageView) setBuild(b jmodel.Build) {
	sv.build = b
	if b.Duration > 0 {
		sv.buildDetailRetries = 0
	}
	if sv.store != nil {
		sv.store.BuildDetail.Put(sv.stageCacheKey(), b)
	}
}

func (sv *StageView) Init() tea.Cmd {
	slog.Debug("stageview.Init", "job", sv.nc.JobPath(), "build", sv.build.Number, "buildStatus", sv.build.Status, "pending", sv.pending, "cachedStages", len(sv.stages))
	if sv.pending {
		return sv.pollForNewBuild()
	}
	sv.syncBuildDetailCache()
	sv.restorePendingInputsCache()
	if len(sv.stages) > 0 {
		return sv.initReentry()
	}
	if cmd, ok := sv.initFromCachedStages(); ok {
		return cmd
	}
	return sv.initFresh()
}

// syncBuildDetailCache pulls the BuildDetail cache into sv.build when empty,
// or pushes the current build into the cache when present. Lets the Pipeline
// row render correctly when returning from a child view.
func (sv *StageView) syncBuildDetailCache() {
	if sv.store == nil {
		return
	}
	if sv.build.Status == "" {
		if e := sv.store.BuildDetail.Get(sv.stageCacheKey()); e != nil {
			slog.Debug("stageview.Init: restored build from cache", "status", e.Value.Status, "duration", e.Value.Duration)
			sv.build = e.Value
		} else {
			slog.Debug("stageview.Init: no cached build found", "key", sv.stageCacheKey())
		}
		return
	}
	slog.Debug("stageview.Init: caching build", "status", sv.build.Status, "key", sv.stageCacheKey())
	sv.store.BuildDetail.Put(sv.stageCacheKey(), sv.build)
}

// restorePendingInputsCache seeds sv.pendingInputs from the store cache so
// the paused-input badge renders on first paint when re-entering the view,
// without waiting for the next 2 s build-detail tick.
func (sv *StageView) restorePendingInputsCache() {
	if sv.store == nil {
		return
	}
	if e := sv.store.PendingInputs.Get(sv.stageCacheKey()); e != nil {
		sv.pendingInputs = e.Value
		sv.recomputeStageStatusForInputs()
	}
}

// initReentry handles the path where stages are already loaded (returning
// from a child view): cancel stale goroutines and restart the preview.
func (sv *StageView) initReentry() tea.Cmd {
	sv.cancel()
	sv.ctx, sv.cancel = context.WithCancel(context.Background())
	sv.preview.SetContext(sv.ctx)

	cmds := []tea.Cmd{sv.preview.Restart(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
	if sv.isRunning() {
		cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick(), sv.fetchPrevStages)
	}
	return tea.Batch(cmds...)
}

// initFromCachedStages serves stages from the cache if available and returns
// (cmd, true). When no cache entry exists returns (nil, false).
func (sv *StageView) initFromCachedStages() (tea.Cmd, bool) {
	if sv.store == nil {
		return nil, false
	}
	e := sv.store.Stages.Get(sv.stageCacheKey())
	if e == nil {
		return nil, false
	}
	sv.stages = e.Value
	if we := sv.store.WhenSkipped.Get(sv.stageCacheKey()); we != nil {
		sv.currentSkipOccs = we.Value
		jenkins.MarkSkipped(sv.stages, we.Value)
	}
	sv.populateTable()
	sv.setInitialCursorFromCache()

	cmds := []tea.Cmd{sv.fetchStages, sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
	if sv.build.Status == "" || sv.isRunning() {
		// For running builds we kick a build-detail fetch up front so the
		// PendingInputs snapshot arrives in ~200ms — without it the top bar
		// and stage rows briefly show progress before the first refresh tick
		// (2s later) flips them to Paused.
		cmds = append(cmds, sv.fetchBuildDetail())
	}
	if sv.isRunning() {
		cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick(), sv.fetchPrevStages)
	} else {
		cmds = append(cmds, sv.buildDataCmds()...)
	}
	return tea.Batch(cmds...), true
}

// setInitialCursorFromCache positions the table cursor when restoring from
// cache, honouring an explicit selectStageName request when present.
func (sv *StageView) setInitialCursorFromCache() {
	if sv.selectStageName != "" {
		if c := sv.findStageByName(sv.selectStageName); c >= 0 {
			sv.table.SetCursor(c)
			sv.autoFollowing = false
			return
		}
	}
	sv.table.SetCursor(sv.initialCursor())
}

// initFresh is the no-cache path — populate empty, kick off fetches.
func (sv *StageView) initFresh() tea.Cmd {
	sv.populateTable()
	sv.table.SetCursor(0) // Pipeline row
	cmds := []tea.Cmd{
		sv.fetchStages,
		sv.preview.UpdateForCursor(pipelinePreviewIdx, sv.stages),
	}
	if sv.build.Status == "" || sv.build.Status == jmodel.BuildStatusRunning {
		// See initFromCachedStages: a fresh fetch surfaces PendingInputs
		// quickly so the paused indicator paints on first refresh, not 2s in.
		cmds = append(cmds, sv.fetchBuildDetail())
	}
	if sv.build.Status == jmodel.BuildStatusRunning {
		cmds = append(cmds, sv.fetchPrevStages)
	} else if sv.build.Status != "" {
		cmds = append(cmds, sv.buildDataCmds()...)
	}
	return tea.Batch(cmds...)
}

func (sv *StageView) fetchStages() tea.Msg {
	stages, err := sv.client.ListStages(sv.ctx, sv.nc.JobPath(), sv.build.Number)
	if sv.ctx.Err() != nil {
		return nil
	}
	return StagesMsg{Stages: stages, Err: err}
}

// buildDataCmds returns commands to fetch test results and artifacts for the
// current build. Should only be called for completed (non-running) builds.
func (sv *StageView) buildDataCmds() []tea.Cmd {
	if sv.store == nil {
		return nil
	}
	return []tea.Cmd{
		fetchTestReport(sv.client, sv.store, sv.nc.JobPath(), sv.build.Number),
		fetchArtifacts(sv.client, sv.store, sv.nc.JobPath(), sv.build.Number),
	}
}

func (sv *StageView) fetchBuildDetail() tea.Cmd {
	ctx := sv.ctx
	client := sv.client
	jobPath := sv.nc.JobPath()
	buildNumber := sv.build.Number
	return func() tea.Msg {
		detail, err := client.GetBuild(ctx, jobPath, buildNumber)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return buildDetailMsg{err: err}
		}
		return buildDetailMsg{build: detail.Build, pendingInputs: detail.PendingInputs}
	}
}

// Post-completion retry budgets. Jenkins typically finalises Duration and
// flushes the console log within a couple of seconds of the terminal status
// appearing; these caps are loose upper bounds so a genuinely empty result
// can't loop forever.
const (
	maxBuildDetailRetries = 10
	buildDetailRetryDelay = 2 * time.Second
	maxWhenSkipRetries    = 3
	whenSkipRetryDelay    = 3 * time.Second
)

// scheduleBuildDetailRetry waits before re-issuing a build-detail fetch,
// used when the first post-completion fetch still returned Duration == 0.
func (sv *StageView) scheduleBuildDetailRetry() tea.Cmd {
	ctx := sv.ctx
	fetch := sv.fetchBuildDetail()
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(buildDetailRetryDelay):
		}
		if ctx.Err() != nil {
			return nil
		}
		return fetch()
	}
}

// scheduleWhenSkipRetry waits before re-running the full console-log parse,
// used when the first post-completion detectWhenSkips run returned nothing
// (Jenkins may not have flushed the stage-skipped lines yet).
func (sv *StageView) scheduleWhenSkipRetry() tea.Cmd {
	ctx := sv.ctx
	detect := sv.detectWhenSkips()
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(whenSkipRetryDelay):
		}
		if ctx.Err() != nil {
			return nil
		}
		return detect()
	}
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
	jobPath := sv.nc.JobPath()
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
		// Fetch the official build status from Jenkins. If GetBuild fails
		// we still ship stageRefreshMsg with build=nil — better to keep
		// stage data flowing than to abort the whole tick on a transient
		// build-detail error.
		var build *jmodel.Build
		var pending []jmodel.PendingInput
		if detail, berr := client.GetBuild(ctx, jobPath, buildNumber); berr == nil {
			build = &detail.Build
			pending = detail.PendingInputs
		} else {
			slog.Warn("stageview.refresh: GetBuild failed", "job", jobPath, "build", buildNumber, "err", berr)
		}
		// Fetch incremental console log for when-skip detection.
		var logChunk string
		var logNext int
		if pl, lerr := client.GetProgressiveLog(ctx, jobPath, buildNumber, logNextStart); lerr == nil {
			logChunk = pl.Text
			logNext = pl.NextStart
		}
		return stageRefreshMsg{stages: stages, build: build, pendingInputs: pending, err: err, logChunk: logChunk, logNextStart: logNext}
	}
}

// pollForNewBuild polls ListBuilds for a build newer than lastKnownBuild.
func (sv *StageView) pollForNewBuild() tea.Cmd {
	ctx := sv.ctx
	client := sv.client
	jobPath := sv.nc.JobPath()
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
	// Behaviors process trigger / artifact / test / cancel messages.
	if handled, cmd := sv.host.HandleMsg(msg); handled {
		return sv, cmd
	}

	switch msg := msg.(type) {
	case ThemeChangedMsg:
		return sv, sv.handleThemeChanged(msg)
	case pendingBuildPollMsg:
		if sv.pending {
			return sv, sv.pollForNewBuild()
		}
	case pendingBuildFoundMsg:
		return sv, sv.handlePendingBuildFound(msg)
	case prevStagesFetchedMsg:
		return sv, sv.handlePrevStagesFetched(msg)
	case StagesMsg:
		return sv, sv.handleStagesMsg(msg)
	case stageProgressTickMsg:
		if sv.isRunning() {
			sv.populateTable()
		}
		return sv, sv.scheduleProgressTick()
	case stageRefreshMsg:
		return sv, sv.handleStageRefresh(msg)
	case whenSkipDetectedMsg:
		return sv, sv.handleWhenSkipDetected(msg)
	case buildDetailMsg:
		return sv, sv.handleBuildDetail(msg)
	case CancelBuildResultMsg:
		if msg.Err != nil {
			return sv, func() tea.Msg { return ErrorMsg(msg) }
		}
		// Don't set status eagerly — let the refresh loop pick up the
		// official status from Jenkins so the progress bar transitions
		// smoothly instead of disappearing and reappearing.
		return sv, sv.scheduleRefresh()
	case BuildCompletedMsg:
		return sv, sv.handleBuildCompleted(msg)
	case tea.KeyMsg:
		return sv.handleKeyMsg(msg)
	}
	return sv, nil
}

func (sv *StageView) handleThemeChanged(msg ThemeChangedMsg) tea.Cmd {
	sv.theme = msg.Theme
	sv.table.SetTheme(msg.Theme)
	sv.progressBar.SetTheme(msg.Theme)
	sv.preview.SetTheme(msg.Theme)
	sv.host.SetTheme(msg.Theme)
	sv.populateTable()
	return nil
}

func (sv *StageView) handlePendingBuildFound(msg pendingBuildFoundMsg) tea.Cmd {
	sv.pending = false
	sv.setBuild(msg.build)
	sv.nc.Build = NavBuildRef{Number: msg.build.Number}
	sv.preview.SetBuildNumber(msg.build.Number)
	// Now start normal operation (fetch stages, Pipeline preview).
	sv.populateTable()
	sv.table.SetCursor(0) // Pipeline row
	cmds := []tea.Cmd{sv.fetchStages, sv.fetchPrevStages}
	cmds = append(cmds, sv.preview.UpdateForCursor(pipelinePreviewIdx, sv.stages))
	return tea.Batch(cmds...)
}

func (sv *StageView) handlePrevStagesFetched(msg prevStagesFetchedMsg) tea.Cmd {
	if msg.err != nil {
		slog.Warn("stageview.prevStagesFetchedMsg error", "err", msg.err)
		return nil
	}
	sv.prevStages = msg.stages
	sv.computeGhostValidity()
	sv.populateTable()
	return nil
}

func (sv *StageView) handleStagesMsg(msg StagesMsg) tea.Cmd {
	if msg.Err != nil {
		slog.Warn("stageview.StagesMsg error", "err", msg.Err)
		return func() tea.Msg { return ErrorMsg{Err: msg.Err} }
	}
	slog.Debug("stageview.StagesMsg", "count", len(msg.Stages))
	sv.lastRefreshAt = time.Now()
	sv.stages = msg.Stages
	if sv.store != nil {
		sv.store.Stages.Put(sv.stageCacheKey(), msg.Stages)
	}
	jenkins.MarkSkipped(sv.stages, sv.currentSkipOccs)
	sv.populateTable()
	sv.applyInitialStagesCursor()
	cmds := []tea.Cmd{sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
	if sv.isRunning() {
		cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick())
	} else {
		cmds = append(cmds, sv.finalizeStagesCmds(msg.Stages)...)
	}
	return tea.Batch(cmds...)
}

func (sv *StageView) applyInitialStagesCursor() {
	if len(sv.stages) == 0 {
		return
	}
	if sv.selectStageName != "" {
		if c := sv.findStageByName(sv.selectStageName); c >= 0 {
			sv.table.SetCursor(c)
			sv.autoFollowing = false
			sv.selectStageName = ""
			return
		}
	}
	sv.table.SetCursor(sv.initialCursor())
}

// finalizeStagesCmds returns commands to run when a StagesMsg arrives for a
// build that is already finished: persist stages to disk and detect when-skips.
func (sv *StageView) finalizeStagesCmds(stages []jmodel.Stage) []tea.Cmd {
	var cmds []tea.Cmd
	if sv.store != nil && sv.store.Disk != nil {
		key := sv.stageCacheKey()
		disk := sv.store.Disk
		cmds = append(cmds, func() tea.Msg {
			_ = disk.SaveStages(key, stages)
			return nil
		})
	}
	cmds = append(cmds, sv.whenSkipCmd())
	return cmds
}

func (sv *StageView) handleStageRefresh(msg stageRefreshMsg) tea.Cmd {
	if msg.err != nil {
		slog.Warn("stageview.stageRefreshMsg error", "err", msg.err)
		if sv.build.Status == jmodel.BuildStatusRunning {
			return sv.scheduleRefresh()
		}
		return nil
	}
	hadStages := len(sv.stages) > 0
	cursorIdx := sv.table.Cursor()
	sv.applyRefreshState(msg)
	sv.applyRefreshCursor(hadStages, cursorIdx)

	var cmds []tea.Cmd
	if cmd := sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, sv.refreshContinuationCmds(msg)...)
	return tea.Batch(cmds...)
}

// applyRefreshState absorbs the message contents into view state: stages,
// build status, console log accumulation, and skip detection.
func (sv *StageView) applyRefreshState(msg stageRefreshMsg) {
	var buildStatus jmodel.BuildStatus
	if msg.build != nil {
		buildStatus = msg.build.Status
	}
	slog.Debug("stageview.stageRefreshMsg", "stages", len(msg.stages), "buildStatus", buildStatus)
	sv.lastRefreshAt = time.Now()
	sv.stages = msg.stages
	if sv.store != nil {
		sv.store.Stages.Put(sv.stageCacheKey(), msg.stages)
	}
	// Update build status before populating the table so the Pipeline
	// row reflects the latest build state on the transition refresh
	// (e.g. Running → Success).
	if msg.build != nil {
		sv.setBuild(*msg.build)
	}
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
	sv.setPendingInputs(msg.pendingInputs)
	sv.computeGhostValidity()
	sv.populateTable()
}

func (sv *StageView) applyRefreshCursor(hadStages bool, prevCursor int) {
	switch {
	case !hadStages && len(sv.stages) > 0:
		newCursor := sv.initialCursor()
		slog.Debug("stageview.refresh cursor: stages appeared", "cursor", newCursor)
		sv.table.SetCursor(newCursor)
	case sv.autoFollowing:
		// initialCursor finds the first running stage, or the last meaningful
		// stage when the build is done — covers fast stages that complete
		// between two refresh ticks.
		newCursor := sv.initialCursor()
		slog.Debug("stageview.refresh cursor: auto-follow", "from", prevCursor, "to", newCursor)
		sv.table.SetCursor(newCursor)
	default:
		sv.table.SetCursor(prevCursor)
	}
}

// refreshContinuationCmds decides whether to reschedule another refresh or
// finalize the build (when stages stopped running and Jenkins reports done).
func (sv *StageView) refreshContinuationCmds(msg stageRefreshMsg) []tea.Cmd {
	buildFinished := msg.build != nil && msg.build.Status != jmodel.BuildStatusRunning
	if sv.anyStageRunning() || !buildFinished {
		return []tea.Cmd{sv.scheduleRefresh()}
	}
	slog.Debug("stageview.refresh: build finished", "buildStatus", sv.build.Status)
	sv.ghostsValid = false
	sv.populateTable()
	if sv.buildFinishedAt.IsZero() {
		sv.buildFinishedAt = time.Now()
	}
	cmds := []tea.Cmd{sv.whenSkipCmd()}
	if sv.build.Duration == 0 {
		cmds = append(cmds, sv.fetchBuildDetail())
	}
	cmds = append(cmds, sv.buildDataCmds()...)
	return cmds
}

func (sv *StageView) handleWhenSkipDetected(msg whenSkipDetectedMsg) tea.Cmd {
	if len(msg.skippedOccs) == 0 {
		// Jenkins may not have flushed the stage-skipped lines to the console
		// log yet. Retry (bounded) for a short window after the build
		// finished before giving up.
		if !sv.buildFinishedAt.IsZero() && sv.whenSkipRetries < maxWhenSkipRetries {
			sv.whenSkipRetries++
			return sv.scheduleWhenSkipRetry()
		}
		return nil
	}
	sv.whenSkipRetries = 0
	sv.currentSkipOccs = msg.skippedOccs
	if sv.store != nil {
		sv.store.WhenSkipped.Put(sv.stageCacheKey(), msg.skippedOccs)
	}
	jenkins.MarkSkipped(sv.stages, msg.skippedOccs)
	sv.populateTable()
	return nil
}

func (sv *StageView) handleBuildDetail(msg buildDetailMsg) tea.Cmd {
	if msg.err != nil {
		return nil
	}
	sv.setBuild(msg.build)
	sv.setPendingInputs(msg.pendingInputs)
	sv.populateTable()
	// Jenkins sometimes reports a terminal status before it has written the
	// final Duration. Retry (bounded) until the duration materialises.
	if msg.build.Duration == 0 && isTerminalStatus(msg.build.Status) &&
		sv.buildDetailRetries < maxBuildDetailRetries {
		sv.buildDetailRetries++
		return sv.scheduleBuildDetailRetry()
	}
	return nil
}

// setPendingInputs updates the pending-inputs snapshot, re-projects stage
// status, and lets the input behavior auto-close its dialog when the input
// is no longer pending. Persists to the store cache so subsequent visits to
// this view see the paused state on first render rather than after the next
// 2 s refresh tick.
func (sv *StageView) setPendingInputs(pending []jmodel.PendingInput) {
	sv.pendingInputs = pending
	sv.recomputeStageStatusForInputs()
	if sv.input != nil {
		sv.input.SyncPending(pending)
	}
	sv.cachePendingInputs(pending)
}

// cachePendingInputs writes the snapshot under the branch's own cache key
// and under each ancestor path with the same build number. Parent joblist
// rows look up `<ancestorPath>:<buildNum>` where buildNum is the parent's
// rolled-up lastBuild number — for a paused branch build this matches the
// rolled-up number, letting the multibranch / folder row render the paused
// badge without an extra HTTP call.
func (sv *StageView) cachePendingInputs(pending []jmodel.PendingInput) {
	if sv.store == nil || sv.store.PendingInputs == nil {
		return
	}
	for path := sv.nc.JobPath(); path != ""; path = parentJobPath(path) {
		sv.store.PendingInputs.Put(fmt.Sprintf("%s:%d", path, sv.build.Number), pending)
	}
}

// parentJobPath returns the parent of a slash-separated job path, or "" when
// jobPath has no parent. The path keeps URL-encoded segments intact ("%2F"
// stays as one segment — splitting is by literal "/").
func parentJobPath(jobPath string) string {
	idx := indexLastSlash(jobPath)
	if idx <= 0 {
		return ""
	}
	return jobPath[:idx]
}

func indexLastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// handleBuildCompleted is delivered by RunningBuildsMonitor within ~1s of the
// build leaving the running set. It's defence-in-depth so the view still
// updates if the 2s refresh chain misses a tick (transient network blip at
// the moment of completion).
func (sv *StageView) handleBuildCompleted(msg BuildCompletedMsg) tea.Cmd {
	if msg.Err != nil {
		return nil
	}
	if msg.JobPath != sv.nc.JobPath() || msg.Number != sv.build.Number {
		return nil
	}
	sv.setBuild(msg.Build)
	sv.populateTable()
	return nil
}

func (sv *StageView) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Behaviors handle trigger param form, trigger confirm, cancel confirm,
	// and the T/A/x/t shortcuts before falling through to view-local keys.
	if handled, cmd := sv.host.HandleKey(msg); handled {
		return sv, cmd
	}

	prevCursor := sv.table.Cursor()
	if model, cmd, returned := sv.handleStageKey(msg); returned {
		return model, cmd
	}
	// If cursor moved by user input, update auto-follow state and preview.
	// Auto-follow only stays on when the user lands on the exact stage that
	// initialCursor() would pick (the deepest running child). Moving to any
	// other stage — even a running parent — disables it.
	if sv.table.Cursor() != prevCursor {
		sv.autoFollowing = sv.table.Cursor() == sv.initialCursor()
		slog.Debug("stageview.keypress: cursor moved",
			"from", prevCursor, "to", sv.table.Cursor(),
			"autoFollowing", sv.autoFollowing)
		return sv, sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)
	}
	return sv, nil
}

// handleStageKey dispatches a single keypress. The third return value is true
// when the caller should return immediately (the key produced a tea.Cmd or
// terminated handling); false means the key only moved the table cursor and
// the caller should fall through to the post-key cursor/preview update.
func (sv *StageView) handleStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
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
		return sv.openSelectedStage()
	case "l":
		return sv.openConsoleSwap()
	case "d":
		build := sv.build
		nc := sv.nc
		return sv, func() tea.Msg {
			return SwapViewMsg{View: NewDescribeView(sv.theme, sv.client, sv.store, nc, build)}
		}, true
	case "t":
		return sv, sv.trigger.startTrigger(sv.build.Number), true
	}
	return sv, nil, false
}

func (sv *StageView) openSelectedStage() (tea.Model, tea.Cmd, bool) {
	if sv.table.IsDisabled(sv.table.Cursor()) {
		return sv, nil, true
	}
	realIdx := sv.realStageIdx(sv.table.Cursor())
	if realIdx == -1 {
		// Pipeline row — open the full console log view, seeded from preview.
		child := sv.newSeededConsole()
		return sv, func() tea.Msg { return PushViewMsg{View: child} }, true
	}
	if realIdx >= 0 && realIdx < len(sv.stages) {
		stage := sv.stages[realIdx]
		if len(stage.NodeIDs) > 0 {
			stageNC := sv.nc.AtStage(stage.Name)
			stageNC.StageParent = parentStageName(sv.stages, realIdx)
			child := NewStageLogViewWithBuild(sv.theme, sv.client, sv.store, stageNC, stage.NodeIDs, sv.build.Status == jmodel.BuildStatusRunning, sv.build)
			return sv, func() tea.Msg { return PushViewMsg{View: child} }, true
		}
	}
	return sv, nil, false
}

func (sv *StageView) openConsoleSwap() (tea.Model, tea.Cmd, bool) {
	if sv.pending {
		return sv, nil, true
	}
	child := sv.newSeededConsole()
	return sv, func() tea.Msg { return SwapViewMsg{View: child} }, true
}

func (sv *StageView) newSeededConsole() *ConsoleView {
	seedLines, seedNext, seedDone := sv.preview.ConsoleSnapshot()
	child := NewConsoleViewSeeded(sv.theme, sv.client, sv.nc, seedLines, seedNext, seedDone)
	child.build = sv.build
	child.store = sv.store
	return child
}

// anyStageRunning returns true if any stage is still in progress.
func (sv *StageView) anyStageRunning() bool {
	for _, s := range sv.stages {
		if s.Status == jmodel.BuildStatusRunning {
			return true
		}
	}
	return false
}

// isRunning is the single authority for "render the running bar / keep
// polling because the build is in progress." The build API status is the
// authoritative signal — if Jenkins says terminal, we trust it, even if
// flowGraph HTML still shows a stage as "in progress" during the
// finalisation window. anyStageRunning is consulted only as a fallback for
// the initial-load races where the build status hasn't been fetched yet.
func (sv *StageView) isRunning() bool {
	if sv.build.Status == jmodel.BuildStatusRunning {
		return true
	}
	if sv.build.Status == "" || sv.build.Status == jmodel.BuildStatusUnknown {
		return sv.anyStageRunning()
	}
	return false
}

// isTerminalStatus reports whether a build status represents a completed
// (non-running) state. Empty and "unknown" are excluded: we only retry
// duration fetches for builds that Jenkins has explicitly marked done.
func isTerminalStatus(s jmodel.BuildStatus) bool {
	switch s {
	case jmodel.BuildStatusSuccess,
		jmodel.BuildStatusFailed,
		jmodel.BuildStatusAborted,
		jmodel.BuildStatusUnstable,
		jmodel.BuildStatusSkipped,
		jmodel.BuildStatusNotBuilt:
		return true
	}
	return false
}

// allStagesFinished returns true when stages exist and none are Running or NotBuilt.
func allStagesFinished(stages []jmodel.Stage) bool {
	if len(stages) == 0 {
		return false
	}
	for _, s := range stages {
		if s.Status == jmodel.BuildStatusRunning || s.Status == jmodel.BuildStatusNotBuilt {
			return false
		}
	}
	return true
}

// inferBuildStatusFromStages derives a terminal build status from stage data.
// Precedence: Failed > Aborted > Unstable > Success. If no recognisable
// terminal status is found, it returns BuildStatusUnknown.
func inferBuildStatusFromStages(stages []jmodel.Stage) jmodel.BuildStatus {
	hasFailed := false
	hasAborted := false
	hasUnstable := false
	hasSuccess := false
	for _, s := range stages {
		switch s.Status {
		case jmodel.BuildStatusFailed:
			hasFailed = true
		case jmodel.BuildStatusAborted:
			hasAborted = true
		case jmodel.BuildStatusUnstable:
			hasUnstable = true
		case jmodel.BuildStatusSuccess, jmodel.BuildStatusSkipped:
			hasSuccess = true
		}
	}
	switch {
	case hasFailed:
		return jmodel.BuildStatusFailed
	case hasAborted:
		return jmodel.BuildStatusAborted
	case hasUnstable:
		return jmodel.BuildStatusUnstable
	case hasSuccess:
		return jmodel.BuildStatusSuccess
	default:
		return jmodel.BuildStatusUnknown
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
		if s.Status == jmodel.BuildStatusRunning {
			lastRunning = i
		}
	}
	if lastRunning >= 0 {
		slog.Debug("stageview.initialCursor: running stage", "idx", lastRunning, "name", sv.stages[lastRunning].Name)
		return sv.tableCursorForStage(lastRunning)
	}
	// No running stage — prefer the last failed stage.
	lastFailed := -1
	for i, s := range sv.stages {
		if s.Status == jmodel.BuildStatusFailed {
			lastFailed = i
		}
	}
	if lastFailed >= 0 {
		slog.Debug("stageview.initialCursor: last failed stage", "idx", lastFailed, "name", sv.stages[lastFailed].Name)
		return sv.tableCursorForStage(lastFailed)
	}
	// No failed stage — pick the last non-skipped/non-not-built stage.
	for i := len(sv.stages) - 1; i >= 0; i-- {
		s := sv.stages[i].Status
		if s != jmodel.BuildStatusSkipped && s != jmodel.BuildStatusNotBuilt {
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

// findStageByName returns the table cursor for the first stage with the given name, or -1.
func (sv *StageView) findStageByName(name string) int {
	for i, s := range sv.stages {
		if s.Name == name {
			return sv.tableCursorForStage(i)
		}
	}
	return -1
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
	rows := make([]component.Row, 0, len(sv.stages)+1)
	rows = append(rows, sv.pipelineRow())
	for i, s := range sv.stages {
		rows = append(rows, sv.stageRow(i, s))
	}
	rows, disabledIndices := sv.appendGhostRows(rows)
	disabledIndices = sv.markSkippedDisabled(disabledIndices)

	sv.table.SetRows(rows)
	sv.table.SetDisabled(disabledIndices)
}

// pipelineRow builds the synthetic Pipeline root row.
func (sv *StageView) pipelineRow() component.Row {
	return component.Row{
		"▸ Pipeline",
		renderStatus(sv.theme, sv.effectiveBuildStatus()),
		sv.pipelineDurationCell(),
	}
}

// effectiveBuildStatus promotes Running → PausedInput when the build has any
// pending input. Lets the Pipeline row + status badge reflect "waiting for
// human" without storing a separate flag.
func (sv *StageView) effectiveBuildStatus() jmodel.BuildStatus {
	if sv.build.Status == jmodel.BuildStatusRunning && len(sv.pendingInputs) > 0 {
		return jmodel.BuildStatusPausedInput
	}
	return sv.build.Status
}

// pipelineDurationCell renders the pipeline duration. While running, the
// duration counter keeps advancing as plain elapsed text instead of the
// progress bar when the build is paused on input — the paused state is
// signalled by the status column and the top bar, leaving the duration
// column as a plain wall-clock counter the user can trust.
func (sv *StageView) pipelineDurationCell() string {
	if sv.build.Status != jmodel.BuildStatusRunning {
		return formatDuration(sv.build.Duration)
	}
	elapsed := time.Since(sv.build.Timestamp)
	if len(sv.pendingInputs) > 0 {
		return formatDuration(elapsed)
	}
	if estimate := sv.effectiveEstimate(); estimate > 0 {
		return sv.progressBar.DualRenderWithText(colStageDurationWidth, elapsed, estimate)
	}
	return formatDuration(elapsed)
}

// stageRow builds a single stage row at index i.
func (sv *StageView) stageRow(i int, s jmodel.Stage) component.Row {
	prefix := pipelineTreePrefix(sv.stages, i)
	icon := stageIcon(s)
	return component.Row{
		prefix + icon + s.Name,
		renderStatus(sv.theme, s.Status),
		sv.stageDurationCell(i, s),
	}
}

// stageIcon picks the prefix glyph (parallel vs sequential).
func stageIcon(s jmodel.Stage) string {
	if s.Parallel {
		return "⇶ "
	}
	return "▸ "
}

// stageDurationCell formats the duration cell, using wall-clock interpolation
// for running stages so Jenkins' coarse Duration jumps don't make the bar leap.
// Paused-input stages render as plain elapsed text (no progress bar) — the
// "Input" label lives in the status column.
func (sv *StageView) stageDurationCell(i int, s jmodel.Stage) string {
	if s.Status == jmodel.BuildStatusPausedInput {
		if sv.stageStartWall == nil {
			sv.stageStartWall = make(map[int]time.Time)
		}
		start, ok := sv.stageStartWall[i]
		if !ok {
			start = time.Now().Add(-s.Duration)
			sv.stageStartWall[i] = start
		}
		return formatDuration(time.Since(start))
	}
	if s.Status != jmodel.BuildStatusRunning {
		return formatDuration(s.Duration)
	}
	if sv.stageStartWall == nil {
		sv.stageStartWall = make(map[int]time.Time)
	}
	start, ok := sv.stageStartWall[i]
	if !ok {
		start = time.Now().Add(-s.Duration)
		sv.stageStartWall[i] = start
	}
	stageElapsed := time.Since(start)
	if sv.ghostsValid && i < len(sv.prevStages) && sv.prevStages[i].Duration > 0 {
		return sv.progressBar.DualRenderWithText(colStageDurationWidth, stageElapsed, sv.prevStages[i].Duration)
	}
	return formatDuration(stageElapsed)
}

// appendGhostRows tacks on dimmed "pending" rows from the previous build for
// stages that haven't started yet in this running build. Returns the updated
// rows + a disabled-indices map (nil when no ghost rows added).
func (sv *StageView) appendGhostRows(rows []component.Row) ([]component.Row, map[int]bool) {
	ghostStart := len(sv.stages)
	ghostEnd := len(sv.prevStages)
	if !sv.ghostsValid || sv.build.Status != jmodel.BuildStatusRunning || ghostStart >= ghostEnd {
		return rows, nil
	}
	dimStyle := sv.theme.Stage.GhostDim
	disabled := make(map[int]bool)
	for i := ghostStart; i < ghostEnd; i++ {
		ps := sv.prevStages[i]
		prefix := pipelineTreePrefix(sv.prevStages, i)
		rows = append(rows, component.Row{
			dimStyle.Render(prefix + stageIcon(ps) + ps.Name),
			dimStyle.Render("pending"),
			dimStyle.Render(formatDuration(ps.Duration)),
		})
		disabled[1+i] = true // +1 for the synthetic Pipeline row
	}
	return rows, disabled
}

// markSkippedDisabled flags skipped stages as non-selectable.
func (sv *StageView) markSkippedDisabled(disabled map[int]bool) map[int]bool {
	for i, s := range sv.stages {
		if s.Status != jmodel.BuildStatusSkipped {
			continue
		}
		if disabled == nil {
			disabled = make(map[int]bool)
		}
		disabled[1+i] = true
	}
	return disabled
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
	if sv.isRunning() {
		elapsed := time.Since(sv.build.Timestamp)
		barWidth := sv.width
		if barWidth < 1 {
			barWidth = 1
		}
		var bar string
		switch {
		case len(sv.pendingInputs) > 0:
			label := iconOr(sv.theme.Icons.StatusPausedInput, "⏸") + " " + sv.pausedBarLabel()
			bar = sv.progressBar.RenderCompleteTall(barWidth, label, sv.theme.BuildStatus.PausedInput)
		case sv.effectiveEstimate() > 0:
			bar = sv.progressBar.RenderWithTextTall(barWidth, elapsed, sv.effectiveEstimate())
		default:
			bar = sv.progressBar.RenderPendingTall(barWidth, "First run - no estimate")
		}
		content = bar + "\n" + sv.table.View()
	} else if sv.build.Status != jmodel.BuildStatusUnknown && sv.build.Status != "" {
		bar := sv.renderFinishedBar()
		content = bar + "\n" + sv.table.View()
	} else {
		content = sv.table.View()
	}

	return content
}

func (sv *StageView) PopupView() string {
	return sv.host.PopupView()
}

// pausedBarLabel returns the centre label for the paused top bar, naming the
// pending input's message when there is exactly one (single confirm-only or
// parameterised input) and falling back to a generic label otherwise.
func (sv *StageView) pausedBarLabel() string {
	if len(sv.pendingInputs) == 1 && sv.pendingInputs[0].Message != "" {
		return "Paused — " + sv.pendingInputs[0].Message
	}
	return "Paused — awaiting input"
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
	case jmodel.BuildStatusSuccess:
		style = sv.theme.BuildStatus.Success
	case jmodel.BuildStatusFailed:
		style = sv.theme.BuildStatus.Failed
	case jmodel.BuildStatusAborted:
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
	// MakeBreadcrumb clips nc to scope and renders Build.Number when > 0.
	// For pending builds, nc.Build.Number == 0, so we append "Pending" manually.
	seg := sv.MakeBreadcrumb("stages")
	if sv.pending {
		seg.Context = append(seg.Context, component.BreadcrumbPart{Text: "Pending", IsBuildNum: true, Separator: " "})
	}
	return seg
}

func (sv *StageView) ItemCount() int {
	return 0 // stages don't show a count badge in the breadcrumb
}

func (sv *StageView) Commands() []command.Command {
	return nil
}

func (sv *StageView) Shortcuts() []component.Shortcut {
	var sc []component.Shortcut
	realIdx := sv.realStageIdx(sv.table.Cursor())
	if realIdx == -1 {
		sc = append(sc, component.Nav("enter", "full log"))
	} else if realIdx >= 0 && realIdx < len(sv.stages) && len(sv.stages[realIdx].NodeIDs) > 0 {
		sc = append(sc, component.Nav("enter", "stage log"))
	}
	sc = append(sc,
		component.Nav("esc", "builds"),
		component.Filter("/", "search", false),
	)
	sc = append(sc, detailViewTabs("s")...)
	// Behaviors append: T (tests), A (artifacts), x (cancel — when running),
	// t (trigger). cancelBehavior's canCancel() already gates on Running, so
	// the previous `!sv.pending` guard is preserved implicitly.
	sc = sv.host.AppendShortcuts(sc)
	return sc
}

// findFailedStage returns the index of the first failed stage, or -1.
func (sv *StageView) SetSize(width, height int) {
	sv.BaseView.SetSize(width, height)
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
	sv.host.SetSize(width, height-6)
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
	jobPath := sv.nc.JobPath()
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
	builds, err := sv.client.ListBuilds(sv.ctx, sv.nc.JobPath())
	if err != nil || sv.ctx.Err() != nil {
		return prevStagesFetchedMsg{err: err}
	}
	currentNum := sv.build.Number
	// Find most recent completed build (not current). Prefer success, accept any terminal.
	var prevBuild *jmodel.Build
	for i := range builds {
		b := &builds[i]
		if b.Number == currentNum {
			continue
		}
		if b.Status == jmodel.BuildStatusRunning {
			continue
		}
		if prevBuild == nil || b.Status == jmodel.BuildStatusSuccess {
			prevBuild = b
		}
		if b.Status == jmodel.BuildStatusSuccess {
			break
		}
	}
	if prevBuild == nil {
		return prevStagesFetchedMsg{}
	}
	stages, err := sv.client.ListStages(sv.ctx, sv.nc.JobPath(), prevBuild.Number)
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
	return sv.build.EstimatedDuration
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

// PreviewBadge implements HasPreviewBadge.
func (sv *StageView) PreviewBadge() string {
	return sv.preview.Badge()
}

func (sv *StageView) HasPopup() bool {
	return sv.host.HasPopup()
}

func (sv *StageView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: sv.table.ScrollOffset(), TotalLines: sv.table.TotalRows(), ViewHeight: sv.table.ContentHeight()}
}

func (sv *StageView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	nc := sv.nc.AtBranch(sv.nc.BranchName)
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

package view

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
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

// buildDetailMsg carries the result of a build detail fetch.
type buildDetailMsg struct {
	build jenkins.Build
	err   error
}

// StageView shows the pipeline stages of a single build.
type StageView struct {
	theme         theme.Theme
	table         component.Table
	client        jenkins.JenkinsClient
	nc            NavigationContext
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
	prevStages  []jenkins.Stage // stages from previous completed build
	ghostsValid bool            // current stages are a prefix of prevStages

	store *cache.Store

	// Test results and artifacts for the current build. Fetched after build completes.
	testResult      *jenkins.TestReport
	artifacts       []jenkins.Artifact
	artifactsLoaded bool

	// Trigger build dialog state.
	confirmTrigger bool
	triggerYes     bool
	paramForm      *component.ParamForm
}

func NewStageView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext, build jenkins.Build) *StageView {
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
		nc:            nc,
		build:         build,
		ctx:           ctx,
		cancel:        cancel,
		progressBar:   component.NewProgressBar(t),
		preview:       NewPreviewPanel(t, client, store, ctx, nc),
		autoFollowing: true,
	}
}

// NewPendingStageView creates a StageView that waits for Jenkins to create
// the build. It polls ListBuilds until a build with a number higher than
// lastKnownBuild appears, then switches to normal operation.
func NewPendingStageView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext, lastKnownBuild int) *StageView {
	ctx, cancel := context.WithCancel(context.Background())
	columns := []component.Column{
		{Title: "STAGE", Width: 40},
		{Title: "STATUS", Width: colStageStatusWidth},
		{Title: "DURATION", Width: colStageDurationWidth},
	}
	pendingNC := nc
	pendingNC.Build = NavBuildRef{}
	return &StageView{
		theme:          t,
		table:          component.NewTable(t, columns),
		client:         client,
		store:          store,
		nc:             pendingNC,
		build:          jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()},
		ctx:            ctx,
		cancel:         cancel,
		progressBar:    component.NewProgressBar(t),
		preview:        NewPreviewPanel(t, client, store, ctx, pendingNC),
		autoFollowing:  true,
		pending:        true,
		lastKnownBuild: lastKnownBuild,
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
	return fmt.Sprintf("%s:%d", sv.nc.JobPath(), sv.build.Number)
}

// setBuild updates the build and caches it for later restoration.
func (sv *StageView) setBuild(b jenkins.Build) {
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
	// Cache / restore build detail so the pipeline row renders correctly
	// when returning from a child view (where only the build number is passed).
	if sv.store != nil {
		if sv.build.Status == "" {
			if e := sv.store.BuildDetail.Get(sv.stageCacheKey()); e != nil {
				slog.Debug("stageview.Init: restored build from cache", "status", e.Value.Status, "duration", e.Value.Duration)
				sv.build = e.Value
			} else {
				slog.Debug("stageview.Init: no cached build found", "key", sv.stageCacheKey())
			}
		} else {
			slog.Debug("stageview.Init: caching build", "status", sv.build.Status, "key", sv.stageCacheKey())
			sv.store.BuildDetail.Put(sv.stageCacheKey(), sv.build)
		}
	}
	if len(sv.stages) > 0 {
		// Re-init (e.g. after child view popped): cancel stale goroutines
		// from the previous session and create a fresh context.
		sv.cancel()
		sv.ctx, sv.cancel = context.WithCancel(context.Background())
		sv.preview.SetContext(sv.ctx)

		cmds := []tea.Cmd{sv.preview.Restart(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
		if sv.isRunning() {
			cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick(), sv.fetchPrevStages)
		}
		return tea.Batch(cmds...)
	}
	// Serve cached stages immediately if available.
	if sv.store != nil {
		if e := sv.store.Stages.Get(sv.stageCacheKey()); e != nil {
			sv.stages = e.Value
			if we := sv.store.WhenSkipped.Get(sv.stageCacheKey()); we != nil {
				sv.currentSkipOccs = we.Value
				jenkins.MarkSkipped(sv.stages, we.Value)
			}
			sv.populateTable()
			if sv.selectStageName != "" {
				if c := sv.findStageByName(sv.selectStageName); c >= 0 {
					sv.table.SetCursor(c)
					sv.autoFollowing = false
				} else {
					sv.table.SetCursor(sv.initialCursor())
				}
			} else {
				sv.table.SetCursor(sv.initialCursor())
			}
			cmds := []tea.Cmd{sv.fetchStages, sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
			if sv.build.Status == "" {
				cmds = append(cmds, sv.fetchBuildDetail())
			}
			if sv.isRunning() {
				cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick(), sv.fetchPrevStages)
			} else {
				cmds = append(cmds, sv.buildDataCmds()...)
			}
			return tea.Batch(cmds...)
		}
	}
	sv.populateTable()
	sv.table.SetCursor(0) // Pipeline row
	cmds := []tea.Cmd{sv.fetchStages}
	// Pipeline row is selected — start streaming full console log in preview.
	cmds = append(cmds, sv.preview.UpdateForCursor(pipelinePreviewIdx, sv.stages))
	if sv.build.Status == "" {
		cmds = append(cmds, sv.fetchBuildDetail())
	}
	if sv.build.Status == jenkins.BuildStatusRunning {
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
		return buildDetailMsg{build: detail.Build}
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
		var build *jenkins.Build
		if detail, berr := client.GetBuild(ctx, jobPath, buildNumber); berr == nil {
			build = &detail.Build
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
		return stageRefreshMsg{stages: stages, build: build, err: err, logChunk: logChunk, logNextStart: logNext}
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

	switch msg := msg.(type) {
	case ThemeChangedMsg:
		sv.theme = msg.Theme
		sv.table.SetTheme(msg.Theme)
		sv.progressBar.SetTheme(msg.Theme)
		sv.preview.SetTheme(msg.Theme)
		if sv.paramForm != nil {
			sv.paramForm.SetTheme(msg.Theme)
		}
		sv.populateTable()
		return sv, nil

	case pendingBuildPollMsg:
		if sv.pending {
			return sv, sv.pollForNewBuild()
		}

	case pendingBuildFoundMsg:
		sv.pending = false
		sv.setBuild(msg.build)
		sv.nc.Build = NavBuildRef{Number: msg.build.Number}
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
			if sv.selectStageName != "" {
				if c := sv.findStageByName(sv.selectStageName); c >= 0 {
					sv.table.SetCursor(c)
					sv.autoFollowing = false
					sv.selectStageName = ""
				} else {
					sv.table.SetCursor(sv.initialCursor())
				}
			} else {
				sv.table.SetCursor(sv.initialCursor())
			}
		}
		cmds := []tea.Cmd{sv.preview.UpdateForCursor(sv.previewIdxForCursor(sv.table.Cursor()), sv.stages)}
		if sv.isRunning() {
			cmds = append(cmds, sv.scheduleRefresh(), sv.scheduleProgressTick())
		} else {
			// Build is finished — persist stages and apply when-skip detection.
			if sv.store != nil && sv.store.Disk != nil {
				key := sv.stageCacheKey()
				stages := msg.Stages
				disk := sv.store.Disk
				cmds = append(cmds, func() tea.Msg {
					_ = disk.SaveStages(key, stages)
					return nil
				})
			}
			cmds = append(cmds, sv.whenSkipCmd())
		}
		return sv, tea.Batch(cmds...)

	case stageProgressTickMsg:
		if sv.isRunning() {
			sv.populateTable()
		}
		return sv, sv.scheduleProgressTick()

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
		// Update build status before populating the table so the Pipeline
		// row reflects the latest build state on the transition refresh
		// (e.g. Running → Success).
		if msg.build != nil {
			sv.setBuild(*msg.build)
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

		// setBuild was already called above so the Pipeline row reflected
		// the latest status during populateTable. Here we only read the
		// status to decide whether to keep refreshing.
		buildFinished := false
		if msg.build != nil {
			buildFinished = msg.build.Status != jenkins.BuildStatusRunning
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
			if sv.buildFinishedAt.IsZero() {
				sv.buildFinishedAt = time.Now()
			}
			cmds = append(cmds, sv.whenSkipCmd())
			if sv.build.Duration == 0 {
				cmds = append(cmds, sv.fetchBuildDetail())
			}
			cmds = append(cmds, sv.buildDataCmds()...)
		}
		return sv, tea.Batch(cmds...)

	case whenSkipDetectedMsg:
		if len(msg.skippedOccs) == 0 {
			// Jenkins may not have flushed the stage-skipped lines to the
			// console log yet. Retry (bounded) for a short window after the
			// build finished before giving up.
			if !sv.buildFinishedAt.IsZero() && sv.whenSkipRetries < maxWhenSkipRetries {
				sv.whenSkipRetries++
				return sv, sv.scheduleWhenSkipRetry()
			}
			return sv, nil
		}
		sv.whenSkipRetries = 0
		sv.currentSkipOccs = msg.skippedOccs
		if sv.store != nil {
			sv.store.WhenSkipped.Put(sv.stageCacheKey(), msg.skippedOccs)
		}
		jenkins.MarkSkipped(sv.stages, msg.skippedOccs)
		sv.populateTable()
		return sv, nil

	case TestReportMsg:
		if msg.Err == nil && msg.JobPath == sv.nc.JobPath() && msg.BuildNum == sv.build.Number {
			sv.testResult = msg.Report
		}
		return sv, nil

	case ArtifactsMsg:
		if msg.Err == nil && msg.JobPath == sv.nc.JobPath() && msg.BuildNum == sv.build.Number {
			sv.artifacts = msg.Artifacts
			sv.artifactsLoaded = true
		}
		return sv, nil

	case buildDetailMsg:
		if msg.err != nil {
			return sv, nil
		}
		sv.setBuild(msg.build)
		sv.populateTable()
		// Jenkins sometimes reports a terminal status before it has written
		// the final Duration. Retry (bounded) until the duration materialises.
		if msg.build.Duration == 0 && isTerminalStatus(msg.build.Status) &&
			sv.buildDetailRetries < maxBuildDetailRetries {
			sv.buildDetailRetries++
			return sv, sv.scheduleBuildDetailRetry()
		}
		return sv, nil

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return sv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		// Don't set status eagerly — let the refresh loop pick up the
		// official status from Jenkins so the progress bar transitions
		// smoothly instead of disappearing and reappearing.
		return sv, sv.scheduleRefresh()

	case JobParamsMsg:
		if msg.Err != nil {
			return sv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		if len(msg.Params) == 0 {
			sv.confirmTrigger = true
			sv.triggerYes = false
			return sv, nil
		}
		form := component.NewParamForm(sv.theme, msg.Params)
		form.SetSize(sv.width, sv.height-6)
		sv.paramForm = &form
		return sv, nil

	case TriggerBuildResultMsg:
		if msg.Err != nil {
			return sv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		lastKnown := sv.build.Number
		nc := sv.nc.AtScope()
		return sv, func() tea.Msg {
			return OpenTriggeredBuildMsg{NC: nc, LastKnownBuild: lastKnown}
		}

	case BuildCompletedMsg:
		// The RunningBuildsMonitor delivers this within ~1s of the build
		// leaving the running set. It's a defence-in-depth signal so the
		// view still updates if the 2s refresh chain misses a tick (e.g.
		// during a transient network blip at the moment of completion).
		if msg.Err != nil {
			return sv, nil
		}
		if msg.JobPath != sv.nc.JobPath() || msg.Number != sv.build.Number {
			return sv, nil
		}
		sv.setBuild(msg.Build)
		sv.populateTable()
		return sv, nil

	case tea.KeyMsg:
		if sv.paramForm != nil {
			result := sv.paramForm.Update(msg)
			switch result.Status {
			case component.ParamFormDone:
				sv.paramForm = nil
				return sv, triggerBuild(sv.client, sv.nc, result.Values)
			case component.ParamFormCancelled:
				sv.paramForm = nil
			}
			return sv, nil
		}

		if sv.confirmTrigger {
			switch msg.String() {
			case "left", "right", "h":
				sv.triggerYes = !sv.triggerYes
			case "y":
				sv.confirmTrigger = false
				return sv, triggerBuild(sv.client, sv.nc, nil)
			case "enter":
				if sv.triggerYes {
					sv.confirmTrigger = false
					return sv, triggerBuild(sv.client, sv.nc, nil)
				}
				sv.confirmTrigger = false
			default:
				sv.confirmTrigger = false
			}
			return sv, nil
		}

		if sv.confirmCancel {
			switch msg.String() {
			case "left", "right", "h":
				sv.confirmYes = !sv.confirmYes
			case "y":
				sv.confirmCancel = false
				jobPath, number := sv.nc.JobPath(), sv.build.Number
				return sv, func() tea.Msg {
					err := sv.client.CancelBuild(context.Background(), jobPath, number)
					return CancelBuildResultMsg{Err: err}
				}
			case "enter":
				if sv.confirmYes {
					sv.confirmCancel = false
					jobPath, number := sv.nc.JobPath(), sv.build.Number
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
				// Pipeline row — open the full console log view, seeded from preview.
				seedLines, seedNext, seedDone := sv.preview.ConsoleSnapshot()
				child := NewConsoleViewSeeded(sv.theme, sv.client, sv.nc, seedLines, seedNext, seedDone)
				child.build = sv.build
				child.store = sv.store
				return sv, func() tea.Msg { return PushViewMsg{View: child} }
			}
			if realIdx >= 0 && realIdx < len(sv.stages) {
				stage := sv.stages[realIdx]
				if len(stage.NodeIDs) > 0 {
					child := NewStageLogView(sv.theme, sv.client, sv.store, sv.nc.AtStage(stage.Name), stage.NodeIDs, sv.build.Status == jenkins.BuildStatusRunning)
					child.build = sv.build
					return sv, func() tea.Msg { return PushViewMsg{View: child} }
				}
			}
		case "l":
			if sv.pending {
				return sv, nil
			}
			seedLines, seedNext, seedDone := sv.preview.ConsoleSnapshot()
			child := NewConsoleViewSeeded(sv.theme, sv.client, sv.nc, seedLines, seedNext, seedDone)
			child.build = sv.build
			child.store = sv.store
			return sv, func() tea.Msg { return SwapViewMsg{View: child} }
		case "d":
			build := sv.build
			nc := sv.nc
			return sv, func() tea.Msg {
				return SwapViewMsg{View: NewDescribeView(sv.theme, sv.client, sv.store, nc, build)}
			}
		case "t":
			return sv, fetchJobParams(sv.client, sv.nc)
		case "T":
			if sv.testResult != nil && len(sv.testResult.Suites) > 0 {
				nc := sv.nc
				build := sv.build
				child := NewTestReportView(sv.theme, *sv.testResult, nc, build, sv.client, sv.store)
				return sv, func() tea.Msg { return SwapViewMsg{View: child} }
			}
		case "A":
			if len(sv.artifacts) == 1 {
				return sv, openURLCmd(sv.artifacts[0].URL)
			} else if len(sv.artifacts) > 1 {
				nc := sv.nc
				build := sv.build
				child := NewArtifactView(sv.theme, sv.artifacts, nc, build, sv.client, sv.store)
				return sv, func() tea.Msg { return SwapViewMsg{View: child} }
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

// isRunning is the single authority for "render the running bar / keep
// polling because the build is in progress." The build API status is the
// authoritative signal — if Jenkins says terminal, we trust it, even if
// flowGraph HTML still shows a stage as "in progress" during the
// finalisation window. anyStageRunning is consulted only as a fallback for
// the initial-load races where the build status hasn't been fetched yet.
func (sv *StageView) isRunning() bool {
	if sv.build.Status == jenkins.BuildStatusRunning {
		return true
	}
	if sv.build.Status == "" || sv.build.Status == jenkins.BuildStatusUnknown {
		return sv.anyStageRunning()
	}
	return false
}

// isTerminalStatus reports whether a build status represents a completed
// (non-running) state. Empty and "unknown" are excluded: we only retry
// duration fetches for builds that Jenkins has explicitly marked done.
func isTerminalStatus(s jenkins.BuildStatus) bool {
	switch s {
	case jenkins.BuildStatusSuccess,
		jenkins.BuildStatusFailed,
		jenkins.BuildStatusAborted,
		jenkins.BuildStatusUnstable,
		jenkins.BuildStatusSkipped,
		jenkins.BuildStatusNotBuilt:
		return true
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
	// No running stage — prefer the last failed stage.
	lastFailed := -1
	for i, s := range sv.stages {
		if s.Status == jenkins.BuildStatusFailed {
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
	dimStyle := sv.theme.Stage.GhostDim

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

		// For running stages, use our own wall-clock timing instead of
		// Jenkins' s.Duration. Jenkins reports Duration in coarse jumps
		// (~minute granularity) for long stages, so reading it on every
		// render would make the bar reset and leap forward. We lock in a
		// start time on first observation and interpolate from there.
		durationCell := formatDuration(s.Duration)
		if s.Status == jenkins.BuildStatusRunning {
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

	// Mark skipped stages as non-selectable.
	for i, s := range sv.stages {
		if s.Status == jenkins.BuildStatusSkipped {
			if disabledIndices == nil {
				disabledIndices = make(map[int]bool)
			}
			disabledIndices[1+i] = true // +1 for the synthetic Pipeline row
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
	if sv.isRunning() {
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
	} else if sv.build.Status != jenkins.BuildStatusUnknown && sv.build.Status != "" {
		bar := sv.renderFinishedBar()
		content = bar + "\n" + sv.table.View()
	} else {
		content = sv.table.View()
	}

	return content
}

func (sv *StageView) PopupView() string {
	if sv.paramForm != nil {
		return sv.paramForm.View()
	}
	if sv.confirmTrigger {
		return renderConfirmBox(sv.theme,
			"Trigger Build",
			fmt.Sprintf("Start a new build of %s?", decodeName(sv.nc.ProjectName)),
			sv.triggerYes,
		)
	}
	if sv.confirmCancel {
		return renderConfirmBox(sv.theme,
			"Cancel Build",
			fmt.Sprintf("Stop build #%d?", sv.build.Number),
			sv.confirmYes,
		)
	}
	return ""
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
	// contextParts already includes Build.Number when > 0.
	// For pending builds, nc.Build.Number == 0, so we append "Pending" manually.
	ctx := contextParts(sv.nc)
	if sv.pending {
		ctx = append(ctx, component.BreadcrumbPart{Text: "Pending", IsBuildNum: true, Separator: " "})
	}
	return BreadcrumbSegment{ViewType: "stages", Context: ctx}
}

func (sv *StageView) ItemCount() int {
	return 0 // stages don't show a count badge in the breadcrumb
}

func (sv *StageView) Commands() []command.Command {
	return nil
}

func (sv *StageView) Shortcuts() []component.Shortcut {
	// enter and esc first for stable grid positioning
	var sc []component.Shortcut
	realIdx := sv.realStageIdx(sv.table.Cursor())
	if realIdx == -1 {
		sc = append(sc, component.Shortcut{Key: "enter", Action: "full log"})
	} else if realIdx >= 0 && realIdx < len(sv.stages) && len(sv.stages[realIdx].NodeIDs) > 0 {
		sc = append(sc, component.Shortcut{Key: "enter", Action: "stage log"})
	}
	sc = append(sc,
		component.Shortcut{Key: "esc", Action: "builds"},
		component.Shortcut{Key: "/", Action: "search"},
		component.Shortcut{Key: "l", Action: "full log"},
		component.Shortcut{Key: "d", Action: "describe"},
	)
	if sv.testResult != nil {
		badge := renderTestBadge(sv.theme, sv.testResult)
		sc = append(sc, component.Shortcut{Key: "T", Action: "tests: " + badge})
	}
	if len(sv.artifacts) > 0 {
		sc = append(sc, component.Shortcut{Key: "A", Action: artifactShortcutAction(sv.artifacts)})
	}
	sc = append(sc, component.Shortcut{Key: "t", Action: "trigger"})
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
	return sv.confirmCancel || sv.confirmTrigger || sv.paramForm != nil
}

func (sv *StageView) NC() NavigationContext { return sv.nc }

func (sv *StageView) ScrollInfo() ScrollInfo {
	return ScrollInfo{Offset: sv.table.ScrollOffset(), TotalLines: sv.table.TotalRows(), ViewHeight: sv.table.ContentHeight()}
}

func (sv *StageView) Close() error {
	if sv.cancel != nil {
		sv.cancel()
	}
	return nil
}

func (sv *StageView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	nc := sv.nc.AtBranch(sv.nc.BranchName)
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

package view

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// pollingProviderBase encapsulates the shared context/cancel lifecycle and
// ticker scheduling used by all build data providers.
type pollingProviderBase struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newPollingProviderBase() pollingProviderBase {
	ctx, cancel := context.WithCancel(context.Background())
	return pollingProviderBase{ctx: ctx, cancel: cancel}
}

// Close cancels the provider's context, stopping all in-flight polls.
func (b *pollingProviderBase) Close() {
	if b.cancel != nil {
		b.cancel()
	}
}

// scheduleContextTick returns a Cmd that fires makeMsg after interval,
// unless the context is cancelled first.
func (b *pollingProviderBase) scheduleContextTick(interval time.Duration, makeMsg func() tea.Msg) tea.Cmd {
	ctx := b.ctx
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
		if ctx.Err() != nil {
			return nil
		}
		return makeMsg()
	}
}

// Column widths shared across build-related views.
const (
	colRefWidth         = 40 // initial width; overridden by SetSize (always flexible)
	colStatusBWidth     = 14
	colDurationWidth    = 12
	colStartedWidth     = 15
	colTestsWidth       = 14
	colTriggeredByWidth = 60
)

// UnifiedBuild is a normalized build record used by BuildsView.
// It embeds jmodel.Build so Number, Status, Duration, Timestamp, TriggeredBy,
// and Cause are all promoted to the top level — no per-provider mapping needed.
type UnifiedBuild struct {
	jmodel.Build
	JobPath     string             // full API path (for cancel/trigger/log/stages)
	BranchName  string             // empty for single-branch views
	DisplayName string             // human-readable project name (for REF column at root/folder level)
	TestResult  *jmodel.TestReport // nil until fetched or unavailable
	Artifacts   []jmodel.Artifact  // nil = not yet fetched; empty slice = confirmed none

	// Queue fields — set when this row is a waiting build-queue item rather than
	// a real build. Queued rows carry Number==0 and Status==BuildStatusQueued.
	Queued     bool
	QueueID    int64
	QueueState string // "buildable" | "blocked" | "pending" | "stuck"
	Why        string // human-readable waiting reason
}

// queueSubState collapses a queue item's flags into a single sub-state badge
// key, by priority (stuck > blocked > pending > buildable).
func queueSubState(it jmodel.QueueItem) string {
	switch {
	case it.Stuck:
		return "stuck"
	case it.Blocked:
		return "blocked"
	case it.Pending:
		return "pending"
	default:
		return "buildable"
	}
}

// queuedUnifiedBuilds returns the scoped build-queue items as UnifiedBuild rows,
// sorted oldest-queued first (closest to running). Providers prepend these
// above their real builds so BuildsView renders the queued section on top.
func queuedUnifiedBuilds(store *cache.Store, filter buildregistry.Filter) []UnifiedBuild {
	if store == nil || store.Queue == nil {
		return nil
	}
	items := store.Queue.Query(filter, jmodel.QueueKindBuild)
	if len(items) == 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].InQueueSince.Before(items[j].InQueueSince)
	})
	out := make([]UnifiedBuild, 0, len(items))
	for _, it := range items {
		// Merge into one active row per job: if the job already has a running
		// build, that running row represents this execution — don't also show a
		// separate queued line for it (this is the queued→running transition).
		if store.Registry != nil && store.Registry.HasRunning(buildregistry.Filter{JobPath: it.JobPath}) {
			continue
		}
		jobName, branchName := extractJobAndBranch(it.JobPath)
		out = append(out, UnifiedBuild{
			Build: jmodel.Build{
				Number:          0,
				Status:          jmodel.BuildStatusQueued,
				Timestamp:       it.InQueueSince,
				Params:          it.Params,
				Cause:           it.Cause,
				TriggeredBy:     it.TriggeredBy,
				TriggeredByName: it.TriggeredByName,
			},
			JobPath:     it.JobPath,
			BranchName:  branchName,
			DisplayName: jobName,
			Queued:      true,
			QueueID:     it.ID,
			QueueState:  queueSubState(it),
			Why:         it.Why,
		})
	}
	return out
}

// BuildsViewConfig declares which shortcuts are active for a provider.
type BuildsViewConfig struct {
	CanTrigger bool // enables t shortcut
}

// BuildDataProvider supplies builds and config to BuildsView.
type BuildDataProvider interface {
	Init() tea.Cmd
	Refresh() tea.Cmd
	Builds() []UnifiedBuild
	HandleMsg(msg tea.Msg) (bool, []tea.Cmd)
	Close()
	Config() BuildsViewConfig
}

// testTracker caches per-build test reports keyed by "jobPath:buildNum".
// Embed it in providers that fetch test reports for their builds.
type testTracker struct {
	tests map[string]*jmodel.TestReport
}

func testKey(jobPath string, buildNum int) string {
	return fmt.Sprintf("%s:%d", jobPath, buildNum)
}

func (tt *testTracker) get(jobPath string, buildNum int) *jmodel.TestReport {
	if tt.tests == nil {
		return nil
	}
	return tt.tests[testKey(jobPath, buildNum)]
}

func (tt *testTracker) set(jobPath string, buildNum int, r *jmodel.TestReport) {
	if tt.tests == nil {
		tt.tests = map[string]*jmodel.TestReport{}
	}
	tt.tests[testKey(jobPath, buildNum)] = r
}

// preloadOne loads a single test report from cache if present.
func (tt *testTracker) preloadOne(store *cache.Store, jobPath string, buildNum int) {
	key := testKey(jobPath, buildNum)
	if entry := store.TestReports.Get(key); entry != nil {
		tt.set(jobPath, buildNum, entry.Value)
	}
}

// handleMsg handles a TestReportMsg, storing the report. Returns true if handled.
func (tt *testTracker) handleMsg(msg TestReportMsg) bool {
	if msg.Err != nil {
		return false
	}
	tt.set(msg.JobPath, msg.BuildNum, msg.Report)
	return true
}

// artifactTracker caches per-build artifact lists keyed by "jobPath:buildNum".
type artifactTracker struct {
	arts map[string][]jmodel.Artifact
}

func (at *artifactTracker) get(jobPath string, buildNum int) []jmodel.Artifact {
	if at.arts == nil {
		return nil
	}
	v, ok := at.arts[testKey(jobPath, buildNum)]
	if !ok {
		return nil
	}
	return v
}

func (at *artifactTracker) set(jobPath string, buildNum int, a []jmodel.Artifact) {
	if at.arts == nil {
		at.arts = map[string][]jmodel.Artifact{}
	}
	at.arts[testKey(jobPath, buildNum)] = a
}

func (at *artifactTracker) preloadOne(store *cache.Store, jobPath string, buildNum int) {
	key := testKey(jobPath, buildNum)
	if entry := store.Artifacts.Get(key); entry != nil {
		at.set(jobPath, buildNum, entry.Value)
	}
}

func (at *artifactTracker) handleMsg(msg ArtifactsMsg) bool {
	if msg.Err != nil {
		return false
	}
	at.set(msg.JobPath, msg.BuildNum, msg.Artifacts)
	return true
}

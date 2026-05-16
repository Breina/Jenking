package view

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
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
// It embeds jenkins.Build so Number, Status, Duration, Timestamp, TriggeredBy,
// and Cause are all promoted to the top level — no per-provider mapping needed.
type UnifiedBuild struct {
	jenkins.Build
	JobPath     string              // full API path (for cancel/trigger/log/stages)
	BranchName  string              // empty for single-branch views
	DisplayName string              // human-readable project name (for REF column at root/folder level)
	TestResult  *jenkins.TestReport // nil until fetched or unavailable
	Artifacts   []jenkins.Artifact  // nil = not yet fetched; empty slice = confirmed none
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
	tests map[string]*jenkins.TestReport
}

func testKey(jobPath string, buildNum int) string {
	return fmt.Sprintf("%s:%d", jobPath, buildNum)
}

func (tt *testTracker) get(jobPath string, buildNum int) *jenkins.TestReport {
	if tt.tests == nil {
		return nil
	}
	return tt.tests[testKey(jobPath, buildNum)]
}

func (tt *testTracker) set(jobPath string, buildNum int, r *jenkins.TestReport) {
	if tt.tests == nil {
		tt.tests = map[string]*jenkins.TestReport{}
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
	arts map[string][]jenkins.Artifact
}

func (at *artifactTracker) get(jobPath string, buildNum int) []jenkins.Artifact {
	if at.arts == nil {
		return nil
	}
	v, ok := at.arts[testKey(jobPath, buildNum)]
	if !ok {
		return nil
	}
	return v
}

func (at *artifactTracker) set(jobPath string, buildNum int, a []jenkins.Artifact) {
	if at.arts == nil {
		at.arts = map[string][]jenkins.Artifact{}
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

package monitor

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
	"github.com/Breina/Jenking/internal/navmsg"
)

// fakeClient is a minimal jmodel.JenkinsClient stub for monitor tests.
// Only the methods the completion cascade uses (GetBuild, ListStages,
// GetTestReport) have meaningful behavior; the rest return zero values.
type fakeClient struct {
	getBuild      func(ctx context.Context, jobPath string, n int) (*jmodel.BuildDetail, error)
	listStages    func(ctx context.Context, jobPath string, n int) ([]jmodel.Stage, error)
	getTestReport func(ctx context.Context, jobPath string, n int) (*jmodel.TestReport, error)
	listQueue     func(ctx context.Context) ([]jmodel.QueueItem, error)

	getBuildCalls      int
	listStagesCalls    int
	getTestReportCalls int
}

func (f *fakeClient) ListJobs(_ context.Context, _ string) ([]jmodel.Job, error) { return nil, nil }
func (f *fakeClient) ListBuilds(_ context.Context, _ string) ([]jmodel.Build, error) {
	return nil, nil
}
func (f *fakeClient) ListProjectBuilds(_ context.Context, _ string) ([]jmodel.ProjectBuild, error) {
	return nil, nil
}
func (f *fakeClient) ListUserBuilds(_ context.Context, _ string) ([]jmodel.UserBuild, error) {
	return nil, nil
}
func (f *fakeClient) ListRunningBuilds(_ context.Context) ([]jmodel.UserBuild, error) {
	return nil, nil
}
func (f *fakeClient) ScanAllBuilds(_ context.Context, _ int) ([]jmodel.UserBuild, error) {
	return nil, nil
}
func (f *fakeClient) ListStages(ctx context.Context, jobPath string, n int) ([]jmodel.Stage, error) {
	f.listStagesCalls++
	if f.listStages != nil {
		return f.listStages(ctx, jobPath, n)
	}
	return nil, nil
}
func (f *fakeClient) GetBuild(ctx context.Context, jobPath string, n int) (*jmodel.BuildDetail, error) {
	f.getBuildCalls++
	if f.getBuild != nil {
		return f.getBuild(ctx, jobPath, n)
	}
	return &jmodel.BuildDetail{}, nil
}
func (f *fakeClient) GetConsoleOutput(_ context.Context, _ string, _ int) (io.ReadCloser, error) {
	return nil, nil
}
func (f *fakeClient) GetFullConsoleText(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (f *fakeClient) GetProgressiveLog(_ context.Context, _ string, _, _ int) (*jmodel.ProgressiveLog, error) {
	return nil, nil
}
func (f *fakeClient) GetNodeLog(_ context.Context, _ string, _, _ int) (string, error) {
	return "", nil
}
func (f *fakeClient) GetNodeLogProgressive(_ context.Context, _ string, _, _, _ int) (*jmodel.NodeLog, error) {
	return nil, nil
}
func (f *fakeClient) GetJobParameters(_ context.Context, _ string) ([]jmodel.ParameterDefinition, error) {
	return nil, nil
}
func (f *fakeClient) GetJobMetadata(_ context.Context, _ string, _ int) (jmodel.MetaNode, error) {
	return jmodel.MetaNode{}, nil
}
func (f *fakeClient) GetBuildMetadata(_ context.Context, _ string, _, _ int) (jmodel.MetaNode, error) {
	return jmodel.MetaNode{}, nil
}
func (f *fakeClient) GetJobSCMURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeClient) GetBuildScript(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (f *fakeClient) FetchPipelineSyntax(_ context.Context, _ string, _ int) (*pipelinesyntax.Symbols, error) {
	return nil, nil
}
func (f *fakeClient) ValidateJenkinsfile(_ context.Context, _ string) (jmodel.ValidationResult, error) {
	return jmodel.ValidationResult{}, nil
}
func (f *fakeClient) GetBuildParameters(_ context.Context, _ string, _ int) (map[string]string, error) {
	return nil, nil
}
func (f *fakeClient) GetTestReport(ctx context.Context, jobPath string, n int) (*jmodel.TestReport, error) {
	f.getTestReportCalls++
	if f.getTestReport != nil {
		return f.getTestReport(ctx, jobPath, n)
	}
	return nil, nil
}
func (f *fakeClient) GetArtifacts(_ context.Context, _ string, _ int) ([]jmodel.Artifact, error) {
	return nil, nil
}
func (f *fakeClient) GetArtifactContent(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}
func (f *fakeClient) TriggerBuild(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (f *fakeClient) ReplayBuild(_ context.Context, _ string, _ int, _ string) error { return nil }
func (f *fakeClient) CancelBuild(_ context.Context, _ string, _ int) error           { return nil }
func (f *fakeClient) CancelQueueItem(_ context.Context, _ int64) error               { return nil }
func (f *fakeClient) ListQueue(_ context.Context) ([]jmodel.QueueItem, error) {
	if f.listQueue != nil {
		return f.listQueue(context.Background())
	}
	return nil, nil
}
func (f *fakeClient) ProceedInput(_ context.Context, _ string, _ int, _ string, _ map[string]string) error {
	return nil
}
func (f *fakeClient) AbortInput(_ context.Context, _ string, _ int, _ string) error { return nil }
func (f *fakeClient) WhoAmI(_ context.Context) (*jmodel.User, error)                { return nil, nil }

func TestDiffSnapshotComputesArrivalsAndDepartures(t *testing.T) {
	prev := map[string]jmodel.UserBuild{
		"folder/a#1": {JobPath: "folder/a", Build: jmodel.Build{Number: 1}},
		"folder/b#2": {JobPath: "folder/b", Build: jmodel.Build{Number: 2}},
	}
	now := []jmodel.UserBuild{
		{JobPath: "folder/a", Build: jmodel.Build{Number: 1}}, // unchanged
		{JobPath: "folder/c", Build: jmodel.Build{Number: 3}}, // arrived
	}

	newLive, arrived, departed := diffSnapshot(prev, now)

	if len(newLive) != 2 {
		t.Fatalf("newLive size = %d, want 2", len(newLive))
	}
	if len(arrived) != 1 || arrived[0].key != jmodel.BuildKey("folder/c", 3) {
		t.Fatalf("arrived = %+v, want single folder/c#3", arrived)
	}
	if len(departed) != 1 || departed[0].key != jmodel.BuildKey("folder/b", 2) {
		t.Fatalf("departed = %+v, want single folder/b#2", departed)
	}
}

func TestDiffSnapshotEmptyPrevYieldsOnlyArrivals(t *testing.T) {
	now := []jmodel.UserBuild{{JobPath: "p", Build: jmodel.Build{Number: 1}}}
	_, arrived, departed := diffSnapshot(nil, now)
	if len(arrived) != 1 {
		t.Fatalf("arrived = %d, want 1", len(arrived))
	}
	if len(departed) != 0 {
		t.Fatalf("departed = %d, want 0", len(departed))
	}
}

func TestCompletionCmdsEmptyDepartedReturnsNil(t *testing.T) {
	m := NewRunningBuildsMonitor(&fakeClient{}, nil)
	if got := m.completionCmds(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCompletionCmdsNilStoreEmitsOnlyDetailFetch(t *testing.T) {
	fc := &fakeClient{}
	m := NewRunningBuildsMonitor(fc, nil)
	departed := []keyedBuild{
		{key: jmodel.BuildKey("j", 4), build: jmodel.UserBuild{JobPath: "j", Build: jmodel.Build{Number: 4}}},
		{key: jmodel.BuildKey("k", 5), build: jmodel.UserBuild{JobPath: "k", Build: jmodel.Build{Number: 5}}},
	}
	cmds := m.completionCmds(departed)
	if len(cmds) != 2 {
		t.Fatalf("len(cmds) = %d, want 2 (one detail-fetch per departed build, no store)", len(cmds))
	}
	// Execute and verify each yields BuildCompletedMsg.
	for i, c := range cmds {
		msg := c()
		bcm, ok := msg.(navmsg.BuildCompletedMsg)
		if !ok {
			t.Fatalf("cmd[%d] returned %T, want BuildCompletedMsg", i, msg)
		}
		if bcm.JobPath != departed[i].build.JobPath || bcm.Number != departed[i].build.Number {
			t.Fatalf("cmd[%d] msg = %+v, mismatched build identity", i, bcm)
		}
		if bcm.Err != nil {
			t.Fatalf("cmd[%d] msg.Err = %v, want nil", i, bcm.Err)
		}
	}
	if fc.getBuildCalls != 2 {
		t.Fatalf("GetBuild calls = %d, want 2", fc.getBuildCalls)
	}
	if fc.listStagesCalls != 0 || fc.getTestReportCalls != 0 {
		t.Fatalf("expected no stage/test calls when store is nil; got stages=%d tests=%d",
			fc.listStagesCalls, fc.getTestReportCalls)
	}
}

func TestCompletionCmdsPropagatesGetBuildError(t *testing.T) {
	wantErr := errors.New("boom")
	fc := &fakeClient{
		getBuild: func(_ context.Context, _ string, _ int) (*jmodel.BuildDetail, error) {
			return nil, wantErr
		},
	}
	m := NewRunningBuildsMonitor(fc, nil)
	departed := []keyedBuild{
		{key: jmodel.BuildKey("j", 7), build: jmodel.UserBuild{JobPath: "j", Build: jmodel.Build{Number: 7}}},
	}
	cmds := m.completionCmds(departed)
	if len(cmds) != 1 {
		t.Fatalf("len(cmds) = %d, want 1", len(cmds))
	}
	bcm, ok := cmds[0]().(navmsg.BuildCompletedMsg)
	if !ok {
		t.Fatalf("expected BuildCompletedMsg")
	}
	if !errors.Is(bcm.Err, wantErr) {
		t.Fatalf("Err = %v, want %v", bcm.Err, wantErr)
	}
	if bcm.JobPath != "j" || bcm.Number != 7 {
		t.Fatalf("identity lost on error path: %+v", bcm)
	}
}

// A failed poll must surface a ConnectionLostMsg (so the app can mark itself
// disconnected) and keep polling, rather than silently rescheduling.
func TestHandleMsgPollErrorEmitsConnectionLost(t *testing.T) {
	wantErr := errors.New("executing request: connection refused")
	m := NewRunningBuildsMonitor(&fakeClient{}, nil)

	handled, cmds := m.HandleMsg(monitorPollMsg{err: wantErr})
	if !handled {
		t.Fatal("expected poll error to be handled")
	}
	if len(cmds) != 2 {
		t.Fatalf("len(cmds) = %d, want 2 (connection-lost + reschedule)", len(cmds))
	}
	clm, ok := cmds[0]().(navmsg.ConnectionLostMsg)
	if !ok {
		t.Fatalf("expected first cmd to emit ConnectionLostMsg, got %T", cmds[0]())
	}
	if !errors.Is(clm.Err, wantErr) {
		t.Fatalf("Err = %v, want %v", clm.Err, wantErr)
	}
	// Second cmd is the reschedule tick; it must keep the poll loop alive.
	if _, ok := cmds[1]().(monitorTickMsg); !ok {
		t.Fatalf("expected second cmd to emit monitorTickMsg, got %T", cmds[1]())
	}
}

func TestHandleMsgReplacesQueueSnapshot(t *testing.T) {
	store := cache.NewStore(nil)
	m := NewRunningBuildsMonitor(&fakeClient{}, store)

	queue := []jmodel.QueueItem{
		{ID: 1, JobPath: "api/main", Buildable: true},
		{ID: 2, JobPath: "web/PR-1", Blocked: true},
	}
	handled, cmds := m.HandleMsg(monitorPollMsg{builds: nil, queue: queue})
	if !handled {
		t.Fatal("expected poll to be handled")
	}
	if got := store.Queue.CountVisible(nil); got != 2 {
		t.Fatalf("queue count = %d, want 2", got)
	}
	// The emitted RunningBuildsUpdatedMsg should carry the queued count.
	if len(cmds) == 0 {
		t.Fatal("expected at least one cmd")
	}
	upd, ok := cmds[0]().(navmsg.RunningBuildsUpdatedMsg)
	if !ok {
		t.Fatalf("expected first cmd to emit RunningBuildsUpdatedMsg, got %T", cmds[0]())
	}
	if upd.QueuedCount != 2 {
		t.Fatalf("QueuedCount = %d, want 2", upd.QueuedCount)
	}

	// A queued item whose job is already running is excluded from the header
	// counter (it is hidden in the list, folded into the running build).
	running := []jmodel.UserBuild{{JobPath: "api/main", Build: jmodel.Build{Number: 5, Status: jmodel.BuildStatusRunning}}}
	_, cmds = m.HandleMsg(monitorPollMsg{builds: running, queue: queue})
	upd, ok = cmds[0]().(navmsg.RunningBuildsUpdatedMsg)
	if !ok {
		t.Fatalf("expected RunningBuildsUpdatedMsg, got %T", cmds[0]())
	}
	if upd.QueuedCount != 1 {
		t.Fatalf("QueuedCount with api/main running = %d, want 1 (api/main excluded)", upd.QueuedCount)
	}

	// A queue-only error keeps the previous snapshot.
	m.HandleMsg(monitorPollMsg{builds: nil, queue: nil, queueErr: errors.New("boom")})
	if got := store.Queue.CountVisible(nil); got != 2 {
		t.Fatalf("queue count after queue error = %d, want 2 (unchanged)", got)
	}
}

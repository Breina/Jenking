package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// fakeClient satisfies jmodel.JenkinsClient by embedding the interface (so any
// method the tested tools don't touch panics loudly if called). Only the
// methods exercised here are overridden.
type fakeClient struct {
	jmodel.JenkinsClient
}

func (fakeClient) WhoAmI(context.Context) (*jmodel.User, error) {
	return &jmodel.User{ID: "jane", FullName: "Jane Doe", JenkinsVersion: "2.500"}, nil
}

// ListJobs doubles as the existence probe behind usecase.CanonicalJobPath, so
// the fake answers for any path: every job path a test passes is already
// canonical and must survive normalization untouched.
func (fakeClient) ListJobs(context.Context, string) ([]jmodel.Job, error) {
	return nil, nil
}

func (fakeClient) ListNodes(context.Context) ([]jmodel.Node, error) {
	return []jmodel.Node{{Name: "built-in", NumExecutors: 2, Labels: []string{"linux", "built-in"}}}, nil
}

func (fakeClient) ListRunningBuilds(context.Context) ([]jmodel.UserBuild, error) {
	return []jmodel.UserBuild{{JobPath: "team/app", Build: jmodel.Build{Number: 42, Status: jmodel.BuildStatusRunning}}}, nil
}

func (fakeClient) ListBuilds(context.Context, string) ([]jmodel.Build, error) {
	return []jmodel.Build{{Number: 42, Status: jmodel.BuildStatusSuccess}}, nil
}

func (fakeClient) FindCommit(_ context.Context, _, prefix string, _ int) ([]jmodel.BuildCommitHit, error) {
	return []jmodel.BuildCommitHit{{BuildNumber: 42, CommitID: prefix + "abc"}}, nil
}

func (fakeClient) GetProgressiveLog(_ context.Context, _ string, _, start int) (*jmodel.ProgressiveLog, error) {
	if start == 0 {
		return &jmodel.ProgressiveLog{Text: "console output\n", MoreData: false, NextStart: 15}, nil
	}
	return &jmodel.ProgressiveLog{MoreData: false, NextStart: start}, nil
}

func (fakeClient) TriggerBuild(context.Context, string, map[string]string) (int64, error) {
	return 1234, nil
}

func (fakeClient) CancelBuild(context.Context, string, int) error { return nil }

// connectTest wires an in-memory client to a server built over deps.
func connectTest(t *testing.T, deps usecase.Deps) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	server := NewServer(deps, "test", false)

	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := server.srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return cs, func() { _ = cs.Close() }
}

func TestListTools(t *testing.T) {
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	want := []string{
		"whoami", "list_jobs", "list_views", "list_builds", "get_build", "get_stages",
		"get_test_report", "list_artifacts", "get_params", "get_metadata",
		"list_queue", "list_scans", "list_running", "resolve_job", "get_queue_history", "list_nodes", "list_inputs", "get_changes", "find_commit",
		"get_logs", "get_artifact", "get_scan_log", "describe_pipeline", "get_pipeline_symbols", "lint_pipeline", "wait_for_build", "wait_for_new_build", "wait_for_log_match",
		"trigger_build", "replay_build", "cancel_build", "dequeue",
		"approve_input", "reject_input", "enable_job", "disable_job",
		"rescan", "set_node_offline", "set_node_online",
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil input schema", tool.Name)
		}
	}
	if len(res.Tools) != len(want) {
		t.Errorf("tool count: got %d, want %d", len(res.Tools), len(want))
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestReadOnlyOmitsMutatingTools(t *testing.T) {
	ctx := context.Background()
	server := NewServer(usecase.Deps{Client: fakeClient{}}, "test", true)
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := server.srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint {
			continue
		}
		t.Errorf("read-only server exposed mutating tool %q", tool.Name)
	}
	// A representative mutating tool must be entirely absent.
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "cancel_build",
		Arguments: map[string]any{"job_path": "app", "build_number": 1},
	}); err == nil {
		t.Error("expected error calling absent cancel_build under --read-only")
	}
}

func TestCallTool_TriggerBuild(t *testing.T) {
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "trigger_build",
		Arguments: map[string]any{"job_path": "team/app"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("trigger_build error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["queue_id"].(float64) != 1234 {
		t.Errorf("queue_id = %v, want 1234", sc["queue_id"])
	}
}

func TestCallTool_CancelRequiresBuildNumber(t *testing.T) {
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	// build_number omitted → must be a tool error, never a latest-default cancel.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "cancel_build",
		Arguments: map[string]any{"job_path": "team/app"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError when build_number is omitted on cancel_build")
	}
}

func TestCallTool_Whoami(t *testing.T) {
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("whoami returned error result: %+v", res.Content)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", res.StructuredContent)
	}
	if sc["id"] != "jane" {
		t.Errorf("whoami id: got %v, want jane", sc["id"])
	}
}

func TestCallTool_FindCommit(t *testing.T) {
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "find_commit",
		Arguments: map[string]any{"job_path": "team/app", "commit": "dead"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("find_commit returned error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["searched_builds"].(float64) != 25 {
		t.Errorf("searched_builds default: got %v, want 25", sc["searched_builds"])
	}
	hits, ok := sc["hits"].([]any)
	if !ok || len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %v", sc["hits"])
	}
}

func TestCallTool_GetLogs(t *testing.T) {
	disk, err := cache.NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}, Store: cache.NewStore(disk)})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_logs",
		Arguments: map[string]any{"job_path": "team/app", "build_number": 42, "max_bytes": 1024},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_logs error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["complete"] != true {
		t.Errorf("expected complete=true, got %v", sc["complete"])
	}
	if sc["window"] != "console output\n" {
		t.Errorf("window = %v", sc["window"])
	}
	path, _ := sc["path"].(string)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file %q: %v", path, err)
	}
	if string(got) != "console output\n" {
		t.Errorf("file content = %q", got)
	}
}

func TestCallTool_ListRunning(t *testing.T) {
	// No Store on Deps: ListRunning falls back to a live client fetch.
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_running"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_running error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", sc["count"])
	}
	if len(sc["builds"].([]any)) != 1 {
		t.Errorf("builds = %v", sc["builds"])
	}
}

func TestCallTool_QueueHistory(t *testing.T) {
	disk, err := cache.NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	var wr [11][4]int
	wr[4][1] = 2 // 1-2m bin, blocked reason
	if err := disk.SaveDashSamples([]cache.DashSample{
		{T: time.Now().Add(-10 * time.Minute), Running: 3, Queued: 2, WaitReason: wr},
	}); err != nil {
		t.Fatalf("SaveDashSamples: %v", err)
	}
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}, Store: cache.NewStore(disk)})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_queue_history",
		Arguments: map[string]any{"window_minutes": 120},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_queue_history error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["samples"].(float64) != 1 {
		t.Errorf("samples = %v, want 1", sc["samples"])
	}
	if sc["max_running"].(float64) != 3 {
		t.Errorf("max_running = %v, want 3", sc["max_running"])
	}
	bins, ok := sc["bins"].([]any)
	if !ok || len(bins) != 1 {
		t.Fatalf("bins = %v, want 1", sc["bins"])
	}
	if bins[0].(map[string]any)["label"] != "1-2m" {
		t.Errorf("bin label = %v, want 1-2m", bins[0])
	}
}

func TestCallTool_MissingRequiredArg(t *testing.T) {
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	// job_path is required; omitting it must yield an error result, not a panic.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "find_commit"})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError for missing required arg")
	}
}

// TestCallTool_WaitForLogMatch follows a finished build's console log: the
// pattern is already in it, so the call returns without ever polling again.
func TestCallTool_WaitForLogMatch(t *testing.T) {
	disk, err := cache.NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}, Store: cache.NewStore(disk)})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "wait_for_log_match",
		Arguments: map[string]any{"job_path": "team/app", "build_number": 42, "pattern": "(?i)CONSOLE (\\w+)"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("wait_for_log_match error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["matched"] != true {
		t.Fatalf("expected matched=true, got %+v", sc)
	}
	if sc["match"] != "console output" {
		t.Errorf("match = %v", sc["match"])
	}
	if sc["line"] != "console output" {
		t.Errorf("line = %v", sc["line"])
	}
	if sc["complete"] != true {
		t.Errorf("expected complete=true for a finished log, got %v", sc["complete"])
	}
	if path, _ := sc["path"].(string); path == "" {
		t.Error("expected the followed log to be written to a file")
	}
}

// TestCallTool_WaitForLogMatch_BadPattern rejects an uncompilable regexp
// instead of blocking on a follow that can never match.
func TestCallTool_WaitForLogMatch_BadPattern(t *testing.T) {
	cs, done := connectTest(t, usecase.Deps{Client: fakeClient{}})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "wait_for_log_match",
		Arguments: map[string]any{"job_path": "team/app", "pattern": "([unclosed"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for an invalid pattern")
	}
}

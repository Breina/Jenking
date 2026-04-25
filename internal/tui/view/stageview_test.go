package view

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

func makeStageView(build jenkins.Build, stages []jenkins.Stage) *StageView {
	t := theme.Default()
	nc := NavigationContext{Level: CtxBranch, ProjectName: "job", BranchName: "test", Build: NavBuildRef{Number: build.Number}}
	sv := NewStageView(t, nil, nil, nc, build)
	sv.stages = stages
	sv.populateTable()
	return sv
}

// stageRefresh simulates a stageRefreshMsg Update call and returns the cursor.
func stageRefresh(sv *StageView, newStages []jenkins.Stage, buildStatus jenkins.BuildStatus) int {
	b := jenkins.Build{Status: buildStatus}
	_, _ = sv.Update(stageRefreshMsg{stages: newStages, build: &b})
	return sv.table.Cursor()
}

// Table cursor values are offset by 1 from stage indices because the synthetic
// Pipeline row occupies index 0.

// TestAutoFollow_SkipsFastStage reproduces the reported bug:
// "Validate" completes in 0 seconds, so the gap between Checkout SCM finishing
// and Build Docker starting spans only one refresh tick — both Checkout SCM and
// Validate are already Success on the first refresh after Checkout SCM finishes.
// The cursor must jump to Build Docker (Running) and not stay on Checkout SCM.
func TestAutoFollow_SkipsFastStage(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Declarative: Checkout SCM", Status: jenkins.BuildStatusRunning},
		{Name: "Validate", Status: jenkins.BuildStatusNotBuilt},
		{Name: "Build Docker", Status: jenkins.BuildStatusNotBuilt},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1) // cursor on Checkout SCM (running), table index 1 = stage index 0

	// Checkout SCM and Validate both finished in the same 2s refresh window.
	// Build Docker is now running.
	newStages := []jenkins.Stage{
		{Name: "Declarative: Checkout SCM", Status: jenkins.BuildStatusSuccess},
		{Name: "Validate", Status: jenkins.BuildStatusSuccess},
		{Name: "Build Docker", Status: jenkins.BuildStatusRunning},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusRunning)

	// Build Docker is stage index 2, table cursor = 3.
	if cursor != 3 {
		t.Errorf("expected cursor to jump to Build Docker (table index 3), got %d", cursor)
	}
}

// TestAutoFollow_InterStageGap verifies the cursor stays at the last finished
// stage when no running stage is visible yet (gap between two stages).
func TestAutoFollow_InterStageGap(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
		{Name: "Test", Status: jenkins.BuildStatusNotBuilt},
		{Name: "Deploy", Status: jenkins.BuildStatusNotBuilt},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1) // Build = table index 1

	// Build finished; Test hasn't started yet.
	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusNotBuilt},
		{Name: "Deploy", Status: jenkins.BuildStatusNotBuilt},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusRunning)

	// No running stage — initialCursor picks last non-skipped/not-built = Build (stage 0, table 1).
	if cursor != 1 {
		t.Errorf("expected cursor to stay at Build (table index 1) during inter-stage gap, got %d", cursor)
	}

	// Next tick: Test is now running.
	newStages2 := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusRunning},
		{Name: "Deploy", Status: jenkins.BuildStatusNotBuilt},
	}
	cursor2 := stageRefresh(sv, newStages2, jenkins.BuildStatusRunning)

	// Test = stage 1, table 2.
	if cursor2 != 2 {
		t.Errorf("expected cursor to advance to Test (table index 2), got %d", cursor2)
	}
}

// TestAutoFollow_MovesToLastWhenDone verifies that when the build finishes,
// the cursor lands on the last meaningful stage.
func TestAutoFollow_MovesToLastWhenDone(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
		{Name: "Test", Status: jenkins.BuildStatusNotBuilt},
		{Name: "Deploy", Status: jenkins.BuildStatusNotBuilt},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1)

	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusSuccess},
		{Name: "Deploy", Status: jenkins.BuildStatusSuccess},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusSuccess)

	// Deploy = stage 2, table 3.
	if cursor != 3 {
		t.Errorf("expected cursor at Deploy (table index 3), got %d", cursor)
	}
}

// TestAutoFollow_ManualNavigationDisables verifies that moving the cursor
// to a non-running stage stops auto-follow so the user's position is preserved.
func TestAutoFollow_ManualNavigationDisables(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
		{Name: "Test", Status: jenkins.BuildStatusNotBuilt},
		{Name: "Deploy", Status: jenkins.BuildStatusNotBuilt},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1) // Build = table 1

	// User manually moves to Deploy (table 3, stage 2, not running) — auto-follow stops.
	sv.table.SetCursor(3)
	sv.autoFollowing = false // mirrors what key handler does

	// Refresh: Test is now running.
	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusRunning},
		{Name: "Deploy", Status: jenkins.BuildStatusNotBuilt},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusRunning)

	// Should stay at 3 — user's manual position is respected.
	if cursor != 3 {
		t.Errorf("expected cursor to stay at user-selected Deploy (table index 3), got %d", cursor)
	}
}

// TestAutoFollow_ResumesWhenCursorOnAutoFollowTarget verifies that moving the
// cursor onto the exact stage initialCursor() would pick re-engages auto-follow.
func TestAutoFollow_ResumesWhenCursorOnAutoFollowTarget(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusRunning},
		{Name: "Deploy", Status: jenkins.BuildStatusNotBuilt},
	}
	sv := makeStageView(initialBuild, stages)

	// Simulate: user navigated away (autoFollowing already false), cursor on Build (table 1).
	sv.autoFollowing = false
	sv.table.SetCursor(1)

	// User moves cursor onto Test (table 2) — which is the stage initialCursor()
	// would pick (last running). Auto-follow resumes.
	sv.table.SetCursor(2)
	sv.autoFollowing = sv.table.Cursor() == sv.initialCursor()

	if !sv.autoFollowing {
		t.Fatal("expected autoFollowing to be re-enabled after cursor moved to auto-follow target")
	}

	// Refresh: Test finishes, Deploy starts.
	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusSuccess},
		{Name: "Deploy", Status: jenkins.BuildStatusRunning},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusRunning)

	// Deploy = stage 2, table 3.
	if cursor != 3 {
		t.Errorf("expected cursor to advance to Deploy (table index 3), got %d", cursor)
	}
}

// TestAutoFollow_RunningParentDoesNotReengage verifies that navigating to a
// running parent stage does NOT re-engage auto-follow (only the deepest running
// child — the initialCursor target — should).
func TestAutoFollow_RunningParentDoesNotReengage(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess, Depth: 0},
		{Name: "Deploy", Status: jenkins.BuildStatusRunning, Depth: 0},
		{Name: "Deploy East", Status: jenkins.BuildStatusRunning, Depth: 1},
	}
	sv := makeStageView(initialBuild, stages)

	// initialCursor() should pick Deploy East (last running, deepest child).
	target := sv.initialCursor()
	if target != 3 { // table cursor: Pipeline=0, Build=1, Deploy=2, Deploy East=3
		t.Fatalf("expected initialCursor = 3 (Deploy East), got %d", target)
	}

	// User navigates to Deploy (table 2, parent, also Running).
	sv.table.SetCursor(2)
	sv.autoFollowing = sv.table.Cursor() == sv.initialCursor() // 2 != 3 → false

	if sv.autoFollowing {
		t.Fatal("expected autoFollowing to be false when cursor is on running parent, not the auto-follow target")
	}

	// Refresh: auto-follow is off, cursor should stay on Deploy (table 2).
	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess, Depth: 0},
		{Name: "Deploy", Status: jenkins.BuildStatusRunning, Depth: 0},
		{Name: "Deploy East", Status: jenkins.BuildStatusRunning, Depth: 1},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusRunning)

	if cursor != 2 {
		t.Errorf("expected cursor to stay on Deploy (table 2), got %d", cursor)
	}
}

// TestAutoFollow_ParentChildRunning (B11) verifies that when a parent stage
// and its child are both Running, the cursor follows the deepest child.
func TestAutoFollow_ParentChildRunning(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess, Depth: 0},
		{Name: "Deploy", Status: jenkins.BuildStatusRunning, Depth: 0},
		{Name: "Deploy East", Status: jenkins.BuildStatusRunning, Depth: 1},
		{Name: "Deploy West", Status: jenkins.BuildStatusNotBuilt, Depth: 1},
	}
	sv := makeStageView(initialBuild, stages)

	cursor := sv.initialCursor()
	// Should select "Deploy East" (deepest running child) = stage 2, table 3.
	if cursor != 3 {
		t.Errorf("expected cursor on Deploy East (table index 3), got %d", cursor)
	}
}

// TestAutoFollow_StopsAtFailed (B13) verifies that when a build finishes
// with a failed stage followed by skipped stages, the cursor lands on the
// first failed stage.
func TestAutoFollow_StopsAtFailed(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1) // Build = table 1

	// Build fails at Test; Deploy is skipped due to earlier failure.
	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusFailed},
		{Name: "Deploy", Status: jenkins.BuildStatusSkipped},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusFailed)

	// Test = stage 1, table 2.
	if cursor != 2 {
		t.Errorf("expected cursor on Test (table index 2), got %d", cursor)
	}
}

// TestAutoFollow_LastSkippedSelectsLastMeaningful (B13 part 2) verifies that
// when the last stage is Skipped (but none failed), the cursor picks the
// last non-skipped stage.
func TestAutoFollow_LastSkippedSelectsLastMeaningful(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1) // Build = table 1

	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusSuccess},
		{Name: "Deploy", Status: jenkins.BuildStatusSkipped},
	}
	cursor := stageRefresh(sv, newStages, jenkins.BuildStatusSuccess)

	// Test = stage 1, table 2.
	if cursor != 2 {
		t.Errorf("expected cursor on Test (table index 2), got %d", cursor)
	}
}

// TestPreview_RefetchWhenNodeIDsAppear verifies that the preview panel
// re-triggers a fetch when a stage that initially had no NodeIDs
// later acquires them (e.g. Jenkins assigns flow nodes after a delay).
func TestPreview_RefetchWhenNodeIDsAppear(t *testing.T) {
	th := theme.Default()
	pp := NewPreviewPanel(th, nil, nil, nil, NavigationContext{Level: CtxBranch, ProjectName: "job", BranchName: "test", Build: NavBuildRef{Number: 1}})
	pp.SetSize(80, 20)

	// First call: stage has no NodeIDs — preview should mark done with no data.
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning, NodeIDs: nil},
	}
	cmd := pp.UpdateForCursor(0, stages)
	if cmd != nil {
		t.Fatal("expected nil cmd when stage has no NodeIDs")
	}
	if !pp.Done() {
		t.Fatal("expected done=true when stage has no NodeIDs")
	}

	// Second call: same index, but NodeIDs now exist.
	// UpdateForCursor should NOT return nil — it should trigger a re-fetch.
	stages[0].NodeIDs = []int{10, 11}
	cmd = pp.UpdateForCursor(0, stages)
	// We can't run the cmd (no real client), but it should be non-nil.
	if cmd != nil {
		// cmd is non-nil = correct, it would start a fetch.
		// The preview should no longer be done.
		if pp.Done() {
			t.Error("expected done=false after re-fetch triggered")
		}
	} else {
		t.Error("expected non-nil cmd when NodeIDs appeared for previously-empty stage")
	}
}

// TestAllStagesFinished verifies the allStagesFinished helper.
func TestAllStagesFinished(t *testing.T) {
	tests := []struct {
		name   string
		stages []jenkins.Stage
		want   bool
	}{
		{"empty", nil, false},
		{"all success", []jenkins.Stage{
			{Status: jenkins.BuildStatusSuccess},
			{Status: jenkins.BuildStatusSuccess},
		}, true},
		{"one running", []jenkins.Stage{
			{Status: jenkins.BuildStatusSuccess},
			{Status: jenkins.BuildStatusRunning},
		}, false},
		{"one not built", []jenkins.Stage{
			{Status: jenkins.BuildStatusSuccess},
			{Status: jenkins.BuildStatusNotBuilt},
		}, false},
		{"mixed terminal", []jenkins.Stage{
			{Status: jenkins.BuildStatusSuccess},
			{Status: jenkins.BuildStatusFailed},
			{Status: jenkins.BuildStatusSkipped},
		}, true},
		{"aborted", []jenkins.Stage{
			{Status: jenkins.BuildStatusAborted},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allStagesFinished(tt.stages)
			if got != tt.want {
				t.Errorf("allStagesFinished() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestInferBuildStatusFromStages verifies precedence: Failed > Aborted > Unstable > Success.
func TestInferBuildStatusFromStages(t *testing.T) {
	tests := []struct {
		name   string
		stages []jenkins.Stage
		want   jenkins.BuildStatus
	}{
		{"all success", []jenkins.Stage{
			{Status: jenkins.BuildStatusSuccess},
			{Status: jenkins.BuildStatusSuccess},
		}, jenkins.BuildStatusSuccess},
		{"one failed", []jenkins.Stage{
			{Status: jenkins.BuildStatusSuccess},
			{Status: jenkins.BuildStatusFailed},
		}, jenkins.BuildStatusFailed},
		{"failed beats unstable", []jenkins.Stage{
			{Status: jenkins.BuildStatusUnstable},
			{Status: jenkins.BuildStatusFailed},
		}, jenkins.BuildStatusFailed},
		{"aborted beats unstable", []jenkins.Stage{
			{Status: jenkins.BuildStatusUnstable},
			{Status: jenkins.BuildStatusAborted},
		}, jenkins.BuildStatusAborted},
		{"unstable only", []jenkins.Stage{
			{Status: jenkins.BuildStatusSuccess},
			{Status: jenkins.BuildStatusUnstable},
		}, jenkins.BuildStatusUnstable},
		{"skipped counts as success", []jenkins.Stage{
			{Status: jenkins.BuildStatusSkipped},
		}, jenkins.BuildStatusSuccess},
		{"empty", nil, jenkins.BuildStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferBuildStatusFromStages(tt.stages)
			if got != tt.want {
				t.Errorf("inferBuildStatusFromStages() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildFinishDetection_DoesNotPreemptAPI verifies that when the build API
// still reports running but all stages have finished, the view does NOT
// prematurely mutate sv.build.Status. The API is the only authoritative source
// for "done" — a new stage (e.g. "Declarative: Post Actions") can still appear
// after existing stages finish, and preempting the status causes the progress
// bar to flip to SUCCESS, hides ghost stages, and stops the animation tick.
func TestBuildFinishDetection_DoesNotPreemptAPI(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1) // Build = table 1

	// All stages finished but build API still says running.
	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusSuccess},
	}
	_ = stageRefresh(sv, newStages, jenkins.BuildStatusRunning)

	if sv.build.Status != jenkins.BuildStatusRunning {
		t.Errorf("expected build status to remain running until API confirms, got %v", sv.build.Status)
	}
}

// TestBuildFinishDetection_DoesNotPreemptAPI_WhenFailing verifies the same
// invariant holds when stages contain a failure — we must still defer to the
// API, because post-actions may still run.
func TestBuildFinishDetection_DoesNotPreemptAPI_WhenFailing(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
	}
	sv := makeStageView(initialBuild, stages)
	sv.table.SetCursor(1)

	newStages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusFailed},
		{Name: "Deploy", Status: jenkins.BuildStatusSkipped},
	}
	_ = stageRefresh(sv, newStages, jenkins.BuildStatusRunning)

	if sv.build.Status != jenkins.BuildStatusRunning {
		t.Errorf("expected build status to remain running until API confirms, got %v", sv.build.Status)
	}
}

// TestSyntheticPipelineRow verifies the Pipeline row appears at table index 0.
func TestSyntheticPipelineRow(t *testing.T) {
	initialBuild := jenkins.Build{
		Status:   jenkins.BuildStatusRunning,
		Duration: 10 * time.Second,
	}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
		{Name: "Test", Status: jenkins.BuildStatusNotBuilt},
	}
	sv := makeStageView(initialBuild, stages)

	if !sv.hasPipelineRow() {
		t.Fatal("expected hasPipelineRow() to be true when stages exist")
	}
	// Pipeline row = table index 0, real stage index -1.
	if sv.realStageIdx(0) != -1 {
		t.Errorf("expected realStageIdx(0) = -1, got %d", sv.realStageIdx(0))
	}
	// Build = table index 1, real stage index 0.
	if sv.realStageIdx(1) != 0 {
		t.Errorf("expected realStageIdx(1) = 0, got %d", sv.realStageIdx(1))
	}
	// tableCursorForStage(0) = 1.
	if sv.tableCursorForStage(0) != 1 {
		t.Errorf("expected tableCursorForStage(0) = 1, got %d", sv.tableCursorForStage(0))
	}
}

// TestSyntheticPipelineRow_NoStages verifies Pipeline row is present even with no stages.
func TestSyntheticPipelineRow_NoStages(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning}
	sv := makeStageView(initialBuild, nil)

	if !sv.hasPipelineRow() {
		t.Fatal("expected hasPipelineRow() to be true even when no stages")
	}
	// Cursor should be on Pipeline row (index 0).
	if sv.initialCursor() != 0 {
		t.Errorf("expected initialCursor() = 0 (Pipeline), got %d", sv.initialCursor())
	}
}

// TestAutoFollow_SkipsPipelineRow verifies auto-follow never lands on the Pipeline row.
func TestAutoFollow_SkipsPipelineRow(t *testing.T) {
	initialBuild := jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}
	stages := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning},
	}
	sv := makeStageView(initialBuild, stages)

	cursor := sv.initialCursor()
	// Should be on Build (table 1), not Pipeline (table 0).
	if cursor != 1 {
		t.Errorf("expected initialCursor to skip Pipeline row (table 0), got %d", cursor)
	}
}

// TestComputeGhostValidity_EmptyPrefix verifies that zero current stages
// is a valid prefix — all prev stages are shown as ghosts immediately.
func TestComputeGhostValidity_EmptyPrefix(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusRunning}, nil)
	sv.stages = nil
	sv.prevStages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 0},
	}
	sv.computeGhostValidity()
	if !sv.ghostsValid {
		t.Error("expected ghostsValid = true when current stages are empty (empty prefix)")
	}
}

// TestComputeGhostValidity_PrefixMatch verifies that current stages being a
// prefix of prevStages (by name and depth) results in ghostsValid = true.
func TestComputeGhostValidity_PrefixMatch(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusRunning}, nil)
	sv.stages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 0},
	}
	sv.prevStages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 0},
		{Name: "Deploy", Depth: 0},
	}
	sv.computeGhostValidity()
	if !sv.ghostsValid {
		t.Error("expected ghostsValid = true when current stages are a prefix of prevStages")
	}
}

// TestComputeGhostValidity_Divergence verifies that a name mismatch
// between current and previous stages invalidates ghosts.
func TestComputeGhostValidity_Divergence(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusRunning}, nil)
	sv.stages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Lint", Depth: 0}, // diverges from prev
	}
	sv.prevStages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 0},
		{Name: "Deploy", Depth: 0},
	}
	sv.computeGhostValidity()
	if sv.ghostsValid {
		t.Error("expected ghostsValid = false when stage names diverge")
	}
}

// TestComputeGhostValidity_DepthMismatch verifies that matching names but
// different depths invalidate ghosts.
func TestComputeGhostValidity_DepthMismatch(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusRunning}, nil)
	sv.stages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 1}, // depth differs
	}
	sv.prevStages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 0},
		{Name: "Deploy", Depth: 0},
	}
	sv.computeGhostValidity()
	if sv.ghostsValid {
		t.Error("expected ghostsValid = false when depths differ")
	}
}

// TestComputeGhostValidity_CurrentLonger verifies that when current has more
// stages than prev, ghosts are invalid.
func TestComputeGhostValidity_CurrentLonger(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusRunning}, nil)
	sv.stages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 0},
		{Name: "Deploy", Depth: 0},
		{Name: "Notify", Depth: 0},
	}
	sv.prevStages = []jenkins.Stage{
		{Name: "Build", Depth: 0},
		{Name: "Test", Depth: 0},
		{Name: "Deploy", Depth: 0},
	}
	sv.computeGhostValidity()
	if sv.ghostsValid {
		t.Error("expected ghostsValid = false when current has more stages than prev")
	}
}

// TestEffectiveEstimate verifies that effectiveEstimate returns the Jenkins estimate directly.
func TestEffectiveEstimate(t *testing.T) {
	tests := []struct {
		name              string
		estimatedDuration time.Duration
		want              time.Duration
	}{
		{
			name:              "returns build estimate",
			estimatedDuration: 60 * time.Second,
			want:              60 * time.Second,
		},
		{
			name:              "returns zero when no estimate",
			estimatedDuration: 0,
			want:              0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := makeStageView(jenkins.Build{
				Status:            jenkins.BuildStatusRunning,
				EstimatedDuration: tt.estimatedDuration,
				Timestamp:         time.Now(),
			}, nil)
			got := sv.effectiveEstimate()
			if got != tt.want {
				t.Errorf("effectiveEstimate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestProgressTick_AlwaysReschedules verifies the 200ms progress tick keeps
// rescheduling itself even when the build is in a terminal state. Without this,
// a transient "all stages finished" state (or an API that briefly reports the
// wrong status) permanently stops the animation thread.
func TestProgressTick_AlwaysReschedules(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusSuccess}, []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
	})
	_, cmd := sv.Update(stageProgressTickMsg{})
	if cmd == nil {
		t.Fatal("expected stageProgressTickMsg to return a reschedule cmd even when build is terminal")
	}
}

// TestBuildDetailMsg_RetriesOnZeroDuration verifies the post-completion retry
// loop: if Jenkins returns a terminal build status with Duration==0, the view
// schedules another fetch (bounded by maxBuildDetailRetries).
func TestBuildDetailMsg_RetriesOnZeroDuration(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusSuccess}, nil)
	_, cmd := sv.Update(buildDetailMsg{build: jenkins.Build{
		Status:   jenkins.BuildStatusSuccess,
		Duration: 0,
	}})
	if cmd == nil {
		t.Fatal("expected retry cmd when terminal build returns Duration=0")
	}
	if sv.buildDetailRetries != 1 {
		t.Errorf("expected buildDetailRetries=1, got %d", sv.buildDetailRetries)
	}
}

// TestBuildDetailMsg_StopsAtMaxRetries verifies the retry loop is bounded.
func TestBuildDetailMsg_StopsAtMaxRetries(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusSuccess}, nil)
	sv.buildDetailRetries = maxBuildDetailRetries
	_, cmd := sv.Update(buildDetailMsg{build: jenkins.Build{
		Status:   jenkins.BuildStatusSuccess,
		Duration: 0,
	}})
	if cmd != nil {
		t.Error("expected no retry cmd once maxBuildDetailRetries is reached")
	}
}

// TestBuildDetailMsg_ResetsCounterOnNonZeroDuration verifies that a successful
// fetch clears the retry counter so a future zero-duration response starts
// fresh rather than being immediately cut off.
func TestBuildDetailMsg_ResetsCounterOnNonZeroDuration(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusSuccess}, nil)
	sv.buildDetailRetries = 5
	_, _ = sv.Update(buildDetailMsg{build: jenkins.Build{
		Status:   jenkins.BuildStatusSuccess,
		Duration: 42 * time.Second,
	}})
	if sv.buildDetailRetries != 0 {
		t.Errorf("expected counter reset on non-zero duration, got %d", sv.buildDetailRetries)
	}
}

// TestWhenSkipDetected_RetriesOnEmpty verifies that an empty when-skip parse
// result triggers a retry while the build has recently finished and the
// retry budget is available.
func TestWhenSkipDetected_RetriesOnEmpty(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusSuccess}, nil)
	sv.buildFinishedAt = time.Now()
	_, cmd := sv.Update(whenSkipDetectedMsg{skippedOccs: map[string][]bool{}})
	if cmd == nil {
		t.Fatal("expected retry cmd when when-skip parse returned empty shortly after build finished")
	}
	if sv.whenSkipRetries != 1 {
		t.Errorf("expected whenSkipRetries=1, got %d", sv.whenSkipRetries)
	}
}

// TestWhenSkipDetected_StopsAtMaxRetries verifies the retry loop is bounded.
func TestWhenSkipDetected_StopsAtMaxRetries(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusSuccess}, nil)
	sv.buildFinishedAt = time.Now()
	sv.whenSkipRetries = maxWhenSkipRetries
	_, cmd := sv.Update(whenSkipDetectedMsg{skippedOccs: map[string][]bool{}})
	if cmd != nil {
		t.Error("expected no retry cmd once maxWhenSkipRetries is reached")
	}
}

// TestWhenSkipDetected_AppliesNonEmpty verifies that a non-empty parse result
// is applied to stages and resets the retry counter.
func TestWhenSkipDetected_AppliesNonEmpty(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusSuccess}, []jenkins.Stage{
		{Name: "DeployProd", Status: jenkins.BuildStatusSuccess},
	})
	sv.buildFinishedAt = time.Now()
	sv.whenSkipRetries = 2
	occs := map[string][]bool{"DeployProd": {true}}
	_, cmd := sv.Update(whenSkipDetectedMsg{skippedOccs: occs})
	if cmd != nil {
		t.Error("expected no retry cmd when skip occurrences were found")
	}
	if sv.whenSkipRetries != 0 {
		t.Errorf("expected counter reset on non-empty result, got %d", sv.whenSkipRetries)
	}
	if sv.stages[0].Status != jenkins.BuildStatusSkipped {
		t.Errorf("expected DeployProd marked as skipped, got %v", sv.stages[0].Status)
	}
}

// ----------------------------------------------------------------------
// isRunning() — single authority for "render the running bar".
// These tests lock down the rule the user explicitly asked for: the build
// API status is authoritative. anyStageRunning() is consulted only when the
// status is empty/Unknown (initial-load races). A stage stuck in "Running"
// after the build is terminal must NOT keep the bar animating.
// ----------------------------------------------------------------------

// TestIsRunning_BuildTerminalDespiteRunningStage is the headline regression
// test for the user's bug. Before the fix, View() rendered the running bar
// as long as anyStageRunning() returned true, even when the build API had
// already reported a terminal status. After the fix, isRunning() must
// return false the moment build.Status is terminal, regardless of stage
// staleness in the flowGraph response.
func TestIsRunning_BuildTerminalDespiteRunningStage(t *testing.T) {
	cases := []jenkins.BuildStatus{
		jenkins.BuildStatusSuccess,
		jenkins.BuildStatusFailed,
		jenkins.BuildStatusAborted,
		jenkins.BuildStatusUnstable,
		jenkins.BuildStatusNotBuilt,
		jenkins.BuildStatusSkipped,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			sv := makeStageView(jenkins.Build{Status: status, Timestamp: time.Now()},
				[]jenkins.Stage{
					{Name: "Build", Status: jenkins.BuildStatusSuccess},
					{Name: "PostActions", Status: jenkins.BuildStatusRunning}, // stale
				})
			if sv.isRunning() {
				t.Errorf("isRunning() = true with terminal build %v but stale running stage; want false", status)
			}
		})
	}
}

// TestIsRunning_BuildRunning verifies the running bar shows whenever the
// build API says running, even if every stage looks terminal in the
// flowGraph response (early-snapshot finalisation race).
func TestIsRunning_BuildRunning(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusRunning, Timestamp: time.Now()},
		[]jenkins.Stage{
			{Name: "Build", Status: jenkins.BuildStatusSuccess},
			{Name: "Test", Status: jenkins.BuildStatusSuccess},
		})
	if !sv.isRunning() {
		t.Error("isRunning() = false with build status Running; want true")
	}
}

// TestIsRunning_FallbackWhenStatusEmpty exercises the fallback path: when
// the build API hasn't reported a status yet (initial Init() before
// fetchBuildDetail returns), trust the stage data.
func TestIsRunning_FallbackWhenStatusEmpty(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: "", Timestamp: time.Now()},
		[]jenkins.Stage{{Name: "Build", Status: jenkins.BuildStatusRunning}})
	if !sv.isRunning() {
		t.Error("isRunning() = false with empty status + running stage; want true (fallback)")
	}

	sv2 := makeStageView(jenkins.Build{Status: "", Timestamp: time.Now()},
		[]jenkins.Stage{{Name: "Build", Status: jenkins.BuildStatusSuccess}})
	if sv2.isRunning() {
		t.Error("isRunning() = true with empty status + non-running stage; want false")
	}
}

// TestIsRunning_FallbackWhenStatusUnknown — same fallback rule for
// BuildStatusUnknown, which is what ParseBuildStatus returns for unfamiliar
// result values.
func TestIsRunning_FallbackWhenStatusUnknown(t *testing.T) {
	sv := makeStageView(jenkins.Build{Status: jenkins.BuildStatusUnknown, Timestamp: time.Now()},
		[]jenkins.Stage{{Name: "Build", Status: jenkins.BuildStatusRunning}})
	if !sv.isRunning() {
		t.Error("isRunning() = false with Unknown status + running stage; want true (fallback)")
	}

	sv2 := makeStageView(jenkins.Build{Status: jenkins.BuildStatusUnknown, Timestamp: time.Now()},
		[]jenkins.Stage{{Name: "Build", Status: jenkins.BuildStatusSuccess}})
	if sv2.isRunning() {
		t.Error("isRunning() = true with Unknown status + non-running stage; want false")
	}
}

// TestView_RendersFinishedBarWhenBuildTerminal is the integration check
// that the View() output actually picks the finished branch. We don't
// assert on exact ANSI bytes (theme-dependent); instead we compare against
// a reference render where everything is unambiguously terminal. Both
// branches go through different code paths (RenderWithTextTall vs
// RenderCompleteTall) so the outputs differ.
func TestView_RendersFinishedBarWhenBuildTerminal(t *testing.T) {
	stale := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "PostActions", Status: jenkins.BuildStatusRunning}, // stale running stage
	}
	clean := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "PostActions", Status: jenkins.BuildStatusSuccess},
	}
	build := jenkins.Build{
		Status:    jenkins.BuildStatusSuccess,
		Duration:  10 * time.Second,
		Timestamp: time.Now().Add(-10 * time.Second),
	}
	svStale := makeStageView(build, stale)
	svStale.SetSize(80, 20)
	svClean := makeStageView(build, clean)
	svClean.SetSize(80, 20)

	staleView := svStale.View()
	cleanView := svClean.View()

	// Both should render the finished bar: outputs identical at the bar
	// region. (Stage rows differ because PostActions has a different
	// status icon, but the bar prefix should match.)
	staleBar := firstNLines(staleView, stageBarHeight)
	cleanBar := firstNLines(cleanView, stageBarHeight)
	if staleBar != cleanBar {
		t.Errorf("stale-stages and clean-stages renders should produce the same finished bar.\nstale:\n%q\nclean:\n%q",
			staleBar, cleanBar)
	}
}

// TestView_RendersRunningBarWhenBuildRunning is the converse: while the
// build is running, the bar must use the animated form even if all stages
// look terminal momentarily.
func TestView_RendersRunningBarWhenBuildRunning(t *testing.T) {
	build := jenkins.Build{
		Status:            jenkins.BuildStatusRunning,
		EstimatedDuration: 60 * time.Second,
		Timestamp:         time.Now().Add(-5 * time.Second),
	}
	allDone := []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess},
		{Name: "Test", Status: jenkins.BuildStatusSuccess},
	}
	svRunning := makeStageView(build, allDone)
	svRunning.SetSize(80, 20)

	finishedBuild := build
	finishedBuild.Status = jenkins.BuildStatusSuccess
	finishedBuild.Duration = 5 * time.Second
	svFinished := makeStageView(finishedBuild, allDone)
	svFinished.SetSize(80, 20)

	runningBar := firstNLines(svRunning.View(), stageBarHeight)
	finishedBar := firstNLines(svFinished.View(), stageBarHeight)
	if runningBar == finishedBar {
		t.Errorf("running-build and finished-build should render different bars but produced identical output:\n%q",
			runningBar)
	}
}

// firstNLines returns the first n lines of s (bar region) for substring
// comparison without depending on terminal width.
func firstNLines(s string, n int) string {
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			count++
			if count == n {
				return s[:i]
			}
		}
	}
	return s
}

// ----------------------------------------------------------------------
// BuildCompletedMsg handler — defence-in-depth path so the StageView
// learns about terminal status from the RunningBuildsMonitor (within ~1s)
// even if its own 2s refresh chain misses a tick.
// ----------------------------------------------------------------------

// TestBuildCompletedMsg_UpdatesStatusOnMatch verifies the happy path: a
// completion broadcast for the current build flips sv.build.Status to the
// final value supplied by the monitor.
func TestBuildCompletedMsg_UpdatesStatusOnMatch(t *testing.T) {
	sv := makeStageView(jenkins.Build{Number: 42, Status: jenkins.BuildStatusRunning, Timestamp: time.Now()},
		[]jenkins.Stage{{Name: "Build", Status: jenkins.BuildStatusRunning}})
	final := jenkins.Build{Number: 42, Status: jenkins.BuildStatusSuccess, Duration: 8 * time.Second}
	_, _ = sv.Update(BuildCompletedMsg{
		JobPath: sv.nc.JobPath(),
		Number:  42,
		Build:   final,
	})
	if sv.build.Status != jenkins.BuildStatusSuccess {
		t.Errorf("expected sv.build.Status=Success after BuildCompletedMsg, got %v", sv.build.Status)
	}
	if sv.build.Duration != 8*time.Second {
		t.Errorf("expected duration to be applied, got %v", sv.build.Duration)
	}
}

// TestBuildCompletedMsg_IgnoresWrongJob — completion for a different job
// must not mutate state. (App routes broadcasts to the active view; the
// view itself filters.)
func TestBuildCompletedMsg_IgnoresWrongJob(t *testing.T) {
	sv := makeStageView(jenkins.Build{Number: 42, Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}, nil)
	before := sv.build.Status
	_, _ = sv.Update(BuildCompletedMsg{
		JobPath: "some/other/job",
		Number:  42,
		Build:   jenkins.Build{Status: jenkins.BuildStatusFailed},
	})
	if sv.build.Status != before {
		t.Errorf("expected status unchanged for wrong job, was %v now %v", before, sv.build.Status)
	}
}

// TestBuildCompletedMsg_IgnoresWrongNumber — completion for a different
// build number on the same job (e.g. an older build finishing while we
// view a newer one) must not mutate state.
func TestBuildCompletedMsg_IgnoresWrongNumber(t *testing.T) {
	sv := makeStageView(jenkins.Build{Number: 42, Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}, nil)
	before := sv.build.Status
	_, _ = sv.Update(BuildCompletedMsg{
		JobPath: sv.nc.JobPath(),
		Number:  41, // not the one we're viewing
		Build:   jenkins.Build{Status: jenkins.BuildStatusFailed},
	})
	if sv.build.Status != before {
		t.Errorf("expected status unchanged for wrong build number, was %v now %v", before, sv.build.Status)
	}
}

// TestBuildCompletedMsg_IgnoresErrors — when the monitor reports an error
// fetching the final detail, we keep our own polling-derived status rather
// than overwriting it with a zero-value Build.
func TestBuildCompletedMsg_IgnoresErrors(t *testing.T) {
	sv := makeStageView(jenkins.Build{Number: 42, Status: jenkins.BuildStatusRunning, Timestamp: time.Now()}, nil)
	_, _ = sv.Update(BuildCompletedMsg{
		JobPath: sv.nc.JobPath(),
		Number:  42,
		Err:     &mockError{"fetch failed"},
	})
	if sv.build.Status != jenkins.BuildStatusRunning {
		t.Errorf("expected status preserved on error, got %v", sv.build.Status)
	}
}

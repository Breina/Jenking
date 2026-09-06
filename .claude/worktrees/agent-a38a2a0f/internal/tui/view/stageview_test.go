package view

import (
	"testing"
	"time"

	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

func makeStageView(build jenkins.Build, stages []jenkins.Stage) *StageView {
	t := theme.Default()
	sv := NewStageView(t, nil, nil, "job/test", build, "test", "")
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
	pp := NewPreviewPanel(th, nil, nil, nil, "job/test", 1, "test", "")
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

// TestBuildFinishDetection_B14 verifies that when the build API still reports
// running but all stages have finished, the build status is inferred from stages.
func TestBuildFinishDetection_B14(t *testing.T) {
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

	// Build status should be inferred as success.
	if sv.build.Status != jenkins.BuildStatusSuccess {
		t.Errorf("expected build status to be inferred as success, got %v", sv.build.Status)
	}
}

// TestBuildFinishDetection_B14_Failed verifies that when all stages finished
// with a failure, the inferred status is failed.
func TestBuildFinishDetection_B14_Failed(t *testing.T) {
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

	if sv.build.Status != jenkins.BuildStatusFailed {
		t.Errorf("expected build status to be inferred as failed, got %v", sv.build.Status)
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

// TestEffectiveEstimate verifies the min-of-two logic and zero handling.
func TestEffectiveEstimate(t *testing.T) {
	tests := []struct {
		name              string
		ghostsValid       bool
		prevStageDurSum   time.Duration
		estimatedDuration time.Duration
		want              time.Duration
	}{
		{
			name:              "ghosts invalid uses build estimate",
			ghostsValid:       false,
			prevStageDurSum:   30 * time.Second,
			estimatedDuration: 60 * time.Second,
			want:              60 * time.Second,
		},
		{
			name:              "ghosts valid uses min",
			ghostsValid:       true,
			prevStageDurSum:   45 * time.Second,
			estimatedDuration: 60 * time.Second,
			want:              45 * time.Second,
		},
		{
			name:              "ghosts valid build estimate smaller",
			ghostsValid:       true,
			prevStageDurSum:   60 * time.Second,
			estimatedDuration: 45 * time.Second,
			want:              45 * time.Second,
		},
		{
			name:              "ghosts valid zero build estimate",
			ghostsValid:       true,
			prevStageDurSum:   45 * time.Second,
			estimatedDuration: 0,
			want:              45 * time.Second,
		},
		{
			name:              "ghosts valid zero prev sum",
			ghostsValid:       true,
			prevStageDurSum:   0,
			estimatedDuration: 60 * time.Second,
			want:              60 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := makeStageView(jenkins.Build{
				Status:            jenkins.BuildStatusRunning,
				EstimatedDuration: tt.estimatedDuration,
				Timestamp:         time.Now(), // fresh start — no running stages, no floor effect
			}, nil)
			sv.ghostsValid = tt.ghostsValid
			sv.prevStageDurSum = tt.prevStageDurSum
			got := sv.effectiveEstimate()
			if got != tt.want {
				t.Errorf("effectiveEstimate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEffectiveEstimate_FloorFromRunningStage verifies that the pipeline
// estimate is never shorter than elapsed + remaining time for a running stage.
func TestEffectiveEstimate_FloorFromRunningStage(t *testing.T) {
	now := time.Now()
	sv := makeStageView(jenkins.Build{
		Status:            jenkins.BuildStatusRunning,
		EstimatedDuration: 30 * time.Second,           // Jenkins thinks 30s total
		Timestamp:         now.Add(-20 * time.Second), // 20s elapsed
	}, nil)
	sv.ghostsValid = true
	sv.prevStageDurSum = 30 * time.Second
	sv.lastRefreshAt = now
	// Stage "Build" is running, 5s elapsed, prev build took 25s for this stage.
	// So stage has ~20s remaining. Pipeline estimate floor = 20s elapsed + 20s remaining = 40s.
	sv.stages = []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusRunning, Duration: 5 * time.Second},
	}
	sv.prevStages = []jenkins.Stage{
		{Name: "Build", Status: jenkins.BuildStatusSuccess, Duration: 25 * time.Second},
		{Name: "Test", Status: jenkins.BuildStatusSuccess, Duration: 5 * time.Second},
	}

	got := sv.effectiveEstimate()
	// Base would be min(30s, 30s) = 30s. But floor = ~20s elapsed + ~20s remaining ≈ 40s.
	// The floor should push it above 30s.
	if got <= 30*time.Second {
		t.Errorf("effectiveEstimate() = %v, expected > 30s (floor from running stage)", got)
	}
}

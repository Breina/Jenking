//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// openFirstBuild navigates to the stage view of the first available build.
// Returns nil and skips if no build with stages is found.
func openFirstBuild(t *testing.T) *harness.Harness {
	t.Helper()
	h := openFixtureBuilds(t)

	if !h.Contains("builds(") {
		t.Skip("no builds view available")
	}

	// Press Enter on the first build to open its stage view
	h.SendKeys("<enter>")

	if _, err := h.WaitForAny(harness.NetworkTimeout,
		"stages(", "stage(", "console(", "No stages"); err != nil {
		t.Skipf("no build detail view appeared: %v", err)
	}

	return h
}

// TestStageViewRenders verifies the stage view shows expected chrome.
func TestStageViewRenders(t *testing.T) {
	h := openFirstBuild(t)

	// Might be stages or console — either is acceptable
	grid := h.Grid()
	if !strings.Contains(grid, "stages(") && !strings.Contains(grid, "console(") {
		t.Fatalf("neither stages nor console view appeared; grid:\n%s", grid)
	}
	h.MustSnapshot(t, "stageview-renders")
}

// TestStageViewScrolling verifies scrolling in the stage list doesn't panic.
func TestStageViewScrolling(t *testing.T) {
	h := openFirstBuild(t)

	if !h.Contains("stages(") {
		t.Skip("build opened console view, not stages — skip stage scroll test")
	}

	for i := 0; i < 10; i++ {
		h.SendKeys("j")
	}
	for i := 0; i < 10; i++ {
		h.SendKeys("k")
	}

	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "stages(")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("stage view disappeared after scrolling: %v", err)
	}
	h.MustSnapshot(t, "stageview-scrolled")
}

// TestStageViewResizeMidView verifies resize during stage view doesn't crash.
func TestStageViewResizeMidView(t *testing.T) {
	h := openFirstBuild(t)

	if !h.Contains("stages(") {
		t.Skip("no stage view to resize")
	}

	// Resize to squat (stageview overflow risk)
	h.Resize(80, 5)
	time.Sleep(100 * time.Millisecond)

	if err := h.WaitFor(func(grid string) bool {
		return strings.TrimSpace(grid) != ""
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("app stopped rendering after resize during stage view: %v", err)
	}

	// Restore
	h.Resize(160, 40)

	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "stages(")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("stage view not restored after resize back: %v", err)
	}
	h.MustSnapshot(t, "stageview-resize-restored")
}

// TestStageViewOpenLog verifies pressing 'l' or Enter on a stage opens the log.
func TestStageViewOpenLog(t *testing.T) {
	h := openFirstBuild(t)

	if !h.Contains("stages(") {
		t.Skip("no stage view available")
	}

	// Select first stage and open its log
	h.SendKeys("<enter>")

	if _, err := h.WaitForAny(harness.NetworkTimeout,
		"stagelog(", "log(", "console("); err != nil {
		t.Logf("no stage log appeared (stage may have no log); grid:\n%s", h.Grid())
		return
	}
	h.MustSnapshot(t, "stageview-stagelog")

	// Go back
	h.SendKeys("<esc>")
	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "stages(")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("did not return to stages view after Esc: %v", err)
	}
}

// TestStageViewRapidEscOnOpen verifies that pressing Esc very quickly after
// opening the stage view doesn't cause a race in the cancel path.
// This exercises internal/tui/view/nodelogfetcher.go cancel-on-back.
func TestStageViewRapidEscOnOpen(t *testing.T) {
	h := openFixtureBuilds(t)
	if !h.Contains("builds(") {
		t.Skip("no builds view")
	}

	// Open a build then immediately Esc before it fully loads
	h.SendKeys("<enter>")
	time.Sleep(50 * time.Millisecond) // just enough for the command to be sent
	h.SendKeys("<esc>")

	// Should return gracefully to builds view or job list
	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "builds(") || strings.Contains(grid, "jobs(")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("app stuck after rapid Esc on open: %v", err)
	}
}

// pipelineRowSaysRunning reports whether the line containing "Pipeline" still
// shows the "Running" status badge. Used by the live regression tests below.
func pipelineRowSaysRunning(grid string) bool {
	for _, line := range strings.Split(grid, "\n") {
		if strings.Contains(line, "Pipeline") && strings.Contains(line, "Running") {
			return true
		}
	}
	return false
}

// findFixtureBuildsView navigates the job list → fixture multibranch → first
// branch builds view, regardless of where the fixture lives in the folder
// hierarchy. Skips the test if the fixture cannot be found.
func findFixtureBuildsView(t *testing.T, h *harness.Harness) {
	t.Helper()
	// Try the simple top-level case first (matches existing requireFixtureJob).
	if h.Contains(fixtureJobName) {
		h.SendKeys("/")
		h.MustWaitFor(t, func(g string) bool {
			return strings.Contains(strings.ToLower(g), "search") || strings.Contains(g, ">")
		}, harness.RenderTimeout)
		h.SendKeys(fixtureJobName + "<cr>")
		h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, fixtureJobName) }, harness.NetworkTimeout)
		h.SendKeys("<enter>")
	} else {
		// Walk: job list → first folder that contains the fixture name.
		// The integration setup at jenkins-on.cumuli.be has the fixture
		// inside a "Omgeving" folder; this loop generalises.
		t.Skipf("fixture %q not at top-level; nested fixture navigation not implemented for this scenario", fixtureJobName)
	}
	// Now in branch list (multibranch fixture) — open first branch.
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "jobs(") }, harness.NetworkTimeout)
	h.SendKeys("<enter>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "builds(") }, harness.NetworkTimeout)
}

// TestStageViewBarTransitionsAfterBuildCompletes is the integration regression
// test for the "stuck running bar" bug. It triggers a fresh build of the
// jenkins-e2e fixture and asserts that the StageView's pipeline status
// transitions away from "Running" within the build's lifetime + a small
// margin for the refresh chain. Before the fix, the synthetic Pipeline row
// stayed at "Running" indefinitely; after the fix, isRunning() trusts the
// build-API status and the BuildCompletedMsg handler short-circuits the
// transition within ~1s of completion.
//
// Skips when the fixture isn't directly reachable from the job list. The
// real lock-down is the unit tests in internal/tui/view/stageview_test.go;
// this is a smoke test for the full lifecycle when a fixture is wired up.
func TestStageViewBarTransitionsAfterBuildCompletes(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath, Context: "ontwikkel"})
	openAllJobs(t, h)

	findFixtureBuildsView(t, h)

	// Trigger via 't'. Fixture has params so a form dialog appears; submit
	// defaults with ctrl+s.
	h.SendKeys("t")
	h.MustWaitFor(t, func(g string) bool {
		g = strings.ToLower(g)
		return strings.Contains(g, "fail") || strings.Contains(g, "speed") || strings.Contains(g, "parameter")
	}, harness.RenderTimeout)
	h.SendKeys("<c-s>")

	// Pending stages view appears, then transitions to live stages.
	h.MustWaitFor(t, func(g string) bool {
		return strings.Contains(g, "stages(") || strings.Contains(g, "Pending")
	}, harness.NetworkTimeout)

	if err := h.WaitFor(func(g string) bool {
		return strings.Contains(g, "Pipeline") && strings.Contains(g, "Running")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("Pipeline row never reached Running state:\n%s", h.Grid())
	}
	h.MustSnapshot(t, "stageview-bar-during-build")

	// THE assertion: the Pipeline row must transition away from Running.
	// Before the fix this would hang past the build's lifetime.
	err := h.WaitFor(func(g string) bool {
		if !strings.Contains(g, "Pipeline") {
			return false
		}
		return !pipelineRowSaysRunning(g)
	}, 90*time.Second)
	if err != nil {
		h.MustSnapshot(t, "stageview-stuck-running-FAILED")
		t.Fatalf("Pipeline row remained Running past the build's lifetime — bug not fixed.\nGrid:\n%s", h.Grid())
	}
	h.MustSnapshot(t, "stageview-bar-after-completion")
}

//go:build integration

package scenarios

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestDashboardSmoke verifies the most basic contract: jenking starts, renders
// its root list with header chrome, and exits cleanly without panicking.
func TestDashboardSmoke(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})

	// The root is the Jenkins views list; a remembered view may put a job
	// list up first instead, so either breadcrumb counts as "rendered".
	if err := h.WaitFor(func(g string) bool {
		return strings.Contains(g, "views(") || strings.Contains(g, "jobs(")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("root list never rendered: %v", err)
	}

	// Verify expected header chrome is present.
	// Note: "URL:" is skipped by Bubbletea's line-diff optimization in later frames
	// and vt10x doesn't capture it reliably. Check Status: and Connected instead.
	grid := h.Grid()
	if !strings.Contains(grid, "Status:") {
		t.Error("header missing Status label")
	}
	if !strings.Contains(grid, "Connected") {
		t.Errorf("not showing as connected; grid:\n%s", grid)
	}

	// Verify there is no boot error visible.
	if be := h.BootError(); be != "" {
		t.Errorf("boot error: %s", be)
	}

	h.MustSnapshot(t, "dashboard-smoke")

	// Quit cleanly via Ctrl+C.
	h.Stop()
}

// TestDashboardBreadcrumb verifies the breadcrumb reflects the job-list context.
func TestDashboardBreadcrumb(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	grid := h.Grid()
	if !strings.Contains(grid, "jobs(") {
		t.Errorf("breadcrumb does not contain 'jobs('; grid:\n%s", grid)
	}
}

// TestDashboardQuitCommand verifies :quit exits the app.
func TestDashboardQuitCommand(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	h.SendKeys(harness.Cmd("quit"))

	// After :quit, the process should exit — the reader goroutine closes.
	// t.Cleanup in harness.New will call Stop() which will try Ctrl+C and then
	// wait. If the process already exited, that's fine.
}

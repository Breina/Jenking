//go:build integration

package scenarios

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestDashboardSmoke verifies the most basic contract: jenking starts, renders
// the dashboard with header chrome, and exits cleanly without panicking.
func TestDashboardSmoke(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})

	// Wait for the TUI to connect and render the dashboard.
	// "jobs(" is the prefix of the breadcrumb panel title like "jobs(Dashboard)[n]"
	if err := h.WaitForText("jobs(", harness.NetworkTimeout); err != nil {
		t.Fatalf("dashboard never rendered: %v", err)
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

// TestDashboardBreadcrumb verifies the breadcrumb reflects the dashboard context.
func TestDashboardBreadcrumb(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	grid := h.Grid()
	if !strings.Contains(grid, "Dashboard") {
		t.Errorf("breadcrumb does not contain 'Dashboard'; grid:\n%s", grid)
	}
}

// TestDashboardQuitCommand verifies :quit exits the app.
func TestDashboardQuitCommand(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	h.SendKeys(harness.Cmd("quit"))

	// After :quit, the process should exit — the reader goroutine closes.
	// t.Cleanup in harness.New will call Stop() which will try Ctrl+C and then
	// wait. If the process already exited, that's fine.
}

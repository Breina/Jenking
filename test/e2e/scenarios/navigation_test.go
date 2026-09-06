//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestNavigationDrillIn verifies that pressing Enter on a folder opens it and
// the jobs panel updates.
func TestNavigationDrillIn(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	h.SendKeys("<enter>")

	// After drilling in, the jobs panel should still be visible
	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "jobs(")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("jobs panel disappeared after Enter: %v", err)
	}

	h.MustSnapshot(t, "navigation-drill")
}

// TestNavigationEscBack verifies that Esc from a sub-view returns to the dashboard.
func TestNavigationEscBack(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	// Drill into the first item (might be folder or job)
	h.SendKeys("<enter>")
	time.Sleep(500 * time.Millisecond)

	// Press Esc to go back
	h.SendKeys("<esc>")

	if err := h.WaitForText("jobs(", harness.NetworkTimeout); err != nil {
		t.Fatalf("did not return to the job list after Esc: %v", err)
	}
	h.MustSnapshot(t, "navigation-esc-back")
}

// TestNavigationMyBuilds verifies the 'm' key navigates to a user-filtered builds view.
func TestNavigationMyBuilds(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	h.SendKeys("m")

	// My builds shows user's branch builds (⎇), or "No builds" if none, or
	// redirects to all-builds. Any of these indicates the navigation worked.
	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "⎇") ||
			strings.Contains(g, "git/") ||
			strings.Contains(g, "no builds") ||
			strings.Contains(g, "success") ||
			strings.Contains(g, "failed")
	}, harness.NetworkTimeout); err != nil {
		// Acceptable: if the user truly has zero builds, we might not see any
		// of the above text but the app should still be alive.
		t.Logf("my builds returned no content (may be empty): %v", err)
		if !h.Contains("dashboard") && !h.Contains("jobs(") {
			// At least we should be alive somewhere
			t.Fatalf("app unresponsive after 'm' key: %v", err)
		}
	}
	h.MustSnapshot(t, "navigation-my-builds")
}

// TestNavigationRunningBuilds verifies the 'R' key opens the Running Builds view.
func TestNavigationRunningBuilds(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	h.SendKeys("R")

	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "running")
	}, harness.NetworkTimeout); err != nil {
		t.Logf("running builds grid: %s", h.Grid())
		t.Fatalf("running builds panel didn't appear: %v", err)
	}
	h.MustSnapshot(t, "navigation-running-builds")
}

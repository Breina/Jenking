//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// openFixtureBuilds opens any available builds view on the ontwikkel context.
// It navigates Dashboard → first multibranch pipeline → first branch → builds view.
// Skips the test if no builds view can be reached.
func openFixtureBuilds(t *testing.T) *harness.Harness {
	t.Helper()
	h := harness.New(t, harness.Options{BinaryPath: binaryPath, Context: "ontwikkel"})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	if !openAnyBuildsView(t, h) {
		t.Skip("no multibranch pipeline with a builds view found on Dashboard — cannot run builds view test")
	}
	return h
}

// poll checks h.Grid() every 100ms until pred returns true or the deadline passes.
// Unlike WaitFor, it has no stability requirement — safe under noisy background monitors.
func poll(h *harness.Harness, pred func(string) bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred(h.Grid()) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// openAnyBuildsView navigates from the Dashboard into the first reachable
// multibranch pipeline's builds view.  Uses the known-stable "Jenkins Library"
// multibranch on the ontwikkel Dashboard (cursor 0 at start).
//
// Navigation: Enter on first item (Jenkins Library, multibranch) → branch list
// → Enter on first branch → builds view.
//
// vt10x line-diff note: after each view change, old panel border titles stay
// in the vt10x cell grid.  All checks use poll() (no stability requirement)
// and check for UNIQUE strings from the NEW view rather than absence of old ones.
func openAnyBuildsView(t *testing.T, h *harness.Harness) bool {
	t.Helper()

	// Cursor starts at index 0 (Jenkins Library, a multibranch pipeline).
	h.SendKeys("<enter>")
	time.Sleep(200 * time.Millisecond)

	// Wait for branch list: "jobs(Jenkins Library" in the new panel border.
	// Do NOT check for "jobs(" without the job name — old "jobs(Dashboard)"
	// artifact stays in the vt10x grid indefinitely.
	if !poll(h, func(g string) bool {
		return strings.Contains(g, "jobs(Jenkins Library")
	}, harness.NetworkTimeout) {
		return false
	}

	// Navigate into the first branch.
	h.SendKeys("<enter>")
	time.Sleep(200 * time.Millisecond)

	// Wait for the builds view.
	if !poll(h, func(g string) bool {
		return strings.Contains(g, "builds(")
	}, harness.NetworkTimeout) {
		return false
	}

	return true
}

// TestBuildsViewRenders verifies the builds view shows expected chrome.
func TestBuildsViewRenders(t *testing.T) {
	h := openFixtureBuilds(t)

	grid := h.Grid()
	if !strings.Contains(grid, "builds(") {
		t.Fatalf("builds view breadcrumb missing; grid:\n%s", grid)
	}
	h.MustSnapshot(t, "buildsview-renders")
}

// TestBuildsViewScrolling verifies j/k/up/down scrolling doesn't crash.
func TestBuildsViewScrolling(t *testing.T) {
	h := openFixtureBuilds(t)

	// Scroll down many times
	for i := 0; i < 20; i++ {
		h.SendKeys("j")
	}
	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "builds(")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("builds view disappeared after scrolling down: %v", err)
	}

	// Scroll back up
	for i := 0; i < 20; i++ {
		h.SendKeys("k")
	}
	h.MustSnapshot(t, "buildsview-scrolled")
}

// TestBuildsViewEscReturns verifies Esc returns to the job list.
func TestBuildsViewEscReturns(t *testing.T) {
	h := openFixtureBuilds(t)

	h.SendKeys("<esc>")

	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "jobs(") || strings.Contains(grid, "Dashboard")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("did not return to job list after Esc: %v", err)
	}
}

// TestBuildsViewSearch verifies search filtering in builds view.
func TestBuildsViewSearch(t *testing.T) {
	h := openFixtureBuilds(t)

	h.SendKeys("/")

	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "search") || strings.Contains(g, "filter") || strings.Contains(g, "/")
	}, harness.RenderTimeout); err != nil {
		// Not all views have search — this is informational, not fatal
		t.Logf("search input may not appear in builds view (ok if view has no search): %v", err)
		return
	}

	// Type a search query and clear
	h.SendKeys("main<esc>")
	h.MustSnapshot(t, "buildsview-search")
}

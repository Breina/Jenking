//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// runCmd types a palette command one keystroke at a time, the way a person
// does. A single PTY write arrives as one paste-like key message carrying
// every rune at once, which neither the ':' binding nor the palette's
// one-rune-at-a-time input handler reacts to.
func runCmd(h *harness.Harness, cmd string) {
	h.SendKeys(":")
	time.Sleep(120 * time.Millisecond)
	for _, r := range cmd {
		h.SendKeys(string(r))
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond)
	h.SendKeys("<cr>")
}

// waitForRootList waits for whichever list the app settles on at the root: the
// Jenkins views list, or the job list of a remembered view when this context
// has one.
func waitForRootList(h *harness.Harness) error {
	return h.WaitFor(func(g string) bool {
		return strings.Contains(g, "views(") || strings.Contains(g, "jobs(")
	}, harness.NetworkTimeout)
}

// openAllJobs takes a fresh session to the unfiltered job list — the "all"
// view, which is where the app used to start before the views list became the
// root. Tests that care about jobs rather than views start here.
func openAllJobs(t *testing.T, h *harness.Harness) {
	t.Helper()
	if err := waitForRootList(h); err != nil {
		t.Fatalf("root list never rendered: %v", err)
	}
	runCmd(h, "view all")
	h.MustWaitForText(t, "jobs(all", harness.NetworkTimeout)
}

// TestViewsListIsRoot verifies the app opens on the Jenkins views list and that
// picking a view opens its job list.
func TestViewsListIsRoot(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	if err := waitForRootList(h); err != nil {
		t.Fatalf("root list never rendered: %v", err)
	}

	// Reach the views list explicitly: a remembered view may have opened a job
	// list over it at startup.
	runCmd(h, "views")
	h.MustWaitForText(t, "views(", harness.NetworkTimeout)

	// Every controller has the built-in "all" view.
	if !h.Contains("all") {
		t.Errorf("views list does not show the built-in 'all' view; grid:\n%s", h.Grid())
	}
	h.MustSnapshot(t, "views-list")

	// Opening a view lands on its job list.
	runCmd(h, "view all")
	h.MustWaitForText(t, "jobs(all", harness.NetworkTimeout)
	h.MustSnapshot(t, "views-all-jobs")
}

// TestViewsEscFromJobsReturnsToViews verifies the views list is the parent of
// the root job list: ESC there goes up to views, not out of the app.
func TestViewsEscFromJobsReturnsToViews(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	h.SendKeys("<esc>")
	h.MustWaitForText(t, "views(", harness.NetworkTimeout)
}

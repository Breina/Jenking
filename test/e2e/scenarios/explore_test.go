//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

func TestExploreFixtureNavigation(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath, Context: "ontwikkel"})
	openAllJobs(t, h)

	t.Log("=== ROOT LEVEL ===")
	t.Logf("Grid:\n%s", h.Grid())

	// Try root search for jenkins-e2e
	h.SendKeys("/")
	time.Sleep(100 * time.Millisecond)
	h.SendKeys("jenkins-e2e")
	time.Sleep(2 * time.Second)
	t.Logf("After root search, grid:\n%s", h.Grid())
	h.SendKeys("<esc>") // dismiss search

	t.Log("=== ATTEMPT: Enter first item ===")
	h.SendKeys("<enter>")
	time.Sleep(1 * time.Second)
	t.Logf("After first enter, grid:\n%s", h.Grid())
	h.MustSnapshot(t, "explore-first-item")

	// Go back
	h.SendKeys("<esc>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "jobs(all") }, harness.NetworkTimeout)

	t.Log("=== ATTEMPT: Enter second item (Omgeving) ===")
	h.SendKeys("j") // move to Omgeving
	time.Sleep(200 * time.Millisecond)
	h.SendKeys("<enter>")
	time.Sleep(2 * time.Second) // wait for Omgeving to load
	t.Logf("After entering Omgeving, grid:\n%s", h.Grid())
	h.MustSnapshot(t, "explore-omgeving")

	// Search for jenkins-e2e in Omgeving
	h.SendKeys("/")
	time.Sleep(100 * time.Millisecond)
	h.SendKeys("jenkins-e2e")
	time.Sleep(2 * time.Second)
	t.Logf("After search in Omgeving, grid:\n%s", h.Grid())
	h.MustSnapshot(t, "explore-omgeving-search")

	found := strings.Contains(h.Grid(), "jenkins-e2e")
	t.Logf("jenkins-e2e found in grid: %v", found)

	if found {
		// Navigate into it
		h.SendKeys("<enter>") // exit search mode
		h.SendKeys("<enter>") // navigate into job
		time.Sleep(2 * time.Second)
		t.Logf("After navigating into job, grid:\n%s", h.Grid())
		h.MustSnapshot(t, "explore-job-entered")

		// Try one more enter (into branch)
		if !strings.Contains(h.Grid(), "builds(") {
			h.SendKeys("<enter>")
			time.Sleep(2 * time.Second)
			t.Logf("After entering branch, grid:\n%s", h.Grid())
			h.MustSnapshot(t, "explore-branch-entered")
		}
	}
}

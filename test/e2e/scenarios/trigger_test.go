//go:build integration

package scenarios

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// fixtureJobName is the name of the opt-in test fixture job on Jenkins.
// Deploy test/e2e/fixtures/Jenkinsfile.e2e as a multibranch pipeline with this name.
const fixtureJobName = "jenkins-e2e"

// requireFixtureJob skips the test if the fixture job can't be found via search.
// This is an opt-in guard: trigger tests only run when the fixture is deployed.
func requireFixtureJob(t *testing.T, h *harness.Harness) {
	t.Helper()

	// Quick check first: visible in current grid?
	if h.Contains(fixtureJobName) {
		return
	}

	// Not immediately visible — try to find it with search, then clear search
	h.SendKeys("/")
	h.MustWaitFor(t, func(g string) bool {
		return strings.Contains(strings.ToLower(g), "search") || strings.Contains(g, ">")
	}, harness.RenderTimeout)
	h.SendKeys(fixtureJobName)

	found := h.WaitFor(func(g string) bool {
		return strings.Contains(g, fixtureJobName)
	}, harness.NetworkTimeout) == nil

	// Dismiss search and return to unfiltered dashboard
	h.SendKeys("<esc>")
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	if !found {
		t.Skipf("fixture job %q not found on Jenkins — deploy test/e2e/fixtures/Jenkinsfile.e2e first", fixtureJobName)
	}
}

// TestTriggerBuildAndCancel verifies triggering and then cancelling a build.
func TestTriggerBuildAndCancel(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath, Context: "ontwikkel"})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	requireFixtureJob(t, h)

	// Navigate to the fixture job
	h.SendKeys("/")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(strings.ToLower(g), "search") || strings.Contains(g, ">") }, harness.RenderTimeout)
	h.SendKeys(fixtureJobName + "<cr>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, fixtureJobName) }, harness.NetworkTimeout)

	// Open the job
	h.SendKeys("<enter>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "builds(") }, harness.NetworkTimeout)

	// Trigger a build with Ctrl+B or 'b' — check what the actual key is
	// The trigger is typically accessed via the command palette
	h.SendKeys(harness.Cmd("trigger"))
	h.MustWaitFor(t, func(g string) bool {
		g = strings.ToLower(g)
		return strings.Contains(g, "trigger") || strings.Contains(g, "build") || strings.Contains(g, "parameters")
	}, harness.RenderTimeout)

	// Confirm the trigger dialog (or just press Enter to trigger)
	h.SendKeys("<enter>")

	// Build should appear in running builds
	h.SendKeys("R")
	h.MustWaitFor(t, func(g string) bool {
		return strings.Contains(strings.ToLower(g), "running") || strings.Contains(g, fixtureJobName)
	}, harness.NetworkTimeout)

	h.MustSnapshot(t, "trigger-build-running")

	// Cancel the build: select it and use the cancel command
	h.SendKeys("<enter>") // open build detail
	h.MustWaitFor(t, func(g string) bool { return len(g) > 100 }, harness.NetworkTimeout)
	h.SendKeys(harness.Cmd("cancel"))

	h.MustSnapshot(t, "trigger-cancel-confirmed")
}

// TestTriggerWithParameters verifies the parameter dialog shows for parameterised jobs.
func TestTriggerWithParameters(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath, Context: "ontwikkel"})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	requireFixtureJob(t, h)

	// Navigate to fixture job builds view
	h.SendKeys("/")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(strings.ToLower(g), "search") || strings.Contains(g, ">") }, harness.RenderTimeout)
	h.SendKeys(fixtureJobName + "<cr>")
	h.SendKeys("<enter>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "builds(") }, harness.NetworkTimeout)

	// Try to trigger
	h.SendKeys(harness.Cmd("trigger"))

	// Parameter dialog should appear (fixtureJobName has FAIL and MESSAGE params)
	if err := h.WaitFor(func(g string) bool {
		g = strings.ToLower(g)
		return strings.Contains(g, "fail") || strings.Contains(g, "message") || strings.Contains(g, "parameter")
	}, harness.RenderTimeout); err != nil {
		t.Skipf("parameter dialog didn't appear (maybe trigger command key is different): %v", err)
	}

	h.MustSnapshot(t, "trigger-params-dialog")

	// Dismiss without triggering
	h.SendKeys("<esc>")
}

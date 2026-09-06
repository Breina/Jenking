//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestRunningBuildsView verifies the 'R' view opens and renders.
func TestRunningBuildsView(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	h.SendKeys("R")

	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "running")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("running builds view didn't appear: %v", err)
	}

	h.MustSnapshot(t, "running-view")
}

// TestRunningBuildsScrolling verifies scrolling doesn't crash the view.
func TestRunningBuildsScrolling(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)
	h.SendKeys("R")
	h.MustWaitFor(t, func(grid string) bool {
		return strings.Contains(strings.ToLower(grid), "running")
	}, harness.NetworkTimeout)

	for i := 0; i < 10; i++ {
		h.SendKeys("j")
	}
	for i := 0; i < 10; i++ {
		h.SendKeys("k")
	}

	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(strings.ToLower(grid), "running")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("running view disappeared after scrolling: %v", err)
	}
}

// TestRunningBuildsEscReturns verifies Esc returns to the dashboard.
func TestRunningBuildsEscReturns(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)
	h.SendKeys("R")
	h.MustWaitFor(t, func(grid string) bool {
		return strings.Contains(strings.ToLower(grid), "running")
	}, harness.NetworkTimeout)

	h.SendKeys("<esc>")
	h.MustWaitForText(t, "jobs(", harness.RenderTimeout)
}

// TestRunningBuildsMonitorRace exercises the background poll goroutine by
// repeatedly opening/closing the running builds view quickly.
// This test is run with -race to detect data races in internal/monitor/running.go.
func TestRunningBuildsMonitorRace(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	for i := 0; i < 5; i++ {
		h.SendKeys("R")
		time.Sleep(200 * time.Millisecond)
		h.SendKeys("<esc>")
		time.Sleep(100 * time.Millisecond)
	}

	h.MustWaitForText(t, "jobs(", harness.RenderTimeout)
}

// TestRunningBuildsResizeDuringPoll verifies resize while the monitor is polling.
func TestRunningBuildsResizeDuringPoll(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)
	h.SendKeys("R")
	h.MustWaitFor(t, func(grid string) bool {
		return strings.Contains(strings.ToLower(grid), "running")
	}, harness.NetworkTimeout)

	// Resize during active poll
	h.Resize(80, 20)
	time.Sleep(150 * time.Millisecond)
	h.Resize(160, 40)

	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(strings.ToLower(grid), "running")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("running view lost after resize during poll: %v", err)
	}
	h.MustSnapshot(t, "running-after-resize")
}

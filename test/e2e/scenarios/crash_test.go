//go:build integration

package scenarios

import (
	"testing"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestNoPanicsAfterAllNavigation does a rapid tour of the UI and confirms
// that the debug.log contains no panic signatures. This is a catch-all that
// exercises multiple views in a single harness lifetime.
func TestNoPanicsAfterAllNavigation(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	// Tour: running builds
	h.SendKeys("R")
	h.MustWaitFor(t, func(g string) bool {
		return len(g) > 0
	}, harness.RenderTimeout)
	h.SendKeys("<esc>")
	openAllJobs(t, h)

	// Tour: my builds
	h.SendKeys("m")
	h.MustWaitFor(t, func(g string) bool {
		return len(g) > 0
	}, harness.RenderTimeout)
	h.SendKeys("<esc>")
	openAllJobs(t, h)

	// Tour: command palette open/close
	h.SendKeys(":")
	h.MustWaitFor(t, func(g string) bool { return len(g) > 0 }, harness.RenderTimeout)
	h.SendKeys("<esc>")

	// Tour: search open/close
	h.SendKeys("/abc<esc>")

	// Tour: drill in and back out several times
	for i := 0; i < 3; i++ {
		h.SendKeys("<enter>")
		h.MustWaitFor(t, func(g string) bool { return len(g) > 0 }, harness.NetworkTimeout)
		h.SendKeys("<esc>")
		h.MustWaitFor(t, func(g string) bool { return len(g) > 0 }, harness.RenderTimeout)
	}

	// Stop and let the cleanup in harness.New check for panics in debug.log.
	// The test will fail in t.Cleanup if panics are found.
}

// TestKeyMashNoPanic rapid-fires a mix of keys to probe for edge cases.
func TestKeyMashNoPanic(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	openAllJobs(t, h)

	// Rapid alternating key presses
	keys := "jjjjkkkkjjkk<enter><esc><enter><esc>jjj<enter>lll<esc><esc>"
	h.SendKeys(keys)

	// App should still render something after key mash
	if err := h.WaitFor(func(g string) bool {
		return len(g) > 100
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("app stopped responding after key mash: %v", err)
	}

	h.MustSnapshot(t, "keymash-final")
}

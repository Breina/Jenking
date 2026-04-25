//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// openConsoleView navigates to the console output of the first available build.
func openConsoleView(t *testing.T) *harness.Harness {
	t.Helper()
	h := openFixtureBuilds(t)

	if !h.Contains("builds(") {
		t.Skip("no builds view")
	}

	// Open the first build (gets stage view or console)
	h.SendKeys("<enter>")

	if _, err := h.WaitForAny(harness.NetworkTimeout,
		"stages(", "console("); err != nil {
		t.Skipf("no build detail view: %v", err)
	}

	// If we're in stage view, press 'l' to go to console/log
	if h.Contains("stages(") {
		h.SendKeys("l")
		if err := h.WaitFor(func(grid string) bool {
			return strings.Contains(grid, "console(") || strings.Contains(grid, "log(")
		}, harness.NetworkTimeout); err != nil {
			t.Skipf("could not open console from stage view: %v", err)
		}
	}

	return h
}

// TestConsoleViewRenders verifies the console view shows output.
func TestConsoleViewRenders(t *testing.T) {
	h := openConsoleView(t)

	grid := h.Grid()
	if !strings.Contains(grid, "console(") && !strings.Contains(grid, "log(") {
		t.Fatalf("console view panel missing; grid:\n%s", grid)
	}
	h.MustSnapshot(t, "console-renders")
}

// TestConsoleViewScrolling verifies scrolling through console output doesn't crash.
func TestConsoleViewScrolling(t *testing.T) {
	h := openConsoleView(t)

	// Scroll to bottom
	h.SendKeys("<pgdn><pgdn><pgdn><end>")
	time.Sleep(200 * time.Millisecond)

	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "console") || strings.Contains(g, "log")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("console view disappeared after scrolling to bottom: %v", err)
	}

	// Scroll back to top
	h.SendKeys("<pgup><pgup><pgup><home>")
	h.MustSnapshot(t, "console-scrolled-top")
}

// TestConsoleViewRapidEsc verifies that pressing Esc almost immediately after
// opening the console view (while the log stream is in-flight) doesn't cause
// a nil-pointer panic in nodelogfetcher.
func TestConsoleViewRapidEsc(t *testing.T) {
	h := openFixtureBuilds(t)
	if !h.Contains("builds(") {
		t.Skip("no builds view")
	}

	// Open build
	h.SendKeys("<enter>")
	time.Sleep(30 * time.Millisecond) // before it even loads

	// Esc out immediately
	h.SendKeys("<esc>")

	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "builds(") || strings.Contains(grid, "jobs(")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("app stuck after rapid Esc on console open: %v", err)
	}
}

// TestConsoleViewResizeDuringStream verifies resize during log streaming doesn't crash.
func TestConsoleViewResizeDuringStream(t *testing.T) {
	h := openConsoleView(t)

	// Resize mid-stream
	h.Resize(40, 15)
	time.Sleep(100 * time.Millisecond)
	h.Resize(160, 40)

	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "console") || strings.Contains(g, "log")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("console view lost after resize: %v", err)
	}
	h.MustSnapshot(t, "console-after-resize")
}

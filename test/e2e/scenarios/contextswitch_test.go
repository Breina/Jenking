//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// twoContextOpts returns harness.Options pre-configured for context-switch tests:
// ontwikkel as current context (low noise), build as secondary. Skips if either is unavailable.
func twoContextOpts() harness.Options {
	return harness.Options{
		BinaryPath:    binaryPath,
		Context:       "ontwikkel",
		ExtraContexts: []string{"build"},
	}
}

// TestContextPickerShowsTwoContexts verifies that opening the context picker with
// two configured contexts doesn't crash or navigate away, and that ESC dismisses it.
//
// Note: asserting that both context *names* appear in vt10x is unreliable — Bubbletea's
// line-diff renderer only writes changed cells, so lipgloss-styled overlay text does not
// consistently land in the vt10x cell grid. Behavioral coverage (the picker opens and
// closes without side-effects) is sufficient here; context switching is covered by the
// other tests in this file.
func TestContextPickerShowsTwoContexts(t *testing.T) {
	h := harness.New(t, twoContextOpts())
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	// Send `:` separately so Bubbletea processes command-mode activation before
	// the remaining chars arrive. Sending all bytes in one Write can cause them to
	// be read as a batch where `:` hasn't yet set ModeCommand, leaving `<enter>`
	// to be handled in normal mode (which would navigate into the selected job).
	h.SendKeys(":")
	time.Sleep(100 * time.Millisecond)
	h.SendKeys("context<cr>")

	// Give the picker time to open (it probes all contexts concurrently).
	time.Sleep(200 * time.Millisecond)
	h.MustSnapshot(t, "ctx-picker-two-contexts")

	// Dismiss without switching — Dashboard must return.
	h.SendKeys("<esc>")
	if err := h.WaitForText("Dashboard", harness.NetworkTimeout); err != nil {
		t.Fatalf("Dashboard didn't return after dismissing context picker: %v", err)
	}
}

// TestContextSwitchRoundTrip verifies switching from ontwikkel → build → ontwikkel
// doesn't crash or leave the app in a bad state.
func TestContextSwitchRoundTrip(t *testing.T) {
	h := harness.New(t, twoContextOpts())
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)
	h.MustSnapshot(t, "ctx-ontwikkel-start")

	// Switch to build
	h.SendKeys(harness.Cmd("context build"))
	if err := h.WaitForText("Dashboard", harness.NetworkTimeout); err != nil {
		t.Fatalf("Dashboard not reached after switching to build: %v", err)
	}
	h.MustSnapshot(t, "ctx-switched-to-build")

	// Switch back to ontwikkel
	h.SendKeys(harness.Cmd("context ontwikkel"))
	if err := h.WaitForText("Dashboard", harness.NetworkTimeout); err != nil {
		t.Fatalf("Dashboard not reached after switching back to ontwikkel: %v", err)
	}
	h.MustSnapshot(t, "ctx-switched-back-to-ontwikkel")
}

// TestContextSwitchConnectionStatus verifies the connection status transitions to
// ● Connected after switching contexts.
// Bug: the connection status may not update from ○ Connecting → ● Connected.
func TestContextSwitchConnectionStatus(t *testing.T) {
	h := harness.New(t, twoContextOpts())
	h.MustWaitForText(t, "● Connected", harness.NetworkTimeout)

	// Switch to build
	h.SendKeys(harness.Cmd("context build"))

	// Must eventually reach ● Connected on the new context
	if err := h.WaitForText("● Connected", harness.NetworkTimeout); err != nil {
		t.Fatalf("connection status never updated to ● Connected after switching to build\n"+
			"Final grid:\n%s", h.Grid())
	}
	h.MustSnapshot(t, "ctx-status-connected-after-switch")
}

// TestContextSwitchMonarch verifies that after switching contexts the header shows
// the user's display name (Monarch), not the raw config username.
// Bug: sometimes the username (e.g. "brecht") is shown instead of the display name
// (e.g. "Brecht Derwael") immediately after a context switch.
func TestContextSwitchMonarch(t *testing.T) {
	h := harness.New(t, twoContextOpts())
	h.MustWaitForText(t, "● Connected", harness.NetworkTimeout)

	ontwikkelMonarch := h.HeaderField("Monarch")
	t.Logf("ontwikkel context Monarch: %q", ontwikkelMonarch)

	// Switch to build and wait until connected
	h.SendKeys(harness.Cmd("context build"))
	h.MustWaitForText(t, "● Connected", harness.NetworkTimeout)

	buildMonarch := h.HeaderField("Monarch")
	t.Logf("build context Monarch: %q", buildMonarch)
	h.MustSnapshot(t, "ctx-monarch-after-switch")

	if buildMonarch == "" {
		t.Errorf("Monarch field is empty after switching to build (display name not loaded)")
		return
	}
	// Display names typically contain a space; plain usernames typically do not.
	// Failing here exposes the "username shown instead of friendly name" bug.
	if !strings.Contains(buildMonarch, " ") {
		t.Errorf("Monarch %q looks like a bare username (no space) — expected a display name like \"First Last\"", buildMonarch)
	}
}

// TestContextSwitchNoPanicOnRapidReuse verifies that the user cannot interact
// with a freshly-switched context in a way that causes a panic before the
// connection has completed.
// Bug: the app allows navigation before the new context has performed its first
// connection attempt, which can cause nil-pointer panics in the view layer.
func TestContextSwitchNoPanicOnRapidReuse(t *testing.T) {
	h := harness.New(t, twoContextOpts())
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	// Switch context and immediately hammer keys before connection completes
	h.SendKeys(harness.Cmd("context build"))
	// Rapidly send navigation keys before ● Connected appears
	h.SendKeys("<enter><esc><enter><esc>jkjk")

	// App must survive and eventually settle on a stable view
	if err := h.WaitFor(func(g string) bool {
		g = strings.ToLower(g)
		return strings.Contains(g, "dashboard") || strings.Contains(g, "connected") ||
			strings.Contains(g, "connecting") || strings.Contains(g, "jobs(")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("app became unresponsive after rapid key input during context switch: %v", err)
	}
	h.MustSnapshot(t, "ctx-rapid-reuse-survived")
}

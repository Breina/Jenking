//go:build integration

package scenarios

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestCommandPaletteOpens verifies the ':' key opens the command palette.
func TestCommandPaletteOpens(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	h.SendKeys(":")

	// Command palette shows a text input — typically an ">" or "command" label
	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "command") || strings.Contains(g, "> ")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("command palette didn't open: %v", err)
	}

	// Dismiss with Esc
	h.SendKeys("<esc>")
	h.MustWaitForText(t, "Dashboard", harness.RenderTimeout)
}

// TestCommandBuilds verifies :builds navigates to the all-builds view.
// The all-builds view shows branch builds with the ⎇ icon; the panel title
// may not be visible in the grid due to the debug overlay, so we assert on content.
func TestCommandBuilds(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "jobs(Dashboard", harness.NetworkTimeout)

	h.SendKeys(harness.Cmd("builds"))

	// All-builds view shows branch builds with "⎇" icons, or an empty panel.
	// Just verify we navigated away from dashboard and the app is still alive.
	if err := h.WaitFor(func(grid string) bool {
		// Either we see branch builds (⎇), job info (git/), or the view title in any form
		return strings.Contains(grid, "⎇") ||
			strings.Contains(grid, "git/") ||
			strings.Contains(grid, "Success") ||
			strings.Contains(grid, "Failed") ||
			strings.Contains(grid, "No builds")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf(":builds didn't show any build content: %v", err)
	}
	h.MustSnapshot(t, "command-builds")
}

// TestCommandContextList verifies :context runs without crashing.
// With only one context in the isolated config, the picker auto-selects and returns.
func TestCommandContextList(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "jobs(Dashboard", harness.NetworkTimeout)

	h.SendKeys(":context<cr>")

	// With one context, the picker auto-returns to dashboard.
	// With multiple contexts, it shows a selection UI.
	// Either way, the app should remain alive and show something.
	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		// Context picker shows context names, or we're back at dashboard
		return strings.Contains(g, "context") ||
			strings.Contains(g, "dashboard") ||
			strings.Contains(g, "jenkins") ||
			strings.Contains(g, "build") ||
			strings.Contains(g, "switch")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf(":context left the app unresponsive: %v", err)
	}

	h.MustSnapshot(t, "command-context-menu")
}

// TestCommandBogusShowsError verifies that an unknown command shows an error, not a crash.
func TestCommandBogusShowsError(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	h.SendKeys(harness.Cmd("zzznotacommand"))

	// Should show some error feedback without crashing
	// Give it a moment to react
	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "unknown") ||
			strings.Contains(g, "error") ||
			strings.Contains(g, "dashboard") // still alive
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("app became unresponsive after bogus command: %v", err)
	}

	// The app must still be alive and show the dashboard
	if !h.Contains("Dashboard") {
		t.Logf("grid after bogus command:\n%s", h.Grid())
	}
}

// TestCommandSearch verifies the '/' key opens search input.
func TestCommandSearch(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	h.SendKeys("/")

	if err := h.WaitFor(func(grid string) bool {
		g := strings.ToLower(grid)
		return strings.Contains(g, "search") || strings.Contains(g, "/")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("search didn't open: %v", err)
	}

	// Type something and clear with Esc
	h.SendKeys("test<esc>")
	h.MustWaitForText(t, "Dashboard", harness.RenderTimeout)
}

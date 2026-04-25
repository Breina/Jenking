//go:build integration

package scenarios

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestResizeTiny verifies the app survives a resize to a very small terminal (20x10).
func TestResizeTiny(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	h.Resize(20, 10)

	// App should still be alive (not panic) — we can't assert much about layout
	// at 20x10, but the process must not crash.
	if err := h.WaitFor(func(grid string) bool {
		// Any non-empty output means the app is still running
		return strings.TrimSpace(grid) != ""
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("app stopped rendering after resize to 20x10: %v", err)
	}

	// Check no panic in debug log
	h.MustSnapshot(t, "resize-tiny")
}

// TestResizeWide verifies the app handles a very wide terminal (300x80).
func TestResizeWide(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	h.Resize(300, 80)

	if err := h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, "Dashboard")
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("dashboard missing after resize to 300x80: %v", err)
	}
	h.MustSnapshot(t, "resize-wide")
}

// TestResizeSqaut verifies the app handles a squat terminal (80x5).
// This is likely to expose stageview row overflow bugs.
func TestResizeSquat(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	h.Resize(80, 5)

	if err := h.WaitFor(func(grid string) bool {
		return strings.TrimSpace(grid) != ""
	}, harness.RenderTimeout); err != nil {
		t.Fatalf("app stopped rendering after resize to 80x5: %v", err)
	}
	h.MustSnapshot(t, "resize-squat")
}

// TestResizeChurn verifies the app handles rapid resize cycles without panicking.
func TestResizeChurn(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath})
	h.MustWaitForText(t, "Dashboard", harness.NetworkTimeout)

	// Rapid resize cycle
	sizes := [][2]int{
		{40, 20}, {160, 40}, {80, 5}, {200, 60}, {60, 15}, {160, 40},
	}
	for _, s := range sizes {
		h.Resize(s[0], s[1])
	}

	// Restore normal size and check we're still alive
	h.Resize(160, 40)

	if err := h.WaitForText("Dashboard", harness.NetworkTimeout); err != nil {
		t.Fatalf("dashboard lost after resize churn: %v", err)
	}
	h.MustSnapshot(t, "resize-churn-final")
}

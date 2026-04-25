//go:build integration

package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Snapshot saves the current grid to the snapshots directory and returns the path.
// name is an optional human-readable label; a timestamp is appended automatically.
func (h *Harness) Snapshot(name string) string {
	grid := h.Grid()
	if name == "" {
		name = "snap"
	}
	ts := time.Now().Format("20060102-150405.000")
	filename := fmt.Sprintf("%s-%s.txt", name, ts)

	// Resolve snapshots dir relative to the binary location or CWD
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.t.Logf("harness: cannot create snapshots dir %s: %v", dir, err)
	}
	path := filepath.Join(dir, filename)

	header := fmt.Sprintf("=== %s  %s ===\n", name, ts)
	if err := os.WriteFile(path, []byte(header+grid), 0644); err != nil {
		if h.t != nil {
			h.t.Logf("harness: cannot write snapshot %s: %v", path, err)
		} else {
			fmt.Fprintf(os.Stderr, "harness: cannot write snapshot %s: %v\n", path, err)
		}
	}
	return path
}

// MustSnapshot calls Snapshot and logs the path.
func (h *Harness) MustSnapshot(t *testing.T, name string) string {
	t.Helper()
	path := h.Snapshot(name)
	t.Logf("snapshot saved: %s", path)
	return path
}

// DiffSnapshots returns a line-diff of two snapshot files.
func DiffSnapshots(a, b string) string {
	dataA, errA := os.ReadFile(a)
	dataB, errB := os.ReadFile(b)
	if errA != nil {
		return fmt.Sprintf("cannot read %s: %v", a, errA)
	}
	if errB != nil {
		return fmt.Sprintf("cannot read %s: %v", b, errB)
	}

	linesA := strings.Split(string(dataA), "\n")
	linesB := strings.Split(string(dataB), "\n")

	var sb strings.Builder
	maxLen := len(linesA)
	if len(linesB) > maxLen {
		maxLen = len(linesB)
	}
	diffs := 0
	for i := 0; i < maxLen; i++ {
		var la, lb string
		if i < len(linesA) {
			la = linesA[i]
		}
		if i < len(linesB) {
			lb = linesB[i]
		}
		if la != lb {
			fmt.Fprintf(&sb, "row %3d - %s\n", i+1, la)
			fmt.Fprintf(&sb, "row %3d + %s\n", i+1, lb)
			diffs++
		}
	}
	if diffs == 0 {
		return "(identical)"
	}
	return sb.String()
}

func snapshotDir() string {
	// Walk up from CWD to find test/e2e/snapshots
	cwd, _ := os.Getwd()
	for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "test", "e2e", "snapshots")
		if _, err := os.Stat(filepath.Join(dir, "test", "e2e")); err == nil {
			return candidate
		}
	}
	return filepath.Join(cwd, "snapshots")
}

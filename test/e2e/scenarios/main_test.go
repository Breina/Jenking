//go:build integration

// Package scenarios contains end-to-end integration tests for jenking.
// Run with: go test -tags=integration -race ./test/e2e/scenarios/...
//
// Prerequisites:
//   - A jenking config at ~/.config/jenking/config.yaml with an "ontwikkel" context
//   - The jenking binary is built fresh by TestMain before each run
package scenarios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// binaryPath is the path to the jenking binary under test, set by TestMain.
var binaryPath string

func TestMain(m *testing.M) {
	// Build the jenking binary fresh into a temp location so we always test
	// the current source, not whatever was previously built.
	binDir, err := os.MkdirTemp("", "jenking-e2e-*")
	if err != nil {
		panic("cannot create temp dir for binary: " + err.Error())
	}
	defer os.RemoveAll(binDir)

	bin := filepath.Join(binDir, "jenking")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	// Find repo root: walk up from this file until we see go.mod
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := findRepoRoot(thisFile)
	if repoRoot == "" {
		panic("cannot find repo root (no go.mod found)")
	}

	cmd := exec.Command("go", "build", "-o", bin, "./cmd/jenking")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("cannot build jenking: " + err.Error())
	}

	binaryPath = bin
	os.Exit(m.Run())
}

func findRepoRoot(fromFile string) string {
	dir := filepath.Dir(fromFile)
	for dir != "/" && dir != "." {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

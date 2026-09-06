package main

import (
	"os/exec"
	"strings"
)

// gitRemoteURL returns the `origin` remote URL for the git repo containing dir
// ("" = current directory). It returns "" (no error) when dir is not a git repo
// or has no origin remote — callers treat that as "no repo to resolve".
func gitRemoteURL(dir string) string {
	return gitOutput(dir, "remote", "get-url", "origin")
}

// gitCurrentBranch returns the checked-out branch name for the repo containing
// dir, or "" when unavailable (detached HEAD, not a repo).
func gitCurrentBranch(dir string) string {
	b := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if b == "HEAD" { // detached
		return ""
	}
	return b
}

// gitOutput runs `git <args...>` in dir and returns trimmed stdout, or "" on any
// error (git absent, not a repo, non-zero exit).
func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

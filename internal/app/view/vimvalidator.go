package view

import (
	"context"
	"os"
	"sync/atomic"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// jenkinsfileValidator is the narrow capability runVimValidator needs from
// jmodel.JenkinsClient. Defined as its own interface so tests can pass a
// one-method stub.
type jenkinsfileValidator interface {
	ValidateJenkinsfile(ctx context.Context, content string) (jmodel.ValidationResult, error)
}

// runVimValidator runs in the background while the user is editing. It polls
// the sentinel file at rt.bufferTmp (written by vim's BufWritePost autocmd),
// validates each new revision via Jenkins' pipeline-model-converter, and
// writes the resulting issues to rt.errorsQF in vim errorformat. Vim's own
// timer-driven jenking#ReloadQF picks the file up and populates the quickfix
// list so the user can navigate errors with :cnext / :cprev.
//
// The loop stops as soon as done is closed (called from the tea.ExecProcess
// callback when the editor process exits). The ctx exists only to cancel
// outstanding HTTP requests during shutdown.
//
// pollInterval is exposed so tests can override the 500ms default.
func runVimValidator(ctx context.Context, client jenkinsfileValidator, rt *vimRuntime, done <-chan struct{}, pollInterval time.Duration) {
	if rt == nil || client == nil {
		return
	}
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	var lastMTime atomic.Int64
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-tick.C:
		}

		info, err := os.Stat(rt.bufferTmp)
		if err != nil {
			continue
		}
		m := info.ModTime().UnixNano()
		if m == lastMTime.Load() {
			continue
		}
		lastMTime.Store(m)

		content, err := os.ReadFile(rt.bufferTmp)
		if err != nil {
			continue
		}
		res, err := client.ValidateJenkinsfile(ctx, string(content))
		if err != nil {
			// Transport failure — surface as a single quickfix entry so the
			// user notices Jenkins is unreachable rather than silently
			// believing validation passed.
			writeErrorsQF(rt.errorsQF, jmodel.ValidationResult{
				Issues: []jmodel.ValidationIssue{{Message: "validator: " + err.Error()}},
			}, rt.bufferTmp)
			continue
		}
		writeErrorsQF(rt.errorsQF, res, rt.bufferTmp)
	}
}

// writeErrorsQF writes the errorformat-formatted issue list to path, atomically
// (temp file + rename). For a clean result, writes an empty file so vim's
// :cgetfile clears the quickfix list.
func writeErrorsQF(path string, res jmodel.ValidationResult, file string) {
	formatted := res.FormatErrorformat(file)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(formatted), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

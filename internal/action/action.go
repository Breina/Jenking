// Package action contains headless executors for read-only verbs that bypass
// the TUI and write their output directly to a writer (typically os.Stdout).
//
// The CLI mode is opt-in via the `--raw` flag in cmd/jenking/main.go. Each
// kind maps to a single Jenkins API call and writes the result verbatim:
//
//	logs     -> full console text
//	describe -> Jenkinsfile / replay script
//	tests    -> JUnit test report as indented JSON
//
// Project-suffix resolution uses the same cache walk as the TUI; on a cold
// start without a disk-persisted Jobs cache, only full project paths will
// resolve.
package action

import (
	"context"
	"fmt"
	"io"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/view"
)

// Kind selects the headless executor.
type Kind int

const (
	KindLogs Kind = iota
	KindDescribe
	KindTests
)

// ParseKind returns the Kind for a CLI verb name (and its short aliases).
func ParseKind(verb string) (Kind, bool) {
	switch verb {
	case "logs", "log", "l":
		return KindLogs, true
	case "describe", "desc":
		return KindDescribe, true
	case "tests", "test":
		return KindTests, true
	}
	return 0, false
}

// Request captures the verb and target for a single headless invocation.
type Request struct {
	Kind   Kind
	Target command.Target
}

// apiClient is the narrow subset of jenkins.JenkinsClient that headless
// executors call. Defined locally so tests can supply a small fake without
// implementing the full 20+ method interface.
type apiClient interface {
	ListBuilds(ctx context.Context, jobPath string) ([]jenkins.Build, error)
	GetFullConsoleText(ctx context.Context, jobPath string, number int) (string, error)
	GetBuildScript(ctx context.Context, jobPath string, buildNumber int) (string, error)
	GetTestReport(ctx context.Context, jobPath string, buildNum int) (*jenkins.TestReport, error)
}

// Run resolves the request's target against the cache and dispatches to the
// appropriate executor. It writes the executor's output to w and returns any
// error encountered.
func Run(ctx context.Context, client jenkins.JenkinsClient, store *cache.Store, req Request, w io.Writer) error {
	return runWith(ctx, client, store, req, w)
}

func runWith(ctx context.Context, client apiClient, store *cache.Store, req Request, w io.Writer) error {
	nc, err := view.ResolveTarget(req.Target, store, view.NavigationContext{})
	if err != nil {
		return err
	}
	if nc.ProjectName == "" {
		return fmt.Errorf("project required (no current view in headless mode)")
	}

	jobPath := nc.JobPath()
	if jobPath == "" {
		return fmt.Errorf("could not determine job path")
	}

	buildNum, err := resolveBuildNumber(ctx, client, jobPath, nc.Build)
	if err != nil {
		return err
	}

	switch req.Kind {
	case KindLogs:
		return runLogs(ctx, client, jobPath, buildNum, w)
	case KindDescribe:
		return runDescribe(ctx, client, jobPath, buildNum, w)
	case KindTests:
		return runTests(ctx, client, jobPath, buildNum, w)
	}
	return fmt.Errorf("unknown action kind: %d", req.Kind)
}

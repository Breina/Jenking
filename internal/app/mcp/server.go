// Package mcp exposes Jenking's headless usecase layer as a Model Context
// Protocol server (stdio transport). It is a thin driver over usecase.Deps: the
// same resolution, fetching, and (later) mutation logic that backs the CLI. All
// go-sdk types are isolated here so the rest of the app never imports the SDK.
package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
)

// instructions teaches connected clients how to drive Jenking effectively.
const instructions = `Jenking exposes a single Jenkins controller as tools.

Conventions:
- job_path is the full folder path, slash-separated, e.g. "TeamA/service/main".
  For multibranch projects the branch is a path segment (".../main").
- build_number is optional on build-scoped tools; omit it for the latest build.
- Results are structured JSON. Timestamps are RFC3339; durations are seconds.

Which job is this repo? Run "git remote get-url origin" in the working copy,
then call resolve_job with that URL to get the Jenkins job path(s). It reads a
warm index (populated as builds run; no live scan), so a never-observed pipeline
may not resolve yet.

Waiting for a build to finish: call wait_for_build — do NOT sleep and re-poll
list_running/get_build in a loop. It blocks server-side and returns the moment
the build settles (or pauses for input), sizing its own wait to the build's
estimatedDuration. If it returns timed_out=true, wait check_back_in_seconds and
call it again.

Waiting for a just-pushed commit to be picked up: call wait_for_new_build — it
blocks until the job starts building (a new build number, or an item in the
queue). A queued build has no number or change set yet, so it matches new
activity, not a specific commit; once you have the build_number, confirm the
commit with get_changes and chain into wait_for_build to await completion.

Waiting for something to appear in a log: call wait_for_log_match with a regex —
it follows the log as it is written and returns the moment the pattern matches,
instead of re-fetching get_logs in a sleep loop. It follows a build's console
(optionally one stage) or, with source="scan", a repository scan log. If it
returns complete=true with matched=false the log ended and the pattern will
never appear; timed_out=true means call again.

Verifying commits: use find_commit to learn which recent builds contain a
commit (one call scans many builds), and get_changes to list the commits in a
specific build.

Editing a Jenkinsfile: call get_pipeline_symbols to discover the exact steps,
globals, and keywords available (resolved against the build's shared libraries)
instead of guessing; edit; lint_pipeline to validate; then replay_build. Console
logs from get_logs are written to a file — grep that file with your own shell
rather than loading it into context.

Mutating tools (trigger_build, replay_build, cancel_build, approve_input, …)
change Jenkins state. They may be absent when the server runs read-only.`

// Server wraps a configured go-sdk server bound to Jenking's usecase layer.
type Server struct {
	deps usecase.Deps
	srv  *mcp.Server
	caps *capabilities
}

// NewServer builds an MCP server exposing the read tools over deps. version is
// reported in the MCP initialize handshake. When readOnly is set, the mutating
// tools are not registered at all, so a read-only client cannot even see them.
func NewServer(deps usecase.Deps, version string, readOnly bool) *Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "jenking", Title: "Jenking (Jenkins)", Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	s := &Server{deps: deps, srv: srv, caps: newCapabilities()}
	s.registerReadTools()
	if !readOnly {
		s.registerMutateTools()
	}
	return s
}

// Serve runs the server over stdio until ctx is cancelled or the client
// disconnects.
func (s *Server) Serve(ctx context.Context) error {
	return s.srv.Run(ctx, &mcp.StdioTransport{})
}

// readOnly is the annotation shared by every read tool.
func readOnlyHint() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true}
}

// mapSlice converts a slice with a per-element converter.
func mapSlice[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

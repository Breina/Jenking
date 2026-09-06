package mcp

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
)

// followDefaultTimeout bounds a single wait_for_log_match call when the caller
// gives none: long enough to outlast a quiet stretch of a build, short enough
// that a wedged follow returns control.
const followDefaultTimeout = 10 * time.Minute

// registerFollowTools wires the regex log-follow tool.
func (s *Server) registerFollowTools() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "wait_for_log_match",
		Description: "Block until a pattern appears in a log — instead of re-fetching get_logs in a sleep loop. " +
			"It follows the log as it is written and returns the instant regex (RE2 syntax; prefix (?i) for case-insensitive) " +
			"matches, reporting the matched text, the whole line, its line number and byte offset. " +
			"Use it to wait for a specific phase, a prompt, or the first sign of a known failure while a build is still running. " +
			"Set source=\"scan\" to follow a multibranch/organization folder's repository scan log instead of a build's console " +
			"(then build_number and stage do not apply); set stage to follow one pipeline stage. " +
			"If the log ends before the pattern shows up, matched=false with complete=true — no more output is coming. " +
			"If the wait bound is hit first, timed_out=true: wait check_back_in_seconds and call again. " +
			"The log itself is written to a file (path) as with get_logs, never returned inline. Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in waitForLogMatchIn) (*mcp.CallToolResult, waitForLogMatchOut, error) {
		re, err := regexp.Compile(in.Pattern)
		if err != nil {
			return nil, waitForLogMatchOut{}, fmt.Errorf("invalid pattern: %w", err)
		}
		scan := in.Source == "scan"
		if in.Source != "" && in.Source != "scan" && in.Source != "build" {
			return nil, waitForLogMatchOut{}, fmt.Errorf("source must be %q or %q, got %q", "build", "scan", in.Source)
		}
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)

		blockFor := followDefaultTimeout
		if in.TimeoutSeconds > 0 {
			blockFor = time.Duration(in.TimeoutSeconds) * time.Second
		}
		start := time.Now()
		opt := usecase.FollowOptions{
			Pattern:  re,
			Deadline: start.Add(blockFor),
			OnPoll:   followProgress(ctx, req, in.JobPath, start, blockFor),
		}

		var (
			res usecase.FollowResult
			n   int
		)
		if scan {
			res, err = d.FollowScanLog(ctx, in.JobPath, opt)
		} else {
			n, err = d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
			if err != nil {
				return nil, waitForLogMatchOut{}, err
			}
			res, err = d.FollowBuildLog(ctx, in.JobPath, n, in.Stage, opt)
		}
		if err != nil {
			return nil, waitForLogMatchOut{}, err
		}
		return nil, followOut(in.JobPath, n, res, start), nil
	})
}

// followProgress returns the per-poll hook that streams progress notifications
// to a client that asked for them, or nil when it did not.
func followProgress(ctx context.Context, req *mcp.CallToolRequest, jobPath string, start time.Time, blockFor time.Duration) func(int) {
	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	return func(sizeBytes int) {
		_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Message:       fmt.Sprintf("following %s log (%d bytes, no match yet)", jobPath, sizeBytes),
			Progress:      time.Since(start).Seconds(),
			Total:         blockFor.Seconds(),
		})
	}
}

// followOut maps a follow result onto the tool's wire shape, adding the
// check-back hint when the call timed out with the log still live.
func followOut(jobPath string, buildNum int, res usecase.FollowResult, start time.Time) waitForLogMatchOut {
	out := waitForLogMatchOut{
		JobPath:        jobPath,
		BuildNumber:    buildNum,
		Matched:        res.Matched,
		Match:          res.MatchText,
		Line:           res.Line,
		LineNumber:     res.LineNumber,
		OffsetBytes:    res.OffsetBytes,
		Complete:       res.Complete,
		TimedOut:       res.TimedOut,
		ElapsedSeconds: int(time.Since(start).Seconds()),
		Path:           res.File.Path,
		SizeBytes:      res.File.Size,
	}
	if res.TimedOut {
		out.CheckBackInSeconds = int(usecase.FollowPollInterval.Seconds())
	}
	return out
}

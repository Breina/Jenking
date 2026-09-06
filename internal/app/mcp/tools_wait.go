package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/dto"
	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

const (
	// waitPollInterval is how often the server re-checks the build while blocking.
	waitPollInterval = 4 * time.Second
	// waitGraceFactor pads the estimate-derived deadline so a build finishing a
	// little late is still caught within the same call.
	waitGraceFactor = 0.50
	// waitNoEstimateDefault is the block length used when Jenkins reports no
	// estimatedDuration (first-ever build of a job, or a non-pipeline job).
	waitNoEstimateDefault = 10 * time.Minute
	// waitNewBuildDefault is how long wait_for_new_build blocks by default:
	// SCM-poll trigger latency and queue waits vary widely, so it errs long.
	waitNewBuildDefault = 15 * time.Minute
)

// registerWaitTools wires the blocking wait tools.
func (s *Server) registerWaitTools() {
	s.registerWaitForBuild()
	s.registerWaitForNewBuild()
}

// registerWaitForBuild wires the wait_for_build tool.
func (s *Server) registerWaitForBuild() {
	d := s.deps
	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "wait_for_build",
		Description: "Block until a build finishes, then return its final result — instead of sleeping and re-polling in a loop. " +
			"The call blocks server-side and returns the instant the build reaches a terminal state (success/failed/aborted/unstable) " +
			"or pauses for an input step (needs your approval). timeout_seconds bounds how long this one call blocks; omit or 0 to " +
			"size it automatically from the build's own estimatedDuration. If the build is still running when the bound is hit, the " +
			"result has timed_out=true, done=false, and check_back_in_seconds — call again after that to keep waiting. Omit build_number " +
			"for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in waitForBuildIn) (*mcp.CallToolResult, waitForBuildOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, waitForBuildOut{}, err
		}
		start := time.Now()
		detail, err := d.GetBuild(ctx, in.JobPath, n)
		if err != nil {
			return nil, waitForBuildOut{}, err
		}
		blockFor := waitCap(in.TimeoutSeconds, *detail)
		deadline := start.Add(blockFor)

		ticker := time.NewTicker(waitPollInterval)
		defer ticker.Stop()
		progressToken := req.Params.GetProgressToken()
		for {
			if waitDone(detail.Status) {
				return nil, buildWaitOut(in.JobPath, n, *detail, start, false), nil
			}
			if !time.Now().Before(deadline) {
				return nil, buildWaitOut(in.JobPath, n, *detail, start, true), nil
			}
			select {
			case <-ctx.Done():
				return nil, waitForBuildOut{}, ctx.Err()
			case <-ticker.C:
			}
			latest, err := d.GetBuild(ctx, in.JobPath, n)
			if err != nil {
				return nil, waitForBuildOut{}, err
			}
			detail = latest
			if progressToken != nil {
				_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: progressToken,
					Message:       "build " + string(detail.Status),
					Progress:      time.Since(start).Seconds(),
					Total:         blockFor.Seconds(),
				})
			}
		}
	})
}

// registerWaitForNewBuild wires the wait_for_new_build tool: block until Jenkins
// picks up a job (a queue item or a build newer than the baseline appears),
// answering "has my just-pushed commit triggered anything yet?". A queued build
// has no number and no change set, so this detects new activity rather than
// matching a commit; confirm the commit afterwards with get_changes/find_commit
// on the returned build, or chain into wait_for_build to await completion.
func (s *Server) registerWaitForNewBuild() {
	d := s.deps
	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "wait_for_new_build",
		Description: "Block until a job starts building — a new build number appears, or an item for it shows up in the queue — " +
			"instead of sleeping and re-polling. Use this right after pushing a commit to catch Jenkins picking it up (SCM poll or " +
			"webhook). It returns as soon as there is activity newer than the baseline (the job's latest build at call time, or " +
			"since_build if given): state=\"started\" with the build_number, or state=\"queued\" when it is only in the queue so far. " +
			"A queued build has no number or change set yet, so this cannot match a specific commit — once you have the build_number, " +
			"confirm with get_changes, or call wait_for_build to await completion. If it returns timed_out=true, wait " +
			"check_back_in_seconds and call again. Omit timeout_seconds to use a 15-minute default.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in waitForNewBuildIn) (*mcp.CallToolResult, waitForNewBuildOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		start := time.Now()
		baseline := in.SinceBuild
		if baseline <= 0 {
			n, err := latestBuildNumber(ctx, d, in.JobPath)
			if err != nil {
				return nil, waitForNewBuildOut{}, err
			}
			baseline = n
		}
		blockFor := waitNewBuildDefault
		if in.TimeoutSeconds > 0 {
			blockFor = time.Duration(in.TimeoutSeconds) * time.Second
		}
		deadline := start.Add(blockFor)

		ticker := time.NewTicker(waitPollInterval)
		defer ticker.Stop()
		progressToken := req.Params.GetProgressToken()
		for {
			out, found, err := pollNewBuild(ctx, d, in.JobPath, baseline, start)
			if err != nil {
				return nil, waitForNewBuildOut{}, err
			}
			if found {
				return nil, out, nil
			}
			if !time.Now().Before(deadline) {
				return nil, newBuildTimedOut(in.JobPath, start), nil
			}
			select {
			case <-ctx.Done():
				return nil, waitForNewBuildOut{}, ctx.Err()
			case <-ticker.C:
			}
			if progressToken != nil {
				_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: progressToken,
					Message:       "waiting for " + in.JobPath + " to start building",
					Progress:      time.Since(start).Seconds(),
					Total:         blockFor.Seconds(),
				})
			}
		}
	})
}

// latestBuildNumber returns the job's newest build number, or 0 when the job has
// no builds yet (a not-yet-built job is a valid baseline — the first build will
// exceed it).
func latestBuildNumber(ctx context.Context, d usecase.Deps, jobPath string) (int, error) {
	builds, err := d.ListBuilds(ctx, jobPath)
	if err != nil {
		return 0, err
	}
	if len(builds) == 0 {
		return 0, nil
	}
	return builds[0].Number, nil
}

// pollNewBuild checks once for activity newer than baseline: a build with a
// higher number (preferred — concrete and matchable) or, failing that, a queue
// item for the job. found is false when nothing new has appeared yet.
func pollNewBuild(ctx context.Context, d usecase.Deps, jobPath string, baseline int, start time.Time) (out waitForNewBuildOut, found bool, err error) {
	builds, err := d.ListBuilds(ctx, jobPath)
	if err != nil {
		return waitForNewBuildOut{}, false, err
	}
	if len(builds) > 0 && builds[0].Number > baseline {
		return waitForNewBuildOut{
			JobPath:        jobPath,
			State:          "started",
			Done:           true,
			BuildNumber:    builds[0].Number,
			Status:         string(builds[0].Status),
			ElapsedSeconds: int(time.Since(start).Seconds()),
			Build:          ptrTo(dto.ToBuild(builds[0])),
		}, true, nil
	}

	queue, err := d.ListQueue(ctx)
	if err != nil {
		return waitForNewBuildOut{}, false, err
	}
	for _, q := range queue {
		if q.JobPath == jobPath {
			return waitForNewBuildOut{
				JobPath:        jobPath,
				State:          "queued",
				Done:           true,
				ElapsedSeconds: int(time.Since(start).Seconds()),
				Queue:          ptrTo(dto.ToQueueItem(q)),
			}, true, nil
		}
	}
	return waitForNewBuildOut{}, false, nil
}

// newBuildTimedOut builds the result for a wait that elapsed before any activity
// appeared, nudging the caller to retry.
func newBuildTimedOut(jobPath string, start time.Time) waitForNewBuildOut {
	return waitForNewBuildOut{
		JobPath:            jobPath,
		State:              "timed_out",
		TimedOut:           true,
		ElapsedSeconds:     int(time.Since(start).Seconds()),
		CheckBackInSeconds: int((waitPollInterval * 4).Seconds()),
	}
}

func ptrTo[T any](v T) *T { return &v }

// waitCap picks how long a single wait_for_build call blocks. An explicit
// timeout_seconds wins and is honored as-is; otherwise the deadline is derived
// from the build's estimatedDuration plus a grace pad, so a wait sizes itself to
// how long the build is actually expected to take. No upper bound is imposed —
// some pipelines legitimately run for hours — so the caller owns the trade-off
// against their own client timeout.
func waitCap(timeoutSeconds int, d jmodel.BuildDetail) time.Duration {
	if timeoutSeconds > 0 {
		return time.Duration(timeoutSeconds) * time.Second
	}
	remaining := estimatedRemaining(d)
	if remaining <= 0 {
		return waitNoEstimateDefault
	}
	grace := time.Duration(float64(d.EstimatedDuration) * waitGraceFactor)
	return remaining + grace
}

// estimatedRemaining is how much longer Jenkins expects the build to run, from
// its start timestamp and estimatedDuration. Non-positive when unknown or the
// build has already overrun its estimate.
func estimatedRemaining(d jmodel.BuildDetail) time.Duration {
	if d.EstimatedDuration <= 0 || d.Timestamp.IsZero() {
		return 0
	}
	return d.EstimatedDuration - time.Since(d.Timestamp)
}

// buildWaitOut assembles the tool result, computing the check-back hint when the
// call timed out with the build still in progress.
func buildWaitOut(jobPath string, n int, d jmodel.BuildDetail, start time.Time, timedOut bool) waitForBuildOut {
	out := waitForBuildOut{
		JobPath:        jobPath,
		BuildNumber:    n,
		Status:         string(d.Status),
		Done:           !timedOut,
		TimedOut:       timedOut,
		ElapsedSeconds: int(time.Since(start).Seconds()),
		Build:          dto.ToBuildDetail(jobPath, d),
	}
	if timedOut {
		checkBack := estimatedRemaining(d)
		if checkBack < waitPollInterval {
			checkBack = waitPollInterval * 4 // overran or unknown: nudge, don't hammer
		}
		out.CheckBackInSeconds = int(checkBack.Seconds())
	}
	return out
}

// waitDone reports whether the build has stopped being actively in progress:
// either a terminal result, or paused on an input step that needs a decision.
// Both are reasons to return control to the caller.
func waitDone(status jmodel.BuildStatus) bool {
	switch status {
	case jmodel.BuildStatusRunning, jmodel.BuildStatusQueued, jmodel.BuildStatusUnknown:
		return false
	default:
		return true
	}
}

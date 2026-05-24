package jenkins

import (
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// Pure parsers for the Jenkins jobs JSON. The HTTP-bound (*Client).ListJobs
// hands the decoded jsonJobList here so all multibranch enrichment is
// independently unit-testable (architecture.md §6).

// parseJobList converts a decoded jsonJobList into domain Jobs, applying
// multibranch enrichment for WorkflowMultiBranchProject items.
func parseJobList(resp jsonJobList, folder string) []jmodel.Job {
	jobs := make([]jmodel.Job, len(resp.Jobs))
	for i, j := range resp.Jobs {
		job := j.toDomain(folder)
		if job.Type == jmodel.JobTypeMultiBranch {
			enrichMultiBranch(&job, j.Jobs)
		} else {
			enrichSingleBranch(&job)
		}
		jobs[i] = job
	}
	return jobs
}

// enrichMultiBranch fills primary-branch lastBuild/color and cross-branch
// stats (RunningCount, LastAnyBuild, LastAnyColor) on a multibranch job.
func enrichMultiBranch(job *jmodel.Job, branches []jsonJob) {
	if primary := primaryBranch(branches); primary != nil {
		job.Color = primary.Color
		job.LastBuild = branchBuildRef(primary.LastBuild)
	}

	job.RunningCount = countRunningBranches(branches)
	latest := latestBuiltBranch(branches)
	if latest != nil && latest.LastBuild != nil {
		job.LastAnyBuild = branchBuildRef(latest.LastBuild)
		job.LastAnyColor = latest.Color
	} else {
		job.LastAnyBuild = job.LastBuild
		job.LastAnyColor = job.Color
	}
}

// enrichSingleBranch sets the "any" fields for a non-multibranch job.
// LastAnyBuild aliases LastBuild; RunningCount is 0 or 1.
func enrichSingleBranch(job *jmodel.Job) {
	job.LastAnyBuild = job.LastBuild
	job.LastAnyColor = job.Color
	if ColorToBuildStatus(job.Color) == jmodel.BuildStatusRunning {
		job.RunningCount = 1
	}
}

// countRunningBranches returns how many branches currently have a running
// build (color suffix "_anime").
func countRunningBranches(branches []jsonJob) int {
	n := 0
	for i := range branches {
		if strings.HasSuffix(branches[i].Color, "_anime") {
			n++
		}
	}
	return n
}

// latestBuiltBranch returns the branch with the highest LastBuild.Timestamp,
// or nil when no branch has a recorded build.
func latestBuiltBranch(branches []jsonJob) *jsonJob {
	var best *jsonJob
	for i := range branches {
		b := &branches[i]
		if b.LastBuild == nil {
			continue
		}
		if best == nil || b.LastBuild.Timestamp > best.LastBuild.Timestamp {
			best = b
		}
	}
	return best
}

// branchBuildRef converts a jsonBuildRef into the domain BuildRef.
// Returns nil for a nil input so callers can assign directly.
func branchBuildRef(b *jsonBuildRef) *jmodel.BuildRef {
	if b == nil {
		return nil
	}
	return &jmodel.BuildRef{
		Number:            b.Number,
		URL:               b.URL,
		Timestamp:         millisToTime(b.Timestamp),
		EstimatedDuration: millisToDuration(b.EstimatedDuration),
	}
}

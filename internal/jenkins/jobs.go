package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// wellKnownPrimaryBranches lists branch names to prefer as the "primary" branch,
// in priority order.
var wellKnownPrimaryBranches = []string{"main", "master", "develop", "development", "trunk"}

// primaryBranch selects the most representative branch from a multi-branch
// project's branch list. Prefers well-known default branch names, falls back
// to the branch with the highest last-build number.
func primaryBranch(branches []jsonJob) *jsonJob {
	for _, want := range wellKnownPrimaryBranches {
		for i := range branches {
			if strings.EqualFold(branches[i].Name, want) {
				return &branches[i]
			}
		}
	}
	// Fall back to most recently built branch.
	var best *jsonJob
	for i := range branches {
		if branches[i].LastBuild == nil {
			continue
		}
		if best == nil || branches[i].LastBuild.Number > best.LastBuild.Number {
			best = &branches[i]
		}
	}
	if best != nil {
		return best
	}
	if len(branches) > 0 {
		return &branches[0]
	}
	return nil
}

// ListJobs returns jobs in the given folder (empty string for root).
func (c *Client) ListJobs(ctx context.Context, folder string) ([]Job, error) {
	path := "/api/json"
	if folder != "" {
		path = JobPathToURL(folder) + "/api/json"
	}
	// Include one level of nested jobs so we can derive the primary branch
	// status/lastBuild for WorkflowMultiBranchProject items.
	path += "?tree=jobs[name,url,color,_class,lastBuild[number,url,timestamp,estimatedDuration],jobs[name,color,lastBuild[number,url,timestamp,estimatedDuration]]]"

	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}

	var resp jsonJobList
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing jobs response: %w", err)
	}

	jobs := make([]Job, len(resp.Jobs))
	for i, j := range resp.Jobs {
		job := j.toDomain(folder)
		if job.Type == JobTypeMultiBranch {
			if primary := primaryBranch(j.Jobs); primary != nil {
				job.Color = primary.Color
				if primary.LastBuild != nil {
					job.LastBuild = &BuildRef{
						Number:            primary.LastBuild.Number,
						URL:               primary.LastBuild.URL,
						Timestamp:         millisToTime(primary.LastBuild.Timestamp),
						EstimatedDuration: millisToDuration(primary.LastBuild.EstimatedDuration),
					}
				}
			}
			// Derive cross-branch stats from all child branches.
			var latestBranch *jsonJob
			for bi := range j.Jobs {
				b := &j.Jobs[bi]
				if strings.HasSuffix(b.Color, "_anime") {
					job.RunningCount++
				}
				if b.LastBuild != nil {
					if latestBranch == nil || b.LastBuild.Timestamp > latestBranch.LastBuild.Timestamp {
						latestBranch = b
					}
				}
			}
			if latestBranch != nil && latestBranch.LastBuild != nil {
				job.LastAnyBuild = &BuildRef{
					Number:            latestBranch.LastBuild.Number,
					URL:               latestBranch.LastBuild.URL,
					Timestamp:         millisToTime(latestBranch.LastBuild.Timestamp),
					EstimatedDuration: millisToDuration(latestBranch.LastBuild.EstimatedDuration),
				}
				job.LastAnyColor = latestBranch.Color
			} else {
				job.LastAnyBuild = job.LastBuild
				job.LastAnyColor = job.Color
			}
		} else {
			// Single-branch: LastAnyBuild == LastBuild; RunningCount 0 or 1.
			job.LastAnyBuild = job.LastBuild
			job.LastAnyColor = job.Color
			if ColorToBuildStatus(job.Color) == BuildStatusRunning {
				job.RunningCount = 1
			}
		}
		jobs[i] = job
	}
	return jobs, nil
}

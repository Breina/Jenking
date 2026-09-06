package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// wellKnownPrimaryBranches lists branch names to prefer as the "primary" branch,
// in priority order.
var wellKnownPrimaryBranches = []string{"main", "master", "HEAD", "develop", "trunk"}

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
		}
		jobs[i] = job
	}
	return jobs, nil
}

package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// allBuildFields is the set of build fields fetched during a full scan.
const allBuildFields = `number,result,building,timestamp,duration,estimatedDuration,` +
	`actions[causes[userId,userName,shortDescription]]`

// allBuildsTree builds a 4-level nested tree query that covers most Jenkins
// folder structures (root/folder/project/branch). The {0,maxPerJob} slice
// limits how many builds are fetched per job.
func allBuildsTree(maxPerJob int) string {
	slice := fmt.Sprintf("{0,%d}", maxPerJob)
	leaf := fmt.Sprintf("name,fullName,url,builds[%s]%s", allBuildFields, slice)
	// jmodel.Build 4 levels of nesting from inside out.
	query := "jobs[" + leaf + "]"
	for i := 1; i < 4; i++ {
		query = "jobs[" + leaf + "," + query + "]"
	}
	return query
}

// BuildKey, ParseBuildKey live in internal/domain/jmodel.

// ScanAllBuilds fetches recent builds across all jobs on the Jenkins instance.
// At most maxPerJob builds per job/branch are returned. This is a potentially
// heavy call: use it with a slow polling interval and cache the result.
func (c *Client) ScanAllBuilds(ctx context.Context, maxPerJob int) ([]jmodel.UserBuild, error) {
	tree := allBuildsTree(maxPerJob)
	path := "/api/json?tree=" + url.QueryEscape(tree)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("scanning all builds: %w", err)
	}
	var list jsonJobList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parsing all builds scan: %w", err)
	}
	var out []jmodel.UserBuild
	flattenJobBuilds(list.Jobs, "", &out)
	return out, nil
}

// flattenJobBuilds recursively extracts UserBuilds from a nested job tree.
func flattenJobBuilds(jobs []jsonJob, parentPath string, out *[]jmodel.UserBuild) {
	for _, job := range jobs {
		jobPath := strings.TrimPrefix(job.FullName, "/")
		if jobPath == "" {
			// FullName not set (shouldn't happen, but fall back to name)
			if parentPath == "" {
				jobPath = job.Name
			} else {
				jobPath = parentPath + "/" + job.Name
			}
		}
		for _, b := range job.Builds {
			*out = append(*out, jmodel.UserBuild{
				JobPath:     jobPath,
				DisplayName: jobPath + " #" + strconv.Itoa(b.Number),
				Build:       b.toDomain(),
			})
		}
		if len(job.Jobs) > 0 {
			flattenJobBuilds(job.Jobs, jobPath, out)
		}
	}
}

package engine

import (
	"context"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// FillSCMURLs populates store.RepoURLs (jobPath -> SCM project/branch web URL)
// for any of jobPaths not already known, using the cheap narrow GetJobSCMURL
// query. This is the population mechanism behind the reverse (SCM URL -> job)
// lookup: it is called from the lightweight running-builds poll (main driver)
// and from the slow build-walk (offline catch-up) — never as a dedicated scan.
//
// Already-cached paths (including those recorded with an empty URL, meaning "no
// SCM URL") are skipped, so steady-state cost is ~zero and the one-time warm of
// a new job is a single narrow request that then persists to disk.
func FillSCMURLs(ctx context.Context, client jmodel.JenkinsClient, store *cache.Store, jobPaths []string) {
	if store == nil || store.RepoURLs == nil || client == nil {
		return
	}
	seen := make(map[string]bool, len(jobPaths))
	for _, jobPath := range jobPaths {
		if jobPath == "" || seen[jobPath] {
			continue
		}
		seen[jobPath] = true
		if store.RepoURLs.Get(jobPath) != nil {
			continue // already known (URL or confirmed-none)
		}
		url, err := client.GetJobSCMURL(ctx, jobPath)
		if err != nil {
			continue // transient; a later poll/scan retries
		}
		store.RepoURLs.Put(jobPath, url)
		if store.Disk != nil {
			_ = store.Disk.SaveRepoURL(jobPath, url) // flock-merged read-modify-write
		}
	}
}

// jobPathsOf extracts the job paths from a slice of user builds.
func jobPathsOf(builds []jmodel.UserBuild) []string {
	paths := make([]string, 0, len(builds))
	for _, b := range builds {
		paths = append(paths, b.JobPath)
	}
	return paths
}

package view

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// repoURLFetchedMsg signals a background SCM-URL fetch completed. It carries no
// value (the result lives in the store cache); it just triggers a re-render so
// the "open repo" shortcut can appear once the URL is known.
type repoURLFetchedMsg struct{ jobPath string }

// fetchRepoURL fetches and caches a job's SCM project/branch URL in the
// background, unless it's already cached. Returns nil when there's nothing to
// do. The cached value may be "" (job has no SCM URL), which is still cached to
// avoid repeated lookups.
func fetchRepoURL(client jmodel.JenkinsClient, store *cache.Store, jobPath string) tea.Cmd {
	if store == nil || store.RepoURLs == nil || jobPath == "" {
		return nil
	}
	if store.RepoURLs.Get(jobPath) != nil {
		return nil
	}
	return func() tea.Msg {
		url, err := client.GetJobSCMURL(context.Background(), jobPath)
		if err != nil {
			return nil
		}
		store.RepoURLs.Put(jobPath, url)
		return repoURLFetchedMsg{jobPath: jobPath}
	}
}

// cachedRepoURL returns the cached SCM URL for jobPath, or "" if unknown/none.
func cachedRepoURL(store *cache.Store, jobPath string) string {
	if store == nil || store.RepoURLs == nil {
		return ""
	}
	if e := store.RepoURLs.Get(jobPath); e != nil {
		return e.Value
	}
	return ""
}

// InspectProvider is implemented by views whose current selection can be
// inspected via the `:inspect` (`:i`) command. The returned nc decides the
// source: a concrete build number ⇒ build metadata, otherwise job metadata.
// ok=false means there's nothing inspectable under the cursor.
type InspectProvider interface {
	InspectTarget() (NavigationContext, bool)
}

// InspectTarget is the default for nc-anchored views: inspect the view's own
// context (a fixed build for build-detail views; a job/branch otherwise).
// Row-aware list views (JobList, BuildsView) override this to target the
// selected row instead of the view scope.
func (b *BaseView) InspectTarget() (NavigationContext, bool) {
	return b.nc, true
}

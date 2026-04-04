package view

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
)

// buildResolver handles finding the best matching build for a given scope and
// set of filters. It is embedded by MyBuildsView and MyConsoleView.
type buildResolver struct {
	client       jenkins.JenkinsClient
	store        *cache.Store
	scope        NavigationContext
	username     string
	gitUsernames []string
	friendlyName string // display name (falls back to username)
	// Filter state.
	filterRunning bool
	filterMine    bool
	// Raw data — kept so filter toggles can re-evaluate without a new API call.
	lastRunningBuilds []jenkins.UserBuild
	lastSlowBuilds    []jenkins.UserBuild
	// Resolved target.
	resolvedPath string
	resolvedNum  int
	// UI state.
	loading bool
	// Cancellation.
	ctx    context.Context
	cancel context.CancelFunc
	// Slow poll interval.
	slowInterval time.Duration
}

func newBuildResolver(client jenkins.JenkinsClient, store *cache.Store, scope NavigationContext, slowInterval time.Duration) buildResolver {
	ctx, cancel := context.WithCancel(context.Background())
	if slowInterval <= 0 {
		slowInterval = 2 * time.Minute
	}
	friendlyName := scope.FriendlyName
	if friendlyName == "" {
		friendlyName = scope.Username
	}
	return buildResolver{
		client:       client,
		store:        store,
		scope:        scope,
		username:     scope.Username,
		gitUsernames: scope.GitUsernames,
		friendlyName: friendlyName,
		loading:      true,
		ctx:          ctx,
		cancel:       cancel,
		slowInterval: slowInterval,
	}
}

// preloadFromCache populates lastRunningBuilds and lastSlowBuilds from
// the shared cache store, if available.
func (r *buildResolver) preloadFromCache() {
	if r.store == nil {
		return
	}
	if cached := r.store.RunningBuilds.Get(""); cached != nil {
		r.lastRunningBuilds = cached.Value
	}
	switch r.scope.Level {
	case CtxRoot, CtxFolder:
		if cached := r.store.AllBuilds.Get(""); cached != nil {
			r.lastSlowBuilds = cached.Value
		}
	case CtxProject:
		if cached := r.store.ProjectBuilds.Get(r.scope.JobPath()); cached != nil {
			r.lastSlowBuilds = projectBuildsToUserBuilds(cached.Value, r.scope.JobPath())
		}
	case CtxBranch:
		if cached := r.store.Builds.Get(r.scope.JobPath()); cached != nil {
			r.lastSlowBuilds = buildsToUserBuilds(cached.Value, r.scope.JobPath())
		}
	}
}

// fetchSlow fetches historical builds using the scope-appropriate API.
func (r *buildResolver) fetchSlow() tea.Msg {
	switch r.scope.Level {
	case CtxProject:
		builds, err := r.client.ListProjectBuilds(r.ctx, r.scope.JobPath())
		if r.ctx.Err() != nil {
			return nil
		}
		return myBuildsFullMsg{builds: projectBuildsToUserBuilds(builds, r.scope.JobPath()), err: err}
	case CtxBranch:
		builds, err := r.client.ListBuilds(r.ctx, r.scope.JobPath())
		if r.ctx.Err() != nil {
			return nil
		}
		return myBuildsFullMsg{builds: buildsToUserBuilds(builds, r.scope.JobPath()), err: err}
	default:
		builds, err := r.client.ScanAllBuilds(r.ctx, maxBuildsPerJob)
		if r.ctx.Err() != nil {
			return nil
		}
		return myBuildsFullMsg{builds: builds, err: err}
	}
}

// slowTickInterval returns the poll interval appropriate for the scope.
func (r *buildResolver) slowTickInterval() time.Duration {
	switch r.scope.Level {
	case CtxBranch:
		return 10 * time.Second
	case CtxProject:
		return 30 * time.Second
	default:
		return r.slowInterval
	}
}

func (r *buildResolver) scheduleSlowTick() tea.Cmd {
	ctx := r.ctx
	interval := r.slowTickInterval()
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
		if ctx.Err() != nil {
			return nil
		}
		return myBuildsSlowTickMsg{}
	}
}

// mergeBuilds combines running and slow-scan data, with running taking precedence.
func (r *buildResolver) mergeBuilds() []jenkins.UserBuild {
	seen := make(map[string]bool, len(r.lastRunningBuilds))
	merged := make([]jenkins.UserBuild, 0, len(r.lastRunningBuilds)+len(r.lastSlowBuilds))
	for _, b := range r.lastRunningBuilds {
		merged = append(merged, b)
		seen[jenkins.BuildKey(b.JobPath, b.Number)] = true
	}
	for _, b := range r.lastSlowBuilds {
		if !seen[jenkins.BuildKey(b.JobPath, b.Number)] {
			merged = append(merged, b)
		}
	}
	return merged
}

// matchesScope checks whether a build falls within the configured scope.
func (r *buildResolver) matchesScope(b jenkins.UserBuild) bool {
	switch r.scope.Level {
	case CtxFolder:
		return strings.HasPrefix(b.JobPath, r.scope.FolderPath+"/")
	case CtxProject:
		projectPath := r.scope.JobPath()
		return b.JobPath == projectPath || strings.HasPrefix(b.JobPath, projectPath+"/")
	case CtxBranch:
		return b.JobPath == r.scope.JobPath()
	default:
		return true
	}
}

// bestMatch picks the most recent build matching scope and active filters.
func (r *buildResolver) bestMatch(builds []jenkins.UserBuild) *jenkins.UserBuild {
	var candidates []jenkins.UserBuild
	for _, b := range builds {
		if !r.matchesScope(b) {
			continue
		}
		if r.filterRunning && b.Status != jenkins.BuildStatusRunning {
			continue
		}
		if r.filterMine && !matchesUser(b.Build, r.username, r.gitUsernames) {
			continue
		}
		candidates = append(candidates, b)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Timestamp.After(candidates[j].Timestamp)
	})
	return &candidates[0]
}

// cacheSlowBuilds stores slow-fetched builds in the appropriate cache.
func (r *buildResolver) cacheSlowBuilds(builds []jenkins.UserBuild) {
	if r.store == nil {
		return
	}
	switch r.scope.Level {
	case CtxRoot, CtxFolder:
		r.store.AllBuilds.Put("", builds)
	}
}

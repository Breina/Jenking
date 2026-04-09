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
		dbg("preloadFromCache: store is nil")
		return
	}
	if cached := r.store.RunningBuilds.Get(""); cached != nil {
		r.lastRunningBuilds = cached.Value
		dbg("preloadFromCache: loaded %d running builds", len(r.lastRunningBuilds))
	}
	switch r.scope.Level {
	case CtxRoot, CtxFolder:
		if cached := r.store.AllBuilds.Get(""); cached != nil {
			r.lastSlowBuilds = cached.Value
			dbg("preloadFromCache: CtxRoot/CtxFolder loaded %d slow builds", len(r.lastSlowBuilds))
		} else {
			dbg("preloadFromCache: CtxRoot/CtxFolder — no slow builds in cache")
		}
	case CtxProject:
		key := r.scope.JobPath()
		if cached := r.store.ProjectBuilds.Get(key); cached != nil {
			r.lastSlowBuilds = projectBuildsToUserBuilds(cached.Value, key)
			dbg("preloadFromCache: CtxProject key=%q loaded %d project builds -> %d user builds", key, len(cached.Value), len(r.lastSlowBuilds))
		} else {
			dbg("preloadFromCache: CtxProject key=%q — no project builds in cache", key)
		}
	case CtxBranch:
		key := r.scope.JobPath()
		if cached := r.store.Builds.Get(key); cached != nil {
			r.lastSlowBuilds = buildsToUserBuilds(cached.Value, key)
			dbg("preloadFromCache: CtxBranch key=%q loaded %d builds", key, len(r.lastSlowBuilds))
		} else {
			dbg("preloadFromCache: CtxBranch key=%q — no builds in cache", key)
		}
	}
}

// fetchSlow fetches historical builds using the scope-appropriate API.
func (r *buildResolver) fetchSlow() tea.Msg {
	switch r.scope.Level {
	case CtxProject:
		key := r.scope.JobPath()
		dbg("fetchSlow: CtxProject key=%q starting", key)
		builds, err := r.client.ListProjectBuilds(r.ctx, key)
		dbg("fetchSlow: CtxProject key=%q got %d builds, err=%v, ctxErr=%v", key, len(builds), err, r.ctx.Err())
		if err == nil && r.store != nil {
			r.store.ProjectBuilds.Put(key, builds)
			dbg("fetchSlow: CtxProject key=%q cached %d project builds", key, len(builds))
		}
		if r.ctx.Err() != nil {
			dbg("fetchSlow: CtxProject key=%q context cancelled, returning nil", key)
			return nil
		}
		return myBuildsFullMsg{builds: projectBuildsToUserBuilds(builds, key), err: err}
	case CtxBranch:
		key := r.scope.JobPath()
		dbg("fetchSlow: CtxBranch key=%q starting", key)
		builds, err := r.client.ListBuilds(r.ctx, key)
		dbg("fetchSlow: CtxBranch key=%q got %d builds, err=%v, ctxErr=%v", key, len(builds), err, r.ctx.Err())
		if err == nil && r.store != nil {
			r.store.Builds.Put(key, builds)
			dbg("fetchSlow: CtxBranch key=%q cached %d builds", key, len(builds))
		}
		if r.ctx.Err() != nil {
			dbg("fetchSlow: CtxBranch key=%q context cancelled, returning nil", key)
			return nil
		}
		return myBuildsFullMsg{builds: buildsToUserBuilds(builds, key), err: err}
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
	dbg("bestMatch: scope=%v/%v jobPath=%q total=%d", r.scope.Level, r.scope.ProjectName, r.scope.JobPath(), len(builds))
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
		dbg("bestMatch: no candidates (scope filtered all %d builds)", len(builds))
		if len(builds) > 0 {
			dbg("bestMatch: first build jobPath=%q", builds[0].JobPath)
		}
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Timestamp.After(candidates[j].Timestamp)
	})
	dbg("bestMatch: picked jobPath=%q #%d", candidates[0].JobPath, candidates[0].Number)
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

package view

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// buildResolver handles finding the best matching build for a given scope and
// set of filters. It is embedded by MyBuildsView and MyConsoleView.
type buildResolver struct {
	client       jmodel.JenkinsClient
	store        *cache.Store
	scope        NavigationContext
	username     string
	gitUsernames []string
	friendlyName string // display name (falls back to username)
	// Filter state.
	filterRunning bool
	filterMine    bool
	// Raw data — kept so filter toggles can re-evaluate without a new API call.
	lastRunningBuilds []jmodel.UserBuild
	lastSlowBuilds    []jmodel.UserBuild
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

func newBuildResolver(client jmodel.JenkinsClient, store *cache.Store, scope NavigationContext, slowInterval time.Duration) buildResolver {
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
// the registry, if available.
func (r *buildResolver) preloadFromCache() {
	if r.store == nil || r.store.Registry == nil {
		dbg("preloadFromCache: store/registry is nil")
		return
	}
	r.lastRunningBuilds = r.scopedRunningBuilds()
	r.lastSlowBuilds = r.store.Registry.Query(r.scopeFilter())
	dbg("preloadFromCache: %d running, %d historical (scope=%v)", len(r.lastRunningBuilds), len(r.lastSlowBuilds), r.scope.Level)
}

// scopeFilter returns the registry filter for the resolver's current scope.
func (r *buildResolver) scopeFilter() buildregistry.Filter {
	switch r.scope.Level {
	case CtxFolder:
		return buildregistry.Filter{FolderPrefix: r.scope.FolderPath}
	case CtxProject:
		return buildregistry.Filter{ProjectPath: r.scope.JobPath()}
	case CtxBranch:
		return buildregistry.Filter{JobPath: r.scope.JobPath()}
	default:
		return buildregistry.Filter{}
	}
}

// scopedRunningBuilds returns the live running set restricted to the scope.
func (r *buildResolver) scopedRunningBuilds() []jmodel.UserBuild {
	all := r.store.Registry.RunningBuilds()
	if r.scope.Level == CtxRoot {
		return all
	}
	out := all[:0:0]
	for _, b := range all {
		if r.matchesScope(b) {
			out = append(out, b)
		}
	}
	return out
}

// fetchSlow fetches historical builds using the scope-appropriate API.
//
// For CtxProject and CtxBranch the algorithm is identical except for the
// client API, registry ingest method, and conversion fn — passed as a single
// closure so the dbg/ctx/error scaffold lives in one place. ScanAllBuilds for
// CtxRoot returns UserBuilds directly and skips the per-scope ingest path.
func (r *buildResolver) fetchSlow() tea.Msg {
	switch r.scope.Level {
	case CtxProject:
		return r.fetchScopedBuilds("CtxProject", func(key string) ([]jmodel.UserBuild, error) {
			builds, err := r.client.ListProjectBuilds(r.ctx, key)
			if err == nil && r.store != nil && r.store.Registry != nil {
				r.store.Registry.IngestProjectList(key, builds)
			}
			return projectBuildsToUserBuilds(builds, key), err
		})
	case CtxBranch:
		return r.fetchScopedBuilds("CtxBranch", func(key string) ([]jmodel.UserBuild, error) {
			builds, err := r.client.ListBuilds(r.ctx, key)
			if err == nil && r.store != nil && r.store.Registry != nil {
				r.store.Registry.IngestBranchList(key, builds)
			}
			return buildsToUserBuilds(builds, key), err
		})
	default:
		builds, err := r.client.ScanAllBuilds(r.ctx, maxBuildsPerJob)
		if r.ctx.Err() != nil {
			return nil
		}
		return myBuildsFullMsg{builds: builds, err: err}
	}
}

func (r *buildResolver) fetchScopedBuilds(label string, fetch func(key string) ([]jmodel.UserBuild, error)) tea.Msg {
	key := r.scope.JobPath()
	dbg("fetchSlow: %s key=%q starting", label, key)
	builds, err := fetch(key)
	dbg("fetchSlow: %s key=%q got %d builds, err=%v, ctxErr=%v", label, key, len(builds), err, r.ctx.Err())
	if r.ctx.Err() != nil {
		dbg("fetchSlow: %s key=%q context cancelled, returning nil", label, key)
		return nil
	}
	return myBuildsFullMsg{builds: builds, err: err}
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
func (r *buildResolver) mergeBuilds() []jmodel.UserBuild {
	seen := make(map[string]bool, len(r.lastRunningBuilds))
	merged := make([]jmodel.UserBuild, 0, len(r.lastRunningBuilds)+len(r.lastSlowBuilds))
	for _, b := range r.lastRunningBuilds {
		merged = append(merged, b)
		seen[jmodel.BuildKey(b.JobPath, b.Number)] = true
	}
	for _, b := range r.lastSlowBuilds {
		if !seen[jmodel.BuildKey(b.JobPath, b.Number)] {
			merged = append(merged, b)
		}
	}
	return merged
}

// matchesScope checks whether a build falls within the configured scope.
func (r *buildResolver) matchesScope(b jmodel.UserBuild) bool {
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
func (r *buildResolver) bestMatch(builds []jmodel.UserBuild) *jmodel.UserBuild {
	dbg("bestMatch: scope=%v/%v jobPath=%q total=%d", r.scope.Level, r.scope.ProjectName, r.scope.JobPath(), len(builds))
	var candidates []jmodel.UserBuild
	for _, b := range builds {
		if !r.matchesScope(b) {
			continue
		}
		if r.filterRunning && b.Status != jmodel.BuildStatusRunning {
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

// cacheSlowBuilds feeds slow-scan results into the registry. Branch/project
// scopes already ingested in fetchSlow; this handles CtxRoot/CtxFolder.
func (r *buildResolver) cacheSlowBuilds(builds []jmodel.UserBuild) {
	if r.store == nil || r.store.Registry == nil {
		return
	}
	switch r.scope.Level {
	case CtxRoot, CtxFolder:
		r.store.Registry.IngestScan(builds)
	}
}

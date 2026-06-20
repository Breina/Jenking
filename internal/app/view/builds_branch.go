package view

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type branchBuildsTickMsg struct{}
type branchBuildsVisualTickMsg struct{}

// BranchBuildsProvider fetches builds for a single branch/job and implements BuildDataProvider.
type BranchBuildsProvider struct {
	pollingProviderBase
	client jmodel.JenkinsClient
	store  *cache.Store
	nc     NavigationContext
	builds []jmodel.Build
	tt     testTracker
	at     artifactTracker
}

// NewBranchBuildsProvider creates a provider for a single branch/job.
func NewBranchBuildsProvider(client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext) *BranchBuildsProvider {
	return &BranchBuildsProvider{
		pollingProviderBase: newPollingProviderBase(),
		client:              client,
		store:               store,
		nc:                  nc,
	}
}

func (p *BranchBuildsProvider) Init() tea.Cmd {
	// Preload of historical builds is implicit: the registry is already
	// populated from disk at startup, and Builds() queries it directly.
	// We still preload test results / artifacts for any builds the registry
	// knows about so they render without a flash on first paint.
	if p.store != nil && p.store.Registry != nil {
		for _, ub := range p.store.Registry.Query(p.registryFilter()) {
			p.tt.preloadOne(p.store, p.nc.JobPath(), ub.Number)
			p.at.preloadOne(p.store, p.nc.JobPath(), ub.Number)
		}
	}
	return p.fetchBuilds
}

func (p *BranchBuildsProvider) Refresh() tea.Cmd {
	return p.fetchBuilds
}

func (p *BranchBuildsProvider) fetchBuilds() tea.Msg {
	builds, err := p.client.ListBuilds(p.ctx, p.nc.JobPath())
	if p.ctx.Err() != nil {
		return nil
	}
	return BuildsMsg{Builds: builds, Err: err}
}

func (p *BranchBuildsProvider) scheduleRefresh() tea.Cmd {
	return p.scheduleContextTick(10*time.Second, func() tea.Msg { return branchBuildsTickMsg{} })
}

func (p *BranchBuildsProvider) scheduleVisualTick() tea.Cmd {
	return p.scheduleContextTick(1*time.Second, func() tea.Msg { return branchBuildsVisualTickMsg{} })
}

func (p *BranchBuildsProvider) registryFilter() buildregistry.Filter {
	return buildregistry.Filter{JobPath: p.nc.JobPath()}
}

func (p *BranchBuildsProvider) hasRunning() bool {
	if p.store != nil && p.store.Registry != nil {
		return p.store.Registry.HasRunning(p.registryFilter())
	}
	for _, b := range p.builds {
		if b.Status == jmodel.BuildStatusRunning {
			return true
		}
	}
	return false
}

func (p *BranchBuildsProvider) HandleMsg(msg tea.Msg) (bool, []tea.Cmd) {
	switch msg := msg.(type) {
	case RunningBuildsUpdatedMsg:
		// A build may have arrived for this job — if dirty, fetch immediately.
		if p.store != nil && p.store.IsDirtyBuilds(p.nc.JobPath()) {
			p.store.ClearDirtyBuilds(p.nc.JobPath())
			return true, []tea.Cmd{p.fetchBuilds}
		}
		// Suppress the message so BuildsView doesn't repopulate unnecessarily.
		return true, nil

	case branchBuildsTickMsg:
		return true, []tea.Cmd{p.fetchBuilds}

	case branchBuildsVisualTickMsg:
		if p.hasRunning() {
			return true, []tea.Cmd{p.scheduleVisualTick()}
		}
		return true, nil

	case BuildsMsg:
		if msg.Err != nil {
			return true, []tea.Cmd{func() tea.Msg { return ErrorMsg{Err: msg.Err} }}
		}
		p.builds = msg.Builds
		if p.store != nil {
			p.store.ClearDirtyBuilds(p.nc.JobPath())
			if p.store.Registry != nil {
				p.store.Registry.IngestBranchList(p.nc.JobPath(), msg.Builds)
			}
		}
		sort.Slice(p.builds, func(i, j int) bool {
			return p.builds[i].Number > p.builds[j].Number
		})
		cmds := []tea.Cmd{p.scheduleRefresh()}
		if p.hasRunning() {
			cmds = append(cmds, p.scheduleVisualTick())
		}
		return true, cmds

	case TestReportMsg:
		p.tt.handleMsg(msg)
		return true, nil

	case ArtifactsMsg:
		p.at.handleMsg(msg)
		return true, nil
	}
	return false, nil
}

func (p *BranchBuildsProvider) Builds() []UnifiedBuild {
	// Prefer registry-backed view (applies invariant 2 for stale Running entries).
	if p.store != nil && p.store.Registry != nil {
		ubs := p.store.Registry.Query(p.registryFilter())
		queued := queuedUnifiedBuilds(p.store, p.registryFilter())
		result := make([]UnifiedBuild, 0, len(queued)+len(ubs))
		result = append(result, queued...)
		for _, ub := range ubs {
			result = append(result, UnifiedBuild{
				Build:      ub.Build,
				JobPath:    p.nc.JobPath(),
				TestResult: p.tt.get(p.nc.JobPath(), ub.Number),
				Artifacts:  p.at.get(p.nc.JobPath(), ub.Number),
			})
		}
		return result
	}
	result := make([]UnifiedBuild, len(p.builds))
	for i, b := range p.builds {
		result[i] = UnifiedBuild{
			Build:      b,
			JobPath:    p.nc.JobPath(),
			TestResult: p.tt.get(p.nc.JobPath(), b.Number),
			Artifacts:  p.at.get(p.nc.JobPath(), b.Number),
		}
	}
	return result
}

func (p *BranchBuildsProvider) Config() BuildsViewConfig {
	return BuildsViewConfig{CanTrigger: true}
}

// fetchTestReport returns a Cmd that retrieves JUnit test results for a build,
// checking the cache first. Dispatches TestReportMsg on completion.
func fetchTestReport(client jmodel.JenkinsClient, store *cache.Store, jobPath string, buildNum int) tea.Cmd {
	cacheKey := fmt.Sprintf("%s:%d", jobPath, buildNum)
	if entry := store.TestReports.Get(cacheKey); entry != nil {
		report := entry.Value
		return func() tea.Msg {
			return TestReportMsg{JobPath: jobPath, BuildNum: buildNum, Report: report}
		}
	}
	return func() tea.Msg {
		report, err := client.GetTestReport(context.Background(), jobPath, buildNum)
		if err == nil {
			store.TestReports.Put(cacheKey, report)
			// Only persist non-nil reports; nil (no tests / 404) is fast to re-check.
			if report != nil && store.Disk != nil {
				_ = store.Disk.SaveTestReport(cacheKey, report)
			}
		}
		return TestReportMsg{JobPath: jobPath, BuildNum: buildNum, Report: report, Err: err}
	}
}

// fetchPendingInputs returns a Cmd that pulls BuildDetail and stores the
// resulting PendingInputs slice into the store cache. Issued once per
// selection from buildsview/joblist so the list view can render the paused
// badge in place of a progress bar. The returned tea.Msg is intentionally
// nil — consumers read from the cache when they next render.
func fetchPendingInputs(client jmodel.JenkinsClient, store *cache.Store, jobPath string, buildNum int) tea.Cmd {
	cacheKey := fmt.Sprintf("%s:%d", jobPath, buildNum)
	return func() tea.Msg {
		detail, err := client.GetBuild(context.Background(), jobPath, buildNum)
		if err != nil {
			return nil
		}
		store.PendingInputs.Put(cacheKey, detail.PendingInputs)
		return nil
	}
}

// fetchArtifacts returns a Cmd that retrieves build artifacts,
// checking the cache first. Dispatches ArtifactsMsg on completion.
func fetchArtifacts(client jmodel.JenkinsClient, store *cache.Store, jobPath string, buildNum int) tea.Cmd {
	cacheKey := fmt.Sprintf("%s:%d", jobPath, buildNum)
	if entry := store.Artifacts.Get(cacheKey); entry != nil {
		artifacts := entry.Value
		return func() tea.Msg {
			return ArtifactsMsg{JobPath: jobPath, BuildNum: buildNum, Artifacts: artifacts}
		}
	}
	return func() tea.Msg {
		artifacts, err := client.GetArtifacts(context.Background(), jobPath, buildNum)
		if err == nil {
			if artifacts == nil {
				artifacts = []jmodel.Artifact{}
			}
			// Only cache once the build is terminal: a running build's artifact
			// set grows as stages archive (e.g. a screenshot mid-pipeline, then
			// a report at the end), so caching a partial list — which is then
			// frozen on disk and never re-fetched — would permanently hide
			// later artifacts. Non-terminal builds fetch live every time.
			if len(artifacts) > 0 && store.Registry.IsTerminal(jobPath, buildNum) {
				store.Artifacts.Put(cacheKey, artifacts)
				if store.Disk != nil {
					_ = store.Disk.SaveArtifacts(cacheKey, artifacts)
				}
			}
		}
		return ArtifactsMsg{JobPath: jobPath, BuildNum: buildNum, Artifacts: artifacts, Err: err}
	}
}

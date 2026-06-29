package view

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// Internal message types for AllBuildsProvider polling.
type allBuildsSlowTickMsg struct{}
type allBuildsVisualTickMsg struct{}

type allBuildsFullMsg struct {
	builds []jmodel.UserBuild
	err    error
}

// maxBuildsPerJob is how many recent builds per job are fetched in the full scan.
const maxBuildsPerJob = 10

// AllBuildsProvider is a BuildDataProvider that shows recent builds across the
// entire Jenkins instance (or scoped to a folder when folderFilter is set).
// All build state lives in the shared buildregistry; this provider is a thin
// query view that triggers a slow ScanAllBuilds every slowInterval to refresh
// historical data. Running state arrives via the running-builds monitor (which
// also feeds the registry).
type AllBuildsProvider struct {
	pollingProviderBase
	client           jmodel.JenkinsClient
	store            *cache.Store
	username         string
	folderFilter     string
	slowInterval     time.Duration
	visualTickActive bool
	onlyRunning      bool
}

// SetOnlyRunning restricts Builds() to currently-running builds, so the
// registry query skips completed records entirely instead of returning them
// for the view to filter out. Implements onlyRunningSetter.
func (p *AllBuildsProvider) SetOnlyRunning(v bool) { p.onlyRunning = v }

// NewAllBuildsProvider creates an AllBuildsProvider. folderFilter may be "" for
// the global all-builds view, or a folder path (e.g. "Code") to scope the view.
func NewAllBuildsProvider(client jmodel.JenkinsClient, store *cache.Store, username string, slowInterval time.Duration, folderFilter string) *AllBuildsProvider {
	if slowInterval <= 0 {
		slowInterval = 2 * time.Minute
	}
	return &AllBuildsProvider{
		pollingProviderBase: newPollingProviderBase(),
		client:              client,
		store:               store,
		username:            username,
		folderFilter:        folderFilter,
		slowInterval:        slowInterval,
	}
}

func (p *AllBuildsProvider) Init() tea.Cmd {
	return p.fetchFull
}

func (p *AllBuildsProvider) Refresh() tea.Cmd {
	return nil
}

func (p *AllBuildsProvider) filter() buildregistry.Filter {
	return buildregistry.Filter{FolderPrefix: p.folderFilter, OnlyRunning: p.onlyRunning}
}

func (p *AllBuildsProvider) Builds() []UnifiedBuild {
	if p.store == nil || p.store.Registry == nil {
		return nil
	}
	flat := p.store.Registry.Query(p.filter())
	queued := queuedUnifiedBuilds(p.store, p.filter())
	out := make([]UnifiedBuild, 0, len(queued)+len(flat))
	out = append(out, queued...)
	for _, b := range flat {
		jobName, branchName := extractJobAndBranch(b.JobPath)
		out = append(out, UnifiedBuild{
			Build:       b.Build,
			JobPath:     b.JobPath,
			BranchName:  branchName,
			DisplayName: jobName,
		})
	}
	return out
}

func (p *AllBuildsProvider) HandleMsg(msg tea.Msg) (bool, []tea.Cmd) {
	switch msg := msg.(type) {
	case RunningBuildsUpdatedMsg:
		// Registry already absorbed the running-set snapshot; we only need to
		// keep the visual tick alive while anything is running in our view.
		var cmds []tea.Cmd
		if !p.visualTickActive && p.store != nil && p.store.Registry != nil &&
			p.store.Registry.HasRunning(p.filter()) {
			p.visualTickActive = true
			cmds = append(cmds, p.scheduleVisualTick())
		}
		return true, cmds

	case BuildCompletedMsg:
		// Registry has already been updated via ApplyCompletion by the monitor.
		// Nothing to do here beyond claiming the message.
		return true, nil

	case allBuildsFullMsg:
		if msg.err != nil {
			return true, []tea.Cmd{p.scheduleSlowTick()}
		}
		if p.store != nil && p.store.Registry != nil {
			p.store.Registry.IngestScan(msg.builds)
		}
		return true, []tea.Cmd{p.scheduleSlowTick()}

	case allBuildsSlowTickMsg:
		return true, []tea.Cmd{p.fetchFull}

	case allBuildsVisualTickMsg:
		if p.store != nil && p.store.Registry != nil && p.store.Registry.HasRunning(p.filter()) {
			return true, []tea.Cmd{p.scheduleVisualTick()}
		}
		p.visualTickActive = false
		return true, nil
	}
	return false, nil
}

func (p *AllBuildsProvider) Config() BuildsViewConfig {
	return BuildsViewConfig{}
}

func (p *AllBuildsProvider) fetchFull() tea.Msg {
	builds, err := p.client.ScanAllBuilds(context.Background(), maxBuildsPerJob)
	return allBuildsFullMsg{builds: builds, err: err}
}

func (p *AllBuildsProvider) scheduleSlowTick() tea.Cmd {
	return p.scheduleContextTick(p.slowInterval, func() tea.Msg { return allBuildsSlowTickMsg{} })
}

func (p *AllBuildsProvider) scheduleVisualTick() tea.Cmd {
	return p.scheduleContextTick(1*time.Second, func() tea.Msg { return allBuildsVisualTickMsg{} })
}

// abKey returns the deduplication key for a UserBuild.
// extractJobAndBranch derives the job name and branch name from a Jenkins JobPath.
func extractJobAndBranch(jobPath string) (string, string) {
	parts := strings.Split(jobPath, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return "", ""
}

// NewAllBuildsView returns a BuildsView backed by AllBuildsProvider scoped to the
// entire Jenkins instance (CtxRoot).
func NewAllBuildsView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store, username string, gitUsernames []string, slowInterval time.Duration) *BuildsView {
	nc := NavigationContext{Level: CtxRoot, Username: username, GitUsernames: gitUsernames}
	p := NewAllBuildsProvider(c, s, username, slowInterval, "")
	return NewBuildsView(t, c, s, nc, p)
}

// NewFolderBuildsView returns a BuildsView backed by AllBuildsProvider scoped to
// a specific folder (CtxFolder). Builds from other folders are filtered out.
func NewFolderBuildsView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store, folderPath, username string, gitUsernames []string, slowInterval time.Duration) *BuildsView {
	nc := NavigationContext{Level: CtxFolder, FolderPath: folderPath, Username: username, GitUsernames: gitUsernames}
	p := NewAllBuildsProvider(c, s, username, slowInterval, folderPath)
	return NewBuildsView(t, c, s, nc, p)
}

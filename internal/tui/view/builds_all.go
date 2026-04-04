package view

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// Internal message types for AllBuildsProvider polling.
type allBuildsSlowTickMsg struct{}
type allBuildsVisualTickMsg struct{}

type allBuildsFullMsg struct {
	builds []jenkins.UserBuild
	err    error
}

// maxBuildsPerJob is how many recent builds per job are fetched in the full scan.
const maxBuildsPerJob = 10

// AllBuildsProvider is a BuildDataProvider that shows recent builds across the
// entire Jenkins instance (or scoped to a folder when folderFilter is set).
// Two data sources:
//   - Fast: RunningBuildsUpdatedMsg from the shared RunningBuildsMonitor (no poll here).
//   - Slow (configurable, default 2min): ScanAllBuilds — full build history.
type AllBuildsProvider struct {
	pollingProviderBase
	client           jenkins.JenkinsClient
	store            *cache.Store
	username         string
	folderFilter     string // when non-empty, only show builds under this folder path
	fastBuilds       map[string]jenkins.UserBuild
	slowBuilds       map[string]jenkins.UserBuild
	slowInterval     time.Duration
	visualTickActive bool // true while a visual tick chain is in flight
}

// NewAllBuildsProvider creates an AllBuildsProvider. folderFilter may be "" for
// the global all-builds view, or a folder path (e.g. "Code") to scope the view.
func NewAllBuildsProvider(client jenkins.JenkinsClient, store *cache.Store, username string, slowInterval time.Duration, folderFilter string) *AllBuildsProvider {
	if slowInterval <= 0 {
		slowInterval = 2 * time.Minute
	}
	return &AllBuildsProvider{
		pollingProviderBase: newPollingProviderBase(),
		client:              client,
		store:               store,
		username:            username,
		folderFilter:        folderFilter,
		fastBuilds:          make(map[string]jenkins.UserBuild),
		slowBuilds:          make(map[string]jenkins.UserBuild),
		slowInterval:        slowInterval,
	}
}

func (p *AllBuildsProvider) Init() tea.Cmd {
	if p.store != nil {
		if cached := p.store.AllBuilds.Get(""); cached != nil {
			p.slowBuilds = make(map[string]jenkins.UserBuild, len(cached.Value))
			for _, b := range cached.Value {
				p.slowBuilds[abKey(b)] = b
			}
		}
		// Pre-populate fastBuilds from the running-builds cache (kept up to date by the monitor).
		if cached := p.store.RunningBuilds.Get(""); cached != nil {
			for _, b := range cached.Value {
				p.fastBuilds[abKey(b)] = b
			}
		}
	}
	return p.fetchFull
}

func (p *AllBuildsProvider) Refresh() tea.Cmd {
	return nil
}

func (p *AllBuildsProvider) Builds() []UnifiedBuild {
	merged := make(map[string]jenkins.UserBuild, len(p.slowBuilds)+len(p.fastBuilds))
	for k, v := range p.slowBuilds {
		merged[k] = v
	}
	// Fast builds overlay slow builds (same key: fast wins).
	for k, v := range p.fastBuilds {
		merged[k] = v
	}
	flat := make([]jenkins.UserBuild, 0, len(merged))
	for _, v := range merged {
		if p.folderFilter != "" && !strings.HasPrefix(v.JobPath, p.folderFilter+"/") {
			continue
		}
		flat = append(flat, v)
	}
	sort.Slice(flat, func(i, j int) bool {
		return flat[i].Timestamp.After(flat[j].Timestamp)
	})
	out := make([]UnifiedBuild, len(flat))
	for i, b := range flat {
		jobName, branchName := extractJobAndBranch(b.JobPath)
		out[i] = UnifiedBuild{
			Build:       b.Build,
			JobPath:     b.JobPath,
			BranchName:  branchName,
			DisplayName: jobName,
		}
	}
	return out
}

func (p *AllBuildsProvider) HandleMsg(msg tea.Msg) (bool, []tea.Cmd) {
	switch msg := msg.(type) {
	case RunningBuildsUpdatedMsg:
		newFast := make(map[string]jenkins.UserBuild, len(msg.Builds))
		for _, b := range msg.Builds {
			newFast[abKey(b)] = b
		}
		p.fastBuilds = newFast
		var cmds []tea.Cmd
		if !p.visualTickActive {
			for _, b := range p.fastBuilds {
				if b.Status == jenkins.BuildStatusRunning {
					p.visualTickActive = true
					cmds = append(cmds, p.scheduleVisualTick())
					break
				}
			}
		}
		return true, cmds

	case BuildCompletedMsg:
		if msg.Err == nil {
			if existing, ok := p.slowBuilds[msg.Key]; ok {
				existing.Status = msg.Build.Status
				existing.Duration = msg.Build.Duration
				p.slowBuilds[msg.Key] = existing
			} else if fastEntry, ok := p.fastBuilds[msg.Key]; ok {
				fastEntry.Status = msg.Build.Status
				fastEntry.Duration = msg.Build.Duration
				p.slowBuilds[msg.Key] = fastEntry
			}
		}
		delete(p.fastBuilds, msg.Key)
		return true, nil

	case allBuildsFullMsg:
		if msg.err != nil {
			return true, []tea.Cmd{p.scheduleSlowTick()}
		}
		if p.store != nil {
			p.store.AllBuilds.Put("", msg.builds)
		}
		p.slowBuilds = make(map[string]jenkins.UserBuild, len(msg.builds))
		for _, b := range msg.builds {
			p.slowBuilds[abKey(b)] = b
		}
		cmds := []tea.Cmd{p.scheduleSlowTick()}
		if p.store != nil && p.store.Disk != nil {
			builds := msg.builds
			disk := p.store.Disk
			cmds = append(cmds, func() tea.Msg {
				_ = disk.SaveAllBuilds(builds)
				return nil
			})
		}
		return true, cmds

	case allBuildsSlowTickMsg:
		return true, []tea.Cmd{p.fetchFull}

	case allBuildsVisualTickMsg:
		for _, b := range p.fastBuilds {
			if b.Status == jenkins.BuildStatusRunning {
				return true, []tea.Cmd{p.scheduleVisualTick()}
			}
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
func abKey(b jenkins.UserBuild) string {
	return jenkins.BuildKey(b.JobPath, b.Number)
}

// extractJobAndBranch derives the job name and branch name from a Jenkins JobPath.
// For "folder/project/branch" it returns ("project", "branch").
// For a single segment it returns (segment, "").
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
func NewAllBuildsView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store, username string, gitUsernames []string, slowInterval time.Duration) *BuildsView {
	nc := NavigationContext{Level: CtxRoot, Username: username, GitUsernames: gitUsernames}
	p := NewAllBuildsProvider(c, s, username, slowInterval, "")
	return NewBuildsView(t, c, s, nc, p)
}

// NewFolderBuildsView returns a BuildsView backed by AllBuildsProvider scoped to
// a specific folder (CtxFolder). Builds from other folders are filtered out.
func NewFolderBuildsView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store, folderPath, username string, gitUsernames []string, slowInterval time.Duration) *BuildsView {
	nc := NavigationContext{Level: CtxFolder, FolderPath: folderPath, Username: username, GitUsernames: gitUsernames}
	p := NewAllBuildsProvider(c, s, username, slowInterval, folderPath)
	return NewBuildsView(t, c, s, nc, p)
}

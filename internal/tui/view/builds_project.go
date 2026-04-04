package view

import (
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
)

type projectBuildsTickMsg struct{}
type projectBuildsVisualTickMsg struct{}

type projectBuildsResultMsg struct {
	builds []jenkins.ProjectBuild
	err    error
}

// ProjectBuildsProvider fetches builds across all branches of a multibranch
// project and implements BuildDataProvider.
type ProjectBuildsProvider struct {
	pollingProviderBase
	client jenkins.JenkinsClient
	store  *cache.Store
	nc     NavigationContext
	builds []jenkins.ProjectBuild
	tt     testTracker
}

// NewProjectBuildsProvider creates a provider for a multibranch project.
func NewProjectBuildsProvider(client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext) *ProjectBuildsProvider {
	return &ProjectBuildsProvider{
		pollingProviderBase: newPollingProviderBase(),
		client:              client,
		store:               store,
		nc:                  nc,
	}
}

func (p *ProjectBuildsProvider) Init() tea.Cmd {
	if p.store != nil {
		if e := p.store.ProjectBuilds.Get(p.nc.JobPath()); e != nil {
			p.builds = e.Value
			for _, b := range p.builds {
				p.tt.preloadOne(p.store, b.BranchPath, b.Number)
			}
		}
	}
	return p.fetchBuilds
}

func (p *ProjectBuildsProvider) Refresh() tea.Cmd {
	return p.fetchBuilds
}

func (p *ProjectBuildsProvider) fetchBuilds() tea.Msg {
	builds, err := p.client.ListProjectBuilds(p.ctx, p.nc.JobPath())
	if p.ctx.Err() != nil {
		return nil
	}
	return projectBuildsResultMsg{builds: builds, err: err}
}

func (p *ProjectBuildsProvider) scheduleRefresh() tea.Cmd {
	return p.scheduleContextTick(30*time.Second, func() tea.Msg { return projectBuildsTickMsg{} })
}

func (p *ProjectBuildsProvider) scheduleVisualTick() tea.Cmd {
	return p.scheduleContextTick(1*time.Second, func() tea.Msg { return projectBuildsVisualTickMsg{} })
}

func (p *ProjectBuildsProvider) hasRunning() bool {
	for _, b := range p.builds {
		if b.Status == jenkins.BuildStatusRunning {
			return true
		}
	}
	return false
}

func (p *ProjectBuildsProvider) HandleMsg(msg tea.Msg) (bool, []tea.Cmd) {
	switch msg := msg.(type) {
	case projectBuildsTickMsg:
		return true, []tea.Cmd{p.fetchBuilds}

	case projectBuildsVisualTickMsg:
		if p.hasRunning() {
			return true, []tea.Cmd{p.scheduleVisualTick()}
		}
		return true, nil

	case projectBuildsResultMsg:
		if msg.err != nil {
			return true, []tea.Cmd{func() tea.Msg { return ErrorMsg{Err: msg.err} }}
		}
		p.builds = msg.builds
		sort.Slice(p.builds, func(i, j int) bool {
			return p.builds[i].Build.Timestamp.After(p.builds[j].Build.Timestamp)
		})
		if p.store != nil {
			p.store.ProjectBuilds.Put(p.nc.JobPath(), msg.builds)
		}
		cmds := []tea.Cmd{p.scheduleRefresh()}
		if p.hasRunning() {
			cmds = append(cmds, p.scheduleVisualTick())
		}
		if p.store != nil {
			for _, b := range p.builds {
				if b.Status != jenkins.BuildStatusRunning {
					cmds = append(cmds, fetchTestReport(p.client, p.store, b.BranchPath, b.Number))
				}
			}
		}
		return true, cmds

	case TestReportMsg:
		p.tt.handleMsg(msg)
		return true, nil
	}
	return false, nil
}

func (p *ProjectBuildsProvider) Builds() []UnifiedBuild {
	result := make([]UnifiedBuild, len(p.builds))
	for i, b := range p.builds {
		result[i] = UnifiedBuild{
			Build:      b.Build,
			JobPath:    b.BranchPath,
			BranchName: b.BranchName,
			TestResult: p.tt.get(b.BranchPath, b.Number),
		}
	}
	return result
}

func (p *ProjectBuildsProvider) Config() BuildsViewConfig {
	return BuildsViewConfig{}
}

package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/view"
)

// Internal message types — never leave this package.
type monitorTickMsg struct{}
type monitorPollMsg struct {
	builds []jenkins.UserBuild
	err    error
}

// RunningBuildsMonitor polls ListRunningBuilds every 1s and feeds the
// buildregistry. It also emits RunningBuildsUpdatedMsg for views that want
// a tick signal, and BuildCompletedMsg for builds that just left the running
// set (so views like StageView can pattern-match on completion).
//
// Departure tracking lives in the registry — we just diff against
// store.Registry's previous live set on each poll.
type RunningBuildsMonitor struct {
	client   jenkins.JenkinsClient
	store    *cache.Store
	prevLive map[string]jenkins.UserBuild
}

// NewRunningBuildsMonitor creates a monitor. store may be nil in tests.
func NewRunningBuildsMonitor(client jenkins.JenkinsClient, store *cache.Store) *RunningBuildsMonitor {
	return &RunningBuildsMonitor{
		client:   client,
		store:    store,
		prevLive: make(map[string]jenkins.UserBuild),
	}
}

// Init returns the first poll command. Call from App.Init().
func (m *RunningBuildsMonitor) Init() tea.Cmd {
	return m.poll
}

// HandleMsg should be called for every message in App.Update().
func (m *RunningBuildsMonitor) HandleMsg(msg tea.Msg) (bool, []tea.Cmd) {
	switch msg := msg.(type) {
	case monitorTickMsg:
		return true, []tea.Cmd{m.poll}
	case monitorPollMsg:
		if msg.err != nil {
			return true, []tea.Cmd{m.scheduleTick()}
		}
		return true, m.processPoll(msg.builds)
	}
	return false, nil
}

// poll is the tea.Cmd that fetches running builds. Runs in a goroutine.
func (m *RunningBuildsMonitor) poll() tea.Msg {
	builds, err := m.client.ListRunningBuilds(context.Background())
	return monitorPollMsg{builds: builds, err: err}
}

func buildKeyJobPath(key string) string {
	if idx := strings.LastIndex(key, "#"); idx >= 0 {
		return key[:idx]
	}
	return key
}

func parentPath(jobPath string) string {
	if idx := strings.LastIndex(jobPath, "/"); idx >= 0 {
		return jobPath[:idx]
	}
	return ""
}

func jobInListing(jobs []jenkins.Job, jobPath string) bool {
	for _, j := range jobs {
		if j.FullPath == jobPath {
			return true
		}
	}
	return false
}

func (m *RunningBuildsMonitor) scheduleTick() tea.Cmd {
	return tea.Tick(1*time.Second, func(time.Time) tea.Msg {
		return monitorTickMsg{}
	})
}

func (m *RunningBuildsMonitor) processPoll(builds []jenkins.UserBuild) []tea.Cmd {
	polledAt := time.Now()
	newLive := make(map[string]jenkins.UserBuild, len(builds))
	for _, b := range builds {
		newLive[jenkins.BuildKey(b.JobPath, b.Number)] = b
	}

	arrived := make([]string, 0)
	for k := range newLive {
		if _, ok := m.prevLive[k]; !ok {
			arrived = append(arrived, k)
		}
	}

	if m.store != nil && len(arrived) > 0 {
		for _, k := range arrived {
			jobPath := buildKeyJobPath(k)
			folderPath := parentPath(jobPath)
			m.store.MarkBuildsDirty(jobPath)
			e := m.store.Jobs.Get(folderPath)
			if e == nil || !jobInListing(e.Value, jobPath) {
				m.store.MarkJobsDirty(folderPath)
			}
		}
	}

	departed := make([]string, 0)
	var completionCmds []tea.Cmd
	for k, b := range m.prevLive {
		if _, ok := newLive[k]; !ok {
			departed = append(departed, k)
			capturedKey := k
			captured := b
			client := m.client
			store := m.store
			completionCmds = append(completionCmds, func() tea.Msg {
				detail, err := client.GetBuild(context.Background(), captured.JobPath, captured.Number)
				if err != nil {
					return view.BuildCompletedMsg{
						Key: capturedKey, JobPath: captured.JobPath,
						Number: captured.Number, Err: err,
					}
				}
				if store != nil && store.Registry != nil {
					store.Registry.ApplyCompletion(
						buildregistry.Key{JobPath: captured.JobPath, Number: captured.Number},
						detail.Build,
					)
				}
				return view.BuildCompletedMsg{
					Key: capturedKey, JobPath: captured.JobPath,
					Number: captured.Number, Build: detail.Build,
				}
			})
			if store != nil {
				cacheKey := fmt.Sprintf("%s:%d", captured.JobPath, captured.Number)
				completionCmds = append(completionCmds, func() tea.Msg {
					stages, err := client.ListStages(context.Background(), captured.JobPath, captured.Number)
					if err == nil {
						store.Stages.Put(cacheKey, stages)
						if store.Disk != nil {
							_ = store.Disk.SaveStages(cacheKey, stages)
						}
					}
					return nil
				})
				completionCmds = append(completionCmds, func() tea.Msg {
					report, err := client.GetTestReport(context.Background(), captured.JobPath, captured.Number)
					if err == nil {
						store.TestReports.Put(cacheKey, report)
						if store.Disk != nil {
							_ = store.Disk.SaveTestReport(cacheKey, report)
						}
					}
					return nil
				})
			}
		}
	}

	m.prevLive = newLive
	if m.store != nil && m.store.Registry != nil {
		m.store.Registry.IngestRunningSnapshot(builds, polledAt)
	}

	updatedMsg := view.RunningBuildsUpdatedMsg{
		Builds:   builds,
		Arrived:  arrived,
		Departed: departed,
		Count:    len(builds),
	}
	cmds := make([]tea.Cmd, 0, len(completionCmds)+2)
	cmds = append(cmds, func() tea.Msg { return updatedMsg })
	cmds = append(cmds, completionCmds...)
	cmds = append(cmds, m.scheduleTick())
	return cmds
}

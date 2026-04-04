package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/view"
)

// Internal message types — never leave this package.
type monitorTickMsg struct{}
type monitorPollMsg struct {
	builds []jenkins.UserBuild
	err    error
}

// RunningBuildsMonitor polls ListRunningBuilds every 1s and emits
// RunningBuildsUpdatedMsg. It tracks arrivals/departures and fetches
// final build status for builds that just left the running set.
//
// Usage: call Init() from App.Init(), and pass every incoming message
// to HandleMsg() at the top of App.Update().
type RunningBuildsMonitor struct {
	client     jenkins.JenkinsClient
	store      *cache.Store
	prevBuilds map[string]jenkins.UserBuild // key -> last known snapshot
}

// NewRunningBuildsMonitor creates a monitor. store may be nil in tests.
func NewRunningBuildsMonitor(client jenkins.JenkinsClient, store *cache.Store) *RunningBuildsMonitor {
	return &RunningBuildsMonitor{
		client:     client,
		store:      store,
		prevBuilds: make(map[string]jenkins.UserBuild),
	}
}

// Init returns the first poll command. Call from App.Init().
func (m *RunningBuildsMonitor) Init() tea.Cmd {
	return m.poll
}

// HandleMsg should be called for every message in App.Update().
// Returns (true, cmds) when the message is an internal monitor message and
// was fully consumed. The caller must not process the message further.
func (m *RunningBuildsMonitor) HandleMsg(msg tea.Msg) (bool, []tea.Cmd) {
	switch msg := msg.(type) {
	case monitorTickMsg:
		return true, []tea.Cmd{m.poll}
	case monitorPollMsg:
		if msg.err != nil {
			// On error keep the previous state; schedule the next tick.
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

// buildKeyJobPath strips the "#number" suffix from a build key to recover the job path.
func buildKeyJobPath(key string) string {
	if idx := strings.LastIndex(key, "#"); idx >= 0 {
		return key[:idx]
	}
	return key
}

// parentPath returns the folder that contains a job path (strips the last segment).
// Returns "" for top-level paths.
func parentPath(jobPath string) string {
	if idx := strings.LastIndex(jobPath, "/"); idx >= 0 {
		return jobPath[:idx]
	}
	return ""
}

// jobInListing reports whether any job in the listing has the given full path.
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

// processPoll is called from HandleMsg (inside App.Update) — safe to mutate state.
func (m *RunningBuildsMonitor) processPoll(builds []jenkins.UserBuild) []tea.Cmd {
	newMap := make(map[string]jenkins.UserBuild, len(builds))
	for _, b := range builds {
		newMap[jenkins.BuildKey(b.JobPath, b.Number)] = b
	}

	arrived := make([]string, 0)
	for k := range newMap {
		if _, ok := m.prevBuilds[k]; !ok {
			arrived = append(arrived, k)
		}
	}

	if m.store != nil && len(arrived) > 0 {
		for _, k := range arrived {
			jobPath := buildKeyJobPath(k)
			folderPath := parentPath(jobPath)
			m.store.MarkBuildsDirty(jobPath)
			// Mark folder dirty only when we don't already know this job —
			// if the job is cached, RunningCount will be updated inline from the message.
			e := m.store.Jobs.Get(folderPath)
			if e == nil || !jobInListing(e.Value, jobPath) {
				m.store.MarkJobsDirty(folderPath)
			}
		}
	}

	departed := make([]string, 0)
	var completionCmds []tea.Cmd
	for k, b := range m.prevBuilds {
		if _, ok := newMap[k]; !ok {
			departed = append(departed, k)
			capturedKey := k
			captured := b
			client := m.client
			completionCmds = append(completionCmds, func() tea.Msg {
				detail, err := client.GetBuild(context.Background(), captured.JobPath, captured.Number)
				if err != nil {
					return view.BuildCompletedMsg{
						Key: capturedKey, JobPath: captured.JobPath,
						Number: captured.Number, Err: err,
					}
				}
				return view.BuildCompletedMsg{
					Key: capturedKey, JobPath: captured.JobPath,
					Number: captured.Number, Build: detail.Build,
				}
			})
			if m.store != nil {
				store := m.store
				cacheKey := fmt.Sprintf("%s:%d", captured.JobPath, captured.Number)
				// Cascade: fetch final stages and cache them (immutable after completion).
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
				// Cascade: fetch test report and cache it (immutable after completion).
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

	m.prevBuilds = newMap
	if m.store != nil {
		m.store.RunningBuilds.Put("", builds)
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

package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// Internal message types — never leave this package.
type monitorTickMsg struct{}
type monitorPollMsg struct {
	builds []jmodel.UserBuild
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
	client   jmodel.JenkinsClient
	store    *cache.Store
	prevLive map[string]jmodel.UserBuild
}

// NewRunningBuildsMonitor creates a monitor. store may be nil in tests.
func NewRunningBuildsMonitor(client jmodel.JenkinsClient, store *cache.Store) *RunningBuildsMonitor {
	return &RunningBuildsMonitor{
		client:   client,
		store:    store,
		prevLive: make(map[string]jmodel.UserBuild),
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

func jobInListing(jobs []jmodel.Job, jobPath string) bool {
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

// processPoll orchestrates a single poll cycle: diff against the previous
// snapshot, propagate dirty marks for arrivals, and fire follow-up work for
// departures. The three concerns are split into helpers below so each can
// be tested independently (see running_test.go).
func (m *RunningBuildsMonitor) processPoll(builds []jmodel.UserBuild) []tea.Cmd {
	polledAt := time.Now()
	newLive, arrived, departed := diffSnapshot(m.prevLive, builds)

	m.markArrivalsDirty(arrived)
	completionCmds := m.completionCmds(departed)

	m.prevLive = newLive
	if m.store != nil && m.store.Registry != nil {
		m.store.Registry.IngestRunningSnapshot(builds, polledAt)
	}

	arrivedKeys := keysOf(arrived)
	departedKeys := keysOf(departed)
	updatedMsg := navmsg.RunningBuildsUpdatedMsg{
		Builds:   builds,
		Arrived:  arrivedKeys,
		Departed: departedKeys,
		Count:    len(builds),
	}
	cmds := make([]tea.Cmd, 0, len(completionCmds)+2)
	cmds = append(cmds, func() tea.Msg { return updatedMsg })
	cmds = append(cmds, completionCmds...)
	cmds = append(cmds, m.scheduleTick())
	return cmds
}

// diffSnapshot is pure poll-dispatch logic: given the previous live set and
// the new poll result, it returns the new live set keyed by build key,
// arrivals (in new but not prev), and departures (in prev but not new).
//
// The returned slices carry the full UserBuild for arrivals (from the new
// snapshot) and departures (from the previous snapshot, so the completion
// cascade has stable identity even after prevLive is replaced).
func diffSnapshot(prev map[string]jmodel.UserBuild, builds []jmodel.UserBuild) (newLive map[string]jmodel.UserBuild, arrived, departed []keyedBuild) {
	newLive = make(map[string]jmodel.UserBuild, len(builds))
	for _, b := range builds {
		newLive[jmodel.BuildKey(b.JobPath, b.Number)] = b
	}
	for k, b := range newLive {
		if _, ok := prev[k]; !ok {
			arrived = append(arrived, keyedBuild{key: k, build: b})
		}
	}
	for k, b := range prev {
		if _, ok := newLive[k]; !ok {
			departed = append(departed, keyedBuild{key: k, build: b})
		}
	}
	return newLive, arrived, departed
}

// keyedBuild pairs a build with its registry key. Used to pass the diff
// results between the three split helpers without re-deriving keys.
type keyedBuild struct {
	key   string
	build jmodel.UserBuild
}

func keysOf(kbs []keyedBuild) []string {
	out := make([]string, 0, len(kbs))
	for _, kb := range kbs {
		out = append(out, kb.key)
	}
	return out
}

// markArrivalsDirty is the dirty-tracking concern: for every newly-arrived
// running build, invalidate the builds cache for its job, and the jobs
// listing of its parent folder if the job is not already listed there.
func (m *RunningBuildsMonitor) markArrivalsDirty(arrived []keyedBuild) {
	if m.store == nil || len(arrived) == 0 {
		return
	}
	for _, kb := range arrived {
		jobPath := buildKeyJobPath(kb.key)
		folderPath := parentPath(jobPath)
		m.store.MarkBuildsDirty(jobPath)
		e := m.store.Jobs.Get(folderPath)
		if e == nil || !jobInListing(e.Value, jobPath) {
			m.store.MarkJobsDirty(folderPath)
		}
	}
}

// completionCmds is the completion-cascade concern: for every build that
// just left the running set, build the follow-up tea.Cmds (fetch detail,
// refresh stages, refresh test report). Pure with respect to the monitor —
// it only reads m.client / m.store — so it can be unit-tested by feeding
// in a synthetic departed slice and a fake JenkinsClient.
func (m *RunningBuildsMonitor) completionCmds(departed []keyedBuild) []tea.Cmd {
	if len(departed) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(departed)*3)
	for _, kb := range departed {
		cmds = append(cmds, m.fetchBuildDetailCmd(kb))
		if m.store == nil {
			continue
		}
		cacheKey := fmt.Sprintf("%s:%d", kb.build.JobPath, kb.build.Number)
		cmds = append(cmds, m.refreshStagesCmd(kb.build, cacheKey))
		cmds = append(cmds, m.refreshTestReportCmd(kb.build, cacheKey))
	}
	return cmds
}

// fetchBuildDetailCmd returns a tea.Cmd that fetches the completed build's
// detail and applies it to the registry. The resulting message is the
// public BuildCompletedMsg that views pattern-match on.
func (m *RunningBuildsMonitor) fetchBuildDetailCmd(kb keyedBuild) tea.Cmd {
	client := m.client
	store := m.store
	capturedKey := kb.key
	captured := kb.build
	return func() tea.Msg {
		detail, err := client.GetBuild(context.Background(), captured.JobPath, captured.Number)
		if err != nil {
			return navmsg.BuildCompletedMsg{
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
		return navmsg.BuildCompletedMsg{
			Key: capturedKey, JobPath: captured.JobPath,
			Number: captured.Number, Build: detail.Build,
		}
	}
}

// refreshStagesCmd returns a tea.Cmd that re-fetches stages for a completed
// build and writes them through to the in-memory + disk cache. Returns nil
// message because no view needs to react synchronously.
func (m *RunningBuildsMonitor) refreshStagesCmd(b jmodel.UserBuild, cacheKey string) tea.Cmd {
	client := m.client
	store := m.store
	return func() tea.Msg {
		stages, err := client.ListStages(context.Background(), b.JobPath, b.Number)
		if err == nil {
			store.Stages.Put(cacheKey, stages)
			if store.Disk != nil {
				_ = store.Disk.SaveStages(cacheKey, stages)
			}
		}
		return nil
	}
}

// refreshTestReportCmd is the test-report counterpart of refreshStagesCmd.
func (m *RunningBuildsMonitor) refreshTestReportCmd(b jmodel.UserBuild, cacheKey string) tea.Cmd {
	client := m.client
	store := m.store
	return func() tea.Msg {
		report, err := client.GetTestReport(context.Background(), b.JobPath, b.Number)
		if err == nil {
			store.TestReports.Put(cacheKey, report)
			if store.Disk != nil {
				_ = store.Disk.SaveTestReport(cacheKey, report)
			}
		}
		return nil
	}
}

package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// eventBuffer is the capacity of the subscriber channel. Poll updates arrive
// about once a second; a healthy TUI drains one per Update cycle, so this only
// absorbs brief bursts (a batch of completions). Sends never block the poll
// loop — a full buffer drops the event (the next poll supersedes a running
// update; a dropped completion is re-observed on the next interaction).
const eventBuffer = 256

// Subscribe attaches a single consumer (the TUI) and returns the channel the
// engine publishes navmsg.* messages on. It must be called before Start so the
// warm poll's events are captured. When no subscriber is attached (the MCP
// host), the engine skips the diff/cascade work entirely and only keeps the
// registry and sampler live.
func (e *Engine) Subscribe() <-chan any {
	e.events = make(chan any, eventBuffer)
	e.prevLive = make(map[string]jmodel.UserBuild)
	return e.events
}

// emit publishes a message to the subscriber, dropping it if the buffer is full
// so a slow consumer can never stall the poll loop.
func (e *Engine) emit(msg any) {
	if e.events == nil {
		return
	}
	select {
	case e.events <- msg:
	default:
	}
}

// Sampler exposes the live sample buffer so the TUI dashboard can read the
// history the engine keeps, instead of sampling independently.
func (e *Engine) Sampler() *Sampler { return e.sampler }

// Queue exposes the live queue-wait tracker for the dashboard's in-progress bars.
func (e *Engine) Queue() *QueueTracker { return e.queue }

// reconcileSnapshot diffs the new running set against the previous one, marks
// caches dirty for arrivals, and launches the completion cascade for departures.
// It runs only when a subscriber (TUI) is attached. It returns the arrival and
// departure registry keys for the RunningBuildsUpdatedMsg.
func (e *Engine) reconcileSnapshot(ctx context.Context, builds []jmodel.UserBuild) (arrived, departed []string) {
	newLive := make(map[string]jmodel.UserBuild, len(builds))
	for _, b := range builds {
		newLive[jmodel.BuildKey(b.JobPath, b.Number)] = b
	}
	var arrivals, departures []keyedBuild
	for k, b := range newLive {
		if _, ok := e.prevLive[k]; !ok {
			arrivals = append(arrivals, keyedBuild{key: k, build: b})
		}
	}
	for k, b := range e.prevLive {
		if _, ok := newLive[k]; !ok {
			departures = append(departures, keyedBuild{key: k, build: b})
		}
	}
	e.prevLive = newLive

	e.markArrivalsDirty(arrivals)
	for _, kb := range departures {
		go e.completeBuild(ctx, kb)
	}
	return keysOf(arrivals), keysOf(departures)
}

// keyedBuild pairs a build with its registry key.
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

// markArrivalsDirty invalidates the builds cache for each newly-running job (and
// its parent folder listing when the job is not yet listed), and evicts stale
// test-report/artifact/stage entries so a previous run's data with the same
// number does not surface as the new build's results.
func (e *Engine) markArrivalsDirty(arrived []keyedBuild) {
	if e.store == nil || len(arrived) == 0 {
		return
	}
	for _, kb := range arrived {
		jobPath := buildKeyJobPath(kb.key)
		folderPath := parentPath(jobPath)
		e.store.MarkBuildsDirty(jobPath)
		entry := e.store.Jobs.Get(folderPath)
		if entry == nil || !jobInListing(entry.Value, jobPath) {
			e.store.MarkJobsDirty(folderPath)
		}
		cacheKey := fmt.Sprintf("%s:%d", kb.build.JobPath, kb.build.Number)
		e.store.TestReports.Delete(cacheKey)
		e.store.Artifacts.Delete(cacheKey)
		e.store.Stages.Delete(cacheKey)
	}
}

// completeBuild runs the completion cascade for a build that just left the
// running set: fetch its final detail, apply it to the registry, emit the
// public BuildCompletedMsg, and refresh the cached stages and test report.
// Runs in its own goroutine so it never blocks the poll loop.
func (e *Engine) completeBuild(ctx context.Context, kb keyedBuild) {
	detail, err := e.client.GetBuild(ctx, kb.build.JobPath, kb.build.Number)
	if err != nil {
		e.emit(navmsg.BuildCompletedMsg{
			Key: kb.key, JobPath: kb.build.JobPath, Number: kb.build.Number, Err: err,
		})
		return
	}
	if e.store != nil && e.store.Registry != nil {
		e.store.Registry.ApplyCompletion(
			buildregistry.Key{JobPath: kb.build.JobPath, Number: kb.build.Number},
			detail.Build,
		)
	}
	e.emit(navmsg.BuildCompletedMsg{
		Key: kb.key, JobPath: kb.build.JobPath, Number: kb.build.Number, Build: detail.Build,
	})
	e.refreshCompletedCaches(ctx, kb.build)
}

// refreshCompletedCaches re-fetches and stores the stages and test report of a
// completed build (best effort — a fetch error just leaves the cache untouched).
func (e *Engine) refreshCompletedCaches(ctx context.Context, b jmodel.UserBuild) {
	if e.store == nil {
		return
	}
	cacheKey := fmt.Sprintf("%s:%d", b.JobPath, b.Number)
	if stages, err := e.client.ListStages(ctx, b.JobPath, b.Number); err == nil {
		e.store.Stages.Put(cacheKey, stages)
		if e.store.Disk != nil {
			_ = e.store.Disk.SaveStages(cacheKey, stages)
		}
	}
	if report, err := e.client.GetTestReport(ctx, b.JobPath, b.Number); err == nil {
		e.store.TestReports.Put(cacheKey, report)
		if e.store.Disk != nil {
			_ = e.store.Disk.SaveTestReport(cacheKey, report)
		}
	}
}

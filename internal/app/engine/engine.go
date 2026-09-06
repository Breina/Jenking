// Package engine hosts Jenking's headless stateful background work: it polls
// the Jenkins controller and keeps the shared buildregistry (and, in a later
// slice, the dashboard sampler) live. It is tea-free and driven by both the
// MCP server and — eventually — the TUI, so the running-state truth is computed
// exactly once regardless of which front end is attached.
package engine

import (
	"context"
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// defaultPollInterval matches the TUI monitor's 1s cadence. It doubles as the
// hot window for the shared live-poll snapshot: concurrent Jenking processes
// (TUI + MCP) on one machine elect a single poller through the cache dir, so
// the controller sees one poll per interval no matter how many instances run.
// See cache.LivePoll and Engine.livePoll.
const defaultPollInterval = time.Second

// sampleSaveEvery persists the sample buffer every N polls. At the 1s cadence
// that is a disk write roughly every 15s — cheap and flock-merged with any TUI.
const sampleSaveEvery = 15

// Engine polls running builds and the queue, feeding the registry (truthful
// running state) and the dashboard sampler (queue-wait history). Build
// completion is handled by the registry's reconciler, wired here.
type Engine struct {
	client   jmodel.JenkinsClient
	store    *cache.Store
	interval time.Duration

	sampler   *Sampler
	queue     *QueueTracker
	sinceSave int

	// events is the TUI subscriber channel (nil for the MCP host). prevLive is
	// the previous poll's running set, diffed each poll to detect arrivals and
	// departures; both are only used when a subscriber is attached.
	events   chan any
	prevLive map[string]jmodel.UserBuild
}

// New builds an Engine over the same client and store the front end uses,
// seeding the sampler with any persisted history.
func New(client jmodel.JenkinsClient, store *cache.Store) *Engine {
	e := &Engine{
		client:   client,
		store:    store,
		interval: defaultPollInterval,
		sampler:  NewSampler(0),
		queue:    NewQueueTracker(),
	}
	if store != nil && store.Disk != nil {
		if samples, err := store.Disk.LoadDashSamples(); err == nil {
			e.sampler.Load(samples)
		}
	}
	return e
}

// Start wires registry reconciliation, ingests an initial snapshot so callers
// serve against a warm registry, and launches the poll loop in the background.
// The loop runs until ctx is cancelled.
func (e *Engine) Start(ctx context.Context) {
	if e.store != nil && e.store.Registry != nil {
		rec := buildregistry.NewReconciler(e.client, e.store.Registry, nil)
		e.store.Registry.SetReconcile(rec.Reconcile)
	}
	e.pollOnce(ctx) // warm start; best-effort
	go e.loop(ctx)
}

func (e *Engine) loop(ctx context.Context) {
	t := time.NewTicker(e.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			e.persist() // final flush of the in-memory tail
			return
		case <-t.C:
			e.pollOnce(ctx)
		}
	}
}

// pollOnce obtains the running set and queue, ingests running builds into the
// registry, and records a dashboard sample. A transient error is swallowed —
// the next tick retries, and the recurring poll doubles as the reconnection
// probe.
func (e *Engine) pollOnce(ctx context.Context) {
	if e.store == nil || e.store.Registry == nil {
		return
	}
	lp, err := e.livePoll(ctx)
	if err != nil {
		// Surface the failure so the TUI can mark the connection lost; the
		// recurring poll doubles as the reconnection probe.
		e.emit(navmsg.ConnectionLostMsg{Err: err})
		return
	}
	now := time.Now()

	// With a TUI subscriber, diff the snapshot for arrivals/departures (cache
	// eviction + completion cascade) and broadcast the running-builds update.
	var arrived, departed []string
	if e.events != nil {
		arrived, departed = e.reconcileSnapshot(ctx, lp.Running)
	}
	e.store.Registry.IngestRunningSnapshot(lp.Running, now)
	// Warm the SCM-URL reverse index from the running set (main, lightweight
	// driver). Deduped against RepoURLs, so steady-state cost is ~zero.
	FillSCMURLs(ctx, e.client, e.store, jobPathsOf(lp.Running))
	counts := e.applyQueue(now, lp)
	if e.events != nil {
		e.emit(navmsg.RunningBuildsUpdatedMsg{
			Builds:      lp.Running,
			Arrived:     arrived,
			Departed:    departed,
			Count:       len(lp.Running),
			QueuedCount: counts.Builds,
			ScanCount:   counts.Scans,
		})
	}
}

// livePoll returns this tick's snapshot of the controller. When a sibling
// Jenking process published one within the last interval, that snapshot is
// reused and no request is made; otherwise this process claims the round,
// fetches, and publishes the result for the others. Without a disk store
// (tests, persistence disabled) it always fetches directly.
//
// Only a running-builds failure is an error: the queue is secondary, so a queue
// failure is carried as QueueOK=false and propagated to siblings, keeping their
// behaviour identical to a first-hand fetch.
func (e *Engine) livePoll(ctx context.Context) (cache.LivePoll, error) {
	disk := e.store.Disk
	if disk != nil {
		if lp, hot := disk.ClaimPoll(e.interval); hot {
			return lp, nil
		}
	}
	builds, err := e.client.ListRunningBuilds(ctx)
	if err != nil {
		// Nothing is published: our claim stamp still holds siblings off for one
		// cycle, after which one of them reclaims and rediscovers the failure.
		return cache.LivePoll{}, err
	}
	items, qerr := e.client.ListQueue(ctx)
	if qerr != nil {
		items = nil
	}
	if disk != nil {
		_ = disk.PublishPoll(builds, items, qerr == nil)
	}
	return cache.LivePoll{Running: builds, Queue: items, QueueOK: qerr == nil}, nil
}

// queueCounts is one poll's queue tally, split by kind. Scans are counted but
// kept out of every build-oriented number: they never become builds, and a
// nightly indexing fanout would otherwise inflate the queue counter and render
// the whole dashboard histogram "blocked".
type queueCounts struct {
	Builds int
	Scans  int
}

// applyQueue refreshes the shared queue snapshot and records one dashboard
// sample (running/queued counts + the wait-bin histogram). A failed queue fetch
// leaves the previous snapshot intact and skips the sample.
func (e *Engine) applyQueue(now time.Time, lp cache.LivePoll) queueCounts {
	if !lp.QueueOK {
		return queueCounts{}
	}
	if e.store.Queue != nil {
		e.store.Queue.Replace(lp.Queue)
	}
	// Queued count excludes items whose job already has a running build (those
	// rows fold into the running build), matching the dashboard's activity line.
	var counts queueCounts
	builds := make([]jmodel.QueueItem, 0, len(lp.Queue))
	for _, it := range lp.Queue {
		if it.KindOrBuild() == jmodel.QueueKindScan {
			counts.Scans++
			continue
		}
		builds = append(builds, it)
		if !e.store.Registry.HasRunning(buildregistry.Filter{JobPath: it.JobPath}) {
			counts.Builds++
		}
	}
	wr := e.queue.Observe(now, builds)
	e.sampler.Add(now, e.store.Registry.RunningCount(), counts.Builds, wr)

	if e.sinceSave++; e.sinceSave >= sampleSaveEvery {
		e.sinceSave = 0
		e.persist()
	}
	return counts
}

func (e *Engine) persist() {
	if e.store != nil && e.store.Disk != nil {
		_ = e.store.Disk.SaveDashSamples(e.sampler.Dump())
	}
}

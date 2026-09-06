package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// engFakeClient serves a canned running set and queue.
type engFakeClient struct {
	jmodel.JenkinsClient
	running []jmodel.UserBuild
	queue   []jmodel.QueueItem
	err     error
}

func (f engFakeClient) ListRunningBuilds(context.Context) ([]jmodel.UserBuild, error) {
	return f.running, f.err
}

func (f engFakeClient) ListQueue(context.Context) ([]jmodel.QueueItem, error) {
	return f.queue, nil
}

func (f engFakeClient) GetJobSCMURL(context.Context, string) (string, error) {
	return "", nil
}

func TestEngine_PollIngestsRunning(t *testing.T) {
	store := cache.NewStore(nil)
	e := New(engFakeClient{running: []jmodel.UserBuild{
		{JobPath: "team/app", Build: jmodel.Build{Number: 42, Status: jmodel.BuildStatusRunning}},
	}}, store)

	e.pollOnce(context.Background())

	running := store.Registry.Query(buildregistry.Filter{OnlyRunning: true})
	if len(running) != 1 || running[0].Number != 42 {
		t.Fatalf("registry running set = %+v, want build 42", running)
	}
	if store.Registry.RunningCount() != 1 {
		t.Errorf("RunningCount = %d, want 1", store.Registry.RunningCount())
	}
}

func TestEngine_PollRecordsSample(t *testing.T) {
	store := cache.NewStore(nil)
	e := New(engFakeClient{
		running: []jmodel.UserBuild{{JobPath: "team/app", Build: jmodel.Build{Number: 42, Status: jmodel.BuildStatusRunning}}},
		queue:   []jmodel.QueueItem{{ID: 1, JobPath: "team/other", Buildable: true}},
	}, store)

	e.pollOnce(context.Background())

	samples := e.sampler.Dump()
	if len(samples) != 1 {
		t.Fatalf("sampler recorded %d samples, want 1", len(samples))
	}
	if samples[0].Running != 1 || samples[0].Queued != 1 {
		t.Errorf("sample = %+v, want running=1 queued=1", samples[0])
	}
}

func TestEngine_ScansAreCountedApartFromBuilds(t *testing.T) {
	// A nightly indexing fanout must not read as queue pressure: scans are
	// counted separately, kept out of the dashboard sample, and never reach the
	// wait tracker (where their permanent "blocked" state would dominate the
	// reason histogram for every bin they pass through).
	queue := []jmodel.QueueItem{
		{ID: 1, Kind: jmodel.QueueKindBuild, JobPath: "team/app", Buildable: true},
		{ID: 2, Kind: jmodel.QueueKindScan, JobPath: "Bodem/codelijst-kleur", Blocked: true},
		{ID: 3, Kind: jmodel.QueueKindScan, JobPath: "Bodem/codelijst-materiaalklasse", Blocked: true},
	}
	store := cache.NewStore(nil)
	e := New(engFakeClient{queue: queue}, store)

	counts := e.applyQueue(time.Now(), cache.LivePoll{Queue: queue, QueueOK: true})

	if counts.Builds != 1 || counts.Scans != 2 {
		t.Errorf("counts = %+v, want builds=1 scans=2", counts)
	}
	samples := e.sampler.Dump()
	if len(samples) != 1 || samples[0].Queued != 1 {
		t.Errorf("sample = %+v, want a single sample with queued=1", samples)
	}
	if n := len(e.queue.items); n != 1 {
		t.Errorf("wait tracker holds %d items, want 1 (builds only)", n)
	}
	// The store keeps every kind — one fetch feeds both the builds views and
	// the scans view — so only the *counting* is split.
	if got := store.Queue.CountVisible(nil, jmodel.QueueKindScan); got != 2 {
		t.Errorf("store scan count = %d, want 2", got)
	}
}

func TestEngine_PollErrorIsSwallowed(t *testing.T) {
	store := cache.NewStore(nil)
	e := New(engFakeClient{err: context.DeadlineExceeded}, store)
	// A transient poll error must not panic or corrupt the registry.
	e.pollOnce(context.Background())
	if store.Registry.RunningCount() != 0 {
		t.Errorf("RunningCount = %d, want 0 after failed poll", store.Registry.RunningCount())
	}
}

// With a disk store attached, a poll must still work end to end and publish its
// snapshot for sibling processes. (The election logic itself — who polls and who
// reuses — is covered by the cache package's ClaimPoll tests.)
func TestEngine_PollPublishesSnapshot(t *testing.T) {
	// Not t.TempDir(): the store's async registry writer may still be flushing
	// when the test ends, and its cleanup fails a non-empty directory.
	dir, err := os.MkdirTemp("", "engine-livepoll")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	disk, err := cache.NewDiskStore(dir)
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	store := cache.NewStore(disk)
	e := New(engFakeClient{
		running: []jmodel.UserBuild{{JobPath: "team/app", Build: jmodel.Build{Number: 42, Status: jmodel.BuildStatusRunning}}},
		queue:   []jmodel.QueueItem{{ID: 1, JobPath: "team/other", Buildable: true}},
	}, store)

	e.pollOnce(context.Background())

	if store.Registry.RunningCount() != 1 {
		t.Fatalf("RunningCount = %d, want 1", store.Registry.RunningCount())
	}
	// The snapshot is now on disk; a sibling process would read it via ClaimPoll.
	// This process is the owner, so it keeps polling on its own cadence.
	if _, hot := disk.ClaimPoll(time.Minute); hot {
		t.Error("the publishing process must not gate itself on its own snapshot")
	}
}

func TestEngine_StartWarmsAndStops(t *testing.T) {
	store := cache.NewStore(nil)
	e := New(engFakeClient{running: []jmodel.UserBuild{
		{JobPath: "team/app", Build: jmodel.Build{Number: 7, Status: jmodel.BuildStatusRunning}},
	}}, store)

	ctx, cancel := context.WithCancel(context.Background())
	e.Start(ctx) // synchronous warm poll before returning
	if store.Registry.RunningCount() != 1 {
		t.Errorf("expected warm registry after Start, RunningCount = %d", store.Registry.RunningCount())
	}
	cancel() // loop goroutine observes cancellation and returns
}

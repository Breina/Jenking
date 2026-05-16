package buildregistry

import (
	"sync"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/jenkins"
)

func ub(jobPath string, num int, status jenkins.BuildStatus, ts time.Time) jenkins.UserBuild {
	return jenkins.UserBuild{
		JobPath: jobPath,
		Build:   jenkins.Build{Number: num, Status: status, Timestamp: ts},
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestRegistry(t *testing.T, clock *fakeClock) (*Registry, *[]Key) {
	t.Helper()
	var mu sync.Mutex
	reconciled := make([]Key, 0)
	r := New(Config{
		RunTTL: 5 * time.Second,
		Now:    clock.now,
		Reconcile: func(k Key) {
			mu.Lock()
			defer mu.Unlock()
			reconciled = append(reconciled, k)
		},
	})
	return r, &reconciled
}

// Invariant 2: scan reports Running but key not in live set → Query downgrades to Unknown and schedules reconcile.
func TestQueryDowngradesUnconfirmedRunning(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r, reconciled := newTestRegistry(t, clock)
	now := clock.t
	r.IngestScan([]jenkins.UserBuild{ub("job/a", 1, jenkins.BuildStatusRunning, now)})

	// Advance past TTL so a previous LastSeenRunning would expire — but here
	// scan ingestion sets no LastSeenRunning at all, so the record is never confirmed.
	clock.t = now.Add(10 * time.Second)
	out := r.Query(Filter{})
	if len(out) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out))
	}
	if out[0].Status != jenkins.BuildStatusUnknown {
		t.Errorf("expected unconfirmed Running to display as Unknown, got %v", out[0].Status)
	}
	if len(*reconciled) == 0 {
		t.Error("expected reconcile to be scheduled for unconfirmed running build")
	}
}

// Live confirmation: scan Running + running-set confirms → Query returns Running.
func TestQueryReturnsRunningWhenLive(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r, _ := newTestRegistry(t, clock)
	now := clock.t
	r.IngestScan([]jenkins.UserBuild{ub("job/a", 1, jenkins.BuildStatusRunning, now)})
	r.IngestRunningSnapshot([]jenkins.UserBuild{ub("job/a", 1, jenkins.BuildStatusRunning, now)}, now)

	out := r.Query(Filter{})
	if len(out) != 1 || out[0].Status != jenkins.BuildStatusRunning {
		t.Fatalf("expected Running, got %+v", out)
	}
}

// Invariant 1: terminal is sticky. ApplyCompletion(Success) then scan reporting Running must not flip it back.
func TestTerminalIsSticky(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r, _ := newTestRegistry(t, clock)
	k := Key{JobPath: "job/a", Number: 1}
	r.ApplyCompletion(k, jenkins.Build{Number: 1, Status: jenkins.BuildStatusSuccess, Timestamp: clock.t})

	// Stale scan that still thinks the build is running.
	r.IngestScan([]jenkins.UserBuild{ub("job/a", 1, jenkins.BuildStatusRunning, clock.t)})
	out := r.Query(Filter{})
	if len(out) != 1 || out[0].Status != jenkins.BuildStatusSuccess {
		t.Fatalf("expected sticky Success after completion, got %+v", out)
	}
}

// Departure: a key in last liveRunning but absent in new snapshot schedules reconcile.
func TestDepartureTriggersReconcile(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r, reconciled := newTestRegistry(t, clock)
	now := clock.t
	k := Key{JobPath: "job/a", Number: 1}
	r.IngestRunningSnapshot([]jenkins.UserBuild{ub("job/a", 1, jenkins.BuildStatusRunning, now)}, now)
	r.IngestRunningSnapshot(nil, now.Add(time.Second))

	found := false
	for _, rk := range *reconciled {
		if rk == k {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reconcile for departed key %v, got %+v", k, *reconciled)
	}
}

// Within TTL of last live confirmation, Running stays Running even if not in current live set
// (covers the transient gap between monitor ticks).
func TestRunningStaysWithinTTL(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r, _ := newTestRegistry(t, clock)
	start := clock.t
	r.IngestRunningSnapshot([]jenkins.UserBuild{ub("job/a", 1, jenkins.BuildStatusRunning, start)}, start)
	// Next tick: build no longer in live set, but TTL hasn't elapsed.
	r.IngestRunningSnapshot(nil, start.Add(time.Second))
	clock.t = start.Add(2 * time.Second)
	out := r.Query(Filter{})
	if len(out) != 1 || out[0].Status != jenkins.BuildStatusRunning {
		t.Fatalf("expected Running within TTL, got %+v", out)
	}
	// Past TTL: downgrade.
	clock.t = start.Add(10 * time.Second)
	out = r.Query(Filter{})
	if out[0].Status != jenkins.BuildStatusUnknown {
		t.Errorf("expected Unknown past TTL, got %v", out[0].Status)
	}
}

// LoadFromDisk with Running records → not visible as Running until live confirmation,
// and a reconcile is scheduled per running record.
func TestLoadFromDiskDoesNotShowStaleRunning(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r, reconciled := newTestRegistry(t, clock)
	r.LoadFromDisk([]Record{{
		Build:           jenkins.Build{Number: 1, Status: jenkins.BuildStatusRunning, Timestamp: clock.t.Add(-time.Hour)},
		JobPath:         "job/a",
		LastSeenRunning: clock.t.Add(-time.Hour), // stale
	}})
	out := r.Query(Filter{})
	if len(out) != 1 {
		t.Fatalf("expected 1 record after disk load, got %d", len(out))
	}
	if out[0].Status == jenkins.BuildStatusRunning {
		t.Errorf("expected disk-loaded Running to be downgraded, got Running")
	}
	if len(*reconciled) == 0 {
		t.Error("expected reconcile to be scheduled for disk-loaded running build")
	}
}

// Folder filter narrows results.
func TestQueryFolderFilter(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r, _ := newTestRegistry(t, clock)
	r.IngestScan([]jenkins.UserBuild{
		ub("Code/proj/main", 1, jenkins.BuildStatusSuccess, clock.t),
		ub("Other/proj/main", 1, jenkins.BuildStatusSuccess, clock.t),
	})
	out := r.Query(Filter{FolderPrefix: "Code"})
	if len(out) != 1 || out[0].JobPath != "Code/proj/main" {
		t.Fatalf("expected only Code/* records, got %+v", out)
	}
}

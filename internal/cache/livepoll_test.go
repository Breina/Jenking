package cache

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// siblings returns two DiskStore instances rooted at the same directory with
// distinct owners, simulating the TUI and the MCP server sharing a cache dir.
func siblings(t *testing.T) (*DiskStore, *DiskStore) {
	t.Helper()
	dir := t.TempDir()
	a, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("NewDiskStore a: %v", err)
	}
	b, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("NewDiskStore b: %v", err)
	}
	a.owner, b.owner = 1, 2
	return a, b
}

func runningSet(job string) []jmodel.UserBuild {
	return []jmodel.UserBuild{{JobPath: job, Build: jmodel.Build{Number: 7}}}
}

// A lone process must never gate itself: its own published stamp is always
// stale to itself, so it keeps polling on its own cadence.
func TestClaimPoll_OwnStampNeverGatesSelf(t *testing.T) {
	a, _ := siblings(t)

	if _, hot := a.ClaimPoll(time.Minute); hot {
		t.Fatal("first claim on an empty dir must not be hot")
	}
	if err := a.PublishPoll(runningSet("job/a"), nil, true); err != nil {
		t.Fatalf("PublishPoll: %v", err)
	}
	if _, hot := a.ClaimPoll(time.Minute); hot {
		t.Fatal("a process must not serve its own published snapshot")
	}
}

// The core win: while one process's snapshot is hot, the sibling reuses it and
// makes no request of its own.
func TestClaimPoll_SiblingReusesHotSnapshot(t *testing.T) {
	a, b := siblings(t)

	a.ClaimPoll(time.Minute)
	queue := []jmodel.QueueItem{{ID: 3, JobPath: "job/q"}}
	if err := a.PublishPoll(runningSet("job/a"), queue, true); err != nil {
		t.Fatalf("PublishPoll: %v", err)
	}

	lp, hot := b.ClaimPoll(time.Minute)
	if !hot {
		t.Fatal("sibling must reuse a hot snapshot instead of polling")
	}
	if len(lp.Running) != 1 || lp.Running[0].JobPath != "job/a" || lp.Running[0].Number != 7 {
		t.Errorf("running set not carried across processes: %+v", lp.Running)
	}
	if len(lp.Queue) != 1 || lp.Queue[0].ID != 3 {
		t.Errorf("queue not carried across processes: %+v", lp.Queue)
	}
	if !lp.QueueOK {
		t.Error("QueueOK not carried across processes")
	}
}

// Once the snapshot ages out, the sibling takes the round over — this is how a
// dead or stalled poller is replaced.
func TestClaimPoll_StaleSnapshotIsReclaimed(t *testing.T) {
	a, b := siblings(t)

	a.ClaimPoll(time.Minute)
	if err := a.PublishPoll(runningSet("job/a"), nil, true); err != nil {
		t.Fatalf("PublishPoll: %v", err)
	}

	if _, hot := b.ClaimPoll(time.Nanosecond); hot {
		t.Fatal("an aged-out snapshot must not be served")
	}
	// b now owns the round, so a backs off for a cycle.
	if _, hot := a.ClaimPoll(time.Minute); !hot {
		t.Fatal("the previous owner must back off once a sibling claims the round")
	}
}

// A claim keeps the previous payload, so a sibling ticking between the claim
// and the publish serves the last known state rather than an empty one.
func TestClaimPoll_ClaimPreservesPreviousPayload(t *testing.T) {
	a, b := siblings(t)

	a.ClaimPoll(time.Minute)
	if err := a.PublishPoll(runningSet("job/a"), nil, true); err != nil {
		t.Fatalf("PublishPoll: %v", err)
	}
	a.ClaimPoll(time.Minute) // a claims the next round; its fetch is still in flight

	lp, hot := b.ClaimPoll(time.Minute)
	if !hot {
		t.Fatal("a fresh claim must hold siblings off")
	}
	if len(lp.Running) != 1 || lp.Running[0].JobPath != "job/a" {
		t.Errorf("claim dropped the previous payload: %+v", lp.Running)
	}
}

// A queue-fetch failure is propagated so siblings skip the dashboard sample
// exactly as the polling process does.
func TestClaimPoll_CarriesQueueFailure(t *testing.T) {
	a, b := siblings(t)

	a.ClaimPoll(time.Minute)
	if err := a.PublishPoll(runningSet("job/a"), nil, false); err != nil {
		t.Fatalf("PublishPoll: %v", err)
	}

	lp, hot := b.ClaimPoll(time.Minute)
	if !hot {
		t.Fatal("sibling must reuse a hot snapshot")
	}
	if lp.QueueOK {
		t.Error("queue failure must be visible to siblings")
	}
}

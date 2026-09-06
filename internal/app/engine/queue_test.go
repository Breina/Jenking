package engine

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// TestQueueTrackerFinalizesOnLeave verifies the example from the spec: an item
// stuck for ~20m that then becomes buildable briefly and leaves the queue is
// counted once, in the 20-45m bin, under its dominant reason (stuck).
func TestQueueTrackerFinalizesOnLeave(t *testing.T) {
	q := NewQueueTracker()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	since := base
	item := jmodel.QueueItem{ID: 7, JobPath: "infra/deploy", Stuck: true, InQueueSince: since}

	// Stuck for 20 minutes (observed at 5-minute steps → no completions yet).
	for m := 0; m <= 20; m += 5 {
		if got := q.Observe(since.Add(time.Duration(m)*time.Minute), []jmodel.QueueItem{item}); nonZero(got) {
			t.Fatalf("unexpected completion while still queued at +%dm", m)
		}
	}
	// Becomes buildable for 5 seconds.
	item.Stuck, item.Buildable = false, true
	q.Observe(since.Add(20*time.Minute+5*time.Second), []jmodel.QueueItem{item})
	// Then it leaves the queue (starts building). With the grace period it is
	// finalized on the second consecutive absence.
	q.Observe(since.Add(20*time.Minute+10*time.Second), nil)
	completed := q.Observe(since.Add(20*time.Minute+12*time.Second), nil)

	if completed[8][ReasonStuck] != 1 { // bin 8 == 20-45m
		t.Errorf("want 1 stuck completion in 20-45m bin, got %+v", completed[8])
	}
	total := 0
	for b := 0; b < WaitBinCount; b++ {
		for r := 0; r < ReasonCount; r++ {
			total += completed[b][r]
		}
	}
	if total != 1 {
		t.Errorf("item counted %d times, want exactly 1", total)
	}
}

// TestQueueTrackerPending shows in-progress items binned by current wait, and
// that they shift bins as they age without being counted as finalized.
func TestQueueTrackerPending(t *testing.T) {
	q := NewQueueTracker()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	item := jmodel.QueueItem{ID: 3, JobPath: "infra/deploy", Blocked: true, InQueueSince: base}

	q.Observe(base.Add(90*time.Second), []jmodel.QueueItem{item}) // waited 90s so far
	p := q.Pending(base.Add(90 * time.Second))
	if p[4][ReasonBlocked] != 1 { // bin 4 == 1-2m
		t.Fatalf("want 1 blocked pending in 1-2m bin, got %+v", p)
	}

	// Still queued 5 minutes later → moved to the 5-10m bin, still not finalized.
	q.Observe(base.Add(6*time.Minute), []jmodel.QueueItem{item})
	p = q.Pending(base.Add(6 * time.Minute))
	if p[4][ReasonBlocked] != 0 || p[6][ReasonBlocked] != 1 { // bin 6 == 5-10m
		t.Errorf("item did not move bins as it aged: %+v", p)
	}
}

func nonZero(m [WaitBinCount][ReasonCount]int) bool {
	for b := 0; b < WaitBinCount; b++ {
		for r := 0; r < ReasonCount; r++ {
			if m[b][r] != 0 {
				return true
			}
		}
	}
	return false
}

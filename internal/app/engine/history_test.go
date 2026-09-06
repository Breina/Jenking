package engine

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/cache"
)

func TestAggregateQueueHistory(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	var w1, w2 [WaitBinCount][ReasonCount]int
	w1[4][ReasonBlocked] = 2
	w2[4][ReasonBlocked] = 1
	w2[8][ReasonStuck] = 3

	samples := []cache.DashSample{
		{T: now.Add(-3 * time.Hour), Running: 9, Queued: 9, WaitReason: w1}, // outside 2h window
		{T: now.Add(-30 * time.Minute), Running: 4, Queued: 2, WaitReason: w1},
		{T: now.Add(-5 * time.Minute), Running: 7, Queued: 1, WaitReason: w2},
	}

	h := AggregateQueueHistory(samples, now, 2*time.Hour)

	if h.Samples != 2 {
		t.Errorf("Samples = %d, want 2 (3h-old sample excluded)", h.Samples)
	}
	if h.MaxRunning != 7 || h.MaxQueued != 2 {
		t.Errorf("MaxRunning/MaxQueued = %d/%d, want 7/2", h.MaxRunning, h.MaxQueued)
	}
	if h.WindowMinutes != 120 {
		t.Errorf("WindowMinutes = %d, want 120", h.WindowMinutes)
	}
	// Two occupied bins: 1-2m (blocked 2+1=3) and 20-45m (stuck 3).
	if len(h.Bins) != 2 {
		t.Fatalf("occupied bins = %d, want 2: %+v", len(h.Bins), h.Bins)
	}
	byLabel := map[string]QueueWaitBin{}
	for _, b := range h.Bins {
		byLabel[b.Label] = b
	}
	if b := byLabel["1-2m"]; b.Total != 3 || b.ByReason["blocked"] != 3 {
		t.Errorf("1-2m bin = %+v, want total 3 blocked 3", b)
	}
	if b := byLabel["20-45m"]; b.Total != 3 || b.ByReason["stuck"] != 3 {
		t.Errorf("20-45m bin = %+v, want total 3 stuck 3", b)
	}
}

func TestAggregateQueueHistory_Empty(t *testing.T) {
	h := AggregateQueueHistory(nil, time.Now(), time.Hour)
	if h.Samples != 0 || len(h.Bins) != 0 {
		t.Errorf("empty history should have no samples/bins, got %+v", h)
	}
}

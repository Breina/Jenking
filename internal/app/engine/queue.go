package engine

import (
	"sync"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// QueueTracker follows queue items across sample ticks, keyed by Jenkins queue
// ID, so each item is counted exactly once — when its ID leaves the queue — in a
// single wait bin (its total wait from InQueueSince) colored by its dominant
// reason (the sub-state it spent the most time in). This replaces per-tick
// snapshot binning, which double-counted a single item across every bin it
// passed through and mislabeled its reason.
//
// Tracking by ID (rather than dismissing items whose job already has a running
// build) means an item is counted once regardless of other builds sharing its
// job path: the queued→running transition removes the ID from the queue, which
// finalizes it here at its real wait time — no duplicates.
//
// A short grace period absorbs transient queue-poll gaps: an ID that vanishes
// for a single poll and reappears is not treated as having left.
type QueueTracker struct {
	// mu guards items: the engine's poll goroutine calls Observe while the TUI
	// dashboard reads Pending from the UI goroutine.
	mu    sync.Mutex
	items map[int64]*queueItemState
}

// queueGraceTicks is how many consecutive absences an item must have before it
// is finalized, so a single dropped queue poll does not double-count it.
const queueGraceTicks = 2

type queueItemState struct {
	since      time.Time                  // when the item entered the queue
	reasonDur  [ReasonCount]time.Duration // time accumulated per reason
	lastReason int                        // reason at the most recent observation
	lastSeen   time.Time                  // last tick the item was still queued
	missing    int                        // consecutive ticks the ID was absent
}

// NewQueueTracker creates an empty tracker.
func NewQueueTracker() *QueueTracker {
	return &QueueTracker{items: map[int64]*queueItemState{}}
}

// reasonOf collapses a queue item's flags into a reason index. It mirrors the
// display badge logic (queueSubState) but maps straight to the sampling index,
// skipping the string round trip.
func reasonOf(it jmodel.QueueItem) int {
	switch {
	case it.Stuck:
		return ReasonStuck
	case it.Blocked:
		return ReasonBlocked
	case it.Pending:
		return ReasonPending
	default:
		return ReasonBuildable
	}
}

// Observe reconciles the tracked items against the current queue and returns a
// [waitBin][reason] count of items that left the queue, each placed in its final
// wait bin under its dominant reason.
func (q *QueueTracker) Observe(now time.Time, items []jmodel.QueueItem) [WaitBinCount][ReasonCount]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	var completed [WaitBinCount][ReasonCount]int
	seen := make(map[int64]bool, len(items))

	for _, it := range items {
		seen[it.ID] = true
		reason := reasonOf(it)
		st := q.items[it.ID]
		if st == nil {
			since := it.InQueueSince
			if since.IsZero() || since.After(now) {
				since = now
			}
			q.items[it.ID] = &queueItemState{since: since, lastReason: reason, lastSeen: now}
			continue
		}
		if dt := now.Sub(st.lastSeen); dt > 0 {
			st.reasonDur[st.lastReason] += dt
		}
		st.lastReason = reason
		st.lastSeen = now
		st.missing = 0
	}

	for id, st := range q.items {
		if seen[id] {
			continue
		}
		if st.missing++; st.missing < queueGraceTicks {
			continue
		}
		bin := waitBinOf(st.lastSeen.Sub(st.since))
		completed[bin][dominantReason(st.reasonDur, st.lastReason)]++
		delete(q.items, id)
	}
	return completed
}

// Pending returns the items still in the queue, each binned by its current wait
// (now − since) under its dominant reason so far. These shift between bins as
// items age, and are shown alongside the finalized history so in-progress waits
// are visible. Items currently in the grace window (briefly absent) are skipped.
func (q *QueueTracker) Pending(now time.Time) [WaitBinCount][ReasonCount]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out [WaitBinCount][ReasonCount]int
	for _, st := range q.items {
		if st.missing > 0 {
			continue
		}
		bin := waitBinOf(now.Sub(st.since))
		out[bin][dominantReason(st.reasonDur, st.lastReason)]++
	}
	return out
}

// dominantReason returns the reason index with the most accumulated time,
// falling back to the last observed reason when no time was accrued (an item
// seen for a single tick).
func dominantReason(dur [ReasonCount]time.Duration, fallback int) int {
	best, bestDur := fallback, time.Duration(0)
	for r := 0; r < ReasonCount; r++ {
		if dur[r] > bestDur {
			best, bestDur = r, dur[r]
		}
	}
	return best
}

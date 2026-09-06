package engine

import (
	"time"

	"github.com/Breina/Jenking/internal/cache"
)

// QueueHistory is an aggregated summary of dashboard samples over a window. It
// deliberately never exposes raw samples — only the wait-bin × reason histogram
// and running/queued extremes, which is what the queue-history tool reports.
type QueueHistory struct {
	WindowMinutes int
	Samples       int
	From          time.Time
	To            time.Time
	MaxRunning    int
	MaxQueued     int
	Bins          []QueueWaitBin // only occupied bins, ascending by wait
}

// QueueWaitBin is one wait bucket's totals across the window.
type QueueWaitBin struct {
	Label    string
	Total    int
	ByReason map[string]int // reason name → count (only non-zero reasons)
}

// AggregateQueueHistory sums the wait-bin × reason counts over the samples that
// fall within [now-window, now], plus the running/queued extremes. It is pure:
// the caller supplies the samples (loaded from the shared cache).
func AggregateQueueHistory(samples []cache.DashSample, now time.Time, window time.Duration) QueueHistory {
	start := now.Add(-window)
	var sum [WaitBinCount][ReasonCount]int
	h := QueueHistory{WindowMinutes: int(window.Minutes())}

	for _, s := range samples {
		if s.T.Before(start) || s.T.After(now) {
			continue
		}
		h.observe(s)
		for b := 0; b < WaitBinCount; b++ {
			for r := 0; r < ReasonCount; r++ {
				sum[b][r] += s.WaitReason[b][r]
			}
		}
	}
	h.Bins = occupiedBins(sum)
	return h
}

// observe folds one in-window sample's scalar fields into the summary.
func (h *QueueHistory) observe(s cache.DashSample) {
	h.Samples++
	if h.From.IsZero() || s.T.Before(h.From) {
		h.From = s.T
	}
	if s.T.After(h.To) {
		h.To = s.T
	}
	if s.Running > h.MaxRunning {
		h.MaxRunning = s.Running
	}
	if s.Queued > h.MaxQueued {
		h.MaxQueued = s.Queued
	}
}

// occupiedBins turns the summed wait-bin × reason matrix into the ascending
// list of non-empty bins, each labeled with its per-reason breakdown.
func occupiedBins(sum [WaitBinCount][ReasonCount]int) []QueueWaitBin {
	var bins []QueueWaitBin
	for b := 0; b < WaitBinCount; b++ {
		total := 0
		byReason := map[string]int{}
		for r := 0; r < ReasonCount; r++ {
			if sum[b][r] > 0 {
				byReason[ReasonNames[r]] = sum[b][r]
				total += sum[b][r]
			}
		}
		if total > 0 {
			bins = append(bins, QueueWaitBin{Label: WaitBins[b].Label, Total: total, ByReason: byReason})
		}
	}
	return bins
}

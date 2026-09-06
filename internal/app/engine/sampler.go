package engine

import (
	"sync"
	"time"

	"github.com/Breina/Jenking/internal/cache"
)

// Queue sub-state reason indices, in display order (match the reason dimension
// of cache.DashSample.WaitReason).
const (
	ReasonStuck = iota
	ReasonBlocked
	ReasonPending
	ReasonBuildable
	ReasonCount
)

// ReasonNames labels each reason index for presentation.
var ReasonNames = [ReasonCount]string{"stuck", "blocked", "pending", "buildable"}

// WaitBin is one fine-grained queue-wait bucket. Consumers display only the
// occupied range, so the visible buckets adapt to the data. The number of bins
// must match the first dimension of cache.DashSample.WaitReason.
type WaitBin struct {
	Upper time.Duration
	Label string
}

// WaitBins are the queue-wait buckets, ascending by upper bound.
var WaitBins = []WaitBin{
	{time.Second, "<1s"},
	{15 * time.Second, "1-15s"},
	{30 * time.Second, "15-30s"},
	{time.Minute, "30-60s"},
	{2 * time.Minute, "1-2m"},
	{5 * time.Minute, "2-5m"},
	{10 * time.Minute, "5-10m"},
	{20 * time.Minute, "10-20m"},
	{45 * time.Minute, "20-45m"},
	{90 * time.Minute, "45-90m"},
	{1<<63 - 1, ">90m"},
}

// WaitBinCount is the fixed number of wait buckets.
const WaitBinCount = 11

func waitBinOf(d time.Duration) int {
	for i, b := range WaitBins {
		if d < b.Upper {
			return i
		}
	}
	return WaitBinCount - 1
}

// Sampler is an in-memory ring buffer of dashboard samples, persisted to disk
// (via the cache) so history survives a restart. Retention bounds memory and
// disk size. Safe for concurrent use.
type Sampler struct {
	mu     sync.Mutex
	buf    []cache.DashSample
	retain time.Duration
}

// NewSampler creates a sampler retaining samples for the given window
// (defaulting to 2h when non-positive).
func NewSampler(retain time.Duration) *Sampler {
	if retain <= 0 {
		retain = 2 * time.Hour
	}
	return &Sampler{retain: retain}
}

// Load replaces the buffer with the given samples (e.g. from disk).
func (s *Sampler) Load(samples []cache.DashSample) {
	if len(samples) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf[:0], samples...)
	s.evictLocked(samples[len(samples)-1].T)
}

// Dump returns a copy of the current buffer.
func (s *Sampler) Dump() []cache.DashSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]cache.DashSample, len(s.buf))
	copy(out, s.buf)
	return out
}

// Add appends a sample and evicts anything older than the retention window.
func (s *Sampler) Add(t time.Time, running, queued int, waitReason [WaitBinCount][ReasonCount]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, cache.DashSample{T: t, Running: running, Queued: queued, WaitReason: waitReason})
	s.evictLocked(t)
}

func (s *Sampler) evictLocked(newest time.Time) {
	cutoff := newest.Add(-s.retain)
	drop := 0
	for drop < len(s.buf) && s.buf[drop].T.Before(cutoff) {
		drop++
	}
	if drop > 0 {
		s.buf = append(s.buf[:0], s.buf[drop:]...)
	}
}

// Points returns a copy of the samples within the last window, oldest first.
func (s *Sampler) Points(now time.Time, window time.Duration) []cache.DashSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := now.Add(-window)
	out := make([]cache.DashSample, 0, len(s.buf))
	for _, sm := range s.buf {
		if sm.T.Before(start) || sm.T.After(now) {
			continue
		}
		out = append(out, sm)
	}
	return out
}

// SumWaitReason totals, for each wait bin and reason, the items that left the
// queue within the window. Each sample records only the items finalized on that
// tick, so summing gives the count of queue items that finished waiting during
// the period, each in its final wait bin under its dominant reason.
func (s *Sampler) SumWaitReason(now time.Time, window time.Duration) [WaitBinCount][ReasonCount]int {
	var out [WaitBinCount][ReasonCount]int
	s.mu.Lock()
	defer s.mu.Unlock()
	start := now.Add(-window)
	for _, sm := range s.buf {
		if sm.T.Before(start) || sm.T.After(now) {
			continue
		}
		for b := 0; b < WaitBinCount; b++ {
			for r := 0; r < ReasonCount; r++ {
				out[b][r] += sm.WaitReason[b][r]
			}
		}
	}
	return out
}

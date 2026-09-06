package cache

import (
	"path/filepath"
	"sort"
	"time"
)

// maxDashSamples bounds the persisted sample buffer. Each process evicts by
// time window in-memory, but merging with a concurrent process's on-disk buffer
// could otherwise grow the file unbounded across restarts. At the ~2s sampling
// cadence this cap covers well over two hours of history.
const maxDashSamples = 5000

// DashSample is one persisted dashboard time-series point: running/queued
// counts plus a 2D queue histogram of wait-bin × reason. WaitReason counts the
// queue items that *finished* waiting on this tick, each in its final wait bin
// under its dominant reason (not a live snapshot) — so it is summed over a
// window. Exported fields so it can be gob-encoded to disk and survive restarts.
// Dimensions match the view package: 11 wait bins × 4 reasons.
type DashSample struct {
	T          time.Time
	Running    int
	Queued     int
	WaitReason [11][4]int // [wait bin][reason: stuck, blocked, pending, buildable]
}

func (d *DiskStore) dashSamplesPath() string { return filepath.Join(d.dir, "dashsamples.gob") }

// LoadDashSamples returns the persisted dashboard samples (nil if none).
func (d *DiskStore) LoadDashSamples() ([]DashSample, error) {
	return readGob[[]DashSample](d.dashSamplesPath())
}

// SaveDashSamples writes the dashboard sample buffer to disk, unioning it with
// whatever is already there by timestamp so a concurrent Jenking process's
// samples aren't lost. The union is trimmed to the newest maxDashSamples points.
// The whole read-modify-write runs under the cross-process lock.
func (d *DiskStore) SaveDashSamples(samples []DashSample) error {
	return d.withLock(func() error {
		existing, err := readGob[[]DashSample](d.dashSamplesPath())
		if err != nil {
			existing = nil // absent or unreadable: treat as empty
		}
		return writeGob(d.dashSamplesPath(), mergeDashSamples(existing, samples))
	})
}

// mergeDashSamples unions two sample slices by timestamp (later write wins on a
// collision), sorts ascending, and keeps the newest maxDashSamples points.
func mergeDashSamples(existing, incoming []DashSample) []DashSample {
	byT := make(map[time.Time]DashSample, len(existing)+len(incoming))
	for _, s := range existing {
		byT[s.T] = s
	}
	for _, s := range incoming {
		byT[s.T] = s // incoming (the live buffer) wins on identical timestamps
	}
	out := make([]DashSample, 0, len(byT))
	for _, s := range byT {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T.Before(out[j].T) })
	if len(out) > maxDashSamples {
		out = out[len(out)-maxDashSamples:]
	}
	return out
}

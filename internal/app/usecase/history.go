package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/Breina/Jenking/internal/app/engine"
)

// QueueHistory returns an aggregated summary of the queue-wait history over the
// last window, read from the shared dashboard-sample cache (produced by the
// engine and/or a running TUI, merged on disk). It never returns raw samples —
// only the wait-bin × reason histogram and running/queued extremes.
func (d Deps) QueueHistory(_ context.Context, window time.Duration) (engine.QueueHistory, error) {
	if d.Store == nil || d.Store.Disk == nil {
		return engine.QueueHistory{}, fmt.Errorf("queue history requires the disk cache")
	}
	samples, err := d.Store.Disk.LoadDashSamples()
	if err != nil {
		return engine.QueueHistory{}, fmt.Errorf("loading queue-history samples: %w", err)
	}
	return engine.AggregateQueueHistory(samples, time.Now(), window), nil
}

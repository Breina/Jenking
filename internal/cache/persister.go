package cache

import (
	"sync"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
)

// asyncPersister runs registry disk writes off the mutating goroutine and
// coalesces bursts into a single write. Registry mutations (running-poll ingest
// every second, the all-builds scan ingest on the UI thread, completion
// reconciles) each hand the latest snapshot here and return immediately; a lone
// background goroutine performs the flock'd read-modify-write. Without this the
// scan ingest — which lands inside Bubble Tea's Update on the UI thread — blocks
// the event loop for seconds on the disk write, freezing the TUI.
type asyncPersister struct {
	save func([]buildregistry.Record) error

	mu      sync.Mutex
	pending []buildregistry.Record
	wake    chan struct{}
}

// newAsyncPersister starts the background writer. save is the underlying
// (blocking) disk write; it is called from a single dedicated goroutine, so it
// never runs concurrently with itself and never blocks a caller of persist.
func newAsyncPersister(save func([]buildregistry.Record) error) *asyncPersister {
	p := &asyncPersister{save: save, wake: make(chan struct{}, 1)}
	go p.loop()
	return p
}

// persist records the latest snapshot and signals the writer. It never blocks
// on disk I/O: repeated calls before the writer catches up overwrite the pending
// snapshot, so only the newest state is ever written (coalescing).
func (p *asyncPersister) persist(records []buildregistry.Record) {
	p.mu.Lock()
	p.pending = records
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default: // a wake is already queued; the pending snapshot it picks up is the latest
	}
}

func (p *asyncPersister) loop() {
	for range p.wake {
		p.mu.Lock()
		records := p.pending
		p.pending = nil
		p.mu.Unlock()
		if records != nil {
			_ = p.save(records)
		}
	}
}

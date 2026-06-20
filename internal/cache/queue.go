package cache

import (
	"strings"
	"sync"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// QueueStore holds the latest Jenkins build-queue snapshot. The queue is
// global and ephemeral — items have no build number — so it lives outside the
// Registry. The running-builds monitor replaces the snapshot on every poll.
type QueueStore struct {
	mu    sync.RWMutex
	items []jmodel.QueueItem
}

// NewQueueStore creates an empty QueueStore.
func NewQueueStore() *QueueStore { return &QueueStore{} }

// Replace swaps in a fresh queue snapshot.
func (q *QueueStore) Replace(items []jmodel.QueueItem) {
	q.mu.Lock()
	q.items = items
	q.mu.Unlock()
}

// CountVisible returns the number of queued items shown as their own rows —
// i.e. excluding items whose job is already running (those are hidden, folded
// into the running build). isRunning reports whether a job path currently has a
// running build; pass nil to count everything.
func (q *QueueStore) CountVisible(isRunning func(jobPath string) bool) int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	n := 0
	for _, it := range q.items {
		if isRunning != nil && isRunning(it.JobPath) {
			continue
		}
		n++
	}
	return n
}

// Query returns the queued items whose JobPath matches filter, using the same
// prefix/exact semantics as buildregistry.Filter so queue scoping mirrors build
// scoping.
func (q *QueueStore) Query(filter buildregistry.Filter) []jmodel.QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]jmodel.QueueItem, 0, len(q.items))
	for _, it := range q.items {
		if queueMatches(filter, it.JobPath) {
			out = append(out, it)
		}
	}
	return out
}

func queueMatches(f buildregistry.Filter, jobPath string) bool {
	if f.JobPath != "" && jobPath != f.JobPath {
		return false
	}
	if f.FolderPrefix != "" && !strings.HasPrefix(jobPath, f.FolderPrefix+"/") {
		return false
	}
	if f.ProjectPath != "" && !strings.HasPrefix(jobPath, f.ProjectPath+"/") {
		return false
	}
	return true
}

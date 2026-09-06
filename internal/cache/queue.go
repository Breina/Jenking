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

// CountVisible returns the number of queued items of kind that are shown as
// their own rows — i.e. excluding items whose job is already running (those are
// hidden, folded into the running build). isRunning reports whether a job path
// currently has a running build; pass nil to count everything.
func (q *QueueStore) CountVisible(isRunning func(jobPath string) bool, kind jmodel.QueueKind) int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	n := 0
	for _, it := range q.items {
		if it.KindOrBuild() != kind {
			continue
		}
		if isRunning != nil && isRunning(it.JobPath) {
			continue
		}
		n++
	}
	return n
}

// Query returns the queued items of kind whose JobPath matches filter, using
// the same prefix/exact semantics as buildregistry.Filter so queue scoping
// mirrors build scoping. The store holds every kind (one fetch feeds both the
// builds views and the scans view); callers ask for the one they render.
func (q *QueueStore) Query(filter buildregistry.Filter, kind jmodel.QueueKind) []jmodel.QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]jmodel.QueueItem, 0, len(q.items))
	for _, it := range q.items {
		if it.KindOrBuild() == kind && queueMatches(filter, it.JobPath) {
			out = append(out, it)
		}
	}
	return out
}

// ScansInScope returns the queued scans of the container at scopePath *and* of
// every container nested beneath it; an empty scopePath returns all of them.
// Scans are the one queue kind where the scope must include itself: a folder's
// own indexing task carries the folder's path, while the scans a user cares
// about when standing in that folder are mostly its children's — buildregistry
// filters express one or the other, never both.
func (q *QueueStore) ScansInScope(scopePath string) []jmodel.QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]jmodel.QueueItem, 0, len(q.items))
	for _, it := range q.items {
		if it.KindOrBuild() != jmodel.QueueKindScan {
			continue
		}
		if scopePath != "" && it.JobPath != scopePath && !strings.HasPrefix(it.JobPath, scopePath+"/") {
			continue
		}
		out = append(out, it)
	}
	return out
}

// ScanFor returns the queued scan for a container path, if one is waiting.
// The job list calls it per row on every render, so it stays a plain snapshot
// lookup: a scan that has left the queue is already running, which Jenkins
// exposes nowhere cheap, and is deliberately not reported here.
func (q *QueueStore) ScanFor(jobPath string) (jmodel.QueueItem, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	for _, it := range q.items {
		if it.KindOrBuild() == jmodel.QueueKindScan && it.JobPath == jobPath {
			return it, true
		}
	}
	return jmodel.QueueItem{}, false
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

package buildregistry

import (
	"context"
	"sync"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// Reconciler is the production ReconcileFn implementation. It debounces
// fetches per key and pushes the result back into the registry via
// ApplyCompletion.
//
// Every Query of a record whose status is unknown asks for a reconcile, and a
// TUI or MCP server queries several times a second, so the debounce is what
// bounds the load: a key is fetched at most once per minGap, a failing key
// backs off exponentially up to maxGap, and a 404 drops the record from the
// registry instead of retrying a build that no longer exists.
//
// Notifier (optional) is called with the completion result so the TUI can
// also forward it as a BuildCompletedMsg to in-flight views (e.g. StageView)
// that pattern-match on that message.
type Reconciler struct {
	client   jmodel.JenkinsClient
	registry *Registry
	notify   func(key Key, build jmodel.Build, err error)
	now      func() time.Time

	mu     sync.Mutex
	state  map[Key]*reconcileState
	minGap time.Duration // suppress repeat reconciles within this window
	maxGap time.Duration // cap on the per-key failure backoff
}

type reconcileState struct {
	inflight bool
	next     time.Time // earliest time the key may be fetched again
	failures int
}

// NewReconciler wires a Reconciler. notify may be nil.
func NewReconciler(client jmodel.JenkinsClient, registry *Registry, notify func(key Key, build jmodel.Build, err error)) *Reconciler {
	return &Reconciler{
		client:   client,
		registry: registry,
		notify:   notify,
		now:      time.Now,
		state:    make(map[Key]*reconcileState),
		minGap:   5 * time.Second,
		maxGap:   5 * time.Minute,
	}
}

// Reconcile is the ReconcileFn. It returns immediately; the fetch runs in a
// background goroutine.
func (r *Reconciler) Reconcile(k Key) {
	if r == nil || r.client == nil || r.registry == nil {
		return
	}
	r.mu.Lock()
	st := r.state[k]
	if st == nil {
		st = &reconcileState{}
		r.state[k] = st
	}
	now := r.now()
	if st.inflight || now.Before(st.next) {
		r.mu.Unlock()
		return
	}
	st.inflight = true
	st.next = now.Add(r.minGap)
	r.mu.Unlock()

	go r.fetch(k)
}

func (r *Reconciler) fetch(k Key) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	detail, err := r.client.GetBuild(ctx, k.JobPath, k.Number)

	r.mu.Lock()
	st := r.state[k]
	st.inflight = false
	switch {
	case err == nil:
		// The gap set at launch still applies: a build that came back still
		// running is not re-fetched on every Query.
		st.failures = 0
	case jmodel.IsNotFound(err):
		delete(r.state, k)
	default:
		st.failures++
		gap := r.minGap << min(st.failures, 10)
		if gap > r.maxGap || gap <= 0 {
			gap = r.maxGap
		}
		st.next = r.now().Add(gap)
	}
	r.mu.Unlock()

	if err != nil {
		if jmodel.IsNotFound(err) {
			// The build is gone (deleted, rotated out, renamed job): stop
			// tracking it rather than asking for it again on every Query.
			r.registry.Forget(k)
		}
		if r.notify != nil {
			r.notify(k, jmodel.Build{}, err)
		}
		return
	}
	r.registry.ApplyCompletion(k, detail.Build)
	if r.notify != nil {
		r.notify(k, detail.Build, nil)
	}
}

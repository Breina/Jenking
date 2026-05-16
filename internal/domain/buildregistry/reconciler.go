package buildregistry

import (
	"context"
	"sync"
	"time"

	"github.com/Breina/Jenking/internal/jenkins"
)

// Reconciler is the production ReconcileFn implementation. It debounces
// concurrent fetches per key and pushes the result back into the registry
// via ApplyCompletion.
//
// Notifier (optional) is called with the completion result so the TUI can
// also forward it as a BuildCompletedMsg to in-flight views (e.g. StageView)
// that pattern-match on that message.
type Reconciler struct {
	client   jenkins.JenkinsClient
	registry *Registry
	notify   func(key Key, build jenkins.Build, err error)

	mu       sync.Mutex
	inflight map[Key]time.Time
	minGap   time.Duration // suppress repeat reconciles within this window
}

// NewReconciler wires a Reconciler. notify may be nil.
func NewReconciler(client jenkins.JenkinsClient, registry *Registry, notify func(key Key, build jenkins.Build, err error)) *Reconciler {
	return &Reconciler{
		client:   client,
		registry: registry,
		notify:   notify,
		inflight: make(map[Key]time.Time),
		minGap:   2 * time.Second,
	}
}

// Reconcile is the ReconcileFn. It returns immediately; the fetch runs in a
// background goroutine.
func (r *Reconciler) Reconcile(k Key) {
	if r == nil || r.client == nil || r.registry == nil {
		return
	}
	now := time.Now()
	r.mu.Lock()
	if last, ok := r.inflight[k]; ok && now.Sub(last) < r.minGap {
		r.mu.Unlock()
		return
	}
	r.inflight[k] = now
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.inflight, k)
			r.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		detail, err := r.client.GetBuild(ctx, k.JobPath, k.Number)
		if err != nil {
			if r.notify != nil {
				r.notify(k, jenkins.Build{}, err)
			}
			return
		}
		r.registry.ApplyCompletion(k, detail.Build)
		if r.notify != nil {
			r.notify(k, detail.Build, nil)
		}
	}()
}

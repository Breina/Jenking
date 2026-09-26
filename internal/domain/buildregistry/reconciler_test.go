package buildregistry

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type statusErr int

func (e statusErr) Error() string       { return "http error" }
func (e statusErr) HTTPStatusCode() int { return int(e) }

type reconcileClient struct {
	jmodel.JenkinsClient
	calls atomic.Int32
	err   error
}

func (c *reconcileClient) GetBuild(context.Context, string, int) (*jmodel.BuildDetail, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return &jmodel.BuildDetail{Build: jmodel.Build{Number: 1, Status: jmodel.BuildStatusSuccess}}, nil
}

// reconcileAndWait runs one Reconcile and waits for its fetch to settle.
func reconcileAndWait(t *testing.T, r *Reconciler, k Key) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	prev := r.notify
	r.notify = func(Key, jmodel.Build, error) { wg.Done() }
	defer func() { r.notify = prev }()
	r.mu.Lock()
	st := r.state[k]
	launching := st == nil || (!st.inflight && !r.now().Before(st.next))
	r.mu.Unlock()
	r.Reconcile(k)
	if launching {
		wg.Wait()
	}
}

func unknownRegistry(k Key) *Registry {
	reg := New(Config{})
	reg.LoadFromDisk([]Record{{JobPath: k.JobPath, Build: jmodel.Build{Number: k.Number, Status: jmodel.BuildStatusRunning}}})
	return reg
}

func TestReconcilerForgetsBuildOn404(t *testing.T) {
	k := Key{JobPath: "f/p/main", Number: 1}
	reg := unknownRegistry(k)
	c := &reconcileClient{err: statusErr(404)}
	r := NewReconciler(c, reg, nil)

	reconcileAndWait(t, r, k)
	if got := reg.Query(Filter{JobPath: k.JobPath}); len(got) != 0 {
		t.Fatalf("404 build still tracked: %+v", got)
	}
	reg.SetReconcile(r.Reconcile)
	for range 50 {
		reg.Query(Filter{})
	}
	time.Sleep(20 * time.Millisecond)
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("404 build fetched %d times, want 1", n)
	}
}

func TestReconcilerBacksOffFailingKey(t *testing.T) {
	k := Key{JobPath: "f/p/main", Number: 1}
	reg := unknownRegistry(k)
	c := &reconcileClient{err: statusErr(503)}
	r := NewReconciler(c, reg, nil)
	clock := time.Now()
	r.now = func() time.Time { return clock }

	reconcileAndWait(t, r, k)
	// Queries in a hot loop (a TUI render / MCP read) must not refetch.
	for range 100 {
		reconcileAndWait(t, r, k)
	}
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("failing key fetched %d times within the gap, want 1", n)
	}
	// Past minGap but inside the doubled failure backoff: still suppressed.
	clock = clock.Add(r.minGap + time.Second)
	reconcileAndWait(t, r, k)
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("failure backoff not applied: %d fetches", n)
	}
	clock = clock.Add(2 * r.minGap)
	reconcileAndWait(t, r, k)
	if n := c.calls.Load(); n != 2 {
		t.Fatalf("key not retried after backoff: %d fetches", n)
	}
	if len(reg.Query(Filter{JobPath: k.JobPath})) != 1 {
		t.Fatalf("transient failure must not drop the record")
	}
}

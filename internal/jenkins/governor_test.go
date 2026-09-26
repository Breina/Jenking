package jenkins

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func hostOf(srv *httptest.Server) string { return strings.TrimPrefix(srv.URL, "http://") }

func TestGovernorBacksOffOn503AndFailsFast(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "u", "t", false)

	_, err := c.GetBuild(t.Context(), "a/b", 1)
	if jmodel.StatusOf(err) != http.StatusServiceUnavailable || !jmodel.IsTransient(err) {
		t.Fatalf("first call: want transient 503, got %v", err)
	}
	for i := range 20 {
		_, err = c.GetBuild(t.Context(), "a/b", i+2)
		var be *BackoffError
		if !errors.As(err, &be) || !jmodel.IsTransient(err) {
			t.Fatalf("call %d: want BackoffError, got %v", i, err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("controller hit %d times while backing off, want 1", n)
	}
}

func TestGovernorHonoursRetryAfterAndRecovers(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"number":1}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "u", "t", false)
	g := governorFor(hostOf(srv))
	clock := time.Now()
	g.mu.Lock()
	g.now = func() time.Time { return clock }
	g.mu.Unlock()

	_, _ = c.GetBuild(t.Context(), "a/b", 1)
	g.mu.Lock()
	wait := g.until.Sub(clock)
	g.mu.Unlock()
	if wait < 30*time.Second {
		t.Fatalf("backoff %s ignores Retry-After: 30", wait)
	}

	fail.Store(false)
	clock = clock.Add(31 * time.Second)
	if _, err := c.GetBuild(t.Context(), "a/b", 2); err != nil {
		t.Fatalf("probe after Retry-After: %v", err)
	}
	if _, err := c.GetBuild(t.Context(), "a/b", 3); err != nil {
		t.Fatalf("after recovery: %v", err)
	}
}

func TestGovernorBackoffGrowsExponentially(t *testing.T) {
	g := &governor{host: "x", sem: make(chan struct{}, 1), now: time.Now, cache: map[string]*apiEntry{}}
	var prev time.Duration
	for n := 1; n <= 12; n++ {
		g.settle(false, true, true, 0)
		d := time.Until(g.until)
		if d > backoffMax {
			t.Fatalf("failure %d: backoff %s exceeds max", n, d)
		}
		if n <= 6 && d < prev/2 {
			t.Fatalf("failure %d: backoff %s did not grow from %s", n, d, prev)
		}
		prev = d
		g.until = time.Time{} // let the next failure compute fresh
	}
	if prev < backoffMax/2 {
		t.Fatalf("backoff never reached the cap: %s", prev)
	}
}

func TestGovernorCancelledCallerDoesNotTriggerBackoff(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)
	c := NewClient(srv.URL, "u", "t", false)

	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, _ = c.GetBuild(ctx, "a/b", 1)

	g := governorFor(hostOf(srv))
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failures != 0 {
		t.Fatalf("caller cancellation counted as controller failure")
	}
}

func TestGovernorCoalescesIdenticalPolls(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"number":7,"building":true}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "u", "t", false)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if d, err := c.GetBuild(t.Context(), "a/b", 7); err != nil || d.Number != 7 {
				t.Errorf("GetBuild: %v %+v", err, d)
			}
		})
	}
	wg.Wait()
	// A follow-up inside the TTL is served from the last response too.
	if _, err := c.GetBuild(t.Context(), "a/b", 7); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("10 concurrent + 1 immediate identical polls hit the controller %d times, want 1", n)
	}
	// A mutation invalidates the coalesced responses.
	_ = c.post(t.Context(), "/job/a/job/b/7/stop", nil)
	if _, err := c.GetBuild(t.Context(), "a/b", 7); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 3 {
		t.Fatalf("after POST: %d hits, want 3", n)
	}
}

func TestGovernorCapsConcurrency(t *testing.T) {
	var cur, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "u", "t", false)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { _, _ = c.GetBuild(t.Context(), "a/b", i) })
	}
	wg.Wait()
	if p := peak.Load(); p > maxConcurrentRequests {
		t.Fatalf("peak concurrency %d exceeds cap %d", p, maxConcurrentRequests)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if d := parseRetryAfter("120", now); d != 2*time.Minute {
		t.Errorf("seconds: %s", d)
	}
	if d := parseRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now); d != time.Minute {
		t.Errorf("http-date: %s", d)
	}
	if d := parseRetryAfter("garbage", now); d != 0 {
		t.Errorf("garbage: %s", d)
	}
}

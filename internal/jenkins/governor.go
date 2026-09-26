package jenkins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The governor keeps Jenking a well-behaved client: every request to a
// controller passes through one per-host governor shared by all Clients in the
// process. It caps concurrent requests, backs off exponentially (with jitter,
// honouring Retry-After) while the controller answers 5xx/429 or does not
// answer at all, and coalesces identical JSON API polls. An unhealthy
// controller renders a full error page for every request, so hammering it
// while it restarts is what keeps it down.
const (
	maxConcurrentRequests = 4
	backoffBase           = time.Second
	backoffMax            = 2 * time.Minute
	retryAfterMax         = 5 * time.Minute
	// apiCacheTTL is how long an identical /api/json GET is answered from the
	// last response. It collapses the bursts where several watchers (engine,
	// reconciler, open views, MCP tools) fetch the same build in the same tick.
	apiCacheTTL     = time.Second
	apiCacheMaxSize = 256
)

// BackoffError is returned without contacting the controller while the
// governor is backing off after failures.
type BackoffError struct {
	Host    string
	RetryIn time.Duration
}

func (e *BackoffError) Error() string {
	return fmt.Sprintf("jenkins %s is unavailable, backing off (retry in %s)", e.Host, e.RetryIn.Round(time.Second))
}

// Transient implements jmodel's transient-error marker.
func (e *BackoffError) Transient() bool { return true }

// unreachableError marks a transport failure (refused, reset, timed out) as
// transient so wait loops keep waiting instead of failing the whole call.
type unreachableError struct{ err error }

func (e *unreachableError) Error() string   { return e.err.Error() }
func (e *unreachableError) Unwrap() error   { return e.err }
func (e *unreachableError) Transient() bool { return true }

var (
	governorsMu sync.Mutex
	governors   = map[string]*governor{}
)

// governorFor returns the process-wide governor for host.
func governorFor(host string) *governor {
	governorsMu.Lock()
	defer governorsMu.Unlock()
	g, ok := governors[host]
	if !ok {
		g = &governor{
			host:  host,
			sem:   make(chan struct{}, maxConcurrentRequests),
			now:   time.Now,
			cache: map[string]*apiEntry{},
		}
		governors[host] = g
	}
	return g
}

type governor struct {
	host string
	sem  chan struct{}
	now  func() time.Time

	mu       sync.Mutex
	failures int
	until    time.Time
	probing  bool
	cache    map[string]*apiEntry
}

// apiEntry is one coalesced /api/json response: in flight until done closes,
// then served to identical requests until expires.
type apiEntry struct {
	done    chan struct{}
	expires time.Time
	status  int
	header  http.Header
	body    []byte
	err     error
}

// governedTransport routes every request through the host's governor.
type governedTransport struct {
	base http.RoundTripper
}

func (t *governedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	g := governorFor(req.URL.Host)
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		g.invalidate() // a mutation may change what the cached polls return
		return g.roundTrip(t.base, req)
	}
	if !strings.HasSuffix(req.URL.Path, "/api/json") {
		return g.roundTrip(t.base, req)
	}
	return g.coalesced(t.base, req)
}

// coalesced serves an /api/json GET from an identical in-flight or just-finished
// request when there is one, so N watchers of one build cost one request.
func (g *governor) coalesced(base http.RoundTripper, req *http.Request) (*http.Response, error) {
	key := req.Header.Get("Authorization") + " " + req.URL.String()
	g.mu.Lock()
	if e, ok := g.cache[key]; ok {
		select {
		case <-e.done:
			if g.now().Before(e.expires) {
				g.mu.Unlock()
				return e.response(req)
			}
		default:
			g.mu.Unlock()
			select {
			case <-e.done:
				return e.response(req)
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
	}
	e := &apiEntry{done: make(chan struct{})}
	g.pruneLocked()
	g.cache[key] = e
	g.mu.Unlock()

	resp, err := g.roundTrip(base, req)
	if err == nil {
		e.status, e.header = resp.StatusCode, resp.Header
		e.body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	e.err = err
	g.mu.Lock()
	e.expires = g.now().Add(apiCacheTTL)
	if err != nil {
		delete(g.cache, key) // waiters get the error; later callers retry
	}
	g.mu.Unlock()
	close(e.done)
	return e.response(req)
}

func (e *apiEntry) response(req *http.Request) (*http.Response, error) {
	if e.err != nil {
		return nil, e.err
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", e.status, http.StatusText(e.status)),
		StatusCode:    e.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        e.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(e.body)),
		ContentLength: int64(len(e.body)),
		Request:       req,
	}, nil
}

// pruneLocked drops expired finished entries once the cache grows.
func (g *governor) pruneLocked() {
	if len(g.cache) < apiCacheMaxSize {
		return
	}
	now := g.now()
	for k, e := range g.cache {
		select {
		case <-e.done:
			if !now.Before(e.expires) {
				delete(g.cache, k)
			}
		default:
		}
	}
}

func (g *governor) invalidate() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, e := range g.cache {
		select {
		case <-e.done:
			delete(g.cache, k)
		default:
		}
	}
}

// roundTrip performs one request under the backoff gate and concurrency cap.
func (g *governor) roundTrip(base http.RoundTripper, req *http.Request) (*http.Response, error) {
	probe, err := g.admit()
	if err != nil {
		return nil, err
	}
	select {
	case g.sem <- struct{}{}:
	case <-req.Context().Done():
		g.settle(probe, false, false, 0)
		return nil, req.Context().Err()
	}
	resp, err := base.RoundTrip(req)
	<-g.sem

	if err != nil {
		// The caller giving up is not the controller's fault; a deadline is.
		if errors.Is(req.Context().Err(), context.Canceled) {
			g.settle(probe, false, false, 0)
			return nil, err
		}
		g.settle(probe, true, true, 0)
		return nil, &unreachableError{err: err}
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		g.settle(probe, true, true, parseRetryAfter(resp.Header.Get("Retry-After"), g.now()))
	} else {
		g.settle(probe, true, false, 0)
	}
	return resp, nil
}

// admit decides whether a request may go out. While backing off everything
// fails fast; once the window passes a single probe is let through and the
// rest keep failing fast until it succeeds, so recovery is not a stampede.
func (g *governor) admit() (probe bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failures == 0 {
		return false, nil
	}
	now := g.now()
	if now.Before(g.until) {
		return false, &BackoffError{Host: g.host, RetryIn: g.until.Sub(now)}
	}
	if g.probing {
		return false, &BackoffError{Host: g.host, RetryIn: backoffBase}
	}
	g.probing = true
	return true, nil
}

// settle records a request's outcome. answered=false means the outcome says
// nothing about the controller (the caller cancelled).
func (g *governor) settle(probe, answered, failed bool, retryAfter time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if probe {
		g.probing = false
	}
	if !answered {
		return
	}
	if !failed {
		g.failures = 0
		g.until = time.Time{}
		return
	}
	g.failures++
	wait := backoffDelay(g.failures)
	if retryAfter > wait {
		wait = min(retryAfter, retryAfterMax)
	}
	if until := g.now().Add(wait); until.After(g.until) {
		g.until = until
	}
}

// backoffDelay is the exponential backoff for the nth consecutive failure,
// with jitter in [d/2, d) so several processes do not retry in lockstep.
func backoffDelay(n int) time.Duration {
	d := backoffBase << min(n-1, 10)
	if d > backoffMax || d <= 0 {
		d = backoffMax
	}
	return d/2 + rand.N(d/2)
}

// parseRetryAfter reads a Retry-After header in either delta-seconds or
// HTTP-date form; 0 when absent or unparsable.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

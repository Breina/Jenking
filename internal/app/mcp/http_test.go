package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
)

type basicAuthTransport struct{ user, token string }

func (b basicAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth(b.user, b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func connectHTTP(t *testing.T, endpoint, user, token string) (*mcp.ClientSession, error) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "0"}, nil)
	return client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: basicAuthTransport{user, token}},
		MaxRetries: -1,
		// No long-lived GET stream: it would keep httptest.Server.Close waiting.
		DisableStandaloneSSE: true,
	}, nil)
}

func TestHTTPRequiresCredentials(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(NewHTTPHandler(func(context.Context, string, string) (usecase.Deps, error) {
		calls.Add(1)
		return usecase.Deps{Client: fakeClient{}}, nil
	}, "test", false))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") == "" {
		t.Errorf("status=%d www-authenticate=%q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	if calls.Load() != 0 {
		t.Errorf("factory called %d times for an anonymous request", calls.Load())
	}

	health, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d", health.StatusCode)
	}
}

func TestHTTPRejectsBadCredentials(t *testing.T) {
	srv := httptest.NewServer(NewHTTPHandler(func(context.Context, string, string) (usecase.Deps, error) {
		return usecase.Deps{}, errors.New("authentication failed")
	}, "test", false))
	defer srv.Close()

	if cs, err := connectHTTP(t, srv.URL+"/mcp", "jane", "wrong"); err == nil {
		_ = cs.Close()
		t.Fatal("connect succeeded with credentials Jenkins rejects")
	}
}

func TestHTTPServesToolsPerCredential(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(NewHTTPHandler(func(context.Context, string, string) (usecase.Deps, error) {
		calls.Add(1)
		return usecase.Deps{Client: fakeClient{}}, nil
	}, "test", true))
	defer srv.Close()

	for _, path := range []string{"/mcp", "/mcp", "/mcp/stateless"} {
		cs, err := connectHTTP(t, srv.URL+path, "jane", "tok")
		if err != nil {
			t.Fatalf("connect %s: %v", path, err)
		}
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami"})
		if err != nil || res.IsError {
			t.Fatalf("whoami over %s: err=%v result=%+v", path, err, res)
		}
		_ = cs.Close()
	}
	if calls.Load() != 1 {
		t.Errorf("factory called %d times for one credential, want 1", calls.Load())
	}

	bob, err := connectHTTP(t, srv.URL+"/mcp", "bob", "tok2")
	if err != nil {
		t.Fatalf("second credential: %v", err)
	}
	_ = bob.Close()
	if calls.Load() != 2 {
		t.Errorf("factory calls = %d after a second credential, want 2", calls.Load())
	}
}

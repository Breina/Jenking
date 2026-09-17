package mcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
)

// DepsFactory builds the usecase deps acting as one Jenkins user. It must verify
// the credentials with Jenkins before allocating anything (cache directories
// included) and return an error when they are not accepted. It is called once
// per distinct credential; the result serves all of that caller's sessions.
type DepsFactory func(ctx context.Context, username, token string) (usecase.Deps, error)

// NewHTTPHandler serves MCP over Streamable HTTP at /mcp (session-based) and
// /mcp/stateless, plus an unauthenticated /healthz probe.
//
// Every MCP request must carry HTTP Basic credentials — a Jenkins username and
// API token — which are passed through to Jenkins, so each caller acts with
// exactly their own Jenkins permissions. Callers are partitioned by credential:
// each gets its own server and session table, so a session id presented under
// different credentials is simply unknown.
func NewHTTPHandler(factory DepsFactory, version string, readOnly bool) http.Handler {
	h := &httpHandler{factory: factory, version: version, readOnly: readOnly, tenants: map[[32]byte]*tenant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.Handle("/mcp", h.serve(false))
	mux.Handle("/mcp/stateless", h.serve(true))
	return mux
}

type tenant struct {
	sessions  http.Handler
	stateless http.Handler
}

type httpHandler struct {
	factory  DepsFactory
	version  string
	readOnly bool

	mu      sync.Mutex
	tenants map[[32]byte]*tenant
}

func (h *httpHandler) serve(stateless bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := h.tenantFor(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="jenking"`)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if stateless {
			t.stateless.ServeHTTP(w, r)
			return
		}
		t.sessions.ServeHTTP(w, r)
	})
}

// tenantFor returns the caller's tenant, creating it on first contact once
// Jenkins has accepted the credentials. Rejected credentials are never cached.
func (h *httpHandler) tenantFor(r *http.Request) (*tenant, error) {
	user, token, ok := r.BasicAuth()
	if !ok || user == "" || token == "" {
		return nil, errors.New("send your Jenkins username and API token as HTTP Basic credentials")
	}
	key := sha256.Sum256([]byte(user + "\x00" + token))
	h.mu.Lock()
	t := h.tenants[key]
	h.mu.Unlock()
	if t != nil {
		return t, nil
	}

	// The underlying error is withheld: it names the Jenkins host to a caller
	// who has not authenticated yet.
	deps, err := h.factory(r.Context(), user, token)
	if err != nil {
		return nil, errors.New("could not verify these credentials with Jenkins")
	}
	srv := newServer(deps, h.version, h.readOnly, instructions+remoteInstructions).srv
	getServer := func(*http.Request) *mcp.Server { return srv }
	origin := http.NewCrossOriginProtection()
	t = &tenant{
		sessions:  mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{CrossOriginProtection: origin}),
		stateless: mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{Stateless: true, CrossOriginProtection: origin}),
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if existing := h.tenants[key]; existing != nil {
		return existing, nil
	}
	h.tenants[key] = t
	return t, nil
}

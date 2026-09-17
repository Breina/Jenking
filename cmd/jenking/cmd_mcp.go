package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app/mcp"
	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/logging"
	"github.com/Breina/Jenking/internal/version"
)

// defaultDaemonAddr is where --daemon listens when --http is not given.
const defaultDaemonAddr = ":8808"

// mcpHTTPOptions configures the HTTP listener shared by both HTTP modes.
type mcpHTTPOptions struct {
	addr, tlsCert, tlsKey string
	readOnly              bool
}

func newMCPCmd() *cobra.Command {
	var (
		opt        mcpHTTPOptions
		daemon     bool
		jenkinsURL string
		insecure   bool
		cacheDir   string
	)
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run Jenking as a Model Context Protocol server (stdio, HTTP, or daemon)",
		Long: `Run a long-lived MCP server exposing Jenkins as tools.

By default it communicates over stdin/stdout. Point an MCP client (e.g. Claude) at:

  jenking mcp --context <name>

The server speaks JSON-RPC on stdout, so keep other output off stdout; logs go
to the Jenking log file. Use --read-only to expose only read tools.

With --http the server instead listens for MCP Streamable HTTP clients at /mcp
(sessions) and /mcp/stateless, with an unauthenticated /healthz probe. Every
request must carry HTTP Basic credentials — a Jenkins username and API token —
which are passed through to Jenkins, so each client acts with exactly its own
Jenkins permissions. Basic credentials are cleartext: serve with
--tls-cert/--tls-key or behind a TLS-terminating proxy.

With --daemon it runs server-side with no config file and no stored
credentials: the Jenkins URL comes from --url (or JENKING_URL), it always serves
HTTP (default ` + defaultDaemonAddr + `), and every request must authenticate:

  jenking mcp --daemon --url https://jenkins.example.com --http :8808

Daemon settings can also come from the environment: JENKING_URL,
JENKING_HTTP_ADDR, JENKING_INSECURE=true, JENKING_CACHE_DIR, JENKING_LOG_LEVEL.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (opt.tlsCert == "") != (opt.tlsKey == "") {
				return fmt.Errorf("--tls-cert and --tls-key must be given together")
			}
			for _, name := range []string{"url", "insecure", "cache-dir"} {
				if cmd.Flags().Changed(name) && !daemon {
					return fmt.Errorf("--%s only applies with --daemon; otherwise the config context supplies it", name)
				}
			}
			// MCP is long-running: shut down cleanly on SIGINT/SIGTERM rather
			// than using the per-call CLI timeout.
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if daemon {
				factory, err := daemonFactory(jenkinsURL, insecure, cacheDir)
				if err != nil {
					return err
				}
				if opt.addr == "" {
					opt.addr = defaultDaemonAddr
				}
				return serveMCPHTTP(ctx, opt, factory)
			}
			if opt.addr != "" {
				return serveMCPHTTP(ctx, opt, contextFactory())
			}
			// No background engine: the server only answers requests. Without
			// a live poll the disk-loaded registry would be stale, so
			// list_running asks Jenkins directly.
			cs.store.Registry = nil
			server := mcp.NewServer(ucDeps(), version.App, opt.readOnly)
			return server.Serve(ctx)
		},
	}
	cmd.Flags().BoolVar(&opt.readOnly, "read-only", false, "Expose only read-only tools")
	cmd.Flags().StringVar(&opt.addr, "http", os.Getenv("JENKING_HTTP_ADDR"), "Serve MCP over Streamable HTTP on this address (e.g. 127.0.0.1:8808) instead of stdio (env JENKING_HTTP_ADDR)")
	cmd.Flags().StringVar(&opt.tlsCert, "tls-cert", "", "TLS certificate file for --http")
	cmd.Flags().StringVar(&opt.tlsKey, "tls-key", "", "TLS private key file for --http")
	cmd.Flags().BoolVar(&daemon, "daemon", false, "Server-side mode: no config file or stored credentials; serve HTTP and authenticate every request")
	cmd.Flags().StringVar(&jenkinsURL, "url", os.Getenv("JENKING_URL"), "Jenkins base URL for --daemon (env JENKING_URL)")
	cmd.Flags().BoolVar(&insecure, "insecure", os.Getenv("JENKING_INSECURE") == "true", "Skip Jenkins TLS certificate verification for --daemon (env JENKING_INSECURE=true)")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", os.Getenv("JENKING_CACHE_DIR"), "Cache directory for --daemon; default the XDG cache dir (env JENKING_CACHE_DIR)")
	return cmd
}

// setupDaemonLogging configures logging for --daemon, which has no config file
// to read the level from.
func setupDaemonLogging() error {
	if _, err := logging.Setup(logging.ParseLevel(os.Getenv("JENKING_LOG_LEVEL"))); err != nil {
		return fmt.Errorf("setting up logging: %w", err)
	}
	return nil
}

// contextFactory serves callers of a config-backed HTTP server. Each caller gets
// their own Jenkins client and cache directory; only the SCM index is shared,
// and resolve_job re-checks it with the caller's permissions.
func contextFactory() mcp.DepsFactory {
	active := cs.cfg.ActiveContext()
	// No background engine: the shared SCM index is the snapshot loaded from
	// disk at startup (kept warm by the TUI).
	shared := cs.store
	return func(ctx context.Context, username, token string) (usecase.Deps, error) {
		deps, err := perCallerDeps(ctx, "", active.URL, username, token, active.Insecure)
		if err != nil {
			return usecase.Deps{}, err
		}
		deps.Store.RepoURLs = shared.RepoURLs
		deps.SharedIndex = true
		return deps, nil
	}
}

// daemonFactory serves callers of a --daemon server: it holds no credentials of
// its own, so nothing is shared between callers.
func daemonFactory(jenkinsURL string, insecure bool, cacheDir string) (mcp.DepsFactory, error) {
	u, err := url.Parse(jenkinsURL)
	if jenkinsURL == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("--daemon needs the Jenkins base URL: pass --url or set JENKING_URL (e.g. https://jenkins.example.com)")
	}
	base := strings.TrimRight(jenkinsURL, "/")
	return func(ctx context.Context, username, token string) (usecase.Deps, error) {
		return perCallerDeps(ctx, cacheDir, base, username, token, insecure)
	}, nil
}

// perCallerDeps builds deps acting as one Jenkins user, with a cache directory
// of their own. The credentials are verified first, so a rejected caller never
// causes anything to be created on disk.
func perCallerDeps(ctx context.Context, cacheDir, jenkinsURL, username, token string, insecure bool) (usecase.Deps, error) {
	client := jenkins.NewClient(jenkinsURL, username, token, insecure)
	if _, err := client.WhoAmI(ctx); err != nil {
		return usecase.Deps{}, err
	}
	store := cache.NewStore(diskStoreIn(cacheDir, jenkinsURL+"\x00"+username))
	// No per-caller engine keeps a registry live, so list_running asks Jenkins
	// directly — with the caller's permissions.
	store.Registry = nil
	return usecase.Deps{Client: client, Store: store}, nil
}

// serveMCPHTTP runs the multi-user HTTP transport until ctx is cancelled.
func serveMCPHTTP(ctx context.Context, opt mcpHTTPOptions, factory mcp.DepsFactory) error {
	srv := &http.Server{
		Addr:              opt.addr,
		Handler:           mcp.NewHTTPHandler(factory, version.App, opt.readOnly),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	var err error
	if opt.tlsCert != "" {
		fmt.Fprintf(os.Stderr, "jenking mcp: serving https on %s\n", opt.addr)
		err = srv.ListenAndServeTLS(opt.tlsCert, opt.tlsKey)
	} else {
		fmt.Fprintf(os.Stderr, "jenking mcp: serving plain http on %s; Basic credentials travel in cleartext unless a TLS proxy fronts this server\n", opt.addr)
		err = srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

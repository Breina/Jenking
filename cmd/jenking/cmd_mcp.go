package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app/engine"
	"github.com/Breina/Jenking/internal/app/mcp"
	"github.com/Breina/Jenking/internal/version"
)

func newMCPCmd() *cobra.Command {
	var readOnly bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run Jenking as a Model Context Protocol server (stdio)",
		Long: `Run a long-lived MCP server exposing the active Jenkins context as tools,
communicating over stdin/stdout. Point an MCP client (e.g. Claude) at:

  jenking mcp --context <name>

The server speaks JSON-RPC on stdout, so keep other output off stdout; logs go
to the Jenking log file. Use --read-only to expose only read tools.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// MCP is long-running: shut down cleanly on SIGINT/SIGTERM rather
			// than using the per-call CLI timeout.
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			// The engine keeps the registry live so list_running reflects
			// current reality rather than the stale disk-loaded snapshot.
			engine.New(cs.client, cs.store).Start(ctx)
			server := mcp.NewServer(ucDeps(), version.App, readOnly)
			return server.Serve(ctx)
		},
	}
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Expose only read-only tools")
	return cmd
}

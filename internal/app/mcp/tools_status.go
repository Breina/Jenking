package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/dto"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// registerStatusTools wires controller health, queue-item lookup, and SCM info.
func (s *Server) registerStatusTools() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_status",
		Description: "Check controller health in one call: reachability and latency, version, quiet-down (preparing for shutdown), " +
			"online node/executor capacity, and queue pressure (stuck and blocked items). An unreachable controller returns reachable=false; " +
			"parts the caller may not read are listed in warnings.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, statusOut, error) {
		st, err := d.Status(ctx)
		if err != nil {
			return nil, statusOut{Warnings: []string{err.Error()}}, nil
		}
		return nil, statusOut{
			Reachable:     true,
			Version:       st.Version,
			Mode:          st.Mode,
			QuietingDown:  st.QuietingDown,
			User:          st.User,
			LatencyMs:     st.LatencyMs,
			Nodes:         st.Nodes,
			OfflineNodes:  st.OfflineNodes,
			Executors:     st.Executors,
			BusyExecutors: st.BusyExecutors,
			QueueLength:   st.QueueLength,
			StuckItems:    st.StuckItems,
			BlockedItems:  st.BlockedItems,
			Warnings:      st.Warnings,
		}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_queue_item",
		Description: "Look up one queue item by id (as returned by trigger_build or rebuild_build): why it is waiting, and its build_number once it has started. " +
			"Jenkins forgets items a few minutes after they start, so switch to get_build/wait_for_build once build_number is known.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in queueItemIn) (*mcp.CallToolResult, queueItemOut, error) {
		item, n, err := d.GetQueueItem(ctx, in.QueueID)
		if jmodel.IsNotFound(err) {
			// Jenkins garbage-collects queue items minutes after they start, so
			// a 404 usually means "already building", not "never existed".
			return nil, queueItemOut{}, fmt.Errorf("queue item %d is not in the queue: it either already started (find it with list_builds or list_running) or never existed", in.QueueID)
		}
		if err != nil {
			return nil, queueItemOut{}, err
		}
		return nil, queueItemOut{Item: dto.ToQueueItem(*item), BuildNumber: n}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_scm",
		Description: "Return a job's SCM web URL and the source revisions (commit, branch, remote URLs) a build checked out — one entry per repository, shared libraries included. " +
			"Omit build_number for the latest build. Revisions are read from whatever the installed SCM plugin records and are empty when it records none.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in buildRefIn) (*mcp.CallToolResult, scmOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n := in.BuildNumber
		if n == 0 {
			// Folders and never-built jobs have no latest build; report the
			// job-level URL alone for those.
			if latest, err := d.ResolveBuild(ctx, in.JobPath, 0); err == nil {
				n = latest
			}
		}
		info, err := d.GetSCM(ctx, in.JobPath, n)
		if err != nil {
			return nil, scmOut{}, err
		}
		return nil, scmOut{
			JobPath:     in.JobPath,
			SCMURL:      info.SCMURL,
			BuildNumber: info.BuildNumber,
			Revisions: mapSlice(info.Revisions, func(r jmodel.SCMRevision) scmRevisionOut {
				return scmRevisionOut{Revision: r.Revision, Branches: r.Branches, RemoteURLs: r.RemoteURLs}
			}),
		}, nil
	})
}

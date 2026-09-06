package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
)

// maxTriggerWait bounds trigger_build --wait so a paused/stuck build can never
// hold a request open indefinitely.
const maxTriggerWait = 600 * time.Second

// mutateHint is the annotation shared by non-destructive mutating tools (they
// create or change state but do not tear down existing builds/data).
func mutateHint() *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &f}
}

// destructiveHint marks tools that abort or remove existing builds/queue items.
func destructiveHint() *mcp.ToolAnnotations {
	t := true
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &t}
}

// registerMutateTools wires the state-changing tools. It is only called when the
// server is not in --read-only mode, so these tools are simply absent otherwise.
func (s *Server) registerMutateTools() {
	s.registerBuildMutations()
	s.registerInputMutations()
	s.registerLifecycleMutations()
}

// registerBuildMutations wires trigger/replay/cancel/dequeue.
func (s *Server) registerBuildMutations() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "trigger_build",
		Description: "Start a new build of a job, optionally with parameters. With wait=true, block until the build finishes (up to wait_timeout_seconds, default 300, max 600) and report its final status; progress notifications are emitted while waiting.",
		Annotations: mutateHint(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in triggerIn) (*mcp.CallToolResult, triggerOut, error) {
		opt := usecase.TriggerOptions{Params: in.Params, Wait: in.Wait}
		if in.Wait {
			timeout := time.Duration(intOr(in.WaitTimeoutSecs, 300)) * time.Second
			if timeout > maxTriggerWait {
				timeout = maxTriggerWait
			}
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
			opt.Progress = progressReporter(ctx, req)
		}
		res, err := d.Trigger(ctx, in.JobPath, opt)
		if err != nil {
			return nil, triggerOut{}, err
		}
		return nil, triggerOut{
			JobPath:     res.JobPath,
			QueueID:     res.QueueID,
			BuildNumber: res.BuildNumber,
			Status:      string(res.Status),
		}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "replay_build",
		Description: "Rerun a build with an edited Jenkinsfile, queuing a new build. Omit build_number to replay the latest build. This is the last step of the authoring loop (get_pipeline_symbols -> edit -> lint_pipeline -> replay_build).",
		Annotations: mutateHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in replayIn) (*mcp.CallToolResult, actionOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, actionOut{}, err
		}
		if err := d.Replay(ctx, in.JobPath, n, in.Script); err != nil {
			return nil, actionOut{}, err
		}
		return nil, actionOut{OK: true, Message: fmt.Sprintf("replayed %s #%d", in.JobPath, n)}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "cancel_build",
		Description: "Abort a running build. build_number is required; this destructive action never defaults to the latest build.",
		Annotations: destructiveHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in cancelIn) (*mcp.CallToolResult, actionOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		if in.BuildNumber <= 0 {
			return nil, actionOut{}, fmt.Errorf("build_number is required to cancel a build")
		}
		if err := d.Cancel(ctx, in.JobPath, in.BuildNumber); err != nil {
			return nil, actionOut{}, err
		}
		return nil, actionOut{OK: true, Message: fmt.Sprintf("cancelled %s #%d", in.JobPath, in.BuildNumber)}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "dequeue",
		Description: "Remove a still-waiting build from the Jenkins queue by its queue id (from list_queue).",
		Annotations: destructiveHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in dequeueIn) (*mcp.CallToolResult, actionOut, error) {
		if err := d.Dequeue(ctx, in.QueueID); err != nil {
			return nil, actionOut{}, err
		}
		return nil, actionOut{OK: true, Message: fmt.Sprintf("dequeued %d", in.QueueID)}, nil
	})
}

// registerInputMutations wires approve/reject of paused pipeline input steps.
func (s *Server) registerInputMutations() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "approve_input",
		Description: "Approve (proceed) a paused pipeline input step, optionally supplying its parameters. Omit input_id when exactly one input is pending. Omit build_number for the latest build.",
		Annotations: mutateHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in approveInputIn) (*mcp.CallToolResult, inputResultOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, inputResultOut{}, err
		}
		id, err := d.ApproveInput(ctx, in.JobPath, n, in.InputID, in.Params)
		if err != nil {
			return nil, inputResultOut{}, err
		}
		return nil, inputResultOut{BuildNumber: n, InputID: id, Action: "approved"}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "reject_input",
		Description: "Reject (abort) a paused pipeline input step. Omit input_id when exactly one input is pending. Omit build_number for the latest build.",
		Annotations: destructiveHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in rejectInputIn) (*mcp.CallToolResult, inputResultOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, inputResultOut{}, err
		}
		id, err := d.RejectInput(ctx, in.JobPath, n, in.InputID)
		if err != nil {
			return nil, inputResultOut{}, err
		}
		return nil, inputResultOut{BuildNumber: n, InputID: id, Action: "rejected"}, nil
	})
}

// registerLifecycleMutations wires enable/disable/rescan and node offline/online.
func (s *Server) registerLifecycleMutations() {
	d := s.deps

	addJobEnabledTool(s, "enable_job", "Enable a job so it can be built.", true)
	addJobEnabledTool(s, "disable_job", "Disable a job so it can no longer be built.", false)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "rescan",
		Description: "Trigger branch indexing (\"Scan Repository Now\") on a multibranch project so Jenkins discovers new branches and drops deleted ones.",
		Annotations: mutateHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobRefIn) (*mcp.CallToolResult, actionOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		// Rescan's multibranch check looks the job up within its parent folder;
		// the folder is the job path minus its last segment.
		folder := ""
		if i := strings.LastIndex(in.JobPath, "/"); i >= 0 {
			folder = in.JobPath[:i]
		}
		if err := d.Rescan(ctx, folder, in.JobPath); err != nil {
			return nil, actionOut{}, err
		}
		return nil, actionOut{OK: true, Message: "rescan triggered for " + in.JobPath}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "set_node_offline",
		Description: "Take a Jenkins node temporarily offline. Idempotent: a node already offline is a no-op. The controller is named \"(built-in)\".",
		Annotations: mutateHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in nodeOfflineIn) (*mcp.CallToolResult, actionOut, error) {
		if err := d.SetNodeOffline(ctx, in.Name, true, in.Reason); err != nil {
			return nil, actionOut{}, err
		}
		return nil, actionOut{OK: true, Message: "node " + in.Name + " is offline"}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "set_node_online",
		Description: "Bring a temporarily-offline Jenkins node back online. Idempotent: a node already online is a no-op.",
		Annotations: mutateHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in nodeOnlineIn) (*mcp.CallToolResult, actionOut, error) {
		if err := d.SetNodeOffline(ctx, in.Name, false, ""); err != nil {
			return nil, actionOut{}, err
		}
		return nil, actionOut{OK: true, Message: "node " + in.Name + " is online"}, nil
	})
}

// addJobEnabledTool registers an enable/disable tool over the same verb.
func addJobEnabledTool(s *Server, name, desc string, enabled bool) {
	d := s.deps
	action := "disabled"
	if enabled {
		action = "enabled"
	}
	mcp.AddTool(s.srv, &mcp.Tool{
		Name: name, Description: desc, Annotations: mutateHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobRefIn) (*mcp.CallToolResult, actionOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		if err := d.SetJobEnabled(ctx, in.JobPath, enabled); err != nil {
			return nil, actionOut{}, err
		}
		return nil, actionOut{OK: true, Message: action + " " + in.JobPath}, nil
	})
}

// progressReporter returns a usecase.TriggerOptions.Progress callback that emits
// MCP progress notifications, but only when the client supplied a progress token
// on the request (otherwise notifications are pointless and are dropped).
func progressReporter(ctx context.Context, req *mcp.CallToolRequest) func(string) {
	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	var n float64
	return func(msg string) {
		n++
		_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Message:       msg,
			Progress:      n,
		})
	}
}

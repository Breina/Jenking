package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/dto"
	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// registerReadTools wires every stateless read-only tool onto the server.
func (s *Server) registerReadTools() {
	s.registerInfoTools()
	s.registerBuildTools()
	s.registerChangeTools()
	s.registerQueueTools()
	s.registerLogTools()
	s.registerPipelineTools()
	s.registerWaitTools()
	s.registerFollowTools()
}

// addBuildScopedTool registers a tool taking a job path + optional build number,
// resolving the build (latest when omitted) before invoking fetch. It factors
// out the resolve-then-fetch pattern shared by the build-detail tools.
func addBuildScopedTool[Out any](s *Server, t *mcp.Tool, fetch func(ctx context.Context, d usecase.Deps, jobPath string, buildNum int) (Out, error)) {
	d := s.deps
	mcp.AddTool(s.srv, t, func(ctx context.Context, _ *mcp.CallToolRequest, in buildRefIn) (*mcp.CallToolResult, Out, error) {
		var zero Out
		jobPath := d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, jobPath, in.BuildNumber)
		if err != nil {
			return nil, zero, err
		}
		out, err := fetch(ctx, d, jobPath, n)
		if err != nil {
			return nil, zero, err
		}
		return nil, out, nil
	})
}

// registerInfoTools registers the job/instance-level lookups (no build ref).
func (s *Server) registerInfoTools() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "whoami",
		Description: "Return the authenticated Jenkins user and controller version.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, dto.User, error) {
		u, err := d.WhoAmI(ctx)
		if err != nil {
			return nil, dto.User{}, err
		}
		return nil, dto.ToUser(*u), nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_jobs",
		Description: "List jobs and folders directly under a folder path (empty = root), or the jobs of a named view.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listJobsIn) (*mcp.CallToolResult, jobsOut, error) {
		in.Folder = d.CanonicalJobPath(ctx, in.Folder)
		var (
			jobs []jmodel.Job
			err  error
		)
		if in.View != "" {
			jobs, err = d.ListViewJobs(ctx, in.Folder, in.View)
		} else {
			jobs, err = d.ListJobs(ctx, in.Folder)
		}
		if err != nil {
			return nil, jobsOut{}, err
		}
		return nil, jobsOut{Jobs: mapSlice(jobs, dto.ToJob)}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_views",
		Description: "List the Jenkins views on a container (empty = root, personal views included). A view is a saved job filter; pass its name to list_jobs's view argument.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listViewsIn) (*mcp.CallToolResult, viewsOut, error) {
		in.Folder = d.CanonicalJobPath(ctx, in.Folder)
		views, err := d.ListViews(ctx, in.Folder)
		if err != nil {
			return nil, viewsOut{}, err
		}
		return nil, viewsOut{Views: mapSlice(views, dto.ToView)}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_builds",
		Description: "List a job's builds, newest first. Set mine to only the authenticated user's builds.",
		Annotations: readOnlyHint(),
	}, s.handleListBuilds)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_params",
		Description: "List a job's build parameter definitions (name, type, default, choices).",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobRefIn) (*mcp.CallToolResult, paramsOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		params, err := d.GetParams(ctx, in.JobPath)
		if err != nil {
			return nil, paramsOut{}, err
		}
		return nil, paramsOut{Params: mapSlice(params, dto.ToParam)}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_metadata",
		Description: "Dump a job's raw Jenkins JSON flattened to path=value rows (plugin-agnostic). depth controls how deep the fetch walks (default 3).",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in metadataIn) (*mcp.CallToolResult, metadataOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		entries, err := d.GetJobMetadata(ctx, in.JobPath, intOr(in.Depth, 3))
		if err != nil {
			return nil, metadataOut{}, err
		}
		return nil, metadataOut{Entries: mapSlice(entries, dto.ToMeta)}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_running",
		Description: "List the builds currently running, from Jenking's live registry (more truthful than a raw API snapshot: it holds terminal builds sticky and confirms running state). Use this to answer \"what is building right now\". Set mine to only the authenticated user's builds.",
		Annotations: readOnlyHint(),
	}, s.handleListRunning)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "resolve_job",
		Description: "Resolve a git remote or SCM URL to the matching Jenkins job path(s) — the \"which job is this repo?\" lookup. Reads Jenking's warm SCM-URL index (populated as builds run; no live scan), so a repo whose pipeline has never been observed may return no matches. Results are ranked primary-branch-first.",
		Annotations: readOnlyHint(),
	}, s.handleResolveJob)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_queue_history",
		Description: "Summarize how long builds have been waiting in the queue over the last window (default 120 min), as a wait-time histogram split by reason (stuck/blocked/pending/buildable), plus peak running/queued counts. This history is Jenking's own — Jenkins does not retain it. Returns aggregates only, never raw samples.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in queueHistoryIn) (*mcp.CallToolResult, dto.QueueHistory, error) {
		window := time.Duration(intOr(in.WindowMinutes, 120)) * time.Minute
		h, err := d.QueueHistory(ctx, window)
		if err != nil {
			return nil, dto.QueueHistory{}, err
		}
		return nil, dto.ToQueueHistory(h), nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_nodes",
		Description: "List Jenkins nodes with executor utilization, health, and labels.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, nodesOut, error) {
		nodes, err := d.ListNodes(ctx)
		if err != nil {
			return nil, nodesOut{}, err
		}
		return nil, nodesOut{Nodes: mapSlice(nodes, dto.ToNode)}, nil
	})
}

// registerBuildTools registers the build-scoped detail lookups.
func (s *Server) registerBuildTools() {
	addBuildScopedTool(s, &mcp.Tool{
		Name:        "get_build",
		Description: "Get full detail for a build (parameters, timing, pending inputs). Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, d usecase.Deps, jobPath string, n int) (dto.BuildDetail, error) {
		detail, err := d.GetBuild(ctx, jobPath, n)
		if err != nil {
			return dto.BuildDetail{}, err
		}
		return dto.ToBuildDetail(jobPath, *detail), nil
	})

	addBuildScopedTool(s, &mcp.Tool{
		Name:        "get_stages",
		Description: "List a build's pipeline stages with status and timing. Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, d usecase.Deps, jobPath string, n int) (stagesOut, error) {
		stages, err := d.GetStages(ctx, jobPath, n)
		if err != nil {
			return stagesOut{}, err
		}
		return stagesOut{BuildNumber: n, Stages: mapSlice(stages, dto.ToStage)}, nil
	})

	addBuildScopedTool(s, &mcp.Tool{
		Name: "list_artifacts",
		Description: "List a build's archived artifacts. Omit build_number for the latest build. " +
			"The returned URLs need Jenkins authentication — fetching one directly returns 403; call get_artifact to download the content.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, d usecase.Deps, jobPath string, n int) (artifactsOut, error) {
		arts, err := d.ListArtifacts(ctx, jobPath, n)
		if err != nil {
			return artifactsOut{}, err
		}
		return artifactsOut{BuildNumber: n, Artifacts: mapSlice(arts, dto.ToArtifact)}, nil
	})

	addBuildScopedTool(s, &mcp.Tool{
		Name:        "list_inputs",
		Description: "List a build's pending pipeline input steps (awaiting approval). Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, d usecase.Deps, jobPath string, n int) (inputsOut, error) {
		inputs, err := d.ListInputs(ctx, jobPath, n)
		if err != nil {
			return inputsOut{}, err
		}
		return inputsOut{BuildNumber: n, Inputs: mapSlice(inputs, dto.ToInput)}, nil
	})

	d := s.deps
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_test_report",
		Description: "Get a build's JUnit test report. failed_only (default true) drops passing cases; max_cases (default 50) caps how many cases are returned.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in testReportIn) (*mcp.CallToolResult, testReportOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, testReportOut{}, err
		}
		report, err := d.GetTestReport(ctx, in.JobPath, n)
		if err != nil {
			return nil, testReportOut{}, err
		}
		out := dto.ToTestReport(*report)
		filterTestReport(&out, boolOr(in.FailedOnly, true), intOr(in.MaxCases, 50))
		return nil, testReportOut{BuildNumber: n, Report: out}, nil
	})
}

// registerChangeTools registers the commit/change tools.
func (s *Server) registerChangeTools() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_changes",
		Description: "List the SCM commits recorded for a build. include_paths (default false) adds each commit's affected file paths. Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in changesIn) (*mcp.CallToolResult, changesOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, changesOut{}, err
		}
		changes, err := d.GetChanges(ctx, in.JobPath, n)
		if err != nil {
			return nil, changesOut{}, err
		}
		out := mapSlice(changes, dto.ToChange)
		if !in.IncludePaths {
			for i := range out {
				out[i].AffectedPaths = nil
			}
		}
		return nil, changesOut{BuildNumber: n, Changes: out}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "find_commit",
		Description: "Find which of a job's recent builds contain a commit (prefix match). One call scans up to max_builds (default 25, max 50) builds. Use this to verify whether a commit shipped in a build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in findCommitIn) (*mcp.CallToolResult, findCommitOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		maxBuilds := intOr(in.MaxBuilds, 25)
		hits, err := d.FindCommit(ctx, in.JobPath, in.Commit, maxBuilds)
		if err != nil {
			return nil, findCommitOut{}, err
		}
		return nil, findCommitOut{Hits: mapSlice(hits, dto.ToCommitHit), SearchedBuilds: maxBuilds}, nil
	})
}

// filterTestReport drops passing cases when failedOnly is set and caps the total
// number of cases across all suites to maxCases. Suite counts (Failed/Passed/…)
// are left intact so totals remain truthful even when cases are trimmed.
func filterTestReport(r *dto.TestReport, failedOnly bool, maxCases int) {
	remaining := maxCases
	suites := r.Suites[:0]
	for _, suite := range r.Suites {
		kept := suite.Cases[:0]
		for _, c := range suite.Cases {
			if failedOnly && c.Status != "failed" {
				continue
			}
			if remaining <= 0 {
				break
			}
			kept = append(kept, c)
			remaining--
		}
		if len(kept) == 0 {
			continue
		}
		suite.Cases = kept
		suites = append(suites, suite)
	}
	r.Suites = suites
}

// handleListBuilds serves list_builds: a job's builds newest-first, optionally
// filtered to the authenticated user, capped to limit (default 15).
func (s *Server) handleListBuilds(ctx context.Context, _ *mcp.CallToolRequest, in listBuildsIn) (*mcp.CallToolResult, buildsOut, error) {
	in.JobPath = s.deps.CanonicalJobPath(ctx, in.JobPath)
	builds, err := s.deps.ListBuilds(ctx, in.JobPath)
	if err != nil {
		return nil, buildsOut{}, err
	}
	if in.Mine {
		if builds, err = s.deps.FilterBuildsMine(ctx, builds); err != nil {
			return nil, buildsOut{}, err
		}
	}
	if limit := intOr(in.Limit, 15); len(builds) > limit {
		builds = builds[:limit]
	}
	return nil, buildsOut{Builds: mapSlice(builds, dto.ToBuild)}, nil
}

// handleListRunning serves list_running, optionally filtered to the
// authenticated user.
func (s *Server) handleListRunning(ctx context.Context, _ *mcp.CallToolRequest, in listRunningIn) (*mcp.CallToolResult, runningOut, error) {
	builds, err := s.deps.ListRunning(ctx)
	if err != nil {
		return nil, runningOut{}, err
	}
	if in.Mine {
		if builds, err = s.deps.FilterRunningMine(ctx, builds); err != nil {
			return nil, runningOut{}, err
		}
	}
	return nil, runningOut{Builds: mapSlice(builds, dto.ToUserBuild), Count: len(builds)}, nil
}

// handleResolveJob serves resolve_job: a cache-only reverse lookup from an SCM
// URL / git remote to the matching Jenkins job path(s).
func (s *Server) handleResolveJob(_ context.Context, _ *mcp.CallToolRequest, in resolveJobIn) (*mcp.CallToolResult, resolveJobOut, error) {
	matches := s.deps.ResolveJob(in.ScmURL)
	return nil, resolveJobOut{Matches: mapSlice(matches, dto.ToJobMatch), Count: len(matches)}, nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func intOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// registerQueueTools registers the queue-facing lookups. Queue and scans live
// together because they come from one endpoint and differ only by kind.
func (s *Server) registerQueueTools() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_queue",
		Description: "List the current Jenkins build queue (items waiting to run and why). Branch-indexing scans travel through the same queue but never produce a build; they are excluded unless kind is \"scan\" or \"all\".",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in queueIn) (*mcp.CallToolResult, queueOut, error) {
		want := jmodel.QueueKindBuild
		switch in.Kind {
		case "", "build":
		case "scan":
			want = jmodel.QueueKindScan
		case "all":
			want = ""
		default:
			return nil, queueOut{}, fmt.Errorf("invalid kind %q: want build, scan or all", in.Kind)
		}
		items, err := d.ListQueueOfKind(ctx, want)
		if err != nil {
			return nil, queueOut{}, err
		}
		return nil, queueOut{Items: mapSlice(items, dto.ToQueueItem)}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_scans",
		Description: "List the branch-indexing scans (multibranch/folder repository scans) currently waiting in the queue, optionally under a folder. A scan is a run of the container itself and never produces a build; once it leaves the queue it is running, which Jenkins exposes no status endpoint for, so only waiting scans appear here.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listJobsIn) (*mcp.CallToolResult, queueOut, error) {
		items, err := d.ListQueueOfKind(ctx, jmodel.QueueKindScan)
		if err != nil {
			return nil, queueOut{}, err
		}
		out := make([]dto.QueueItem, 0, len(items))
		for _, q := range items {
			if in.Folder != "" && !strings.HasPrefix(q.JobPath, in.Folder) {
				continue
			}
			out = append(out, dto.ToQueueItem(q))
		}
		return nil, queueOut{Items: out}, nil
	})
}

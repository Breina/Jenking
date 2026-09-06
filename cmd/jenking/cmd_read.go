package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app/dto"
	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
	"github.com/Breina/Jenking/internal/tui/command"
)

func newViewsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "views [<folder>]",
		Short: "List Jenkins views",
		Long: `List the views defined on a container, plus your personal views.

A view is a saved job filter; pass its name to "jenking jobs --view" to list
the jobs it shows.

Arguments:
  folder  Folder whose views to list (default: root)

Examples:
  jenking views
  jenking views my-team
  jenking views --output json | jq -r '.[].name'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := ""
			if len(args) > 0 {
				folder = args[0]
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			views, err := cs.client.ListViews(ctx, folder)
			if err != nil {
				return writeError(err)
			}
			// Personal views hang off the user, not off a folder.
			if folder == "" {
				if user, err := cs.client.WhoAmI(ctx); err == nil {
					if mine, err := cs.client.ListMyViews(ctx, user.ID); err == nil {
						views = append(views, mine...)
					}
				}
			}
			out := make([]outView, len(views))
			for i, v := range views {
				out[i] = dto.ToView(v)
			}
			switch outputFlag {
			case "json":
				return printJSON(os.Stdout, out)
			case "yaml":
				return printYAML(os.Stdout, out)
			default:
				return printViewsTable(os.Stdout, out)
			}
		},
	}
}

func newJobsCmd() *cobra.Command {
	var viewName string
	cmd := &cobra.Command{
		Use:   "jobs [<folder>]",
		Short: "List Jenkins jobs",
		Long: `List jobs in a folder or at the root.

Arguments:
  folder  Folder path to list (default: root)

Use --view to list the jobs of a Jenkins view instead. A view can show jobs
from anywhere in the folder tree, so their paths are not necessarily under
the container the view belongs to.

Examples:
  jenking jobs
  jenking jobs my-team
  jenking jobs --view "Team Infra"
  jenking jobs my-team --output json | jq '.[].full_path'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := ""
			if len(args) > 0 {
				folder = args[0]
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			jobs, err := listJobsMaybeView(ctx, folder, viewName)
			if err != nil {
				return writeError(err)
			}
			out := make([]outJob, len(jobs))
			for i, j := range jobs {
				out[i] = toOutJob(j)
			}
			switch outputFlag {
			case "json":
				return printJSON(os.Stdout, out)
			case "yaml":
				return printYAML(os.Stdout, out)
			default:
				return printJobsTable(os.Stdout, out)
			}
		},
	}
	cmd.Flags().StringVar(&viewName, "view", "", "List the jobs of this Jenkins view")
	return cmd
}

// listJobsMaybeView lists a folder's jobs, or a named view's jobs when one is
// given. The name is resolved against the folder's views (personal views
// included at the root), so the caller never has to know a view's kind.
func listJobsMaybeView(ctx context.Context, folder, viewName string) ([]jmodel.Job, error) {
	if viewName == "" {
		return cs.client.ListJobs(ctx, folder)
	}
	views, err := cs.client.ListViews(ctx, folder)
	if err != nil {
		return nil, err
	}
	if folder == "" {
		if user, err := cs.client.WhoAmI(ctx); err == nil {
			if mine, err := cs.client.ListMyViews(ctx, user.ID); err == nil {
				views = append(views, mine...)
			}
		}
	}
	v, ok := view.FindView(views, viewName)
	if !ok {
		return nil, fmt.Errorf("no such view: %s", viewName)
	}
	return cs.client.ListViewJobs(ctx, v)
}

func newBuildsCmd() *cobra.Command {
	var mine bool
	cmd := &cobra.Command{
		Use:   "builds <project> [<branch>]",
		Short: "List builds for a project or branch",
		Long: `List recent builds for a project or branch, newest first.

Arguments:
  project  Project name or suffix (resolved from cache)
  branch   Branch name; if omitted, shows all branches for a multibranch project

Use --mine to show only builds you triggered or pushed (matched against your
Jenkins user and configured git usernames).

Examples:
  jenking builds my-project
  jenking builds my-project --mine
  jenking builds my-project main
  jenking builds my-project main --output json | jq '.[] | select(.status=="failed")'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := command.ParseTarget(args)
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			nc, err := navmsg.ResolveTarget(target, cs.store, navmsg.NavigationContext{})
			if err != nil {
				return writeError(err)
			}

			if nc.Level == navmsg.CtxBranch || (nc.Level == navmsg.CtxProject && nc.BranchName != "") {
				return emitBranchBuilds(ctx, nc, mine)
			}
			return emitProjectBuilds(ctx, nc, mine)
		},
	}
	cmd.Flags().BoolVar(&mine, "mine", false, "Only builds you triggered or pushed")
	return cmd
}

// emitBranchBuilds lists a single branch's builds, optionally filtered to the
// authenticated user, in the selected output format.
func emitBranchBuilds(ctx context.Context, nc navmsg.NavigationContext, mine bool) error {
	builds, err := cs.client.ListBuilds(ctx, nc.JobPath())
	if err != nil {
		return writeError(enrichBranchNotFound(ctx, nc, err))
	}
	if mine {
		if builds, err = ucDeps().FilterBuildsMine(ctx, builds); err != nil {
			return writeError(err)
		}
	}
	out := make([]outBuild, len(builds))
	for i, b := range builds {
		out[i] = toOutBuild(b)
	}
	return emitList(out, printBuildsTable)
}

// emitProjectBuilds lists a multibranch project's builds across all branches,
// optionally filtered to the authenticated user, in the selected output format.
func emitProjectBuilds(ctx context.Context, nc navmsg.NavigationContext, mine bool) error {
	pbuilds, err := cs.client.ListProjectBuilds(ctx, nc.JobPath())
	if err != nil {
		return writeError(err)
	}
	if mine {
		if pbuilds, err = ucDeps().FilterProjectBuildsMine(ctx, pbuilds); err != nil {
			return writeError(err)
		}
	}
	out := make([]outProjectBuild, len(pbuilds))
	for i, b := range pbuilds {
		out[i] = toOutProjectBuild(b)
	}
	return emitList(out, printProjectBuildsTable)
}

func newStagesCmd() *cobra.Command {
	return newProjectBuildListCmd(
		"stages <project> <branch> [#N|#last]",
		"List pipeline stages for a build",
		`List pipeline stages for a specific build or the latest build.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking stages my-project main
  jenking stages my-project main #42
  jenking stages my-project main #last --output json`,
		func(ctx context.Context, jobPath string, buildNum int) ([]jmodel.Stage, error) {
			return cs.client.ListStages(ctx, jobPath, buildNum)
		}, toOutStage, printStagesTable,
	)
}

func newRunningCmd() *cobra.Command {
	var mine bool
	cmd := &cobra.Command{
		Use:   "running",
		Short: "List currently running builds",
		Long: `List all builds currently executing across all Jenkins nodes.

Use --mine to show only builds you triggered or pushed (matched against your
Jenkins user and configured git usernames).

Examples:
  jenking running
  jenking running --mine
  jenking running --output json | jq '.[].job_path'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			builds, err := cs.client.ListRunningBuilds(ctx)
			if err != nil {
				return writeError(err)
			}
			if mine {
				if builds, err = ucDeps().FilterRunningMine(ctx, builds); err != nil {
					return writeError(err)
				}
			}
			out := make([]outUserBuild, len(builds))
			for i, b := range builds {
				out[i] = toOutUserBuild(b)
			}
			switch outputFlag {
			case "json":
				return printJSON(os.Stdout, out)
			case "yaml":
				return printYAML(os.Stdout, out)
			default:
				return printUserBuildsTable(os.Stdout, out)
			}
		},
	}
	cmd.Flags().BoolVar(&mine, "mine", false, "Only builds you triggered or pushed")
	return cmd
}

func newQueueCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "List builds waiting in the queue",
		Long: `List all builds currently waiting in the Jenkins build queue.

Branch-indexing scans share the queue endpoint but never produce a build, so
they are excluded by default. Use --kind to see them.

Examples:
  jenking queue
  jenking queue --kind scan
  jenking queue --output json | jq '.[].job_path'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			var want jmodel.QueueKind
			switch kind {
			case "build", "scan":
				want = jmodel.QueueKind(kind)
			case "all":
				want = ""
			default:
				return writeError(fmt.Errorf("invalid --kind %q: want build, scan or all", kind))
			}
			items, err := ucDeps().ListQueueOfKind(ctx, want)
			if err != nil {
				return writeError(err)
			}
			out := make([]outQueueItem, len(items))
			for i, q := range items {
				out[i] = toOutQueueItem(q)
			}
			return emitList(out, printQueueTable)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "build", "queue item kind: build, scan or all")
	return cmd
}

func newScansCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scans [<folder>]",
		Short: "List branch-indexing scans waiting in the queue",
		Long: `List the multibranch/folder scans currently waiting in the Jenkins queue,
optionally narrowed to a folder.

Scans are queued runs of a container itself, so they never produce a build.
A scan that has left the queue is already running; Jenkins exposes no cheap
status for it, so only waiting scans are listed here.

Examples:
  jenking scans
  jenking scans Bodemondergrond`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			items, err := ucDeps().ListQueueOfKind(ctx, jmodel.QueueKindScan)
			if err != nil {
				return writeError(err)
			}
			out := make([]outQueueItem, 0, len(items))
			for _, q := range items {
				if len(args) == 1 && !strings.HasPrefix(q.JobPath, args[0]) {
					continue
				}
				out = append(out, toOutQueueItem(q))
			}
			return emitList(out, printQueueTable)
		},
	}
}

// emitList writes a slice in the format selected by --output: json, yaml, or
// the provided table printer (default).
func emitList[T any](out []T, table func(io.Writer, []T) error) error {
	switch outputFlag {
	case "json":
		return printJSON(os.Stdout, out)
	case "yaml":
		return printYAML(os.Stdout, out)
	default:
		return table(os.Stdout, out)
	}
}

func newWhoAmICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print current Jenkins user and version",
		Long: `Print the authenticated user and Jenkins server version.

Examples:
  jenking whoami
  jenking whoami --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			user, err := cs.client.WhoAmI(ctx)
			if err != nil {
				return writeError(err)
			}
			out := toOutUser(*user)
			switch outputFlag {
			case "json":
				return printJSON(os.Stdout, out)
			case "yaml":
				return printYAML(os.Stdout, out)
			default:
				return printUserTable(os.Stdout, out)
			}
		},
	}
}

func newParamsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "params <project> [<branch>]",
		Short: "List parameter definitions for a job",
		Long: `List the parameter definitions for a Jenkins job.

Arguments:
  project  Project name or suffix
  branch   Branch name (for multibranch projects)

Examples:
  jenking params my-project main
  jenking params my-project main --output json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := command.ParseTarget(args)
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			nc, err := navmsg.ResolveTarget(target, cs.store, navmsg.NavigationContext{})
			if err != nil {
				return writeError(err)
			}
			if nc.ProjectName == "" {
				return fmt.Errorf("project required")
			}
			if nc.BranchName == "" {
				if j, ok := cache.LookupJob(cs.store, nc.FolderPath, nc.JobPath()); ok && j.Type == jmodel.JobTypeMultiBranch {
					project := navmsg.DecodePath(nc.JobPath())
					return writeError(fmt.Errorf("%s is a multibranch project; specify a branch, e.g. `jenking params %s <branch>`", project, project))
				}
			}

			params, err := cs.client.GetJobParameters(ctx, nc.JobPath())
			if err != nil {
				return writeError(err)
			}
			out := make([]outParam, len(params))
			for i, p := range params {
				out[i] = toOutParam(p)
			}
			switch outputFlag {
			case "json":
				return printJSON(os.Stdout, out)
			case "yaml":
				return printYAML(os.Stdout, out)
			default:
				return printParamsTable(os.Stdout, out)
			}
		},
	}
}

func newMetadataCmd() *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:     "metadata <project> [<branch>]",
		Aliases: []string{"meta"},
		Short:   "Dump a job's raw Jenkins metadata (all fields)",
		Long: `Fetch a job's Jenkins JSON and list every field as a path=value pair.

Generic and plugin-agnostic: it shows whatever the API returns (SCM remote URLs,
project properties, actions, etc.) without interpreting any of it. Use --depth to
control how deep the fetch walks the object graph (larger = more fields, bigger
payload).

Arguments:
  project  Project name or suffix
  branch   Branch name (for multibranch projects)

Examples:
  jenking metadata my-project
  jenking metadata my-project main --depth 1
  jenking metadata my-project main --output json | jq '.[] | select(.path | contains("remoteUrls"))'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := command.ParseTarget(args)
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			nc, err := navmsg.ResolveTarget(target, cs.store, navmsg.NavigationContext{})
			if err != nil {
				return writeError(err)
			}

			tree, err := cs.client.GetJobMetadata(ctx, nc.JobPath(), depth)
			if err != nil {
				return writeError(err)
			}
			entries := tree.Flatten()
			out := make([]outMeta, len(entries))
			for i, e := range entries {
				out[i] = toOutMeta(e)
			}
			return emitList(out, printMetadataTable)
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 3, "how deep to fetch the metadata tree")
	return cmd
}

func newArtifactsCmd() *cobra.Command {
	return newProjectBuildListCmd(
		"artifacts <project> <branch> [#N|#last]",
		"List artifacts for a build",
		`List artifacts produced by a build.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking artifacts my-project main
  jenking artifacts my-project main #42 --output json`,
		func(ctx context.Context, jobPath string, buildNum int) ([]jmodel.Artifact, error) {
			return cs.client.GetArtifacts(ctx, jobPath, buildNum)
		}, toOutArtifact, printArtifactsTable,
	)
}

func newArtifactCmd() *cobra.Command {
	var savePath string

	cmd := &cobra.Command{
		Use:   "artifact <project> <branch> [#N|#last] <file>",
		Short: "Download a build artifact",
		Long: `Download a single build artifact. Raw bytes go to stdout by default;
use -O to save to a file instead (a trailing '/' or existing directory keeps
the artifact's own file name).

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)
  file     Artifact display path (as shown by 'jenking artifacts')

Examples:
  jenking artifact my-project main report.log
  jenking artifact my-project main #42 dist/app.jar -O app.jar
  jenking artifact my-project main #42 dist/app.jar -O downloads/`,
		Args: cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[len(args)-1]
			targetArgs := args[:len(args)-1]
			return withProjectBuild(targetArgs, func(ctx context.Context, jobPath string, buildNum int) error {
				arts, err := cs.client.GetArtifacts(ctx, jobPath, buildNum)
				if err != nil {
					return writeError(err)
				}
				art, ok := jmodel.FindArtifact(arts, file)
				if !ok {
					return writeError(artifactNotFoundErr(file, arts))
				}
				content, _, err := cs.client.GetArtifactContent(ctx, art.URL)
				if err != nil {
					return writeError(err)
				}
				if savePath == "" {
					_, err = io.WriteString(os.Stdout, content)
					return err
				}
				dest := savePath
				if fi, statErr := os.Stat(dest); strings.HasSuffix(dest, "/") || (statErr == nil && fi.IsDir()) {
					dest = filepath.Join(dest, filepath.Base(art.DisplayPath))
				}
				if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
					return writeError(err)
				}
				fmt.Fprintf(os.Stderr, "saved %s (%d bytes)\n", dest, len(content))
				return nil
			})
		},
	}

	cmd.Flags().StringVarP(&savePath, "save", "O", "", "Save to file or directory instead of stdout")
	return cmd
}

func artifactNotFoundErr(name string, arts []jmodel.Artifact) error {
	if len(arts) == 0 {
		return fmt.Errorf("artifact %q not found: build has no artifacts", name)
	}
	names := make([]string, len(arts))
	for i, a := range arts {
		names[i] = a.DisplayPath
	}
	return fmt.Errorf("artifact %q not found; available: %s", name, strings.Join(names, ", "))
}

func newBuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "build <project> [<branch>] [#N|#last]",
		Short: "Show details for a single build",
		Long: `Show detailed information for one build: status, timing, cause,
parameters, and any pending input steps.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking build my-project main
  jenking build my-project main #42 --output json`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				detail, err := cs.client.GetBuild(ctx, jobPath, buildNum)
				if err != nil {
					return writeError(err)
				}
				out := toOutBuildDetail(jobPath, *detail)
				return printFormatted(out, func() error { return printBuildDetailTable(os.Stdout, out) })
			})
		},
	}
}

func newNodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nodes",
		Short: "List Jenkins nodes (agents) and their status",
		Long: `List all Jenkins nodes with executor usage, disk, memory, and ping.

Examples:
  jenking nodes
  jenking nodes --output json | jq '.[] | select(.offline)'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			nodes, err := cs.client.ListNodes(ctx)
			if err != nil {
				return writeError(err)
			}
			out := make([]outNode, len(nodes))
			for i, n := range nodes {
				out[i] = toOutNode(n)
			}
			return emitList(out, printNodesTable)
		},
	}
}

func newProjectBuildListCmd[In, Out any](
	use, short, long string,
	fetch func(context.Context, string, int) ([]In, error),
	convert func(In) Out,
	printTable func(io.Writer, []Out) error,
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				items, err := fetch(ctx, jobPath, buildNum)
				if err != nil {
					return writeError(err)
				}
				out := make([]Out, len(items))
				for i, item := range items {
					out[i] = convert(item)
				}
				return printFormatted(out, func() error { return printTable(os.Stdout, out) })
			})
		},
	}
}

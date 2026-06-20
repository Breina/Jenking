package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
	"github.com/Breina/Jenking/internal/tui/command"
)

func newJobsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "jobs [<folder>]",
		Short: "List Jenkins jobs",
		Long: `List jobs in a folder or at the root.

Arguments:
  folder  Folder path to list (default: root)

Examples:
  jenking jobs
  jenking jobs my-team
  jenking jobs my-team --output json | jq '.[].full_path'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := ""
			if len(args) > 0 {
				folder = args[0]
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			jobs, err := cs.client.ListJobs(ctx, folder)
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
}

func newBuildsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "builds <project> [<branch>]",
		Short: "List builds for a project or branch",
		Long: `List recent builds for a project or branch, newest first.

Arguments:
  project  Project name or suffix (resolved from cache)
  branch   Branch name; if omitted, shows all branches for a multibranch project

Examples:
  jenking builds my-project
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
				// Specific branch: show branch builds.
				jobPath := nc.JobPath()
				builds, err := cs.client.ListBuilds(ctx, jobPath)
				if err != nil {
					return writeError(enrichBranchNotFound(ctx, nc, err))
				}
				out := make([]outBuild, len(builds))
				for i, b := range builds {
					out[i] = toOutBuild(b)
				}
				switch outputFlag {
				case "json":
					return printJSON(os.Stdout, out)
				case "yaml":
					return printYAML(os.Stdout, out)
				default:
					return printBuildsTable(os.Stdout, out)
				}
			}

			// Multibranch project: show all branches.
			projectPath := nc.JobPath()
			pbuilds, err := cs.client.ListProjectBuilds(ctx, projectPath)
			if err != nil {
				return writeError(err)
			}
			out := make([]outProjectBuild, len(pbuilds))
			for i, b := range pbuilds {
				out[i] = toOutProjectBuild(b)
			}
			switch outputFlag {
			case "json":
				return printJSON(os.Stdout, out)
			case "yaml":
				return printYAML(os.Stdout, out)
			default:
				return printProjectBuildsTable(os.Stdout, out)
			}
		},
	}
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
	return &cobra.Command{
		Use:   "running",
		Short: "List currently running builds",
		Long: `List all builds currently executing across all Jenkins nodes.

Examples:
  jenking running
  jenking running --output json | jq '.[].job_path'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			builds, err := cs.client.ListRunningBuilds(ctx)
			if err != nil {
				return writeError(err)
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
}

func newQueueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "List builds waiting in the queue",
		Long: `List all builds currently waiting in the Jenkins build queue.

Examples:
  jenking queue
  jenking queue --output json | jq '.[].job_path'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			items, err := cs.client.ListQueue(ctx)
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
	return &cobra.Command{
		Use:   "artifact <project> <branch> [#N|#last] <file>",
		Short: "Download a build artifact to stdout",
		Long: `Download a single build artifact and write its raw bytes to stdout.
Works for any file type (text or binary); redirect to a file to save it.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)
  file     Artifact display path (as shown by 'jenking artifacts')

Examples:
  jenking artifact my-project main report.log
  jenking artifact my-project main report.log > report.log
  jenking artifact my-project main #42 dist/app.jar > app.jar`,
		Args: cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[len(args)-1]
			targetArgs := args[:len(args)-1]
			return withProjectBuild(targetArgs, func(ctx context.Context, jobPath string, buildNum int) error {
				arts, err := cs.client.GetArtifacts(ctx, jobPath, buildNum)
				if err != nil {
					return writeError(err)
				}
				art, ok := view.FindArtifact(arts, file)
				if !ok {
					return writeError(artifactNotFoundErr(file, arts))
				}
				content, _, err := cs.client.GetArtifactContent(ctx, art.URL)
				if err != nil {
					return writeError(err)
				}
				_, err = io.WriteString(os.Stdout, content)
				return err
			})
		},
	}
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

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/action"
	"github.com/Breina/Jenking/internal/navmsg"
	"github.com/Breina/Jenking/internal/tui/command"
)

func newLogsCmd() *cobra.Command {
	return newBuildTextCmd(
		"logs <project> <branch> [#N|#last]",
		"Print full console log for a build",
		`Print the full console log for a build to stdout.

The --output flag is ignored; logs are always written as plain text.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking logs my-project main
  jenking logs my-project main #42
  jenking logs my-project main #last | grep ERROR`,
		action.KindLogs,
	)
}

func newDescribeCmd() *cobra.Command {
	return newBuildTextCmd(
		"describe <project> <branch> [#N|#last]",
		"Print Jenkinsfile for a build",
		`Print the Jenkinsfile (replay script) for a build to stdout.

The --output flag is ignored; Jenkinsfile content is always written as plain text.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking describe my-project main
  jenking describe my-project main #42`,
		action.KindDescribe,
	)
}

func newBuildTextCmd(use, short, long string, kind action.Kind) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := command.ParseTarget(args)
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()
			return action.Run(ctx, cs.client, cs.store, action.Request{Kind: kind, Target: target}, os.Stdout)
		},
	}
}

func newTestsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tests <project> <branch> [#N|#last]",
		Short: "Show JUnit test report for a build",
		Long: `Show the JUnit test report for a build.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking tests my-project main
  jenking tests my-project main #42 --output json
  jenking tests my-project main --output json | jq '.failed'`,
		Args: cobra.RangeArgs(1, 3),
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

			jobPath := nc.JobPath()
			buildNum, err := resolveBuildNum(ctx, cs.client, jobPath, nc.Build)
			if err != nil {
				return writeError(err)
			}

			report, err := cs.client.GetTestReport(ctx, jobPath, buildNum)
			if err != nil {
				return writeError(err)
			}
			if report == nil {
				return fmt.Errorf("no test report for %s #%d", jobPath, buildNum)
			}

			out := toOutTestReport(*report)
			switch outputFlag {
			case "json":
				return printJSON(os.Stdout, out)
			case "yaml":
				return printYAML(os.Stdout, out)
			default:
				return printTestReportTable(os.Stdout, out)
			}
		},
	}
}

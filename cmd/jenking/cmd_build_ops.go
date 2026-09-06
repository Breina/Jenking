package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/navmsg"
	"github.com/Breina/Jenking/internal/tui/command"
)

func newLogsCmd() *cobra.Command {
	var tail int
	var stage string

	cmd := &cobra.Command{
		Use:   "logs <project> <branch> [#N|#last]",
		Short: "Print console log for a build",
		Long: `Print the console log for a build to stdout.

The --output flag is ignored; logs are always written as plain text.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Flags:
  --tail N        Print only the last N lines
  --stage <name>  Print only the log of the named stage (case-insensitive)

Examples:
  jenking logs my-project main
  jenking logs my-project main #42 --tail 200
  jenking logs my-project main #last --stage Deploy
  jenking logs my-project main #last | grep ERROR`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				text, _, err := ucDeps().FetchLog(ctx, jobPath, buildNum, stage)
				if err != nil {
					return err
				}
				if tail > 0 {
					text = lastLines(text, tail)
				}
				_, err = io.WriteString(os.Stdout, text)
				if err == nil && !strings.HasSuffix(text, "\n") {
					fmt.Println()
				}
				return err
			})
		},
	}

	cmd.Flags().IntVar(&tail, "tail", 0, "Print only the last N lines")
	cmd.Flags().StringVar(&stage, "stage", "", "Print only the log of the named stage")
	return cmd
}

// lastLines returns the final n lines of text.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

func newDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <project> <branch> [#N|#last]",
		Short: "Print Jenkinsfile for a build",
		Long: `Print the Jenkinsfile (replay script) for a build to stdout.

The --output flag is ignored; Jenkinsfile content is always written as plain text.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking describe my-project main
  jenking describe my-project main #42`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				script, err := ucDeps().Describe(ctx, jobPath, buildNum)
				if err != nil {
					return err
				}
				_, err = io.WriteString(os.Stdout, script)
				return err
			})
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
			buildNum, err := resolveBuildNum(ctx, jobPath, nc.Build)
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

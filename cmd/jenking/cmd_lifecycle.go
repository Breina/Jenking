package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/navmsg"
)

// resolveJobPath resolves CLI args to a job-level navigation context,
// delegating to the usecase layer.
func resolveJobPath(args []string) (navmsg.NavigationContext, error) {
	return ucDeps().ResolveJobPath(args)
}

func newEnableCmd() *cobra.Command {
	return newJobEnabledCmd("enable", true)
}

func newDisableCmd() *cobra.Command {
	return newJobEnabledCmd("disable", false)
}

func newJobEnabledCmd(verb string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <project> [<branch>]",
		Short: strings.ToUpper(verb[:1]) + verb[1:] + " a job (buildable flag)",
		Long: fmt.Sprintf(`%s a Jenkins job so it can%s be built.

Arguments:
  project  Project name or suffix
  branch   Branch name (for a single branch of a multibranch project)

Examples:
  jenking %s my-project`, verb, map[bool]string{true: "", false: " no longer"}[enabled], verb),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nc, err := resolveJobPath(args)
			if err != nil {
				return writeError(err)
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()
			if err := cs.client.SetJobEnabled(ctx, nc.JobPath(), enabled); err != nil {
				return writeError(err)
			}
			return printFormatted(
				map[string]any{"job_path": nc.JobPath(), "action": verb + "d"},
				func() error {
					fmt.Printf("%sd %s\n", verb, nc.JobPath())
					return nil
				})
		},
	}
}

func newRescanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rescan <project>",
		Short: "Trigger branch indexing for a multibranch project",
		Long: `Trigger "Scan Repository Now" on a multibranch project, so Jenkins
discovers new branches and drops deleted ones.

Examples:
  jenking rescan my-project`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nc, err := resolveJobPath(args)
			if err != nil {
				return writeError(err)
			}
			jobPath := nc.JobPath()
			ctx, cancel := ctxWithTimeout()
			defer cancel()
			if err := ucDeps().Rescan(ctx, nc.FolderPath, jobPath); err != nil {
				return writeError(err)
			}
			return printFormatted(
				map[string]any{"job_path": jobPath, "action": "rescan"},
				func() error {
					fmt.Printf("rescan triggered for %s\n", jobPath)
					return nil
				})
		},
	}
}

func newScanLogCmd() *cobra.Command {
	var tail int
	cmd := &cobra.Command{
		Use:   "scan-log <container>",
		Short: "Print the repository scan log of a multibranch project or folder",
		Long: `Print a container's scan log — branch indexing for a multibranch project,
or the computation log for an organization folder.

Jenkins keeps exactly one scan log per container, always the latest, so there
is no build number to select. The --output flag is ignored; logs are always
written as plain text.

Examples:
  jenking scan-log my-project
  jenking scan-log my-project --tail 50
  jenking scan-log my-org | grep -i error`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := ctxWithTimeout()
			defer cancel()
			text, _, err := ucDeps().FetchScanLog(ctx, containerPath(args))
			if err != nil {
				return writeError(err)
			}
			if tail > 0 {
				text = lastLines(text, tail)
			}
			if _, err := io.WriteString(os.Stdout, text); err != nil {
				return err
			}
			if !strings.HasSuffix(text, "\n") {
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&tail, "tail", 0, "Print only the last N lines")
	return cmd
}

// containerPath resolves args to a container's job path. Suffix matching is
// tried first, but the project index only holds jobs — organization folders and
// plain folders are never in it — so an unresolvable argument is taken as a
// literal path rather than rejected. Scannable containers are exactly the
// things the resolver cannot see.
func containerPath(args []string) string {
	if nc, err := resolveJobPath(args); err == nil {
		return nc.JobPath()
	}
	return strings.Trim(args[0], "/")
}

func newNodeCmd() *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:   "node <offline|online> <name>",
		Short: "Take a Jenkins node offline or bring it back online",
		Long: `Change a node's temporarily-offline state. Idempotent: asking for a
state the node is already in is a no-op. The controller node is named
"(built-in)".

Flags:
  --reason <msg>  Offline reason shown in Jenkins (offline only)

Examples:
  jenking node offline my-agent --reason "disk maintenance"
  jenking node online my-agent`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			verb, name := args[0], args[1]
			var wantOffline bool
			switch verb {
			case "offline":
				wantOffline = true
			case "online":
				wantOffline = false
			default:
				return fmt.Errorf("unknown verb %q: expected offline or online", verb)
			}

			ctx, cancel := ctxWithTimeout()
			defer cancel()

			if err := ucDeps().SetNodeOffline(ctx, name, wantOffline, reason); err != nil {
				return writeError(err)
			}
			return printFormatted(
				map[string]any{"node": name, "offline": wantOffline},
				func() error {
					fmt.Printf("node %s is %s\n", name, verb)
					return nil
				})
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Offline reason shown in Jenkins")
	return cmd
}

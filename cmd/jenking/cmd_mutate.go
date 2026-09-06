package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
	"github.com/Breina/Jenking/internal/tui/command"
)

func newDequeueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dequeue <id>",
		Short: "Cancel a build waiting in the queue",
		Long: `Remove a build from the Jenkins queue by its queue id.

Get queue ids with 'jenking queue'.

Examples:
  jenking dequeue 1234`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid queue id %q: %w", args[0], err)
			}
			ctx, cancel := ctxWithTimeout()
			defer cancel()

			if err := cs.client.CancelQueueItem(ctx, id); err != nil {
				return err
			}
			fmt.Printf("dequeued %d\n", id)
			return nil
		},
	}
}

func newTriggerCmd() *cobra.Command {
	var params []string
	var wait bool

	cmd := &cobra.Command{
		Use:   "trigger <project> [<branch>]",
		Short: "Trigger a new build",
		Long: `Trigger a new build for a project or branch.

Prints the queue id of the new build. With --wait, blocks until the build
leaves the queue and finishes, then reports the final status; the command
fails when the build does not succeed. --wait is bounded by --timeout, so
raise it for long builds (e.g. --timeout 30m).

Arguments:
  project  Project name or suffix
  branch   Branch name (required for multibranch projects)

Flags:
  --param K=V  Build parameter (repeatable)
  --wait       Wait for the build to complete

Examples:
  jenking trigger my-project main
  jenking trigger my-project main --param ENV=prod --param VERSION=1.2
  jenking trigger my-project main --wait --timeout 30m --output json`,
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

			paramMap := make(map[string]string, len(params))
			for _, p := range params {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("invalid param %q: expected K=V", p)
				}
				paramMap[k] = v
			}

			jobPath := nc.JobPath()
			res, err := ucDeps().Trigger(ctx, jobPath, usecase.TriggerOptions{
				Params:   paramMap,
				Wait:     wait,
				Progress: func(m string) { fmt.Fprintln(os.Stderr, m) },
			})
			if err != nil {
				return writeError(err)
			}
			out := outTriggerResult{
				JobPath:     res.JobPath,
				QueueID:     res.QueueID,
				BuildNumber: res.BuildNumber,
				Status:      string(res.Status),
			}
			if emitErr := emitTriggerResult(out); emitErr != nil {
				return emitErr
			}
			if wait && res.Status != jmodel.BuildStatusSuccess {
				return fmt.Errorf("build %s #%d finished with status %s", jobPath, res.BuildNumber, res.Status)
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&params, "param", nil, "Build parameter in K=V form (repeatable)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for the build to complete and report its final status")
	return cmd
}

func newCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <project> <branch> #N",
		Short: "Cancel a running build",
		Long: `Cancel a running build.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number to cancel

Examples:
  jenking cancel my-project main #42`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := command.ParseTarget(args)
			if err != nil {
				return err
			}
			if !target.Build.Set || target.Build.Number <= 0 {
				return fmt.Errorf("build number required (e.g. #42)")
			}

			ctx, cancel := ctxWithTimeout()
			defer cancel()

			nc, err := navmsg.ResolveTarget(target, cs.store, navmsg.NavigationContext{})
			if err != nil {
				return err
			}
			if nc.ProjectName == "" {
				return fmt.Errorf("project required")
			}

			if err := cs.client.CancelBuild(ctx, nc.JobPath(), target.Build.Number); err != nil {
				return err
			}
			fmt.Printf("cancelled %s #%d\n", nc.JobPath(), target.Build.Number)
			return nil
		},
	}
}

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

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

	cmd := &cobra.Command{
		Use:   "trigger <project> [<branch>]",
		Short: "Trigger a new build",
		Long: `Trigger a new build for a project or branch.

Arguments:
  project  Project name or suffix
  branch   Branch name (required for multibranch projects)

Flags:
  --param K=V  Build parameter (repeatable)

Examples:
  jenking trigger my-project main
  jenking trigger my-project main --param ENV=prod --param VERSION=1.2`,
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
				return err
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

			if err := cs.client.TriggerBuild(ctx, nc.JobPath(), paramMap); err != nil {
				return err
			}
			fmt.Printf("triggered %s\n", nc.JobPath())
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&params, "param", nil, "Build parameter in K=V form (repeatable)")
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

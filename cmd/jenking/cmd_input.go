package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newInputsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inputs <project> <branch> [#N|#last]",
		Short: "List pending pipeline input steps for a build",
		Long: `List the pending input steps (approval gates) of a build.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Examples:
  jenking inputs my-project main
  jenking inputs my-project main #42 --output json`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				detail, err := cs.client.GetBuild(ctx, jobPath, buildNum)
				if err != nil {
					return writeError(err)
				}
				out := make([]outInput, len(detail.PendingInputs))
				for i, in := range detail.PendingInputs {
					out[i] = toOutInput(in)
				}
				return emitList(out, printInputsTable)
			})
		},
	}
}

func newApproveCmd() *cobra.Command {
	var inputID string
	var params []string

	cmd := &cobra.Command{
		Use:   "approve <project> <branch> [#N|#last]",
		Short: "Approve a pending pipeline input step",
		Long: `Approve (proceed) a pending input step of a build.

When the build has exactly one pending input, --id may be omitted.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Flags:
  --id <inputID>  Input step id (see 'jenking inputs')
  --param K=V     Input parameter (repeatable)

Examples:
  jenking approve my-project main
  jenking approve my-project main #42 --id Deploy --param TARGET=prod`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			paramMap := make(map[string]string, len(params))
			for _, p := range params {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("invalid param %q: expected K=V", p)
				}
				paramMap[k] = v
			}
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				id, err := ucDeps().ResolveInputID(ctx, jobPath, buildNum, inputID)
				if err != nil {
					return writeError(err)
				}
				if err := cs.client.ProceedInput(ctx, jobPath, buildNum, id, paramMap); err != nil {
					return writeError(err)
				}
				return emitInputResult(outInputResult{JobPath: jobPath, BuildNumber: buildNum, InputID: id, Action: "approved"})
			})
		},
	}

	cmd.Flags().StringVar(&inputID, "id", "", "Input step id (required when several inputs are pending)")
	cmd.Flags().StringArrayVar(&params, "param", nil, "Input parameter in K=V form (repeatable)")
	return cmd
}

func newRejectCmd() *cobra.Command {
	var inputID string

	cmd := &cobra.Command{
		Use:   "reject <project> <branch> [#N|#last]",
		Short: "Reject a pending pipeline input step",
		Long: `Reject (abort) a pending input step of a build.

When the build has exactly one pending input, --id may be omitted.

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number (default: latest)

Flags:
  --id <inputID>  Input step id (see 'jenking inputs')

Examples:
  jenking reject my-project main
  jenking reject my-project main #42 --id Deploy`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				id, err := ucDeps().ResolveInputID(ctx, jobPath, buildNum, inputID)
				if err != nil {
					return writeError(err)
				}
				if err := cs.client.AbortInput(ctx, jobPath, buildNum, id); err != nil {
					return writeError(err)
				}
				return emitInputResult(outInputResult{JobPath: jobPath, BuildNumber: buildNum, InputID: id, Action: "rejected"})
			})
		},
	}

	cmd.Flags().StringVar(&inputID, "id", "", "Input step id (required when several inputs are pending)")
	return cmd
}

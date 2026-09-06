package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app/dto"
)

func newChangesCmd() *cobra.Command {
	var find string
	var maxBuilds int

	cmd := &cobra.Command{
		Use:   "changes <project> [<branch>] [#N|#last]",
		Short: "Show the commits in a build, or find which build contains a commit",
		Long: `Show the SCM commits recorded for a build, or with --find locate which of a
job's recent builds first contain a commit.

Arguments:
  project  Project name or suffix
  branch   Branch name (required for multibranch projects)
  #N       Build number (default: latest; ignored with --find)

Flags:
  --find <commit>    Report which recent builds contain this commit (prefix match)
  --max-builds <n>   How many recent builds --find scans (default 25, max 50)

Examples:
  jenking changes my-project main
  jenking changes my-project main #42 --output json
  jenking changes my-project main --find a1b2c3d`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if find != "" {
				nc, err := resolveJobPath(args)
				if err != nil {
					return writeError(err)
				}
				ctx, cancel := ctxWithTimeout()
				defer cancel()
				hits, err := ucDeps().FindCommit(ctx, nc.JobPath(), find, maxBuilds)
				if err != nil {
					return writeError(err)
				}
				out := make([]dto.CommitHit, len(hits))
				for i, h := range hits {
					out[i] = dto.ToCommitHit(h)
				}
				return emitList(out, printCommitHitsTable)
			}
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				changes, err := ucDeps().GetChanges(ctx, jobPath, buildNum)
				if err != nil {
					return writeError(err)
				}
				out := make([]dto.Change, len(changes))
				for i, c := range changes {
					out[i] = dto.ToChange(c)
				}
				return emitList(out, printChangesTable)
			})
		},
	}

	cmd.Flags().StringVar(&find, "find", "", "Report which recent builds contain this commit (prefix match)")
	cmd.Flags().IntVar(&maxBuilds, "max-builds", 25, "How many recent builds --find scans (max 50)")
	return cmd
}

func shortSHA(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// firstLine returns the first line of a commit message (subject).
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			return s[:i]
		}
	}
	return s
}

func printChangesTable(w io.Writer, changes []dto.Change) error {
	if len(changes) == 0 {
		_, err := fmt.Fprintln(w, "no changes")
		return err
	}
	tw := newTab(w)
	fmt.Fprintln(tw, "COMMIT\tAUTHOR\tWHEN\tMESSAGE")
	for _, c := range changes {
		when := "-"
		if c.Timestamp > 0 {
			when = time.Unix(c.Timestamp, 0).Format("2006-01-02")
		}
		author := c.Author
		if author == "" {
			author = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", shortSHA(c.CommitID), author, when, firstLine(c.Message))
	}
	return tw.Flush()
}

func printCommitHitsTable(w io.Writer, hits []dto.CommitHit) error {
	if len(hits) == 0 {
		_, err := fmt.Fprintln(w, "commit not found in the searched builds")
		return err
	}
	tw := newTab(w)
	fmt.Fprintln(tw, "BUILD\tCOMMIT")
	for _, h := range hits {
		fmt.Fprintf(tw, "#%d\t%s\n", h.BuildNumber, shortSHA(h.CommitID))
	}
	return tw.Flush()
}

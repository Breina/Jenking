package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newResolveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resolve [<scm-url>]",
		Short: "Find the Jenkins job(s) for a git remote / SCM URL",
		Long: `Resolve a git remote or SCM URL to the matching Jenkins job path(s), using
Jenking's warm SCM-URL index (no live scan). With no argument, the origin remote
of the current git repository is used.

The index is populated as builds run and persists across sessions, so a repo
whose pipeline has never been observed may not resolve yet.

Examples:
  cd my-repo && jenking resolve
  jenking resolve "$(git remote get-url origin)"
  jenking resolve git@github.com:org/repo.git --output json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := ""
			if len(args) == 1 {
				url = args[0]
			} else if url = gitRemoteURL(""); url == "" {
				return fmt.Errorf("no SCM URL given and the current directory is not a git repo with an 'origin' remote; pass a URL explicitly")
			}

			matches := ucDeps().ResolveJob(url)
			out := make([]outJobMatch, len(matches))
			for i, m := range matches {
				out[i] = toOutJobMatch(m)
			}
			return emitList(out, printJobMatchesTable)
		},
	}
}

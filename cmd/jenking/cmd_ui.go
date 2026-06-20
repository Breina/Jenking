package main

import "github.com/spf13/cobra"

// newUICmd returns the `ui` subcommand group that launches the TUI pre-navigated
// to a specific view.
func newUICmd() *cobra.Command {
	ui := &cobra.Command{
		Use:   "ui <verb> [args...]",
		Short: "Launch TUI pre-navigated to a view",
		Long: `Launch the TUI pre-navigated to a specific view.

Available verbs: logs, stages, builds, jobs

Examples:
  jenking ui logs my-project main #42
  jenking ui stages my-project main
  jenking ui builds my-project
  jenking ui jobs my-folder`,
	}

	ui.AddCommand(
		newUISubCmd("logs", "log", "l", "Open TUI at the console log view"),
		newUISubCmd("stages", "stage", "s", "Open TUI at the pipeline stages view"),
		newUISubCmd("builds", "build", "b", "Open TUI at the builds list"),
		newUISubCmd("jobs", "job", "j", "Open TUI at the job list"),
	)
	return ui
}

func newUISubCmd(primary, alias1, alias2, short string) *cobra.Command {
	return &cobra.Command{
		Use:     primary + " [args...]",
		Aliases: []string{alias1, alias2},
		Short:   short,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUIAt(primary, args)
		},
	}
}

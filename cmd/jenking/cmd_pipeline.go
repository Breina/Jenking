package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint [<file>]",
		Short: "Validate a Jenkinsfile against the server",
		Long: `Validate a declarative Jenkinsfile using the Jenkins server's
pipeline validator. Reads from the given file, or stdin when the argument
is omitted or '-'.

Exits non-zero when the Jenkinsfile is invalid.

Examples:
  jenking lint Jenkinsfile
  cat Jenkinsfile | jenking lint
  jenking lint Jenkinsfile --output json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var content []byte
			var err error
			if len(args) == 0 || args[0] == "-" {
				content, err = io.ReadAll(os.Stdin)
			} else {
				content, err = os.ReadFile(args[0])
			}
			if err != nil {
				return fmt.Errorf("reading Jenkinsfile: %w", err)
			}

			ctx, cancel := ctxWithTimeout()
			defer cancel()

			result, err := cs.client.ValidateJenkinsfile(ctx, string(content))
			if err != nil {
				return writeError(err)
			}
			out := toOutValidation(result)
			if emitErr := printFormatted(out, func() error { return printValidationTable(os.Stdout, out) }); emitErr != nil {
				return emitErr
			}
			if !result.OK {
				return fmt.Errorf("invalid Jenkinsfile")
			}
			return nil
		},
	}
}

func newReplayCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "replay <project> <branch> [#N|#last]",
		Short: "Replay a build, optionally with a modified Jenkinsfile",
		Long: `Replay a pipeline build. Without --file the original pipeline
script is fetched and replayed unchanged (a rerun); with --file the given
script is used instead. Requires the build to be replayable (pipeline jobs).

Arguments:
  project  Project name or suffix
  branch   Branch name
  #N       Build number to replay (default: latest)

Flags:
  --file <path>  Jenkinsfile to replay instead of the original ('-' = stdin)

Examples:
  jenking replay my-project main #42
  jenking replay my-project main #42 --file Jenkinsfile.fixed`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProjectBuild(args, func(ctx context.Context, jobPath string, buildNum int) error {
				var script string
				switch file {
				case "":
					orig, err := cs.client.GetBuildScript(ctx, jobPath, buildNum)
					if err != nil {
						return writeError(fmt.Errorf("fetching original script: %w", err))
					}
					script = orig
				case "-":
					data, err := io.ReadAll(os.Stdin)
					if err != nil {
						return fmt.Errorf("reading stdin: %w", err)
					}
					script = string(data)
				default:
					data, err := os.ReadFile(file)
					if err != nil {
						return fmt.Errorf("reading %s: %w", file, err)
					}
					script = string(data)
				}
				if err := cs.client.ReplayBuild(ctx, jobPath, buildNum, script); err != nil {
					return writeError(err)
				}
				return printFormatted(
					map[string]any{"job_path": jobPath, "replayed_build": buildNum},
					func() error {
						fmt.Printf("replayed %s #%d\n", jobPath, buildNum)
						return nil
					})
			})
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Jenkinsfile to replay instead of the original ('-' = stdin)")
	return cmd
}

// outValidation is the result of `jenking lint`.
type outValidation struct {
	Valid  bool                 `json:"valid" yaml:"valid"`
	Issues []outValidationIssue `json:"issues,omitempty" yaml:"issues,omitempty"`
}

type outValidationIssue struct {
	Line    int    `json:"line,omitempty" yaml:"line,omitempty"`
	Col     int    `json:"col,omitempty" yaml:"col,omitempty"`
	Message string `json:"message" yaml:"message"`
}

func toOutValidation(r jmodel.ValidationResult) outValidation {
	issues := make([]outValidationIssue, len(r.Issues))
	for i, is := range r.Issues {
		issues[i] = outValidationIssue{Line: is.Line, Col: is.Col, Message: is.Message}
	}
	return outValidation{Valid: r.OK, Issues: issues}
}

func printValidationTable(w io.Writer, v outValidation) error {
	if v.Valid {
		_, err := fmt.Fprintln(w, "Jenkinsfile is valid")
		return err
	}
	fmt.Fprintln(w, "Jenkinsfile is INVALID:")
	for _, is := range v.Issues {
		if is.Line > 0 {
			fmt.Fprintf(w, "  line %d, col %d: %s\n", is.Line, is.Col, is.Message)
		} else {
			fmt.Fprintf(w, "  %s\n", is.Message)
		}
	}
	return nil
}

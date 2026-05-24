package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Breina/Jenking/internal/action"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
)

// stripRawFlag removes the first occurrence of "--raw" from args and returns
// the cleaned slice along with a boolean indicating whether the flag was
// present. Order of remaining args is preserved.
func stripRawFlag(args []string) ([]string, bool) {
	for i, a := range args {
		if a == "--raw" {
			out := make([]string, 0, len(args)-1)
			out = append(out, args[:i]...)
			out = append(out, args[i+1:]...)
			return out, true
		}
	}
	return args, false
}

// runHeadless executes a headless action and writes its output to stdout.
// All errors print to stderr and terminate the process with exit code 1.
func runHeadless(client jmodel.JenkinsClient, store *cache.Store, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "jenking: --raw requires a verb (logs, describe, tests)")
		os.Exit(1)
	}
	kind, ok := action.ParseKind(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "jenking: --raw does not support %q (expected: logs, describe, tests)\n", args[0])
		os.Exit(1)
	}
	target, err := command.ParseTarget(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "jenking: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := action.Run(ctx, client, store, action.Request{Kind: kind, Target: target}, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "jenking: %v\n", err)
		os.Exit(1)
	}
}

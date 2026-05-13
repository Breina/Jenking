package command

import (
	"fmt"
	"strconv"
	"strings"
)

// BuildRef is the build component of a Target.
// Set only when the user provided a #<n> or #last marker.
type BuildRef struct {
	Number int
	IsLast bool
	Set    bool
}

// Target is the parsed form of a command's positional + marker arguments.
// It is purely syntactic: it does not know about the cache, the current view,
// or NavigationContext. Empty fields mean "inherit from current scope".
type Target struct {
	ProjectSuffix string
	Branch        string
	Build         BuildRef
	Stage         string
}

// IsEmpty reports whether the target carries no information at all (use
// current scope verbatim).
func (t Target) IsEmpty() bool {
	return t.ProjectSuffix == "" && t.Branch == "" && !t.Build.Set && t.Stage == ""
}

// ParseTarget parses positional + marker arguments according to the grammar:
//
//	<projectSuffix> <branch> [#<n>|#last] [:<stageRest>]
//
// Rules:
//   - Tokens starting with '#' are build markers.
//   - The first token starting with ':' begins the stage; the stage value is
//     that token (without ':') joined with all subsequent tokens by single
//     spaces. ':' must therefore be the last marker.
//   - All other tokens are positional, in order: project, then branch.
//   - Bare integers without '#' are NOT accepted as a build (avoids confusion
//     with branch names that happen to be numeric).
//
// Returns an error for malformed markers; returns an empty Target with no
// error when args is empty.
func ParseTarget(args []string) (Target, error) {
	var t Target
	var positional []string

	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "" {
			continue
		}
		switch tok[0] {
		case '#':
			ref, err := parseBuildMarker(tok)
			if err != nil {
				return Target{}, err
			}
			if t.Build.Set {
				return Target{}, fmt.Errorf("multiple build markers")
			}
			t.Build = ref
		case ':':
			rest := tok[1:]
			tail := args[i+1:]
			parts := make([]string, 0, 1+len(tail))
			if rest != "" {
				parts = append(parts, rest)
			}
			parts = append(parts, tail...)
			stage := strings.Join(parts, " ")
			if stage == "" {
				return Target{}, fmt.Errorf("empty stage marker")
			}
			t.Stage = stage
			i = len(args) // consume rest
		default:
			positional = append(positional, tok)
		}
	}

	switch len(positional) {
	case 0:
		// nothing
	case 1:
		t.ProjectSuffix = positional[0]
	case 2:
		t.ProjectSuffix = positional[0]
		t.Branch = positional[1]
	default:
		return Target{}, fmt.Errorf("too many positional arguments: expected at most 2 (project, branch), got %d", len(positional))
	}

	return t, nil
}

func parseBuildMarker(tok string) (BuildRef, error) {
	body := tok[1:]
	if body == "" {
		return BuildRef{}, fmt.Errorf("empty build marker")
	}
	if body == "last" {
		return BuildRef{IsLast: true, Set: true}, nil
	}
	n, err := strconv.Atoi(body)
	if err != nil {
		return BuildRef{}, fmt.Errorf("invalid build %q: must be #<n> or #last", tok)
	}
	if n <= 0 {
		return BuildRef{}, fmt.Errorf("invalid build %q: must be positive", tok)
	}
	return BuildRef{Number: n, Set: true}, nil
}

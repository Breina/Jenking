// Package navmsg holds the navigation-context and cross-layer message types
// shared between the TUI views and the headless app code (monitor, action).
// It depends only on stdlib + domain + cache + tui/command (the parser).
// Importing this package does NOT pull in any view or component code, which
// is what lets monitor/action emit messages without violating the layering.
package navmsg

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
)

// ContextLevel describes where in the job hierarchy a view is anchored.
type ContextLevel int

const (
	CtxRoot    ContextLevel = iota // "*" — global scope
	CtxFolder                      // a folder job
	CtxProject                     // a multibranch project (or standalone job)
	CtxBranch                      // a branch/MR within a multibranch project
	CtxBuild                       // a specific build
	CtxStage                       // a specific stage
)

// NavBuildRef is a navigation cursor for a build: either a concrete number or
// the "#last" moving reference.
type NavBuildRef struct {
	Number int
	IsLast bool
}

// NavigationContext is the unified navigation state passed through view
// constructors.
type NavigationContext struct {
	Level        ContextLevel
	FolderPath   string
	ProjectName  string
	BranchName   string
	Build        NavBuildRef
	StageName    string
	Username     string
	GitUsernames []string
	FriendlyName string
}

// NavigationContextProvider is implemented by views that carry a NavigationContext.
type NavigationContextProvider interface {
	NC() NavigationContext
}

// AtBuild returns a CtxBuild NC for the given build number.
func (nc NavigationContext) AtBuild(number int) NavigationContext {
	nc.Level = CtxBuild
	nc.Build = NavBuildRef{Number: number}
	nc.StageName = ""
	return nc
}

// AtStage returns a CtxStage NC for the given stage name.
func (nc NavigationContext) AtStage(stageName string) NavigationContext {
	nc.Level = CtxStage
	nc.StageName = stageName
	return nc
}

// AtBranch returns a CtxBranch NC for the given branch name.
func (nc NavigationContext) AtBranch(branchName string) NavigationContext {
	nc.Level = CtxBranch
	nc.BranchName = branchName
	nc.Build = NavBuildRef{}
	nc.StageName = ""
	return nc
}

// AtScope strips build- and stage-level detail, returning the highest scope
// implied by the populated fields.
func (nc NavigationContext) AtScope() NavigationContext {
	nc.Build = NavBuildRef{}
	nc.StageName = ""
	if nc.BranchName != "" {
		nc.Level = CtxBranch
		return nc
	}
	nc.BranchName = ""
	if nc.ProjectName != "" {
		nc.Level = CtxProject
		return nc
	}
	nc.ProjectName = ""
	if nc.FolderPath != "" {
		nc.Level = CtxFolder
		return nc
	}
	nc.FolderPath = ""
	nc.Level = CtxRoot
	return nc
}

// JobPath reconstructs the slash-joined API path from the context fields.
func (nc NavigationContext) JobPath() string {
	var parts []string
	if nc.FolderPath != "" {
		parts = append(parts, nc.FolderPath)
	}
	if nc.ProjectName != "" {
		parts = append(parts, nc.ProjectName)
	}
	if nc.BranchName != "" {
		parts = append(parts, nc.BranchName)
	}
	return strings.Join(parts, "/")
}

// RunningBuildsUpdatedMsg is broadcast by the RunningBuildsMonitor each poll.
type RunningBuildsUpdatedMsg struct {
	Builds      []jmodel.UserBuild
	Arrived     []string
	Departed    []string
	Count       int
	QueuedCount int
}

// ConnectionLostMsg is emitted by the RunningBuildsMonitor when a poll fails.
// It lets the app notice a dropped Jenkins connection immediately (on the next
// 1s poll) instead of only when the user navigates and triggers a fresh
// request. The app decides via isConnError whether the failure is a genuine
// connectivity problem.
type ConnectionLostMsg struct {
	Err error
}

// BuildCompletedMsg carries the final status of a build that just left the
// running set.
type BuildCompletedMsg struct {
	Key     string
	JobPath string
	Number  int
	Build   jmodel.Build
	Err     error
}

// ResolveError describes a failure to resolve a Target.ProjectSuffix.
type ResolveError struct {
	Suffix     string
	Candidates []string
}

func (e *ResolveError) Error() string {
	if len(e.Candidates) > 1 {
		return fmt.Sprintf("ambiguous project %q matches: %s", e.Suffix, strings.Join(e.Candidates, ", "))
	}
	return fmt.Sprintf("unknown project: %q", e.Suffix)
}

// ResolveTarget translates a parsed Target into a NavigationContext.
func ResolveTarget(t command.Target, store *cache.Store, current NavigationContext) (NavigationContext, error) {
	if t.IsEmpty() {
		return current, nil
	}

	nc := NavigationContext{
		Username:     current.Username,
		GitUsernames: current.GitUsernames,
		FriendlyName: current.FriendlyName,
	}

	if t.ProjectSuffix == "" {
		nc.FolderPath = current.FolderPath
		nc.ProjectName = current.ProjectName
		nc.BranchName = current.BranchName
		nc.Level = current.Level
		nc.Build = NavBuildRef{}
		nc.StageName = ""
		if !t.Build.Set && current.Build != (NavBuildRef{}) {
			nc.Build = current.Build
		}
		if t.Branch != "" && t.Branch != current.BranchName {
			nc.Build = NavBuildRef{}
		}
	} else {
		paths := cache.AllProjectPaths(store)
		matches := MatchProjectSuffix(paths, t.ProjectSuffix)
		switch len(matches) {
		case 0:
			return NavigationContext{}, &ResolveError{Suffix: t.ProjectSuffix}
		case 1:
			folder, project := SplitProjectPath(matches[0])
			nc.FolderPath = folder
			nc.ProjectName = project
			nc.Level = CtxProject
		default:
			decoded := make([]string, len(matches))
			for i, m := range matches {
				decoded[i] = DecodePath(m)
			}
			return NavigationContext{}, &ResolveError{Suffix: t.ProjectSuffix, Candidates: decoded}
		}
	}

	if t.Branch != "" {
		nc = nc.AtBranch(EncodeBranchName(t.Branch))
	}

	if t.Build.Set {
		if t.Build.IsLast {
			nc.Level = CtxBuild
			nc.Build = NavBuildRef{IsLast: true}
			nc.StageName = ""
		} else {
			nc = nc.AtBuild(t.Build.Number)
		}
	}

	if t.Stage != "" {
		nc = nc.AtStage(t.Stage)
	}

	return nc, nil
}

func MatchProjectSuffix(paths []string, suffix string) []string {
	// Decode the suffix too, so an encoded path (e.g. "Code/git%2Fwebidm" as
	// older output emitted) resolves the same as its decoded form.
	suffix = DecodePath(suffix)
	var out []string
	prefixed := "/" + suffix
	for _, p := range paths {
		decoded := DecodePath(p)
		if decoded == suffix || strings.HasSuffix(decoded, prefixed) {
			out = append(out, p)
		}
	}
	return out
}

func EncodeBranchName(s string) string {
	return strings.ReplaceAll(s, "/", "%2F")
}

// DecodeName URL-decodes a single name segment.
func DecodeName(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}

func DecodePath(p string) string {
	parts := strings.Split(p, "/")
	changed := false
	for i, s := range parts {
		d := DecodeName(s)
		if d != s {
			parts[i] = d
			changed = true
		}
	}
	if !changed {
		return p
	}
	return strings.Join(parts, "/")
}

func SplitProjectPath(path string) (folder, project string) {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "", path
	}
	return path[:idx], path[idx+1:]
}

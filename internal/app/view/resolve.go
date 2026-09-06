package view

import (
	"fmt"
	"strings"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/tui/command"
)

// ResolveError describes a failure to resolve a Target.ProjectSuffix against
// the cached job paths. Candidates is populated when the suffix matched
// multiple paths.
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

// ResolveTarget translates a parsed Target into a NavigationContext, using
// the cache to resolve a project suffix to a full path and inheriting
// user/identity fields from current.
//
// An empty Target returns current verbatim.
//
// When Target.ProjectSuffix is empty, current's Folder/Project/Branch are
// used as the base; the marker fields (Branch, Build, Stage) override the
// matching fields on top.
//
// When Target.ProjectSuffix is non-empty, the cache is consulted for
// non-folder job paths and the suffix is matched on full path-segment
// boundaries. Exactly one match is required.
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
		// Inherit folder/project/branch from current as the base.
		nc.FolderPath = current.FolderPath
		nc.ProjectName = current.ProjectName
		nc.BranchName = current.BranchName
		nc.Level = current.Level
		// Strip build/stage from current — they will be re-applied below if set.
		nc.Build = NavBuildRef{}
		nc.StageName = ""
		// If the current scope already had a build/stage and the user
		// supplied no build marker, preserve the current build (so e.g.
		// `:logs :Deploy` from a CtxBuild view stays on that build).
		if !t.Build.Set && current.Build != (NavBuildRef{}) {
			nc.Build = current.Build
		}
		// If branch is being overridden, clear Build (different scope).
		if t.Branch != "" && t.Branch != current.BranchName {
			nc.Build = NavBuildRef{}
		}
	} else {
		paths := cache.AllProjectPaths(store)
		matches := matchProjectSuffix(paths, t.ProjectSuffix)
		switch len(matches) {
		case 0:
			return NavigationContext{}, &ResolveError{Suffix: t.ProjectSuffix}
		case 1:
			folder, project := splitProjectPath(matches[0])
			nc.FolderPath = folder
			nc.ProjectName = project
			nc.Level = CtxProject
		default:
			decoded := make([]string, len(matches))
			for i, m := range matches {
				decoded[i] = decodePath(m)
			}
			return NavigationContext{}, &ResolveError{Suffix: t.ProjectSuffix, Candidates: decoded}
		}
	}

	if t.Branch != "" {
		nc = nc.AtBranch(encodeBranchName(t.Branch))
	}

	if t.Build.Set {
		if t.Build.IsLast {
			nc = nc.AtLastBuild(NavBuildRef{})
		} else {
			nc = nc.AtBuild(t.Build.Number)
		}
	}

	if t.Stage != "" {
		nc = nc.AtStage(t.Stage)
	}

	return nc, nil
}

// matchProjectSuffix returns the cached project paths whose decoded form
// matches the given suffix on full path-segment boundaries. A suffix matches
// when the decoded path equals the suffix exactly, or ends with "/<suffix>".
//
// Matching is always performed against the URL-decoded path so that projects
// whose names contain encoded slashes ("git%2Fcas%2Fwebidm" -> "git/cas/
// webidm") behave like any other multi-segment path. The encoded form is
// never compared directly — %2F is an implementation detail of the Jenkins
// API and should not be user-facing.
//
// The returned slice contains the original (encoded) paths so downstream
// code that builds API URLs continues to work.
func matchProjectSuffix(paths []string, suffix string) []string {
	var out []string
	prefixed := "/" + suffix
	for _, p := range paths {
		decoded := decodePath(p)
		if decoded == suffix || strings.HasSuffix(decoded, prefixed) {
			out = append(out, p)
		}
	}
	return out
}

// encodeBranchName encodes a user-typed branch name into the form Jenkins
// stores in job paths. Only "/" is escaped (to %2F) — Jenkins keeps spaces,
// parentheses and other characters verbatim in branch names. The result
// must always be one segment of NC.JobPath() so that JobPathToURL further
// double-encodes the %2F to %252F.
func encodeBranchName(s string) string {
	return strings.ReplaceAll(s, "/", "%2F")
}

// decodePath URL-decodes each "/"-separated segment of a job path. Encoded
// slashes (%2F) become real slashes, expanding the segment count.
func decodePath(p string) string {
	parts := strings.Split(p, "/")
	changed := false
	for i, s := range parts {
		d := decodeName(s)
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

// splitProjectPath splits a slash-separated job path into folder + project,
// where project is the last segment.
func splitProjectPath(path string) (folder, project string) {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "", path
	}
	return path[:idx], path[idx+1:]
}

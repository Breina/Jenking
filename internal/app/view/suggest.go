package view

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// TargetArgSuggest returns completion candidates for the argument portion of
// a navigation slash command (everything after the verb and its trailing
// space).
//
// It infers which field the user is currently typing — project, branch, or
// build (#) — from the shape of argPrefix, then queries the cache for
// matching candidates. Suggestions are returned as full replacements of
// argPrefix; the registry concatenates "<verb> " in front.
//
// Resolution per field:
//   - Project (1st positional): walks Jobs cache via cache.AllProjectPaths.
//   - Branch (2nd positional): reads Jobs[<resolvedProject>] (branches are
//     children of the multibranch project), with ProjectBuilds as fallback.
//   - Build (#, also surfaced after the branch arg): reads Builds[
//     <branchPath>] or ProjectBuilds[<project>], prefixed with "#last".
//
// Stage autocomplete is intentionally not provided — stage names are
// expensive to obtain reliably from the cache and add little value.
//
// All matching and output is in URL-decoded space — %2F is an
// implementation detail of the Jenkins API and never surfaces in
// suggestions.
func TargetArgSuggest(store *cache.Store, argPrefix string) []string {
	// Once the user has opened a stage marker, stop suggesting — stages
	// are not autocompleted and any further input is stage text.
	if stageMarkerIndex(argPrefix) >= 0 {
		return nil
	}

	completed, current, kind := splitForCompletion(argPrefix)
	pos, _, _ := scanCompleted(completed)

	var candidates []string
	switch kind {
	case completePositional:
		switch len(pos) {
		case 0:
			candidates = ProjectArgSuggest(store, current)
		case 1:
			projectPath := uniqueProjectPath(store, pos[0])
			if projectPath != "" {
				candidates = branchCandidates(store, projectPath, current)
			}
		case 2:
			// Project + branch already typed. The next token is a build
			// marker (`#last` or `#<n>`) — surface those directly.
			projectPath := uniqueProjectPath(store, pos[0])
			if projectPath != "" {
				branchEnc := encodeBranchName(pos[1])
				candidates = buildCandidates(store, projectPath, branchEnc, current)
			}
		}

	case completeBuild:
		if len(pos) >= 1 {
			projectPath := uniqueProjectPath(store, pos[0])
			if projectPath != "" {
				var branchEnc string
				if len(pos) >= 2 {
					branchEnc = encodeBranchName(pos[1])
				}
				candidates = buildCandidates(store, projectPath, branchEnc, current)
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, completed+c)
	}
	return out
}

// ProjectArgSuggest is the project-only suggester, exposed for use cases
// that don't need branch/build/stage completion. Behaviour matches the
// arg-1 branch of TargetArgSuggest.
func ProjectArgSuggest(store *cache.Store, prefix string) []string {
	paths := cache.AllProjectPaths(store)
	if len(paths) == 0 {
		return nil
	}
	if strings.Contains(prefix, "/") {
		return fullPathMatches(paths, prefix)
	}
	out := lastSegmentMatches(paths, prefix)
	if len(out) == 0 {
		out = fullPathMatches(paths, prefix)
	}
	return out
}

// completionKind classifies what the user is currently typing.
type completionKind int

const (
	completePositional completionKind = iota
	completeBuild
)

// splitForCompletion splits argPrefix into a "completed" portion that should
// be preserved verbatim and the "current" partial token the user is typing.
func splitForCompletion(argPrefix string) (completed, current string, kind completionKind) {
	if i := lastSpaceIndex(argPrefix); i >= 0 {
		completed = argPrefix[:i+1]
		current = argPrefix[i+1:]
	} else {
		current = argPrefix
	}
	if strings.HasPrefix(current, "#") {
		return completed, current, completeBuild
	}
	return completed, current, completePositional
}

// stageMarkerIndex returns the index of the first `:` that starts a new
// token (either at position 0 or preceded by whitespace), or -1. Used to
// detect that the user has entered stage-name territory, at which point
// autocomplete bows out.
func stageMarkerIndex(s string) int {
	for i, r := range s {
		if r == ':' && (i == 0 || isSpaceByte(s[i-1])) {
			return i
		}
	}
	return -1
}

func lastSpaceIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if isSpaceByte(s[i]) {
			return i
		}
	}
	return -1
}

func isSpaceByte(b byte) bool { return unicode.IsSpace(rune(b)) }

// scanCompleted reads the already-typed portion of argPrefix and returns the
// positional args, any build number found, and the stage name (if any
// preceded the completion point — currently always empty since stage marker
// always implies completion is mid-stage).
func scanCompleted(completed string) (positionals []string, build int, stage string) {
	for _, tok := range strings.Fields(completed) {
		if tok == "" {
			continue
		}
		switch tok[0] {
		case '#':
			if tok == "#last" {
				build = 0
			} else if n, err := strconv.Atoi(tok[1:]); err == nil && n > 0 {
				build = n
			}
		case ':':
			// Stage as a completed marker (rare — splitForCompletion would
			// usually classify mid-stage input as completeStage).
		default:
			if len(positionals) < 2 {
				positionals = append(positionals, tok)
			}
		}
	}
	return positionals, build, stage
}

// uniqueProjectPath resolves a project suffix against the cache. Returns the
// encoded full path if exactly one project matches, otherwise "".
func uniqueProjectPath(store *cache.Store, suffix string) string {
	paths := cache.AllProjectPaths(store)
	matches := matchProjectSuffix(paths, suffix)
	if len(matches) != 1 {
		return ""
	}
	return matches[0]
}

func branchCandidates(store *cache.Store, projectPath, prefix string) []string {
	if store == nil {
		return nil
	}
	seen := make(map[string]bool)
	out := branchesFromJobsCache(store, projectPath, prefix, seen)
	if len(out) == 0 {
		out = branchesFromRegistry(store, projectPath, prefix, seen)
	}
	sort.Strings(out)
	return out
}

// branchesFromJobsCache reads branch names from the Jobs cache (populated when
// the user opens the multibranch project's job list — each child Job is a branch).
func branchesFromJobsCache(store *cache.Store, projectPath, prefix string, seen map[string]bool) []string {
	if store.Jobs == nil {
		return nil
	}
	e := store.Jobs.Get(projectPath)
	if e == nil {
		return nil
	}
	var out []string
	for _, j := range e.Value {
		if j.Type == jmodel.JobTypeFolder {
			continue
		}
		if d, ok := acceptBranchName(decodeName(j.Name), prefix, seen); ok {
			out = append(out, d)
		}
	}
	return out
}

// branchesFromRegistry pulls branch names from project-scoped registry queries.
func branchesFromRegistry(store *cache.Store, projectPath, prefix string, seen map[string]bool) []string {
	if store.Registry == nil {
		return nil
	}
	var out []string
	for _, b := range store.Registry.QueryProject(projectPath) {
		if d, ok := acceptBranchName(decodeName(b.BranchName), prefix, seen); ok {
			out = append(out, d)
		}
	}
	return out
}

// acceptBranchName applies dedupe + prefix filtering. Returns the name and
// true if it should be included in the candidate list.
func acceptBranchName(d, prefix string, seen map[string]bool) (string, bool) {
	if seen[d] {
		return "", false
	}
	seen[d] = true
	if !strings.HasPrefix(d, prefix) || d == prefix {
		return "", false
	}
	return d, true
}

func buildCandidates(store *cache.Store, projectPath, branchEnc, prefix string) []string {
	if store == nil {
		return nil
	}
	body := strings.TrimPrefix(prefix, "#")
	seen := make(map[int]bool)
	var numbers []int

	if branchEnc != "" && store.Registry != nil {
		for _, b := range store.Registry.Query(buildregistry.Filter{JobPath: projectPath + "/" + branchEnc}) {
			if !seen[b.Number] {
				seen[b.Number] = true
				numbers = append(numbers, b.Number)
			}
		}
	}
	if len(numbers) == 0 && store.Registry != nil {
		for _, pb := range store.Registry.QueryProject(projectPath) {
			if !seen[pb.Number] {
				seen[pb.Number] = true
				numbers = append(numbers, pb.Number)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(numbers)))

	var out []string
	if strings.HasPrefix("#last", prefix) && prefix != "#last" {
		out = append(out, "#last")
	}
	for _, n := range numbers {
		s := strconv.Itoa(n)
		if !strings.HasPrefix(s, body) || s == body {
			continue
		}
		out = append(out, "#"+s)
	}
	return out
}

func fullPathMatches(paths []string, prefix string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, p := range paths {
		d := decodePath(p)
		if strings.HasPrefix(d, prefix) && d != prefix && !seen[d] {
			out = append(out, d)
			seen[d] = true
		}
	}
	sort.Strings(out)
	return out
}

func lastSegmentMatches(paths []string, prefix string) []string {
	bySegment := make(map[string][]string)
	for _, p := range paths {
		d := decodePath(p)
		bySegment[lastPathSegment(d)] = append(bySegment[lastPathSegment(d)], d)
	}
	seen := make(map[string]bool)
	var out []string
	for last, fulls := range bySegment {
		if !strings.HasPrefix(last, prefix) || last == prefix {
			continue
		}
		if len(fulls) == 1 {
			if !seen[last] {
				out = append(out, last)
				seen[last] = true
			}
			continue
		}
		for _, full := range fulls {
			if !seen[full] {
				out = append(out, full)
				seen[full] = true
			}
		}
	}
	sort.Strings(out)
	return out
}

func lastPathSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

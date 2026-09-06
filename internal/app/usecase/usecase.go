// Package usecase holds headless orchestration for Jenking's verbs: target
// resolution plus the fetch/mutate logic, expressed over the jmodel.JenkinsClient
// port and the cache store, returning domain (jmodel) types. Both the CLI and
// the MCP server drive Jenkins through this package so resolution and error
// handling live in exactly one place.
//
// usecase performs I/O through the port and the cache; it never imports the
// tui or formats output (presentation lives in the callers).
package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
	"github.com/Breina/Jenking/internal/tui/command"
)

// Deps carries the collaborators every verb needs. Store may be nil in tests
// that don't exercise cache-backed resolution.
type Deps struct {
	Client jmodel.JenkinsClient
	Store  *cache.Store
	// GitUsernames are the current user's additional SCM identities, matched
	// against a build's trigger cause by the "mine" filter (empty disables the
	// cause-substring fallback).
	GitUsernames []string
}

// Resolved is a concrete project + build target produced from CLI-style words.
type Resolved struct {
	JobPath     string
	BuildNumber int
	NC          navmsg.NavigationContext
}

// Resolve parses CLI-style target words, resolves them against the cache, and
// resolves the build number (latest when unspecified). It is the single entry
// point the MCP server uses to turn a fuzzy target into a concrete build.
func (d Deps) Resolve(ctx context.Context, words []string) (Resolved, error) {
	target, err := command.ParseTarget(words)
	if err != nil {
		return Resolved{}, err
	}
	nc, err := navmsg.ResolveTarget(target, d.Store, navmsg.NavigationContext{})
	if err != nil {
		return Resolved{}, err
	}
	if nc.ProjectName == "" {
		return Resolved{}, fmt.Errorf("project required")
	}
	jobPath := nc.JobPath()
	buildNum, err := d.ResolveBuildNum(ctx, jobPath, nc.Build)
	if err != nil {
		return Resolved{}, d.EnrichBranchNotFound(ctx, nc, err)
	}
	return Resolved{JobPath: jobPath, BuildNumber: buildNum, NC: nc}, nil
}

// ResolveJobPath resolves CLI-style target words to a job-level navigation
// context (no branch/build handling). Cache-only, so it takes no context.
func (d Deps) ResolveJobPath(words []string) (navmsg.NavigationContext, error) {
	target, err := command.ParseTarget(words)
	if err != nil {
		return navmsg.NavigationContext{}, err
	}
	nc, err := navmsg.ResolveTarget(target, d.Store, navmsg.NavigationContext{})
	if err != nil {
		return navmsg.NavigationContext{}, err
	}
	if nc.ProjectName == "" {
		return navmsg.NavigationContext{}, fmt.Errorf("project required")
	}
	return nc, nil
}

// ResolveInputID picks the pending input to act on. An explicit wanted id is
// validated against the build's pending inputs; otherwise the single pending
// input is used, and multiple candidates is an error listing their ids.
func (d Deps) ResolveInputID(ctx context.Context, jobPath string, buildNum int, wanted string) (string, error) {
	detail, err := d.Client.GetBuild(ctx, jobPath, buildNum)
	if err != nil {
		return "", err
	}
	inputs := detail.PendingInputs
	if len(inputs) == 0 {
		return "", fmt.Errorf("no pending inputs for %s #%d", jobPath, buildNum)
	}
	if wanted != "" {
		for _, in := range inputs {
			if in.ID == wanted {
				return wanted, nil
			}
		}
		return "", fmt.Errorf("input %q not pending for %s #%d; pending: %s", wanted, jobPath, buildNum, strings.Join(inputIDs(inputs), ", "))
	}
	if len(inputs) > 1 {
		return "", fmt.Errorf("%d inputs pending for %s #%d; pick one with --id: %s", len(inputs), jobPath, buildNum, strings.Join(inputIDs(inputs), ", "))
	}
	return inputs[0].ID, nil
}

func inputIDs(inputs []jmodel.PendingInput) []string {
	ids := make([]string, len(inputs))
	for i, in := range inputs {
		ids[i] = in.ID
	}
	return ids
}

// ResolveBuildNum resolves a NavBuildRef to a concrete build number, calling
// ListBuilds to find the latest when ref carries no explicit number.
func (d Deps) ResolveBuildNum(ctx context.Context, jobPath string, ref navmsg.NavBuildRef) (int, error) {
	if ref.Number > 0 {
		return ref.Number, nil
	}
	builds, err := d.Client.ListBuilds(ctx, jobPath)
	if err != nil {
		return 0, fmt.Errorf("listing builds for %s: %w", jobPath, err)
	}
	if len(builds) == 0 {
		return 0, fmt.Errorf("no builds found for %s (multibranch projects require a branch)", jobPath)
	}
	return builds[0].Number, nil
}

// EnrichBranchNotFound turns a 404 from a branch-level call into an error that
// names the missing branch and lists the project's available branches. Any
// non-404 error (or a failure to look up branches) is returned unchanged.
func (d Deps) EnrichBranchNotFound(ctx context.Context, nc navmsg.NavigationContext, err error) error {
	if err == nil || nc.BranchName == "" || !jmodel.IsNotFound(err) {
		return err
	}

	projectPath := nc.FolderPath
	if nc.ProjectName != "" {
		if projectPath != "" {
			projectPath += "/"
		}
		projectPath += nc.ProjectName
	}
	pbuilds, perr := d.Client.ListProjectBuilds(ctx, projectPath)
	if perr != nil {
		return err
	}

	seen := map[string]bool{}
	var branches []string
	for _, b := range pbuilds {
		name := navmsg.DecodeName(b.BranchName)
		if name != "" && !seen[name] {
			seen[name] = true
			branches = append(branches, name)
		}
	}
	sort.Strings(branches)

	wanted := navmsg.DecodeName(nc.BranchName)
	project := navmsg.DecodePath(projectPath)
	if len(branches) == 0 {
		return fmt.Errorf("branch %q not found in %s", wanted, project)
	}
	return fmt.Errorf("branch %q not found in %s; available branches: %s", wanted, project, strings.Join(branches, ", "))
}

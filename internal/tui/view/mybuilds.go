package view

import (
	"fmt"
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

type myBuildsFullMsg struct {
	builds []jenkins.UserBuild
	err    error
}

type myBuildsSlowTickMsg struct{}

// MyBuildsView shows the stages of the most recent build matching the active
// scope and filters. It combines a fast running-builds poll with a slower
// scope-appropriate fetch so that builds are tracked whether or not they are
// currently executing.
//
// The scope (NavigationContext) determines which builds are considered:
//   - Root (*):   all builds on the instance (ScanAllBuilds)
//   - Folder:     builds under the folder prefix (ScanAllBuilds, filtered)
//   - Project:    builds across all branches (ListProjectBuilds)
//   - Branch:     builds for a single job (ListBuilds)
//
// Filters (toggled with r / m):
//   - running (default OFF): only consider currently-running builds.
//   - mine   (default OFF): only consider builds triggered by the user.
//
// Running-builds data comes from RunningBuildsUpdatedMsg (broadcast by the
// shared monitor). When running is ON only running builds are shown. When
// running is OFF the view merges running + slow data.
type MyBuildsView struct {
	*ScopedView
}

// NewMyBuildsView creates a scoped last-build stage view. The scope NC
// determines which builds are candidates; mine filter defaults to OFF.
func NewMyBuildsView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, scope NavigationContext, slowInterval time.Duration) *MyBuildsView {
	resolver := newBuildResolver(client, store, scope, slowInterval)
	sv := NewScopedView(t, resolver, ScopedViewConfig{
		Title:                 "Stages",
		BreadcrumbType:        "stages",
		HandleSlowFetch:       true,
		AppendFilterShortcuts: true,
		NewInner: func(nc NavigationContext, build jenkins.UserBuild) View {
			return NewStageView(t, client, store, nc, build.Build)
		},
	})
	return &MyBuildsView{ScopedView: sv}
}

// resolverParts returns breadcrumb parts for the resolved #last build.
// Only includes what #last adds beyond the scope — no redundancy.
func resolverParts(r *buildResolver) []component.BreadcrumbPart {
	if r.resolvedPath == "" || r.resolvedNum == 0 {
		return nil
	}
	resolvedNC := ncFromJobPath(r.resolvedPath)
	var parts []component.BreadcrumbPart
	if r.scope.ProjectName == "" && resolvedNC.ProjectName != "" {
		parts = append(parts, component.BreadcrumbPart{
			Text: shortName(decodeName(resolvedNC.ProjectName)),
		})
	}
	if r.scope.BranchName == "" && resolvedNC.BranchName != "" {
		parts = append(parts, component.BreadcrumbPart{
			Text:      decodeName(resolvedNC.BranchName),
			Separator: branchIcon(resolvedNC.BranchName),
		})
	}
	parts = append(parts, component.BreadcrumbPart{
		Text:       fmt.Sprintf("%d", r.resolvedNum),
		IsBuildNum: true,
		Separator:  " ",
	})
	return parts
}

// filterShortcuts returns the r/m shortcut hints for scoped views.
func filterShortcuts(filterRunning, filterMine bool) []component.Shortcut {
	return []component.Shortcut{
		{Key: "r", Action: "filter running", Active: filterRunning},
		{Key: "m", Action: "filter mine", Active: filterMine},
	}
}

// projectBuildsToUserBuilds converts ProjectBuild records to UserBuild for
// uniform handling in mergeBuilds/bestMatch.
func projectBuildsToUserBuilds(pbs []jenkins.ProjectBuild, projectPath string) []jenkins.UserBuild {
	ubs := make([]jenkins.UserBuild, len(pbs))
	for i, pb := range pbs {
		ubs[i] = jenkins.UserBuild{
			JobPath: pb.BranchPath,
			Build:   pb.Build,
		}
	}
	return ubs
}

// buildsToUserBuilds converts plain Build records to UserBuild for uniform handling.
func buildsToUserBuilds(builds []jenkins.Build, jobPath string) []jenkins.UserBuild {
	ubs := make([]jenkins.UserBuild, len(builds))
	for i, b := range builds {
		ubs[i] = jenkins.UserBuild{
			JobPath: jobPath,
			Build:   b,
		}
	}
	return ubs
}

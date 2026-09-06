package view

import (
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

type myBuildsFullMsg struct {
	builds []jmodel.UserBuild
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
func NewMyBuildsView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, scope NavigationContext, slowInterval time.Duration) *MyBuildsView {
	resolver := newBuildResolver(client, store, scope, slowInterval)
	sv := NewScopedView(t, resolver, ScopedViewConfig{
		Title:                 "Stages",
		BreadcrumbType:        "stages",
		HandleSlowFetch:       true,
		AppendFilterShortcuts: true,
		NewInner: func(nc NavigationContext, build jmodel.UserBuild) View {
			return NewStageView(t, client, store, nc, build.Build)
		},
	})
	return &MyBuildsView{ScopedView: sv}
}

// filterShortcuts returns the r/m shortcut hints for scoped views.
func filterShortcuts(filterRunning, filterMine bool) []component.Shortcut {
	return []component.Shortcut{
		component.Filter("r", "filter running", filterRunning),
		component.Filter("m", "filter mine", filterMine),
	}
}

// detailViewTabs returns the standard l/s/d View shortcuts for build-detail views.
// active is "l", "s", or "d" to mark that tab as currently open; pass "" for none.
func detailViewTabs(active string) []component.Shortcut {
	return []component.Shortcut{
		component.ViewSCRanked("l", "full log", active == "l", rankViewFullLog),
		component.ViewSCRanked("s", "stages", active == "s", rankViewStages),
		component.ViewSCRanked("d", "describe", active == "d", rankViewDescribe),
	}
}

// projectBuildsToUserBuilds converts ProjectBuild records to UserBuild for
// uniform handling in mergeBuilds/bestMatch.
func projectBuildsToUserBuilds(pbs []jmodel.ProjectBuild, projectPath string) []jmodel.UserBuild {
	ubs := make([]jmodel.UserBuild, len(pbs))
	for i, pb := range pbs {
		ubs[i] = jmodel.UserBuild{
			JobPath: pb.BranchPath,
			Build:   pb.Build,
		}
	}
	return ubs
}

// buildsToUserBuilds converts plain Build records to UserBuild for uniform handling.
func buildsToUserBuilds(builds []jmodel.Build, jobPath string) []jmodel.UserBuild {
	ubs := make([]jmodel.UserBuild, len(builds))
	for i, b := range builds {
		ubs[i] = jmodel.UserBuild{
			JobPath: jobPath,
			Build:   b,
		}
	}
	return ubs
}

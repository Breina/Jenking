package main

import (
	"fmt"
	"time"

	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// deepLinkArgs bundles the immutable runtime state needed to construct any
// deep-linked view from CLI arguments.
type deepLinkArgs struct {
	theme        theme.Theme
	client       jmodel.JenkinsClient
	store        *cache.Store
	username     string
	friendlyName string
	gitUsernames []string
	slowInterval time.Duration
}

// buildDeepLinkView parses CLI args of the form `<verb> [args...]` and returns
// a fully constructed view ready to be passed to tui.NewApp as initialView.
//
// Project-suffix resolution relies on the Jobs cache. On a cold start with no
// disk-persisted cache, suffix matching will fail with "unknown project" — the
// user must supply a full path or run the TUI once to warm the cache.
func buildDeepLinkView(verb string, args []string, d deepLinkArgs) (view.View, error) {
	target, err := command.ParseTarget(args)
	if err != nil {
		return nil, err
	}

	current := view.NavigationContext{
		Username:     d.username,
		FriendlyName: d.friendlyName,
		GitUsernames: d.gitUsernames,
	}
	nc, err := view.ResolveTarget(target, d.store, current)
	if err != nil {
		return nil, err
	}
	nc.Username = d.username
	nc.FriendlyName = d.friendlyName
	nc.GitUsernames = d.gitUsernames

	switch verb {
	case "logs", "log", "l":
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			return view.NewConsoleView(d.theme, d.client, nc), nil
		}
		return view.NewMyConsoleView(d.theme, d.client, d.store, nc.AtScope(), d.slowInterval), nil

	case "stages", "stage", "s":
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			return view.NewStageView(d.theme, d.client, d.store, nc, jmodel.Build{Number: nc.Build.Number}), nil
		}
		return view.NewMyBuildsView(d.theme, d.client, d.store, nc.AtScope(), d.slowInterval), nil

	case "builds", "build", "b":
		return buildsViewForCLI(nc, d), nil

	case "jobs", "job", "j":
		return jobListForCLI(nc, d), nil

	default:
		return nil, fmt.Errorf("unknown verb: %s (expected one of: logs, stages, builds, jobs)", verb)
	}
}

// buildsViewForCLI mirrors App.buildsViewFor — kept separate to avoid
// reaching into the tui package from main. The dispatch logic must stay
// in sync with app.go's handleOpenTarget.
func buildsViewForCLI(nc view.NavigationContext, d deepLinkArgs) view.View {
	nc = nc.AtScope()
	switch nc.Level {
	case view.CtxBranch:
		return view.NewBuildsView(d.theme, d.client, d.store, nc, view.NewBranchBuildsProvider(d.client, d.store, nc))
	case view.CtxProject:
		return view.NewBuildsView(d.theme, d.client, d.store, nc, view.NewProjectBuildsProvider(d.client, d.store, nc))
	case view.CtxFolder:
		return view.NewFolderBuildsView(d.theme, d.client, d.store, nc.FolderPath, d.username, d.gitUsernames, d.slowInterval)
	default:
		return view.NewAllBuildsView(d.theme, d.client, d.store, d.username, d.gitUsernames, d.slowInterval)
	}
}

// jobListForCLI mirrors App.jobListForTarget — drills INTO the targeted
// project/folder, since the CLI always carries explicit args.
func jobListForCLI(nc view.NavigationContext, d deepLinkArgs) view.View {
	switch nc.Level {
	case view.CtxFolder:
		title := lastSegment(nc.FolderPath)
		return view.NewJobList(d.theme, d.client, d.store, nc.FolderPath, title, false, d.username, d.gitUsernames)
	case view.CtxProject, view.CtxBranch, view.CtxBuild, view.CtxStage:
		pp := nc.ProjectName
		if nc.FolderPath != "" {
			pp = nc.FolderPath + "/" + nc.ProjectName
		}
		return view.NewJobList(d.theme, d.client, d.store, pp, nc.ProjectName, true, d.username, d.gitUsernames)
	default:
		return view.NewJobList(d.theme, d.client, d.store, "", "Dashboard", false, d.username, d.gitUsernames)
	}
}

func lastSegment(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

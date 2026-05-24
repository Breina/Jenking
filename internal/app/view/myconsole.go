package view

import (
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// MyConsoleView shows the console log of the most recent build matching the
// active scope and filters. It mirrors MyBuildsView but wraps a ConsoleView
// instead of a StageView.
type MyConsoleView struct {
	*ScopedView
}

// NewMyConsoleView creates a scoped last-build console view.
func NewMyConsoleView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, scope NavigationContext, slowInterval time.Duration) *MyConsoleView {
	resolver := newBuildResolver(client, store, scope, slowInterval)
	sv := NewScopedView(t, resolver, ScopedViewConfig{
		Title:                 "Console",
		BreadcrumbType:        "log",
		HandleSlowFetch:       true,
		AppendFilterShortcuts: true,
		NewInner: func(nc NavigationContext, build jmodel.UserBuild) View {
			return NewConsoleView(t, client, nc)
		},
	})
	return &MyConsoleView{ScopedView: sv}
}

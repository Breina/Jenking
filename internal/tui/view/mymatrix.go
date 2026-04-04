package view

import (
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// MyMatrixView shows build logs in Matrix-mode for the most recent build
// matching the active scope and filters.
type MyMatrixView struct {
	*ScopedView
}

// NewMyMatrixView creates a scoped Matrix view that only tracks running builds.
func NewMyMatrixView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, scope NavigationContext, slowInterval time.Duration) *MyMatrixView {
	resolver := newBuildResolver(client, store, scope, slowInterval)
	resolver.filterRunning = true
	sv := NewScopedView(t, resolver, ScopedViewConfig{
		Title:                 "Matrix",
		BreadcrumbType:        "matrix",
		HandleSlowFetch:       false,
		AppendFilterShortcuts: false,
		FullScreenWhenActive:  true,
		NewInner: func(nc NavigationContext, build jenkins.UserBuild) View {
			return NewMatrixView(client, nc)
		},
	})
	return &MyMatrixView{ScopedView: sv}
}

// IsFullScreen overrides the ScopedView's implementation explicitly,
// documenting that Matrix is full-screen when a build is active.
func (mv *MyMatrixView) IsFullScreen() bool {
	return mv.ScopedView.IsFullScreen()
}

// ToggleRunning is a no-op: Matrix view always shows running builds only.
func (mv *MyMatrixView) ToggleRunning() {}

// ParentView returns a fresh ConsoleView for the resolved build so ESC brings
// the user back to the log they launched Matrix from.
func (mv *MyMatrixView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	if mv.inner == nil {
		return nil
	}
	matrixInner, ok := mv.inner.(*MatrixView)
	if !ok {
		return nil
	}
	return NewConsoleView(t, c, matrixInner.nc)
}

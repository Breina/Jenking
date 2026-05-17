package view

import (
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// addFixedBuildActions registers the four cross-cutting behaviors that apply
// to any view targeting a single, fixed build: open test report (T), open
// artifacts (A), cancel running build (x), and trigger a new build (t).
//
// This is the recommended one-line wiring for fixed-build views (describe,
// testreport, console, stageview, stagelog). Views with row-aware targets
// (buildsview, joblist) still register behaviors individually because their
// accessor must close over the cursor.
//
// nc/build/store are pointers so the behaviors always see the view's
// current state — including post-construction assignments (e.g. ConsoleView's
// store is set by callers after NewConsoleView).
func addFixedBuildActions(
	h *behaviorHost,
	t theme.Theme,
	client jenkins.JenkinsClient,
	nc *NavigationContext,
	build *jenkins.Build,
	store **cache.Store,
	trigger *triggerMixin,
	navigate navigateCmd,
) {
	access := fixedBuildAccessor(nc, build)
	storeFn := func() *cache.Store { return *store }
	h.Add(newTestReportBehavior(t, client, storeFn, access, navigate))
	h.Add(newArtifactBehavior(t, client, storeFn, access, navigate))
	h.Add(newCancelBehavior(t, client, access))
	h.Add(newTriggerBehavior(trigger))
}

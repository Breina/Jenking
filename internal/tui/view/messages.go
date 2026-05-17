package view

import (
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// PopViewMsg asks the app to pop the active view off the nav stack.
type PopViewMsg struct{}

// ContextSwitchRequestMsg is emitted by ContextView when the user selects a context.
type ContextSwitchRequestMsg struct{ Name string }

// ContextDeleteRequestMsg is emitted by ContextView when the user confirms a delete.
type ContextDeleteRequestMsg struct{ Name string }

// OpenAddContextDialogMsg is emitted by ContextView when the user asks to add a new context.
type OpenAddContextDialogMsg struct{}

// ContextListUpdatedMsg is broadcast by the app after the context list changes
// (add or delete). ContextView rebuilds its rows from it.
type ContextListUpdatedMsg struct {
	Contexts []config.ContextConfig
	Current  string
}

// ContextProbeMsg carries the result of a context connection probe.
type ContextProbeMsg struct {
	Name string
	OK   bool
}

// ColorblindPreviewMsg asks the app to apply a colorblindness type without persisting.
type ColorblindPreviewMsg struct{ Type theme.ColorblindnessType }

// ColorblindConfirmMsg asks the app to apply and persist a colorblindness type.
type ColorblindConfirmMsg struct{ Type theme.ColorblindnessType }

// ThemePreviewMsg asks the app to apply a theme without persisting.
type ThemePreviewMsg struct {
	ID       theme.ThemeID
	Degraded bool
}

// ThemeConfirmMsg asks the app to apply and persist a theme.
type ThemeConfirmMsg struct{ ID theme.ThemeID }

// ThemeLockedRoyalMsg asks the app to open the Royal paywall. The originals
// are what to restore to if the user cancels.
type ThemeLockedRoyalMsg struct {
	OriginalID       theme.ThemeID
	OriginalDegraded bool
}

// ThemeChangedMsg is broadcast by the app when the active theme changes
// (e.g. colorblind mode toggled). Each view should update its stored theme.
type ThemeChangedMsg struct{ Theme theme.Theme }

// PushViewMsg is returned by views that want to navigate into a child view.
// app.go handles this by pushing the new view onto the stack.
type PushViewMsg struct{ View View }

// SwapViewMsg replaces the current view without touching the nav stack.
// Use for "sideways" navigation (e.g. stages → tests) where ESC from the new
// view should return to the parent of the replaced view, not back to it.
type SwapViewMsg struct{ View View }

// PopSwapViewMsg pops one entry from the nav stack and then makes View active.
// Use when navigating sideways from a pushed sub-view: discards the stale
// parent stack entry so ESC from the new view reaches the grandparent instead.
// Example: stagelog (pushed from stageview) → full log; ESC should reach
// builds, not stageview.
type PopSwapViewMsg struct{ View View }

// PushViewsMsg pushes a chain of views onto the nav stack in one step.
// The current view is pushed first, then each view in Views except the last,
// then the last becomes the active view. This gives the full ESC chain:
// current → Views[0] → Views[1] → ... → Views[n-1] (active).
type PushViewsMsg struct{ Views []View }

// ErrorMsg is returned by views to surface errors to the status bar.
type ErrorMsg struct{ Err error }

// BuildsMsg carries fetched builds to the view.
type BuildsMsg struct {
	Builds []jenkins.Build
	Err    error
}

// RunningBuildsMsg carries the result of a running-builds fetch.
type RunningBuildsMsg struct {
	Builds []jenkins.UserBuild
	Err    error
}

// BuildDetailMsg carries a fetched BuildDetail (stages + params) to the view.
type BuildDetailMsg struct {
	Detail *jenkins.BuildDetail
	Err    error
}

// StagesMsg carries the pipeline stages for a build.
type StagesMsg struct {
	Stages []jenkins.Stage
	Err    error
}

// StageLogMsg carries the aggregated log for a pipeline stage.
type StageLogMsg struct {
	Text  string
	Nodes map[int]*nodeLogState // per-node progressive state (nil for legacy callers)
	Err   error
}

// CancelBuildResultMsg carries the outcome of a cancel operation.
type CancelBuildResultMsg struct{ Err error }

// JobParamsMsg carries fetched parameter definitions for a job.
type JobParamsMsg struct {
	NC     NavigationContext
	Params []jenkins.ParameterDefinition
	Err    error
}

// TriggerBuildResultMsg carries the outcome of a trigger operation.
type TriggerBuildResultMsg struct {
	NC  NavigationContext
	Err error
}

// OpenTriggeredBuildMsg asks the app to push a BuildList + pending StageView.
// Used by joblist so the breadcrumb includes the job/branch name.
type OpenTriggeredBuildMsg struct {
	NC             NavigationContext
	LastKnownBuild int
}

// ArtifactsMsg carries build artifacts for a build.
// Artifacts is an empty slice when the build has no artifacts.
type ArtifactsMsg struct {
	JobPath   string
	BuildNum  int
	Artifacts []jenkins.Artifact
	Err       error
}

// TestReportMsg carries JUnit test results for a build.
// Report is nil when the build has no test results recorded.
type TestReportMsg struct {
	JobPath  string
	BuildNum int
	Report   *jenkins.TestReport
	Err      error
}

// FailedStageMsg carries the result of looking up a failed stage for a build.
type FailedStageMsg struct {
	NC          NavigationContext
	Build       jenkins.Build
	Stages      []jenkins.Stage // all stages (for pre-populating StageView)
	FailedStage *jenkins.Stage  // nil if no failed stage found
	FailedIdx   int             // index of failed stage in Stages, or -1
	Err         error
}

// RunningBuildsUpdatedMsg is broadcast by the RunningBuildsMonitor each poll.
// App handles it to update the header count, then forwards it to the active view.
type RunningBuildsUpdatedMsg struct {
	Builds   []jenkins.UserBuild
	Arrived  []string // build keys (jobPath#number) newly in running set
	Departed []string // build keys just left the running set
	Count    int
}

// OpenScopedStagesMsg asks the app to open a scoped last-build stage view.
// Emitted by views (e.g. JobList) that know the scope but not the gitUsernames.
type OpenScopedStagesMsg struct {
	NC NavigationContext
}

// ConnectionRestoredMsg is broadcast by the app when the Jenkins connection
// recovers after a failure. Streaming views use it to resume where they stopped.
type ConnectionRestoredMsg struct{}

// BuildCompletedMsg carries the final status of a build that just left the running set.
// The monitor fetches this after detecting a departure.
type BuildCompletedMsg struct {
	Key     string // jobPath#number
	JobPath string
	Number  int
	Build   jenkins.Build
	Err     error
}

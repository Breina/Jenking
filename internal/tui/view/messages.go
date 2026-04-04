package view

import (
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// ThemeChangedMsg is broadcast by the app when the active theme changes
// (e.g. colorblind mode toggled). Each view should update its stored theme.
type ThemeChangedMsg struct{ Theme theme.Theme }

// PushViewMsg is returned by views that want to navigate into a child view.
// app.go handles this by pushing the new view onto the stack.
type PushViewMsg struct{ View View }

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

// BuildCompletedMsg carries the final status of a build that just left the running set.
// The monitor fetches this after detecting a departure.
type BuildCompletedMsg struct {
	Key     string // jobPath#number
	JobPath string
	Number  int
	Build   jenkins.Build
	Err     error
}

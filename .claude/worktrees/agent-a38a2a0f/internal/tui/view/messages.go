package view

import (
	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

// ThemeChangedMsg is broadcast by the app when the active theme changes
// (e.g. colorblind mode toggled). Each view should update its stored theme.
type ThemeChangedMsg struct{ Theme theme.Theme }

// PushViewMsg is returned by views that want to navigate into a child view.
// app.go handles this by pushing the new view onto the stack.
type PushViewMsg struct{ View View }

// PushTwoViewsMsg pushes two views at once (e.g. BuildList + StageView).
// Only the second view's Init is called.
type PushTwoViewsMsg struct {
	First  View
	Second View
}

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
	JobPath    string
	JobName    string
	BranchName string
	Params     []jenkins.ParameterDefinition
	Err        error
}

// TriggerBuildResultMsg carries the outcome of a trigger operation.
type TriggerBuildResultMsg struct {
	JobPath string
	Err     error
}

// OpenTriggeredBuildMsg asks the app to push a BuildList + pending StageView.
// Used by joblist so the breadcrumb includes the job/branch name.
type OpenTriggeredBuildMsg struct {
	JobPath        string
	JobName        string
	BranchName     string
	LastKnownBuild int
}

// FailedStageMsg carries the result of looking up a failed stage for a build.
type FailedStageMsg struct {
	JobPath     string
	JobName     string
	BranchName  string
	Build       jenkins.Build
	Stages      []jenkins.Stage // all stages (for pre-populating StageView)
	FailedStage *jenkins.Stage  // nil if no failed stage found
	FailedIdx   int             // index of failed stage in Stages, or -1
	Err         error
}

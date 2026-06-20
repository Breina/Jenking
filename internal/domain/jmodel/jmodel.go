// Package jmodel holds the pure domain model: types, enums, and the
// JenkinsClient port. No I/O, no third-party deps beyond stdlib +
// domain/pipelinesyntax. The internal/jenkins adapter implements
// JenkinsClient; everything else in the codebase consumes these types.
package jmodel

import (
	"context"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

// JobType represents the type of a Jenkins job.
type JobType string

const (
	JobTypeFolder      JobType = "folder"
	JobTypeFreeStyle   JobType = "freestyle"
	JobTypePipeline    JobType = "pipeline"
	JobTypeMultiBranch JobType = "multibranch"
	JobTypeUnknown     JobType = "unknown"
)

// BranchType represents whether a multibranch child is a branch or MR.
type BranchType string

const (
	BranchTypeBranch       BranchType = "branch"
	BranchTypeMergeRequest BranchType = "merge_request"
	BranchTypeNone         BranchType = "none"
)

// ParamType represents the type of a Jenkins parameter.
type ParamType string

const (
	ParamTypeString   ParamType = "string"
	ParamTypeBool     ParamType = "bool"
	ParamTypeChoice   ParamType = "choice"
	ParamTypePassword ParamType = "password"
)

// ParameterDefinition describes a parameter of a parameterized job.
type ParameterDefinition struct {
	Name        string
	Type        ParamType
	Default     string
	Description string
	Choices     []string
}

// BuildStatus represents the status of a build.
type BuildStatus string

const (
	BuildStatusRunning     BuildStatus = "running"
	BuildStatusSuccess     BuildStatus = "success"
	BuildStatusFailed      BuildStatus = "failed"
	BuildStatusAborted     BuildStatus = "aborted"
	BuildStatusUnstable    BuildStatus = "unstable"
	BuildStatusSkipped     BuildStatus = "skipped"
	BuildStatusNotBuilt    BuildStatus = "not_built"
	BuildStatusPausedInput BuildStatus = "paused_input"
	BuildStatusUnknown     BuildStatus = "unknown"
)

// BuildRef is a lightweight reference to a build.
type BuildRef struct {
	Number            int
	URL               string
	Timestamp         time.Time
	EstimatedDuration time.Duration
}

// Job represents a job.
type Job struct {
	Name         string
	FullPath     string
	Type         JobType
	BranchType   BranchType
	LastBuild    *BuildRef
	Color        string
	LastAnyBuild *BuildRef
	LastAnyColor string
	RunningCount int
	Disabled     bool
}

// Build represents a build.
type Build struct {
	Number            int
	Status            BuildStatus
	Duration          time.Duration
	EstimatedDuration time.Duration
	Timestamp         time.Time
	Params            map[string]string
	TriggeredBy       string
	TriggeredByName   string
	Cause             string
}

// BuildDetail extends Build with stage information.
type BuildDetail struct {
	Build
	Stages        []Stage
	PendingInputs []PendingInput
}

// PendingInput describes a paused `input` step awaiting decision.
// Lives at the run level — a single build may have multiple pending inputs
// (one per parallel branch). Use ApplyPendingInputs to project this onto
// per-stage status for rendering.
type PendingInput struct {
	ID                 string                // input step id
	Message            string                // prompt text
	OkLabel            string                // proceed button label
	AbortLabel         string                // abort button label
	Submitter          string                // submitter restriction; "" = any
	SubmitterParameter string                // optional param name to receive the submitter id
	Parameters         []ParameterDefinition // empty for confirm-only
}

// Stage represents a pipeline stage.
type Stage struct {
	Name     string
	Status   BuildStatus
	Duration time.Duration
	NodeIDs  []int
	Depth    int
	Parallel bool
}

// ProjectBuild is a build from a multibranch project.
type ProjectBuild struct {
	Build
	BranchName string
	BranchPath string
}

// UserBuild is a build associated with its job path.
type UserBuild struct {
	JobPath     string
	Node        string
	DisplayName string
	Build
}

// User represents a Jenkins user.
type User struct {
	ID             string
	FullName       string
	JenkinsVersion string
}

// TestStatus represents the outcome of a single test case.
type TestStatus string

const (
	TestStatusPassed  TestStatus = "passed"
	TestStatusFailed  TestStatus = "failed"
	TestStatusSkipped TestStatus = "skipped"
)

// TestCase represents a single test case within a suite.
type TestCase struct {
	ClassName    string
	Name         string
	Status       TestStatus
	Duration     time.Duration
	ErrorDetails string
}

// TestSuite represents a group of test cases.
type TestSuite struct {
	Name     string
	Duration time.Duration
	Cases    []TestCase
}

// TestReport holds aggregated test results for a build.
type TestReport struct {
	Duration time.Duration
	Failed   int
	Passed   int
	Skipped  int
	Suites   []TestSuite
}

// Artifact represents a single build artifact.
type Artifact struct {
	DisplayPath string
	URL         string
}

// ProgressiveLog is a chunk of console output from the progressive API.
type ProgressiveLog struct {
	Text      string
	MoreData  bool
	NextStart int
}

// NodeLog is the progressive output for a single flow-graph node.
type NodeLog struct {
	Text      string
	MoreData  bool
	NextStart int
}

// ValidationIssue is one problem reported by the pipeline-model-converter.
type ValidationIssue struct {
	Line    int
	Col     int
	Message string
}

// ValidationResult is the parsed response from /pipeline-model-converter/validate.
type ValidationResult struct {
	OK     bool
	Issues []ValidationIssue
	Raw    string
}

// JenkinsClient is the port through which the rest of the system reaches the
// Jenkins server. The adapter at internal/jenkins implements this.
type JenkinsClient interface {
	ListJobs(ctx context.Context, folder string) ([]Job, error)
	ListBuilds(ctx context.Context, jobPath string) ([]Build, error)
	ListProjectBuilds(ctx context.Context, projectPath string) ([]ProjectBuild, error)
	ListUserBuilds(ctx context.Context, username string) ([]UserBuild, error)
	ListRunningBuilds(ctx context.Context) ([]UserBuild, error)
	ScanAllBuilds(ctx context.Context, maxPerJob int) ([]UserBuild, error)
	ListStages(ctx context.Context, jobPath string, buildNumber int) ([]Stage, error)
	GetBuild(ctx context.Context, jobPath string, number int) (*BuildDetail, error)
	GetConsoleOutput(ctx context.Context, jobPath string, number int) (io.ReadCloser, error)
	GetFullConsoleText(ctx context.Context, jobPath string, number int) (string, error)
	GetProgressiveLog(ctx context.Context, jobPath string, number, start int) (*ProgressiveLog, error)
	GetNodeLog(ctx context.Context, jobPath string, buildNumber, nodeID int) (string, error)
	GetNodeLogProgressive(ctx context.Context, jobPath string, buildNumber, nodeID, start int) (*NodeLog, error)
	GetJobParameters(ctx context.Context, jobPath string) ([]ParameterDefinition, error)
	GetBuildScript(ctx context.Context, jobPath string, buildNumber int) (string, error)
	FetchPipelineSyntax(ctx context.Context, jobPath string, buildNumber int) (*pipelinesyntax.Symbols, error)
	ValidateJenkinsfile(ctx context.Context, content string) (ValidationResult, error)
	GetBuildParameters(ctx context.Context, jobPath string, buildNumber int) (map[string]string, error)
	GetTestReport(ctx context.Context, jobPath string, buildNum int) (*TestReport, error)
	GetArtifacts(ctx context.Context, jobPath string, buildNum int) ([]Artifact, error)
	GetArtifactContent(ctx context.Context, artifactURL string) (content, contentType string, err error)
	TriggerBuild(ctx context.Context, jobPath string, params map[string]string) error
	ReplayBuild(ctx context.Context, jobPath string, buildNum int, script string) error
	CancelBuild(ctx context.Context, jobPath string, number int) error
	ProceedInput(ctx context.Context, jobPath string, buildNumber int, inputID string, params map[string]string) error
	AbortInput(ctx context.Context, jobPath string, buildNumber int, inputID string) error
	WhoAmI(ctx context.Context) (*User, error)
}

// ApplyPendingInputs marks any currently-running stage as PausedInput when
// the build has at least one pending input. The Jenkins JSON does not surface
// a flow-node id on the input execution, so this is a best-effort projection:
// the input lives inside whatever stage is currently running.
//
// Returns the modified slice (same underlying array). Pure function — no I/O.
func ApplyPendingInputs(stages []Stage, inputs []PendingInput) []Stage {
	if len(inputs) == 0 {
		return stages
	}
	for i := range stages {
		if stages[i].Status == BuildStatusRunning {
			stages[i].Status = BuildStatusPausedInput
		}
	}
	return stages
}

// BuildKey returns a stable string key for deduplicating builds across sources.
func BuildKey(jobPath string, number int) string {
	return jobPath + "#" + strconv.Itoa(number)
}

// ParseBuildKey splits a build key (jobPath#number) into its components.
// Returns number=0 when the key is malformed.
func ParseBuildKey(key string) (jobPath string, number int) {
	idx := strings.LastIndex(key, "#")
	if idx < 0 {
		return key, 0
	}
	n, err := strconv.Atoi(key[idx+1:])
	if err != nil {
		return key[:idx], 0
	}
	return key[:idx], n
}

// JobPathToURL converts a slash-separated job path to a Jenkins URL path.
// Each segment is percent-escaped (without prior unescaping), so a branch
// stored as "feature%2Fbranch" becomes "%252F" — the double-encoded form
// Jenkins requires.
func JobPathToURL(jobPath string) string {
	if jobPath == "" {
		return ""
	}
	parts := strings.Split(jobPath, "/")
	encoded := make([]string, len(parts))
	for i, p := range parts {
		encoded[i] = url.PathEscape(p)
	}
	return "/job/" + strings.Join(encoded, "/job/")
}

// FormatErrorformat renders the validation issues in vim's errorformat-friendly
// shape: `%f:%l:%c: %m` when col present, `%f:%l: %m` otherwise.
func (r ValidationResult) FormatErrorformat(file string) string {
	if len(r.Issues) == 0 {
		return ""
	}
	var b strings.Builder
	for _, iss := range r.Issues {
		if iss.Col > 0 {
			b.WriteString(file)
			b.WriteString(":")
			b.WriteString(strconv.Itoa(iss.Line))
			b.WriteString(":")
			b.WriteString(strconv.Itoa(iss.Col))
			b.WriteString(": ")
			b.WriteString(iss.Message)
			b.WriteString("\n")
		} else {
			line := iss.Line
			if line == 0 {
				line = 1
			}
			b.WriteString(file)
			b.WriteString(":")
			b.WriteString(strconv.Itoa(line))
			b.WriteString(": ")
			b.WriteString(iss.Message)
			b.WriteString("\n")
		}
	}
	return b.String()
}

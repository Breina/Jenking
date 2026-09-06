package jenkins

import (
	"net/url"
	"strings"
	"time"
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
	ParamTypeString ParamType = "string"
	ParamTypeBool   ParamType = "bool"
	ParamTypeChoice ParamType = "choice"
)

// ParameterDefinition describes a parameter of a parameterized Jenkins job.
type ParameterDefinition struct {
	Name        string
	Type        ParamType
	Default     string
	Description string
	Choices     []string // only for choice type
}

// BuildStatus represents the status of a Jenkins build.
type BuildStatus string

const (
	BuildStatusRunning  BuildStatus = "running"
	BuildStatusSuccess  BuildStatus = "success"
	BuildStatusFailed   BuildStatus = "failed"
	BuildStatusAborted  BuildStatus = "aborted"
	BuildStatusUnstable BuildStatus = "unstable"
	BuildStatusSkipped  BuildStatus = "skipped"
	BuildStatusNotBuilt BuildStatus = "not_built"
	BuildStatusUnknown  BuildStatus = "unknown"
)

// BuildRef is a lightweight reference to a build.
type BuildRef struct {
	Number            int
	URL               string
	Timestamp         time.Time
	EstimatedDuration time.Duration
}

// Job represents a Jenkins job.
type Job struct {
	Name       string
	FullPath   string
	Type       JobType
	BranchType BranchType
	LastBuild  *BuildRef
	Color      string
}

// Build represents a Jenkins build.
type Build struct {
	Number            int
	Status            BuildStatus
	Duration          time.Duration
	EstimatedDuration time.Duration
	Timestamp         time.Time
	Params            map[string]string
}

// BuildDetail extends Build with stage information.
type BuildDetail struct {
	Build
	Stages []Stage
}

// Stage represents a pipeline stage.
type Stage struct {
	Name     string
	Status   BuildStatus
	Duration time.Duration
	NodeIDs  []int // flow graph node IDs that have log output within this stage
	Depth    int   // nesting depth (0 = top-level)
	Parallel bool  // true if this stage's children run in parallel
}

// UserBuild is a build associated with its job path.
type UserBuild struct {
	JobPath     string
	Node        string // build agent name
	DisplayName string // full human-readable name, e.g. "FolderA » Pipeline #42"
	Cause       string // human-readable trigger description, e.g. "Started by user brecht"
	Build
}

// User represents a Jenkins user.
type User struct {
	ID             string
	FullName       string
	JenkinsVersion string
}

// JSON response structs (unexported) for unmarshalling Jenkins API responses.

type jsonJob struct {
	Class     string        `json:"_class"`
	Name      string        `json:"name"`
	URL       string        `json:"url"`
	Color     string        `json:"color"`
	LastBuild *jsonBuildRef `json:"lastBuild"`
	Jobs      []jsonJob     `json:"jobs"` // populated for multi-branch: the branch jobs
}

type jsonBuildRef struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	Timestamp         int64  `json:"timestamp"`
	EstimatedDuration int64  `json:"estimatedDuration"`
}

type jsonBuild struct {
	Class             string  `json:"_class"`
	Number            int     `json:"number"`
	Result            *string `json:"result"`
	Building          bool    `json:"building"`
	Duration          int64   `json:"duration"`
	EstimatedDuration int64   `json:"estimatedDuration"`
	Timestamp         int64   `json:"timestamp"`
	URL               string  `json:"url"`
}

type jsonBuildDetail struct {
	jsonBuild
	Actions []jsonAction `json:"actions"`
}

type jsonAction struct {
	Class      string          `json:"_class"`
	Parameters []jsonParameter `json:"parameters"`
	Causes     []jsonCause     `json:"causes"`
}

type jsonParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type jsonDefaultValue struct {
	Value interface{} `json:"value"`
}

type jsonParameterDefinition struct {
	Name                  string            `json:"name"`
	Type                  string            `json:"type"`
	DefaultParameterValue *jsonDefaultValue `json:"defaultParameterValue"`
	Description           string            `json:"description"`
	Choices               []string          `json:"choices"`
}

type jsonProperty struct {
	Class                string                    `json:"_class"`
	ParameterDefinitions []jsonParameterDefinition `json:"parameterDefinitions"`
}

type jsonJobDetail struct {
	Property []jsonProperty `json:"property"`
}

type jsonJobList struct {
	Jobs []jsonJob `json:"jobs"`
}

type jsonBuildList struct {
	Builds []jsonBuild `json:"builds"`
}

type jsonUser struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
}

type jsonStage struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	DurationMillis int64  `json:"durationMillis"`
}

type jsonWfDescribe struct {
	Stages []jsonStage `json:"stages"`
}

// Conversion helpers

func (j *jsonJob) toDomain(parentPath string) Job {
	job := Job{
		Name:       j.Name,
		Type:       ParseJobType(j.Class),
		BranchType: BranchTypeNone,
		Color:      j.Color,
	}
	if parentPath == "" {
		job.FullPath = j.Name
	} else {
		job.FullPath = parentPath + "/" + j.Name
	}
	if j.LastBuild != nil {
		job.LastBuild = &BuildRef{
			Number:            j.LastBuild.Number,
			URL:               j.LastBuild.URL,
			Timestamp:         millisToTime(j.LastBuild.Timestamp),
			EstimatedDuration: millisToDuration(j.LastBuild.EstimatedDuration),
		}
	}
	return job
}

func (j *jsonBuild) toDomain() Build {
	return Build{
		Number:            j.Number,
		Status:            ParseBuildStatus(j.Result, j.Building),
		Duration:          millisToDuration(j.Duration),
		EstimatedDuration: millisToDuration(j.EstimatedDuration),
		Timestamp:         millisToTime(j.Timestamp),
	}
}

func (j *jsonBuildDetail) toDomain() BuildDetail {
	bd := BuildDetail{
		Build: j.jsonBuild.toDomain(),
	}
	// Extract parameters from actions
	for _, action := range j.Actions {
		if len(action.Parameters) > 0 {
			bd.Params = make(map[string]string, len(action.Parameters))
			for _, p := range action.Parameters {
				bd.Params[p.Name] = p.Value
			}
			break
		}
	}
	return bd
}

func (j *jsonStage) toDomain() Stage {
	return Stage{
		Name:     j.Name,
		Status:   parsePipelineStatus(j.Status),
		Duration: millisToDuration(j.DurationMillis),
	}
}

// ParseJobType converts a Jenkins _class string to a JobType.
func ParseJobType(class string) JobType {
	switch {
	case strings.Contains(class, "Folder"):
		return JobTypeFolder
	case strings.Contains(class, "FreeStyleProject"):
		return JobTypeFreeStyle
	case strings.Contains(class, "WorkflowJob"):
		return JobTypePipeline
	case strings.Contains(class, "WorkflowMultiBranchProject") ||
		strings.Contains(class, "MultiBranchProject"):
		return JobTypeMultiBranch
	default:
		return JobTypeUnknown
	}
}

// ParseBuildStatus converts Jenkins result/building fields to a BuildStatus.
func ParseBuildStatus(result *string, building bool) BuildStatus {
	if building {
		return BuildStatusRunning
	}
	if result == nil {
		return BuildStatusRunning
	}
	switch *result {
	case "SUCCESS":
		return BuildStatusSuccess
	case "FAILURE":
		return BuildStatusFailed
	case "ABORTED":
		return BuildStatusAborted
	case "UNSTABLE":
		return BuildStatusUnstable
	case "NOT_BUILT":
		return BuildStatusNotBuilt
	default:
		return BuildStatusUnknown
	}
}

// ColorToBuildStatus converts a Jenkins color string to a BuildStatus.
func ColorToBuildStatus(color string) BuildStatus {
	// Strip "_anime" suffix (indicates running)
	if strings.HasSuffix(color, "_anime") {
		return BuildStatusRunning
	}
	switch color {
	case "blue":
		return BuildStatusSuccess
	case "red":
		return BuildStatusFailed
	case "aborted", "grey":
		return BuildStatusAborted
	case "yellow":
		return BuildStatusUnstable
	case "notbuilt", "disabled":
		return BuildStatusNotBuilt
	default:
		return BuildStatusUnknown
	}
}

// parsePipelineStatus converts a wfapi stage status string.
func parsePipelineStatus(status string) BuildStatus {
	switch status {
	case "SUCCESS":
		return BuildStatusSuccess
	case "FAILED":
		return BuildStatusFailed
	case "ABORTED":
		return BuildStatusAborted
	case "UNSTABLE":
		return BuildStatusUnstable
	case "NOT_EXECUTED":
		return BuildStatusNotBuilt
	case "IN_PROGRESS":
		return BuildStatusRunning
	default:
		return BuildStatusUnknown
	}
}

func millisToDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

func millisToTime(ms int64) time.Time {
	return time.UnixMilli(ms)
}

// parseParamType maps a Jenkins parameter class name to a ParamType.
func parseParamType(class string) ParamType {
	switch {
	case strings.Contains(class, "BooleanParameterDefinition"):
		return ParamTypeBool
	case strings.Contains(class, "ChoiceParameterDefinition"):
		return ParamTypeChoice
	default:
		return ParamTypeString
	}
}

// JobPathToURL converts a slash-separated job path to a Jenkins URL path.
// Segments are passed through url.PathEscape WITHOUT first unescaping, so that
// branch names stored by Jenkins with literal %2F (e.g. "feature%2Fbranch")
// become %252F in the URL — matching the double-encoded form Jenkins requires
// (browsers show this as %252F in the address bar).
// Plain names are encoded normally: spaces → %20, etc.
// e.g. "Code Private/feature%2Fbranch" -> "/job/Code%20Private/job/feature%252Fbranch"
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

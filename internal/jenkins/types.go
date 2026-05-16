package jenkins

import (
	"encoding/json"
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
	ParamTypeString   ParamType = "string"
	ParamTypeBool     ParamType = "bool"
	ParamTypeChoice   ParamType = "choice"
	ParamTypePassword ParamType = "password"
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
	Name         string
	FullPath     string
	Type         JobType
	BranchType   BranchType
	LastBuild    *BuildRef // primary branch last build
	Color        string    // primary branch color (may include _anime for running)
	LastAnyBuild *BuildRef // most recently built branch across all branches
	LastAnyColor string    // color of the most recently built branch
	RunningCount int       // number of currently running builds/branches
}

// Build represents a Jenkins build.
type Build struct {
	Number            int
	Status            BuildStatus
	Duration          time.Duration
	EstimatedDuration time.Duration
	Timestamp         time.Time
	Params            map[string]string
	TriggeredBy       string // user ID who triggered the build (empty if unknown)
	TriggeredByName   string // display name of the trigger user (e.g. "Brecht Derwael"; empty if unknown)
	Cause             string // human-readable trigger description (e.g. "Started by user Brecht")
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

// ProjectBuild is a build from a multibranch project, associated with its branch.
type ProjectBuild struct {
	Build
	BranchName string
	BranchPath string // full job path of the branch job (for API calls)
}

// UserBuild is a build associated with its job path.
type UserBuild struct {
	JobPath     string
	Node        string // build agent name
	DisplayName string // full human-readable name, e.g. "FolderA » Pipeline #42"
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
	DisplayPath string // human-readable name shown in Jenkins UI
	URL         string // absolute download URL
}

// JSON response structs (unexported) for unmarshalling Jenkins API responses.

type jsonJob struct {
	Class     string        `json:"_class"`
	Name      string        `json:"name"`
	FullName  string        `json:"fullName"`
	URL       string        `json:"url"`
	Color     string        `json:"color"`
	LastBuild *jsonBuildRef `json:"lastBuild"`
	Jobs      []jsonJob     `json:"jobs"`   // populated for multi-branch: the branch jobs
	Builds    []jsonBuild   `json:"builds"` // populated by ScanAllBuilds tree query
}

type jsonBuildRef struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	Timestamp         int64  `json:"timestamp"`
	EstimatedDuration int64  `json:"estimatedDuration"`
}

type jsonBuild struct {
	Class             string       `json:"_class"`
	Number            int          `json:"number"`
	Result            *string      `json:"result"`
	Building          bool         `json:"building"`
	Duration          int64        `json:"duration"`
	EstimatedDuration int64        `json:"estimatedDuration"`
	Timestamp         int64        `json:"timestamp"`
	URL               string       `json:"url"`
	Actions           []jsonAction `json:"actions"`
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

type jsonCause struct {
	Class            string `json:"_class"`
	ShortDescription string `json:"shortDescription"`
	UserName         string `json:"userName"`
	UserID           string `json:"userId"`
}

// extractUserID returns the userId of the first cause that has one.
func extractUserID(actions []jsonAction) string {
	for _, a := range actions {
		for _, c := range a.Causes {
			if c.UserID != "" {
				return c.UserID
			}
		}
	}
	return ""
}

// extractUserName returns the userName of the first cause that has one.
func extractUserName(actions []jsonAction) string {
	for _, a := range actions {
		for _, c := range a.Causes {
			if c.UserName != "" {
				return c.UserName
			}
		}
	}
	return ""
}

// extractCause returns the most informative shortDescription from a build's causes.
// It skips BranchEventCause ("Push event to branch X") and BranchIndexingCause
// ("Branch indexing") which carry no user identity, preferring causes like
// UserIdCause or GitLabWebHookCause that name the actual trigger.
func extractCause(actions []jsonAction) string {
	const branchEvent = "jenkins.branch.BranchEventCause"
	const branchIndex = "jenkins.branch.BranchIndexingCause"
	var fallback string
	for _, a := range actions {
		for _, c := range a.Causes {
			if c.ShortDescription == "" {
				continue
			}
			if c.Class == branchEvent || c.Class == branchIndex {
				if fallback == "" {
					fallback = c.ShortDescription
				}
				continue
			}
			return c.ShortDescription
		}
	}
	return fallback
}

// jsonParameter holds a build-time parameter value. Value is RawMessage
// because Jenkins serialises parameters using their native JSON type — bool
// for BooleanParameterValue, number for NumberParameterValue, etc. Decoding
// into a `string` field crashes the entire build-detail unmarshal and was
// the root cause of GetBuild silently failing for any pipeline with a
// non-string parameter.
type jsonParameter struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// stringValue returns the parameter value as a string regardless of its
// underlying JSON type. Strings are unquoted; bool/number/null fall back to
// their literal JSON form (e.g. "true", "42", "null").
func (p *jsonParameter) stringValue() string {
	if len(p.Value) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(p.Value, &s); err == nil {
		return s
	}
	return strings.Trim(string(p.Value), `"`)
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

type jsonTestCase struct {
	ClassName    string  `json:"className"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	Duration     float64 `json:"duration"`
	ErrorDetails string  `json:"errorDetails"`
}

type jsonTestSuite struct {
	Name     string         `json:"name"`
	Duration float64        `json:"duration"`
	Cases    []jsonTestCase `json:"cases"`
}

type jsonTestReport struct {
	Duration  float64         `json:"duration"`
	FailCount int             `json:"failCount"`
	PassCount int             `json:"passCount"`
	SkipCount int             `json:"skipCount"`
	Suites    []jsonTestSuite `json:"suites"`
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
		TriggeredBy:       extractUserID(j.Actions),
		TriggeredByName:   extractUserName(j.Actions),
		Cause:             extractCause(j.Actions),
	}
}

func (j *jsonBuildDetail) toDomain() BuildDetail {
	bd := BuildDetail{
		Build: j.jsonBuild.toDomain(),
	}
	for _, action := range j.Actions {
		if len(action.Parameters) > 0 && bd.Params == nil {
			bd.Params = make(map[string]string, len(action.Parameters))
			for i := range action.Parameters {
				p := &action.Parameters[i]
				bd.Params[p.Name] = p.stringValue()
			}
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

// ColorToLastCompletedStatus returns the last-completed BuildStatus from a
// Jenkins color string, stripping any "_anime" running suffix first so that a
// currently-running build reports its previous result rather than Running.
func ColorToLastCompletedStatus(color string) BuildStatus {
	return ColorToBuildStatus(strings.TrimSuffix(color, "_anime"))
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
	case strings.Contains(class, "PasswordParameterDefinition"):
		return ParamTypePassword
	case strings.Contains(class, "BooleanParameterDefinition"):
		return ParamTypeBool
	case strings.Contains(class, "ChoiceParameterDefinition"):
		return ParamTypeChoice
	default:
		return ParamTypeString
	}
}

func secondsToDuration(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

func parseTestStatus(s string) TestStatus {
	switch s {
	case "PASSED", "FIXED":
		return TestStatusPassed
	case "FAILED", "REGRESSION":
		return TestStatusFailed
	default:
		return TestStatusSkipped
	}
}

func (j *jsonTestReport) toDomain() *TestReport {
	r := &TestReport{
		Duration: secondsToDuration(j.Duration),
		Failed:   j.FailCount,
		Passed:   j.PassCount,
		Skipped:  j.SkipCount,
	}
	for _, s := range j.Suites {
		suite := TestSuite{
			Name:     s.Name,
			Duration: secondsToDuration(s.Duration),
		}
		for _, c := range s.Cases {
			suite.Cases = append(suite.Cases, TestCase{
				ClassName:    c.ClassName,
				Name:         c.Name,
				Status:       parseTestStatus(c.Status),
				Duration:     secondsToDuration(c.Duration),
				ErrorDetails: c.ErrorDetails,
			})
		}
		r.Suites = append(r.Suites, suite)
	}
	return r
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

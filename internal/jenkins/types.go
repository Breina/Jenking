package jenkins

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// JSON response structs (unexported) for unmarshalling Jenkins API responses.

type jsonJob struct {
	Class     string        `json:"_class"`
	Name      string        `json:"name"`
	FullName  string        `json:"fullName"`
	URL       string        `json:"url"`
	Color     string        `json:"color"`
	LastBuild *jsonBuildRef `json:"lastBuild"`
	Jobs      []jsonJob     `json:"jobs"`
	Builds    []jsonBuild   `json:"builds"`
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
	DisplayName       string       `json:"displayName"`
	Description       string       `json:"description"`
	Actions           []jsonAction `json:"actions"`
}

// buildName returns the custom display name, or "" when the build carries the
// default "#<number>" that Jenkins reports when no name was set.
func (j *jsonBuild) buildName() string {
	if j.DisplayName == "" || j.DisplayName == fmt.Sprintf("#%d", j.Number) {
		return ""
	}
	return j.DisplayName
}

type jsonBuildDetail struct {
	jsonBuild
	Actions []jsonAction `json:"actions"`
}

type jsonAction struct {
	Class      string          `json:"_class"`
	Parameters []jsonParameter `json:"parameters"`
	Causes     []jsonCause     `json:"causes"`
	Executions []jsonInputExec `json:"executions"`
}

// jsonInputExec is one entry of an InputAction.executions array, populated
// when a pipeline is paused at an `input` step.
type jsonInputExec struct {
	ID          string        `json:"id"`
	DisplayName string        `json:"displayName"`
	Settled     bool          `json:"settled"`
	Input       jsonInputStep `json:"input"`
}

type jsonInputStep struct {
	Message            string                    `json:"message"`
	OK                 string                    `json:"ok"`
	Cancel             string                    `json:"cancel"`
	Submitter          string                    `json:"submitter"`
	SubmitterParameter string                    `json:"submitterParameter"`
	Parameters         []jsonParameterDefinition `json:"parameters"`
}

type jsonCause struct {
	Class            string `json:"_class"`
	ShortDescription string `json:"shortDescription"`
	UserName         string `json:"userName"`
	UserID           string `json:"userId"`
}

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

// extractCause returns the most informative shortDescription. BranchEventCause
// and BranchIndexingCause carry no user identity, so they're treated as
// fallbacks behind causes like UserIdCause or GitLabWebHookCause.
//
// UserInterruption ranks last: it lives on InterruptedBuildAction (so it can be
// seen before the real CauseAction) and says who aborted, not what started the
// build. Its shortDescription also interpolates the raw Jenkins user id rather
// than the display name, which reads as a hex blob under SSO realms.
func extractCause(actions []jsonAction) string {
	const branchEvent = "jenkins.branch.BranchEventCause"
	const branchIndex = "jenkins.branch.BranchIndexingCause"
	const userInterrupt = "jenkins.model.CauseOfInterruption$UserInterruption"
	var fallback, lastResort string
	for _, a := range actions {
		for _, c := range a.Causes {
			if c.ShortDescription == "" {
				continue
			}
			switch c.Class {
			case userInterrupt:
				if lastResort == "" {
					lastResort = c.ShortDescription
				}
			case branchEvent, branchIndex:
				if fallback == "" {
					fallback = c.ShortDescription
				}
			default:
				return c.ShortDescription
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return lastResort
}

// jsonParameter — Value is RawMessage because Jenkins serialises parameter
// values using their native JSON type (bool, number, string). Decoding into a
// `string` crashes the whole build-detail unmarshal for any non-string param.
type jsonParameter struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

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
	Class                 string            `json:"_class"`
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

type jsonView struct {
	Class string `json:"_class"`
	Name  string `json:"name"`
	URL   string `json:"url"`
}

// jsonViewContainer is any object that owns views: the root, a folder, or a
// user's my-views collection.
type jsonViewContainer struct {
	Views       []jsonView `json:"views"`
	PrimaryView *jsonView  `json:"primaryView"`
}

type jsonBuildList struct {
	Builds []jsonBuild `json:"builds"`
}

type jsonUser struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
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

// Conversion helpers — adapter-side parsing into domain types.

func (j *jsonJob) toDomain(parentPath string) jmodel.Job {
	job := jmodel.Job{
		Name:       j.Name,
		Type:       ParseJobType(j.Class),
		BranchType: jmodel.BranchTypeNone,
		Color:      j.Color,
		Disabled:   j.Color == "disabled",
	}
	if parentPath == "" {
		job.FullPath = j.Name
	} else {
		job.FullPath = parentPath + "/" + j.Name
	}
	if j.LastBuild != nil {
		job.LastBuild = &jmodel.BuildRef{
			Number:            j.LastBuild.Number,
			URL:               j.LastBuild.URL,
			Timestamp:         millisToTime(j.LastBuild.Timestamp),
			EstimatedDuration: millisToDuration(j.LastBuild.EstimatedDuration),
		}
	}
	return job
}

func (j *jsonBuild) toDomain() jmodel.Build {
	return jmodel.Build{
		Number:            j.Number,
		Status:            ParseBuildStatus(j.Result, j.Building),
		Duration:          millisToDuration(j.Duration),
		EstimatedDuration: millisToDuration(j.EstimatedDuration),
		Timestamp:         millisToTime(j.Timestamp),
		TriggeredBy:       extractUserID(j.Actions),
		TriggeredByName:   extractUserName(j.Actions),
		Cause:             extractCause(j.Actions),
		Name:              j.buildName(),
		Description:       j.Description,
	}
}

func (j *jsonBuildDetail) toDomain() jmodel.BuildDetail {
	bd := jmodel.BuildDetail{
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
		if strings.Contains(action.Class, "InputAction") && len(action.Executions) > 0 {
			for i := range action.Executions {
				e := &action.Executions[i]
				if e.Settled || e.ID == "" {
					continue
				}
				bd.PendingInputs = append(bd.PendingInputs, e.toDomain())
			}
		}
	}
	return bd
}

func (e *jsonInputExec) toDomain() jmodel.PendingInput {
	pi := jmodel.PendingInput{
		ID:                 e.ID,
		Message:            e.Input.Message,
		OkLabel:            e.Input.OK,
		AbortLabel:         e.Input.Cancel,
		Submitter:          e.Input.Submitter,
		SubmitterParameter: e.Input.SubmitterParameter,
	}
	if pi.Message == "" {
		pi.Message = e.DisplayName
	}
	if pi.OkLabel == "" {
		pi.OkLabel = "Proceed"
	}
	if pi.AbortLabel == "" {
		pi.AbortLabel = "Abort"
	}
	pi.Parameters = parseParamDefs(e.Input.Parameters)
	return pi
}

// parseParamDefs converts adapter-side parameter definitions into the domain
// shape, handling both the `type` field (job parameter definitions) and the
// `_class` field (input step parameter definitions).
func parseParamDefs(defs []jsonParameterDefinition) []jmodel.ParameterDefinition {
	if len(defs) == 0 {
		return nil
	}
	out := make([]jmodel.ParameterDefinition, 0, len(defs))
	for _, pd := range defs {
		discriminator := pd.Type
		if discriminator == "" {
			discriminator = pd.Class
		}
		def := ""
		if pd.DefaultParameterValue != nil && pd.DefaultParameterValue.Value != nil {
			def = fmt.Sprintf("%v", pd.DefaultParameterValue.Value)
		}
		out = append(out, jmodel.ParameterDefinition{
			Name:        pd.Name,
			Type:        parseParamType(discriminator),
			Default:     def,
			Description: pd.Description,
			Choices:     pd.Choices,
		})
	}
	return out
}

func ParseJobType(class string) jmodel.JobType {
	switch {
	case strings.Contains(class, "Folder"):
		return jmodel.JobTypeFolder
	case strings.Contains(class, "FreeStyleProject"):
		return jmodel.JobTypeFreeStyle
	case strings.Contains(class, "WorkflowJob"):
		return jmodel.JobTypePipeline
	case strings.Contains(class, "WorkflowMultiBranchProject") ||
		strings.Contains(class, "MultiBranchProject"):
		return jmodel.JobTypeMultiBranch
	default:
		return jmodel.JobTypeUnknown
	}
}

func ParseBuildStatus(result *string, building bool) jmodel.BuildStatus {
	if building {
		return jmodel.BuildStatusRunning
	}
	if result == nil {
		return jmodel.BuildStatusRunning
	}
	switch *result {
	case "SUCCESS":
		return jmodel.BuildStatusSuccess
	case "FAILURE":
		return jmodel.BuildStatusFailed
	case "ABORTED":
		return jmodel.BuildStatusAborted
	case "UNSTABLE":
		return jmodel.BuildStatusUnstable
	case "NOT_BUILT":
		return jmodel.BuildStatusNotBuilt
	default:
		return jmodel.BuildStatusUnknown
	}
}

// ColorToLastCompletedStatus strips any "_anime" running suffix so a
// currently-running build reports its previous result rather than Running.
func ColorToLastCompletedStatus(color string) jmodel.BuildStatus {
	return ColorToBuildStatus(strings.TrimSuffix(color, "_anime"))
}

func ColorToBuildStatus(color string) jmodel.BuildStatus {
	if strings.HasSuffix(color, "_anime") {
		return jmodel.BuildStatusRunning
	}
	switch color {
	case "blue":
		return jmodel.BuildStatusSuccess
	case "red":
		return jmodel.BuildStatusFailed
	case "aborted", "grey":
		return jmodel.BuildStatusAborted
	case "yellow":
		return jmodel.BuildStatusUnstable
	case "notbuilt", "disabled":
		return jmodel.BuildStatusNotBuilt
	default:
		return jmodel.BuildStatusUnknown
	}
}

func millisToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
func millisToTime(ms int64) time.Time         { return time.UnixMilli(ms) }

func parseParamType(class string) jmodel.ParamType {
	switch {
	case strings.Contains(class, "PasswordParameterDefinition"):
		return jmodel.ParamTypePassword
	case strings.Contains(class, "BooleanParameterDefinition"):
		return jmodel.ParamTypeBool
	case strings.Contains(class, "ChoiceParameterDefinition"):
		return jmodel.ParamTypeChoice
	default:
		return jmodel.ParamTypeString
	}
}

func secondsToDuration(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

func parseTestStatus(s string) jmodel.TestStatus {
	switch s {
	case "PASSED", "FIXED":
		return jmodel.TestStatusPassed
	case "FAILED", "REGRESSION":
		return jmodel.TestStatusFailed
	default:
		return jmodel.TestStatusSkipped
	}
}

func (j *jsonTestReport) toDomain() *jmodel.TestReport {
	r := &jmodel.TestReport{
		Duration: secondsToDuration(j.Duration),
		Failed:   j.FailCount,
		Passed:   j.PassCount,
		Skipped:  j.SkipCount,
	}
	for _, s := range j.Suites {
		suite := jmodel.TestSuite{
			Name:     s.Name,
			Duration: secondsToDuration(s.Duration),
		}
		for _, c := range s.Cases {
			suite.Cases = append(suite.Cases, jmodel.TestCase{
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

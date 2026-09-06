// Package dto holds the wire-format data-transfer types shared by the CLI
// (`-o json`/`yaml`) and the MCP server. Keeping a single set of tagged
// structs and their jmodel converters here guarantees that both surfaces emit
// byte-identical JSON — one documented contract.
//
// dto is pure mapping: it takes domain (jmodel) values and produces tagged
// output structs. It performs no I/O and never imports an adapter or the tui.
package dto

import (
	"time"

	"github.com/Breina/Jenking/internal/app/engine"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
	"github.com/Breina/Jenking/internal/navmsg"
)

// TriggerResult is the result of `jenking trigger` (and `--wait`).
type TriggerResult struct {
	JobPath     string `json:"job_path" yaml:"job_path"`
	QueueID     int64  `json:"queue_id" yaml:"queue_id"`
	BuildNumber int    `json:"build_number,omitempty" yaml:"build_number,omitempty"`
	Status      string `json:"status,omitempty" yaml:"status,omitempty"`
}

// Input is a pending pipeline input step.
type Input struct {
	ID         string  `json:"id" yaml:"id"`
	Message    string  `json:"message" yaml:"message"`
	OkLabel    string  `json:"ok_label,omitempty" yaml:"ok_label,omitempty"`
	AbortLabel string  `json:"abort_label,omitempty" yaml:"abort_label,omitempty"`
	Submitter  string  `json:"submitter,omitempty" yaml:"submitter,omitempty"`
	Parameters []Param `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// InputResult is the result of `jenking approve|reject`.
type InputResult struct {
	JobPath     string `json:"job_path" yaml:"job_path"`
	BuildNumber int    `json:"build_number" yaml:"build_number"`
	InputID     string `json:"input_id" yaml:"input_id"`
	Action      string `json:"action" yaml:"action"`
}

// BuildDetail is the single-build view of `jenking build`.
type BuildDetail struct {
	Build         `yaml:",inline"`
	JobPath       string            `json:"job_path" yaml:"job_path"`
	Parameters    map[string]string `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	PendingInputs []Input           `json:"pending_inputs,omitempty" yaml:"pending_inputs,omitempty"`
}

// Node is a Jenkins node (agent/controller) for `jenking nodes`.
type Node struct {
	Name          string   `json:"name" yaml:"name"`
	Offline       bool     `json:"offline" yaml:"offline"`
	OfflineCause  string   `json:"offline_cause,omitempty" yaml:"offline_cause,omitempty"`
	Executors     int      `json:"executors" yaml:"executors"`
	BusyExecutors int      `json:"busy_executors" yaml:"busy_executors"`
	FreeDiskBytes int64    `json:"free_disk_bytes,omitempty" yaml:"free_disk_bytes,omitempty"`
	FreeMemBytes  int64    `json:"free_mem_bytes,omitempty" yaml:"free_mem_bytes,omitempty"`
	ResponseMs    int64    `json:"response_ms,omitempty" yaml:"response_ms,omitempty"`
	Labels        []string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// Job is a Jenkins job/folder listing entry.
//
// FullPath is the canonical, machine-facing job path: slash-separated segments,
// each percent-encoded, so a job or branch whose own name contains a slash stays
// one segment. It is what every other command and tool takes as job_path, and it
// matches the job_path emitted by the running/queue listings. DisplayPath is the
// decoded, human-readable rendering of the same path — never feed it back in.
type Job struct {
	Name         string `json:"name" yaml:"name"`
	FullPath     string `json:"full_path" yaml:"full_path"`
	DisplayPath  string `json:"display_path,omitempty" yaml:"display_path,omitempty"`
	Type         string `json:"type" yaml:"type"`
	BranchType   string `json:"branch_type,omitempty" yaml:"branch_type,omitempty"`
	Disabled     bool   `json:"disabled,omitempty" yaml:"disabled,omitempty"`
	RunningCount int    `json:"running_count,omitempty" yaml:"running_count,omitempty"`
	LastBuildNum int    `json:"last_build_num,omitempty" yaml:"last_build_num,omitempty"`
}

// View is a Jenkins view: a saved job filter on a container.
type View struct {
	Name      string `json:"name" yaml:"name"`
	Kind      string `json:"kind" yaml:"kind"`
	OwnerPath string `json:"owner_path,omitempty" yaml:"owner_path,omitempty"`
	Personal  bool   `json:"personal,omitempty" yaml:"personal,omitempty"`
	Primary   bool   `json:"primary,omitempty" yaml:"primary,omitempty"`
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
}

// Build is a single build record.
type Build struct {
	Number          int    `json:"number" yaml:"number"`
	Status          string `json:"status" yaml:"status"`
	DurationMs      int64  `json:"duration_ms" yaml:"duration_ms"`
	TimestampUnix   int64  `json:"timestamp" yaml:"timestamp"`
	TriggeredBy     string `json:"triggered_by,omitempty" yaml:"triggered_by,omitempty"`
	TriggeredByName string `json:"triggered_by_name,omitempty" yaml:"triggered_by_name,omitempty"`
	Cause           string `json:"cause,omitempty" yaml:"cause,omitempty"`
}

// ProjectBuild is a build within a multibranch project, tagged with its branch.
type ProjectBuild struct {
	Build      `yaml:",inline"`
	BranchName string `json:"branch_name" yaml:"branch_name"`
	BranchPath string `json:"branch_path" yaml:"branch_path"`
}

// UserBuild is a build with its owning job path (running/all-builds views).
type UserBuild struct {
	JobPath     string `json:"job_path" yaml:"job_path"`
	DisplayName string `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Node        string `json:"node,omitempty" yaml:"node,omitempty"`
	Build       Build  `json:"build" yaml:"build"`
}

// QueueItem is one entry in the Jenkins build queue.
type QueueItem struct {
	ID           int64  `json:"id" yaml:"id"`
	Kind         string `json:"kind" yaml:"kind"` // "build" or "scan"
	JobPath      string `json:"job_path" yaml:"job_path"`
	State        string `json:"state" yaml:"state"`
	Why          string `json:"why,omitempty" yaml:"why,omitempty"`
	QueuedSinceS int64  `json:"queued_since_unix" yaml:"queued_since_unix"`
	Cause        string `json:"cause,omitempty" yaml:"cause,omitempty"`
}

// Stage is a pipeline stage.
type Stage struct {
	Name       string `json:"name" yaml:"name"`
	Status     string `json:"status" yaml:"status"`
	DurationMs int64  `json:"duration_ms" yaml:"duration_ms"`
	Depth      int    `json:"depth" yaml:"depth"`
	Parallel   bool   `json:"parallel,omitempty" yaml:"parallel,omitempty"`
}

// Artifact is a build artifact reference.
type Artifact struct {
	DisplayPath string `json:"display_path" yaml:"display_path"`
	URL         string `json:"url" yaml:"url"`
}

// Param is a job parameter definition.
type Param struct {
	Name        string   `json:"name" yaml:"name"`
	Type        string   `json:"type" yaml:"type"`
	Default     string   `json:"default,omitempty" yaml:"default,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Choices     []string `json:"choices,omitempty" yaml:"choices,omitempty"`
}

// User is the authenticated Jenkins user.
type User struct {
	ID             string `json:"id" yaml:"id"`
	FullName       string `json:"full_name" yaml:"full_name"`
	JenkinsVersion string `json:"jenkins_version" yaml:"jenkins_version"`
}

// Meta is one flattened raw-metadata entry (path + value).
type Meta struct {
	Path  string `json:"path" yaml:"path"`
	Value string `json:"value" yaml:"value"`
}

// TestCase is a single JUnit test case.
type TestCase struct {
	ClassName    string `json:"class_name" yaml:"class_name"`
	Name         string `json:"name" yaml:"name"`
	Status       string `json:"status" yaml:"status"`
	DurationMs   int64  `json:"duration_ms" yaml:"duration_ms"`
	ErrorDetails string `json:"error_details,omitempty" yaml:"error_details,omitempty"`
}

// TestSuite groups test cases.
type TestSuite struct {
	Name       string     `json:"name" yaml:"name"`
	DurationMs int64      `json:"duration_ms" yaml:"duration_ms"`
	Cases      []TestCase `json:"cases" yaml:"cases"`
}

// TestReport is a build's JUnit test report.
type TestReport struct {
	DurationMs int64       `json:"duration_ms" yaml:"duration_ms"`
	Failed     int         `json:"failed" yaml:"failed"`
	Passed     int         `json:"passed" yaml:"passed"`
	Skipped    int         `json:"skipped" yaml:"skipped"`
	Suites     []TestSuite `json:"suites" yaml:"suites"`
}

// Change is a single SCM commit associated with a build.
type Change struct {
	CommitID      string   `json:"commit_id" yaml:"commit_id"`
	Author        string   `json:"author,omitempty" yaml:"author,omitempty"`
	AuthorEmail   string   `json:"author_email,omitempty" yaml:"author_email,omitempty"`
	Message       string   `json:"message" yaml:"message"`
	Timestamp     int64    `json:"timestamp" yaml:"timestamp"`
	AffectedPaths []string `json:"affected_paths,omitempty" yaml:"affected_paths,omitempty"`
}

// CommitHit records a build whose change set contains a searched commit.
type CommitHit struct {
	BuildNumber int    `json:"build_number" yaml:"build_number"`
	CommitID    string `json:"commit_id" yaml:"commit_id"`
}

// JobMatch is a job whose SCM URL matched a resolve query.
type JobMatch struct {
	JobPath string `json:"job_path" yaml:"job_path"`
	Branch  string `json:"branch,omitempty" yaml:"branch,omitempty"`
	SCMURL  string `json:"scm_url,omitempty" yaml:"scm_url,omitempty"`
}

// ---- Converters ----

func ToChange(c jmodel.Change) Change {
	var ts int64
	if !c.Timestamp.IsZero() {
		ts = c.Timestamp.Unix()
	}
	return Change{
		CommitID:      c.CommitID,
		Author:        c.Author,
		AuthorEmail:   c.AuthorEmail,
		Message:       c.Message,
		Timestamp:     ts,
		AffectedPaths: c.AffectedPaths,
	}
}

func ToView(v jmodel.JenkinsView) View {
	return View{
		Name:      v.Name,
		Kind:      viewKindName(v.Kind),
		OwnerPath: v.OwnerPath,
		Personal:  v.Personal,
		Primary:   v.IsPrimary,
		URL:       v.URL,
	}
}

// viewKindName renders a ViewKind for machine-readable output. Anything a
// plugin contributes reports as "other" — we list it without pretending to
// know what it is.
func viewKindName(k jmodel.ViewKind) string {
	switch k {
	case jmodel.ViewAll:
		return "all"
	case jmodel.ViewList:
		return "list"
	case jmodel.ViewMy:
		return "my"
	default:
		return "other"
	}
}

func ToJobMatch(m jmodel.JobSCMMatch) JobMatch {
	return JobMatch{JobPath: m.JobPath, Branch: m.Branch, SCMURL: m.SCMURL}
}

func ToCommitHit(h jmodel.BuildCommitHit) CommitHit {
	return CommitHit{BuildNumber: h.BuildNumber, CommitID: h.CommitID}
}

func ToInput(in jmodel.PendingInput) Input {
	params := make([]Param, len(in.Parameters))
	for i, p := range in.Parameters {
		params[i] = ToParam(p)
	}
	return Input{
		ID:         in.ID,
		Message:    in.Message,
		OkLabel:    in.OkLabel,
		AbortLabel: in.AbortLabel,
		Submitter:  in.Submitter,
		Parameters: params,
	}
}

func ToBuildDetail(jobPath string, d jmodel.BuildDetail) BuildDetail {
	inputs := make([]Input, len(d.PendingInputs))
	for i, in := range d.PendingInputs {
		inputs[i] = ToInput(in)
	}
	return BuildDetail{
		Build:         ToBuild(d.Build),
		JobPath:       jobPath,
		Parameters:    d.Params,
		PendingInputs: inputs,
	}
}

func ToNode(n jmodel.Node) Node {
	return Node{
		Name:          n.Name,
		Offline:       n.Offline,
		OfflineCause:  n.OfflineCause,
		Executors:     n.NumExecutors,
		BusyExecutors: n.BusyExecutors,
		FreeDiskBytes: n.FreeDiskBytes,
		FreeMemBytes:  n.FreeMemBytes,
		ResponseMs:    n.ResponseMs,
		Labels:        n.Labels,
	}
}

func ToJob(j jmodel.Job) Job {
	// FullPath stays in the canonical encoded form so it round-trips as the
	// job_path of any other command or tool; a job name containing a slash
	// (a branch, or a project named after its SCM path) would otherwise be
	// indistinguishable from a folder boundary once decoded. The decoded form
	// is offered alongside it, for reading only.
	decoded := navmsg.DecodePath(j.FullPath)
	o := Job{
		Name:         navmsg.DecodeName(j.Name),
		FullPath:     j.FullPath,
		Type:         string(j.Type),
		BranchType:   string(j.BranchType),
		Disabled:     j.Disabled,
		RunningCount: j.RunningCount,
	}
	if decoded != j.FullPath {
		o.DisplayPath = decoded
	}
	if j.LastBuild != nil {
		o.LastBuildNum = j.LastBuild.Number
	}
	return o
}

func ToBuild(b jmodel.Build) Build {
	return Build{
		Number:          b.Number,
		Status:          string(b.Status),
		DurationMs:      b.Duration.Milliseconds(),
		TimestampUnix:   b.Timestamp.Unix(),
		TriggeredBy:     b.TriggeredBy,
		TriggeredByName: b.TriggeredByName,
		Cause:           b.Cause,
	}
}

func ToProjectBuild(p jmodel.ProjectBuild) ProjectBuild {
	return ProjectBuild{
		Build:      ToBuild(p.Build),
		BranchName: p.BranchName,
		BranchPath: p.BranchPath,
	}
}

func ToUserBuild(u jmodel.UserBuild) UserBuild {
	return UserBuild{
		JobPath:     u.JobPath,
		DisplayName: u.DisplayName,
		Node:        u.Node,
		Build:       ToBuild(u.Build),
	}
}

func queueState(q jmodel.QueueItem) string {
	switch {
	case q.Stuck:
		return "stuck"
	case q.Blocked:
		return "blocked"
	case q.Pending:
		return "pending"
	default:
		return "buildable"
	}
}

func ToQueueItem(q jmodel.QueueItem) QueueItem {
	return QueueItem{
		ID:           q.ID,
		Kind:         string(q.Kind),
		JobPath:      q.JobPath,
		State:        queueState(q),
		Why:          q.Why,
		QueuedSinceS: q.InQueueSince.Unix(),
		Cause:        q.Cause,
	}
}

func ToStage(s jmodel.Stage) Stage {
	return Stage{
		Name:       s.Name,
		Status:     string(s.Status),
		DurationMs: s.Duration.Milliseconds(),
		Depth:      s.Depth,
		Parallel:   s.Parallel,
	}
}

func ToUser(u jmodel.User) User {
	return User{ID: u.ID, FullName: u.FullName, JenkinsVersion: u.JenkinsVersion}
}

func ToParam(p jmodel.ParameterDefinition) Param {
	return Param{
		Name:        p.Name,
		Type:        string(p.Type),
		Default:     p.Default,
		Description: p.Description,
		Choices:     p.Choices,
	}
}

func ToArtifact(a jmodel.Artifact) Artifact {
	return Artifact{DisplayPath: a.DisplayPath, URL: a.URL}
}

func ToMeta(e jmodel.MetaEntry) Meta {
	return Meta{Path: e.Path, Value: e.Value}
}

func ToTestCase(c jmodel.TestCase) TestCase {
	return TestCase{
		ClassName:    c.ClassName,
		Name:         c.Name,
		Status:       string(c.Status),
		DurationMs:   c.Duration.Milliseconds(),
		ErrorDetails: c.ErrorDetails,
	}
}

func ToTestSuite(s jmodel.TestSuite) TestSuite {
	cases := make([]TestCase, len(s.Cases))
	for i, c := range s.Cases {
		cases[i] = ToTestCase(c)
	}
	return TestSuite{Name: s.Name, DurationMs: s.Duration.Milliseconds(), Cases: cases}
}

func ToTestReport(r jmodel.TestReport) TestReport {
	suites := make([]TestSuite, len(r.Suites))
	for i, s := range r.Suites {
		suites[i] = ToTestSuite(s)
	}
	return TestReport{
		DurationMs: r.Duration.Milliseconds(),
		Failed:     r.Failed,
		Passed:     r.Passed,
		Skipped:    r.Skipped,
		Suites:     suites,
	}
}

// PipelineParam is one parameter of a pipeline step's signature.
type PipelineParam struct {
	Name  string `json:"name" yaml:"name"`
	Type  string `json:"type,omitempty" yaml:"type,omitempty"`
	Named bool   `json:"named,omitempty" yaml:"named,omitempty"`
}

// PipelineStep is a callable pipeline step (sh, git, or a library-defined var).
// In list mode only Name and Signature are populated; Params/ReturnType/Doc are
// filled when a single symbol's full detail is requested.
type PipelineStep struct {
	Name       string          `json:"name" yaml:"name"`
	Signature  string          `json:"signature,omitempty" yaml:"signature,omitempty"`
	ReturnType string          `json:"return_type,omitempty" yaml:"return_type,omitempty"`
	Params     []PipelineParam `json:"params,omitempty" yaml:"params,omitempty"`
	Doc        string          `json:"doc,omitempty" yaml:"doc,omitempty"`
}

// PipelineMember is one callable or property on a pipeline global.
type PipelineMember struct {
	Name      string `json:"name" yaml:"name"`
	Signature string `json:"signature,omitempty" yaml:"signature,omitempty"`
	Doc       string `json:"doc,omitempty" yaml:"doc,omitempty"`
}

// PipelineGlobal is a globally accessible pipeline variable (env, currentBuild,
// a library-provided global). Doc/Members are populated only in detail mode.
type PipelineGlobal struct {
	Name    string           `json:"name" yaml:"name"`
	Doc     string           `json:"doc,omitempty" yaml:"doc,omitempty"`
	Members []PipelineMember `json:"members,omitempty" yaml:"members,omitempty"`
}

// LintIssue is one problem reported by the declarative pipeline validator.
type LintIssue struct {
	Line    int    `json:"line,omitempty" yaml:"line,omitempty"`
	Col     int    `json:"col,omitempty" yaml:"col,omitempty"`
	Message string `json:"message" yaml:"message"`
}

// ToPipelineStepName returns a step with only its name and signature — the
// list-mode projection used to keep symbol listings compact.
func ToPipelineStepName(s pipelinesyntax.Step) PipelineStep {
	return PipelineStep{Name: s.Name, Signature: s.Signature()}
}

// ToPipelineStepDetail returns a step's full detail (params, return type, doc).
func ToPipelineStepDetail(s pipelinesyntax.Step) PipelineStep {
	params := make([]PipelineParam, len(s.Params))
	for i, p := range s.Params {
		params[i] = PipelineParam{Name: p.Name, Type: p.Type, Named: p.Named}
	}
	return PipelineStep{
		Name:       s.Name,
		Signature:  s.Signature(),
		ReturnType: s.ReturnType,
		Params:     params,
		Doc:        s.Doc,
	}
}

// ToPipelineGlobalName returns a global with only its name.
func ToPipelineGlobalName(g pipelinesyntax.GlobalVar) PipelineGlobal {
	return PipelineGlobal{Name: g.Name}
}

// ToPipelineGlobalDetail returns a global's full detail (doc + members).
func ToPipelineGlobalDetail(g pipelinesyntax.GlobalVar) PipelineGlobal {
	members := make([]PipelineMember, len(g.Members))
	for i, m := range g.Members {
		members[i] = PipelineMember{Name: m.Name, Signature: m.Signature, Doc: m.Doc}
	}
	return PipelineGlobal{Name: g.Name, Doc: g.Doc, Members: members}
}

// ToLintIssue maps a domain validation issue to its wire form.
func ToLintIssue(i jmodel.ValidationIssue) LintIssue {
	return LintIssue{Line: i.Line, Col: i.Col, Message: i.Message}
}

// QueueWaitBin is one wait bucket's totals in the queue-history summary.
type QueueWaitBin struct {
	Label    string         `json:"label" yaml:"label"`
	Total    int            `json:"total" yaml:"total"`
	ByReason map[string]int `json:"by_reason" yaml:"by_reason"`
}

// QueueHistory is the aggregated queue-wait summary over a window.
type QueueHistory struct {
	WindowMinutes int            `json:"window_minutes" yaml:"window_minutes"`
	Samples       int            `json:"samples" yaml:"samples"`
	From          time.Time      `json:"from,omitempty" yaml:"from,omitempty"`
	To            time.Time      `json:"to,omitempty" yaml:"to,omitempty"`
	MaxRunning    int            `json:"max_running" yaml:"max_running"`
	MaxQueued     int            `json:"max_queued" yaml:"max_queued"`
	Bins          []QueueWaitBin `json:"bins,omitempty" yaml:"bins,omitempty"`
}

// ToQueueHistory maps the engine's aggregate to its wire form.
func ToQueueHistory(h engine.QueueHistory) QueueHistory {
	bins := make([]QueueWaitBin, len(h.Bins))
	for i, b := range h.Bins {
		bins[i] = QueueWaitBin{Label: b.Label, Total: b.Total, ByReason: b.ByReason}
	}
	return QueueHistory{
		WindowMinutes: h.WindowMinutes,
		Samples:       h.Samples,
		From:          h.From,
		To:            h.To,
		MaxRunning:    h.MaxRunning,
		MaxQueued:     h.MaxQueued,
		Bins:          bins,
	}
}

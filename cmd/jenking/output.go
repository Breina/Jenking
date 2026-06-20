package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// printFormatted writes v as JSON/YAML or calls printTable for text output.
func printFormatted(v any, printTable func() error) error {
	switch outputFlag {
	case "json":
		return printJSON(os.Stdout, v)
	case "yaml":
		return printYAML(os.Stdout, v)
	default:
		return printTable()
	}
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printYAML(w io.Writer, v any) error {
	// Marshal to JSON first, then convert to YAML via round-trip through any.
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var tmp any
	if err := json.Unmarshal(raw, &tmp); err != nil {
		return err
	}
	return yaml.NewEncoder(w).Encode(tmp)
}

func newTab(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func fmtDur(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m == 0 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

// ---- Output types (used for JSON/YAML serialization) ----

type outJob struct {
	Name         string `json:"name" yaml:"name"`
	FullPath     string `json:"full_path" yaml:"full_path"`
	Type         string `json:"type" yaml:"type"`
	BranchType   string `json:"branch_type,omitempty" yaml:"branch_type,omitempty"`
	Disabled     bool   `json:"disabled,omitempty" yaml:"disabled,omitempty"`
	RunningCount int    `json:"running_count,omitempty" yaml:"running_count,omitempty"`
	LastBuildNum int    `json:"last_build_num,omitempty" yaml:"last_build_num,omitempty"`
}

type outBuild struct {
	Number          int    `json:"number" yaml:"number"`
	Status          string `json:"status" yaml:"status"`
	DurationMs      int64  `json:"duration_ms" yaml:"duration_ms"`
	TimestampUnix   int64  `json:"timestamp" yaml:"timestamp"`
	TriggeredBy     string `json:"triggered_by,omitempty" yaml:"triggered_by,omitempty"`
	TriggeredByName string `json:"triggered_by_name,omitempty" yaml:"triggered_by_name,omitempty"`
	Cause           string `json:"cause,omitempty" yaml:"cause,omitempty"`
}

type outProjectBuild struct {
	outBuild   `yaml:",inline"`
	BranchName string `json:"branch_name" yaml:"branch_name"`
	BranchPath string `json:"branch_path" yaml:"branch_path"`
}

type outUserBuild struct {
	JobPath     string   `json:"job_path" yaml:"job_path"`
	DisplayName string   `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Node        string   `json:"node,omitempty" yaml:"node,omitempty"`
	Build       outBuild `json:"build" yaml:"build"`
}

type outQueueItem struct {
	ID           int64  `json:"id" yaml:"id"`
	JobPath      string `json:"job_path" yaml:"job_path"`
	State        string `json:"state" yaml:"state"`
	Why          string `json:"why,omitempty" yaml:"why,omitempty"`
	QueuedSinceS int64  `json:"queued_since_unix" yaml:"queued_since_unix"`
	Cause        string `json:"cause,omitempty" yaml:"cause,omitempty"`
}

type outStage struct {
	Name       string `json:"name" yaml:"name"`
	Status     string `json:"status" yaml:"status"`
	DurationMs int64  `json:"duration_ms" yaml:"duration_ms"`
	Depth      int    `json:"depth" yaml:"depth"`
	Parallel   bool   `json:"parallel,omitempty" yaml:"parallel,omitempty"`
}

type outArtifact struct {
	DisplayPath string `json:"display_path" yaml:"display_path"`
	URL         string `json:"url" yaml:"url"`
}

type outParam struct {
	Name        string   `json:"name" yaml:"name"`
	Type        string   `json:"type" yaml:"type"`
	Default     string   `json:"default,omitempty" yaml:"default,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Choices     []string `json:"choices,omitempty" yaml:"choices,omitempty"`
}

type outUser struct {
	ID             string `json:"id" yaml:"id"`
	FullName       string `json:"full_name" yaml:"full_name"`
	JenkinsVersion string `json:"jenkins_version" yaml:"jenkins_version"`
}

type outMeta struct {
	Path  string `json:"path" yaml:"path"`
	Value string `json:"value" yaml:"value"`
}

type outTestCase struct {
	ClassName    string `json:"class_name" yaml:"class_name"`
	Name         string `json:"name" yaml:"name"`
	Status       string `json:"status" yaml:"status"`
	DurationMs   int64  `json:"duration_ms" yaml:"duration_ms"`
	ErrorDetails string `json:"error_details,omitempty" yaml:"error_details,omitempty"`
}

type outTestSuite struct {
	Name       string        `json:"name" yaml:"name"`
	DurationMs int64         `json:"duration_ms" yaml:"duration_ms"`
	Cases      []outTestCase `json:"cases" yaml:"cases"`
}

type outTestReport struct {
	DurationMs int64          `json:"duration_ms" yaml:"duration_ms"`
	Failed     int            `json:"failed" yaml:"failed"`
	Passed     int            `json:"passed" yaml:"passed"`
	Skipped    int            `json:"skipped" yaml:"skipped"`
	Suites     []outTestSuite `json:"suites" yaml:"suites"`
}

// ---- Converters ----

func toOutJob(j jmodel.Job) outJob {
	// Decode %2F so the emitted name/path are usable as input to other
	// commands (e.g. `builds`); the encoded form is a Jenkins API detail and
	// is never meant to be user-facing.
	o := outJob{
		Name:         navmsg.DecodeName(j.Name),
		FullPath:     navmsg.DecodePath(j.FullPath),
		Type:         string(j.Type),
		BranchType:   string(j.BranchType),
		Disabled:     j.Disabled,
		RunningCount: j.RunningCount,
	}
	if j.LastBuild != nil {
		o.LastBuildNum = j.LastBuild.Number
	}
	return o
}

func toOutBuild(b jmodel.Build) outBuild {
	return outBuild{
		Number:          b.Number,
		Status:          string(b.Status),
		DurationMs:      b.Duration.Milliseconds(),
		TimestampUnix:   b.Timestamp.Unix(),
		TriggeredBy:     b.TriggeredBy,
		TriggeredByName: b.TriggeredByName,
		Cause:           b.Cause,
	}
}

func toOutProjectBuild(p jmodel.ProjectBuild) outProjectBuild {
	return outProjectBuild{
		outBuild:   toOutBuild(p.Build),
		BranchName: p.BranchName,
		BranchPath: p.BranchPath,
	}
}

func toOutUserBuild(u jmodel.UserBuild) outUserBuild {
	return outUserBuild{
		JobPath:     u.JobPath,
		DisplayName: u.DisplayName,
		Node:        u.Node,
		Build:       toOutBuild(u.Build),
	}
}

func queueItemState(q jmodel.QueueItem) string {
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

func toOutQueueItem(q jmodel.QueueItem) outQueueItem {
	return outQueueItem{
		ID:           q.ID,
		JobPath:      q.JobPath,
		State:        queueItemState(q),
		Why:          q.Why,
		QueuedSinceS: q.InQueueSince.Unix(),
		Cause:        q.Cause,
	}
}

func toOutStage(s jmodel.Stage) outStage {
	return outStage{
		Name:       s.Name,
		Status:     string(s.Status),
		DurationMs: s.Duration.Milliseconds(),
		Depth:      s.Depth,
		Parallel:   s.Parallel,
	}
}

func toOutUser(u jmodel.User) outUser {
	return outUser{ID: u.ID, FullName: u.FullName, JenkinsVersion: u.JenkinsVersion}
}

func toOutParam(p jmodel.ParameterDefinition) outParam {
	return outParam{
		Name:        p.Name,
		Type:        string(p.Type),
		Default:     p.Default,
		Description: p.Description,
		Choices:     p.Choices,
	}
}

func toOutArtifact(a jmodel.Artifact) outArtifact {
	return outArtifact{DisplayPath: a.DisplayPath, URL: a.URL}
}

func toOutMeta(e jmodel.MetaEntry) outMeta {
	return outMeta{Path: e.Path, Value: e.Value}
}

func toOutTestCase(c jmodel.TestCase) outTestCase {
	return outTestCase{
		ClassName:    c.ClassName,
		Name:         c.Name,
		Status:       string(c.Status),
		DurationMs:   c.Duration.Milliseconds(),
		ErrorDetails: c.ErrorDetails,
	}
}

func toOutTestSuite(s jmodel.TestSuite) outTestSuite {
	cases := make([]outTestCase, len(s.Cases))
	for i, c := range s.Cases {
		cases[i] = toOutTestCase(c)
	}
	return outTestSuite{Name: s.Name, DurationMs: s.Duration.Milliseconds(), Cases: cases}
}

func toOutTestReport(r jmodel.TestReport) outTestReport {
	suites := make([]outTestSuite, len(r.Suites))
	for i, s := range r.Suites {
		suites[i] = toOutTestSuite(s)
	}
	return outTestReport{
		DurationMs: r.Duration.Milliseconds(),
		Failed:     r.Failed,
		Passed:     r.Passed,
		Skipped:    r.Skipped,
		Suites:     suites,
	}
}

// ---- Table printers ----

func printJobsTable(w io.Writer, jobs []outJob) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "NAME\tPATH\tTYPE\tRUNNING\tLAST BUILD")
	for _, j := range jobs {
		running := ""
		if j.RunningCount > 0 {
			running = fmt.Sprintf("%d", j.RunningCount)
		}
		lb := ""
		if j.LastBuildNum > 0 {
			lb = fmt.Sprintf("#%d", j.LastBuildNum)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.Name, j.FullPath, j.Type, running, lb)
	}
	return tw.Flush()
}

func printBuildsTable(w io.Writer, builds []outBuild) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "#\tSTATUS\tDURATION\tTRIGGERED BY")
	for _, b := range builds {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", b.Number, b.Status, fmtDur(time.Duration(b.DurationMs)*time.Millisecond), b.TriggeredByName)
	}
	return tw.Flush()
}

func printProjectBuildsTable(w io.Writer, builds []outProjectBuild) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "#\tBRANCH\tSTATUS\tDURATION\tTRIGGERED BY")
	for _, b := range builds {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", b.Number, b.BranchName, b.Status, fmtDur(time.Duration(b.DurationMs)*time.Millisecond), b.TriggeredByName)
	}
	return tw.Flush()
}

func printUserBuildsTable(w io.Writer, builds []outUserBuild) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "JOB\t#\tSTATUS\tDURATION\tNODE")
	for _, b := range builds {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", b.JobPath, b.Build.Number, b.Build.Status, fmtDur(time.Duration(b.Build.DurationMs)*time.Millisecond), b.Node)
	}
	return tw.Flush()
}

func printQueueTable(w io.Writer, items []outQueueItem) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "ID\tJOB\tSTATE\tWHY")
	for _, q := range items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", q.ID, q.JobPath, q.State, q.Why)
	}
	return tw.Flush()
}

func printStagesTable(w io.Writer, stages []outStage) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "NAME\tSTATUS\tDURATION\tDEPTH")
	for _, s := range stages {
		indent := ""
		for i := 0; i < s.Depth; i++ {
			indent += "  "
		}
		fmt.Fprintf(tw, "%s%s\t%s\t%s\t%d\n", indent, s.Name, s.Status, fmtDur(time.Duration(s.DurationMs)*time.Millisecond), s.Depth)
	}
	return tw.Flush()
}

func printArtifactsTable(w io.Writer, arts []outArtifact) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "PATH\tURL")
	for _, a := range arts {
		fmt.Fprintf(tw, "%s\t%s\n", a.DisplayPath, a.URL)
	}
	return tw.Flush()
}

func printMetadataTable(w io.Writer, entries []outMeta) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "PATH\tVALUE")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\n", e.Path, e.Value)
	}
	return tw.Flush()
}

func printParamsTable(w io.Writer, params []outParam) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "NAME\tTYPE\tDEFAULT\tDESCRIPTION")
	for _, p := range params {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, p.Type, p.Default, p.Description)
	}
	return tw.Flush()
}

func printTestReportTable(w io.Writer, r outTestReport) error {
	tw := newTab(w)
	fmt.Fprintf(tw, "PASSED\tFAILED\tSKIPPED\tDURATION\n")
	fmt.Fprintf(tw, "%d\t%d\t%d\t%s\n\n", r.Passed, r.Failed, r.Skipped, fmtDur(time.Duration(r.DurationMs)*time.Millisecond))
	tw.Flush()

	if r.Failed > 0 {
		fmt.Fprintln(w, "FAILURES:")
		for _, suite := range r.Suites {
			for _, c := range suite.Cases {
				if c.Status == "failed" {
					fmt.Fprintf(w, "  %s.%s\n", c.ClassName, c.Name)
					if c.ErrorDetails != "" {
						fmt.Fprintf(w, "    %s\n", c.ErrorDetails)
					}
				}
			}
		}
	}
	return nil
}

func printUserTable(w io.Writer, u outUser) error {
	tw := newTab(w)
	fmt.Fprintf(tw, "ID:\t%s\n", u.ID)
	fmt.Fprintf(tw, "Name:\t%s\n", u.FullName)
	fmt.Fprintf(tw, "Jenkins:\t%s\n", u.JenkinsVersion)
	return tw.Flush()
}

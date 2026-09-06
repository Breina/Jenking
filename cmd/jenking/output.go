package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Breina/Jenking/internal/app/dto"
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

func fmtBytes(b int64) string {
	if b <= 0 {
		return "-"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(b)/float64(div), "KMGTPE"[exp])
}

// ---- Wire types (aliases of the shared dto package) ----
//
// The CLI keeps its historical `out*` names as aliases so command code reads
// naturally; the canonical definitions and JSON/YAML tags live in
// internal/app/dto, shared byte-for-byte with the MCP server.
type (
	outJob           = dto.Job
	outBuild         = dto.Build
	outProjectBuild  = dto.ProjectBuild
	outUserBuild     = dto.UserBuild
	outQueueItem     = dto.QueueItem
	outStage         = dto.Stage
	outArtifact      = dto.Artifact
	outParam         = dto.Param
	outUser          = dto.User
	outMeta          = dto.Meta
	outNode          = dto.Node
	outInput         = dto.Input
	outBuildDetail   = dto.BuildDetail
	outTestReport    = dto.TestReport
	outTriggerResult = dto.TriggerResult
	outInputResult   = dto.InputResult
	outJobMatch      = dto.JobMatch
	outView          = dto.View
)

// Converter forwarders to the dto package.
var (
	toOutJob          = dto.ToJob
	toOutBuild        = dto.ToBuild
	toOutProjectBuild = dto.ToProjectBuild
	toOutUserBuild    = dto.ToUserBuild
	toOutQueueItem    = dto.ToQueueItem
	toOutStage        = dto.ToStage
	toOutArtifact     = dto.ToArtifact
	toOutParam        = dto.ToParam
	toOutUser         = dto.ToUser
	toOutMeta         = dto.ToMeta
	toOutNode         = dto.ToNode
	toOutInput        = dto.ToInput
	toOutBuildDetail  = dto.ToBuildDetail
	toOutTestReport   = dto.ToTestReport
	toOutJobMatch     = dto.ToJobMatch
)

// ---- Emit helpers (format a single result via printFormatted) ----

func emitTriggerResult(r outTriggerResult) error {
	return printFormatted(r, func() error {
		if r.Status != "" {
			fmt.Printf("build %s #%d finished: %s\n", r.JobPath, r.BuildNumber, r.Status)
			return nil
		}
		if r.QueueID > 0 {
			fmt.Printf("triggered %s (queue id %d)\n", r.JobPath, r.QueueID)
			return nil
		}
		fmt.Printf("triggered %s\n", r.JobPath)
		return nil
	})
}

func emitInputResult(r outInputResult) error {
	return printFormatted(r, func() error {
		fmt.Printf("%s input %q of %s #%d\n", r.Action, r.InputID, r.JobPath, r.BuildNumber)
		return nil
	})
}

// ---- Table printers ----

func printInputsTable(w io.Writer, inputs []outInput) error {
	if len(inputs) == 0 {
		_, err := fmt.Fprintln(w, "no pending inputs")
		return err
	}
	tw := newTab(w)
	fmt.Fprintln(tw, "ID\tMESSAGE\tSUBMITTER\tPARAMS")
	for _, in := range inputs {
		names := make([]string, len(in.Parameters))
		for i, p := range in.Parameters {
			names[i] = p.Name
		}
		sub := in.Submitter
		if sub == "" {
			sub = "-"
		}
		params := strings.Join(names, ",")
		if params == "" {
			params = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", in.ID, in.Message, sub, params)
	}
	return tw.Flush()
}

func printBuildDetailTable(w io.Writer, d outBuildDetail) error {
	tw := newTab(w)
	fmt.Fprintf(tw, "Job:\t%s\n", d.JobPath)
	fmt.Fprintf(tw, "Build:\t#%d\n", d.Number)
	fmt.Fprintf(tw, "Status:\t%s\n", d.Status)
	fmt.Fprintf(tw, "Duration:\t%s\n", fmtDur(time.Duration(d.DurationMs)*time.Millisecond))
	fmt.Fprintf(tw, "Started:\t%s\n", time.Unix(d.TimestampUnix, 0).Format(time.RFC3339))
	if d.Cause != "" {
		fmt.Fprintf(tw, "Cause:\t%s\n", d.Cause)
	}
	if d.TriggeredByName != "" {
		fmt.Fprintf(tw, "Triggered by:\t%s\n", d.TriggeredByName)
	}
	for k, v := range d.Parameters {
		fmt.Fprintf(tw, "Param %s:\t%s\n", k, v)
	}
	for _, in := range d.PendingInputs {
		fmt.Fprintf(tw, "Pending input:\t%s (%s)\n", in.ID, in.Message)
	}
	return tw.Flush()
}

func printNodesTable(w io.Writer, nodes []outNode) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "NAME\tSTATUS\tEXECUTORS\tDISK\tMEM\tPING")
	for _, n := range nodes {
		status := "online"
		if n.Offline {
			status = "offline"
			if n.OfflineCause != "" {
				status += " (" + n.OfflineCause + ")"
			}
		}
		ping := "-"
		if n.ResponseMs > 0 {
			ping = fmt.Sprintf("%dms", n.ResponseMs)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%s\t%s\t%s\n",
			n.Name, status, n.BusyExecutors, n.Executors, fmtBytes(n.FreeDiskBytes), fmtBytes(n.FreeMemBytes), ping)
	}
	return tw.Flush()
}

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

func printViewsTable(w io.Writer, views []outView) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "NAME\tKIND\tOWNER\tPRIMARY")
	for _, v := range views {
		owner := v.OwnerPath
		if v.Personal {
			owner = "(personal)"
		}
		primary := ""
		if v.Primary {
			primary = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.Name, v.Kind, owner, primary)
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

func printJobMatchesTable(w io.Writer, matches []outJobMatch) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "JOB\tBRANCH\tSCM URL")
	for _, m := range matches {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", m.JobPath, m.Branch, m.SCMURL)
	}
	return tw.Flush()
}

func printQueueTable(w io.Writer, items []outQueueItem) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "ID\tJOB\tKIND\tSTATE\tWHY")
	for _, q := range items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", q.ID, q.JobPath, q.Kind, q.State, q.Why)
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

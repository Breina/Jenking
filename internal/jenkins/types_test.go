package jenkins

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func TestParseJobType(t *testing.T) {
	tests := []struct {
		class string
		want  jmodel.JobType
	}{
		{"com.cloudbees.hudson.plugins.folder.Folder", jmodel.JobTypeFolder},
		{"org.jenkinsci.plugins.workflow.job.WorkflowJob", jmodel.JobTypePipeline},
		{"hudson.model.FreeStyleProject", jmodel.JobTypeFreeStyle},
		{"org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject", jmodel.JobTypeMultiBranch},
		{"jenkins.branch.MultiBranchProject", jmodel.JobTypeMultiBranch},
		{"some.unknown.Class", jmodel.JobTypeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			got := ParseJobType(tt.class)
			if got != tt.want {
				t.Errorf("ParseJobType(%q) = %q, want %q", tt.class, got, tt.want)
			}
		})
	}
}

func TestParseBuildStatus(t *testing.T) {
	str := func(s string) *string { return &s }

	tests := []struct {
		name     string
		result   *string
		building bool
		want     jmodel.BuildStatus
	}{
		{"building", nil, true, jmodel.BuildStatusRunning},
		{"null result not building", nil, false, jmodel.BuildStatusRunning},
		{"success", str("SUCCESS"), false, jmodel.BuildStatusSuccess},
		{"failure", str("FAILURE"), false, jmodel.BuildStatusFailed},
		{"aborted", str("ABORTED"), false, jmodel.BuildStatusAborted},
		{"unstable", str("UNSTABLE"), false, jmodel.BuildStatusUnstable},
		{"not built", str("NOT_BUILT"), false, jmodel.BuildStatusNotBuilt},
		{"unknown", str("WEIRD"), false, jmodel.BuildStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseBuildStatus(tt.result, tt.building)
			if got != tt.want {
				t.Errorf("ParseBuildStatus(%v, %v) = %q, want %q", tt.result, tt.building, got, tt.want)
			}
		})
	}
}

func TestColorToBuildStatus(t *testing.T) {
	tests := []struct {
		color string
		want  jmodel.BuildStatus
	}{
		{"blue", jmodel.BuildStatusSuccess},
		{"blue_anime", jmodel.BuildStatusRunning},
		{"red", jmodel.BuildStatusFailed},
		{"red_anime", jmodel.BuildStatusRunning},
		{"aborted", jmodel.BuildStatusAborted},
		{"grey", jmodel.BuildStatusAborted},
		{"yellow", jmodel.BuildStatusUnstable},
		{"notbuilt", jmodel.BuildStatusNotBuilt},
		{"disabled", jmodel.BuildStatusNotBuilt},
		{"something_else", jmodel.BuildStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.color, func(t *testing.T) {
			got := ColorToBuildStatus(tt.color)
			if got != tt.want {
				t.Errorf("ColorToBuildStatus(%q) = %q, want %q", tt.color, got, tt.want)
			}
		})
	}
}

func TestMillisToDuration(t *testing.T) {
	got := millisToDuration(5000)
	want := 5 * time.Second
	if got != want {
		t.Errorf("millisToDuration(5000) = %v, want %v", got, want)
	}
}

func TestJobPathToURL(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"", ""},
		{"my-job", "/job/my-job"},
		{"folder/job", "/job/folder/job/job"},
		{"a/b/c", "/job/a/job/b/job/c"},
		{"Code/git%2Fbranch", "/job/Code/job/git%252Fbranch"},
		{"Code Private/feature%2Fbranch", "/job/Code%20Private/job/feature%252Fbranch"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := jmodel.JobPathToURL(tt.path)
			if got != tt.want {
				t.Errorf("jmodel.JobPathToURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractCause(t *testing.T) {
	cause := func(class, desc string) jsonCause {
		return jsonCause{Class: class, ShortDescription: desc}
	}
	const (
		userIDCause   = "hudson.model.Cause$UserIdCause"
		branchEvent   = "jenkins.branch.BranchEventCause"
		userInterrupt = "jenkins.model.CauseOfInterruption$UserInterruption"
	)

	tests := []struct {
		name    string
		actions []jsonAction
		want    string
	}{
		{"no causes", []jsonAction{{}}, ""},
		{
			"user id cause wins over branch event",
			[]jsonAction{{Causes: []jsonCause{cause(branchEvent, "Branch event"), cause(userIDCause, "Started by user Brecht Derwael")}}},
			"Started by user Brecht Derwael",
		},
		{
			// InterruptedBuildAction is listed before CauseAction on aborted builds.
			"interruption never masks the real cause",
			[]jsonAction{
				{Causes: []jsonCause{cause(userInterrupt, "Aborted by edb3908acd8b7ec9")}},
				{Causes: []jsonCause{cause(userIDCause, "Started by user Brecht Derwael")}},
			},
			"Started by user Brecht Derwael",
		},
		{
			"interruption ranks below branch event",
			[]jsonAction{
				{Causes: []jsonCause{cause(userInterrupt, "Aborted by edb3908acd8b7ec9")}},
				{Causes: []jsonCause{cause(branchEvent, "Branch event")}},
			},
			"Branch event",
		},
		{
			"interruption used when nothing else is known",
			[]jsonAction{{Causes: []jsonCause{cause(userInterrupt, "Aborted by edb3908acd8b7ec9")}}},
			"Aborted by edb3908acd8b7ec9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractCause(tt.actions); got != tt.want {
				t.Errorf("extractCause() = %q, want %q", got, tt.want)
			}
		})
	}
}

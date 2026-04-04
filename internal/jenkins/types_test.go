package jenkins

import (
	"testing"
	"time"
)

func TestParseJobType(t *testing.T) {
	tests := []struct {
		class string
		want  JobType
	}{
		{"com.cloudbees.hudson.plugins.folder.Folder", JobTypeFolder},
		{"org.jenkinsci.plugins.workflow.job.WorkflowJob", JobTypePipeline},
		{"hudson.model.FreeStyleProject", JobTypeFreeStyle},
		{"org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject", JobTypeMultiBranch},
		{"jenkins.branch.MultiBranchProject", JobTypeMultiBranch},
		{"some.unknown.Class", JobTypeUnknown},
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
		want     BuildStatus
	}{
		{"building", nil, true, BuildStatusRunning},
		{"null result not building", nil, false, BuildStatusRunning},
		{"success", str("SUCCESS"), false, BuildStatusSuccess},
		{"failure", str("FAILURE"), false, BuildStatusFailed},
		{"aborted", str("ABORTED"), false, BuildStatusAborted},
		{"unstable", str("UNSTABLE"), false, BuildStatusUnstable},
		{"not built", str("NOT_BUILT"), false, BuildStatusNotBuilt},
		{"unknown", str("WEIRD"), false, BuildStatusUnknown},
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
		want  BuildStatus
	}{
		{"blue", BuildStatusSuccess},
		{"blue_anime", BuildStatusRunning},
		{"red", BuildStatusFailed},
		{"red_anime", BuildStatusRunning},
		{"aborted", BuildStatusAborted},
		{"grey", BuildStatusAborted},
		{"yellow", BuildStatusUnstable},
		{"notbuilt", BuildStatusNotBuilt},
		{"disabled", BuildStatusNotBuilt},
		{"something_else", BuildStatusUnknown},
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
			got := JobPathToURL(tt.path)
			if got != tt.want {
				t.Errorf("JobPathToURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

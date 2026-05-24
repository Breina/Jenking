package jenkins

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// branchRef is a tiny helper for table tests below.
func branchRef(num int, ts int64) *jsonBuildRef {
	return &jsonBuildRef{Number: num, URL: "/u", Timestamp: ts, EstimatedDuration: 0}
}

func TestParseJobList(t *testing.T) {
	tests := []struct {
		name        string
		folder      string
		resp        jsonJobList
		wantLen     int
		check       func(t *testing.T, jobs []jmodel.Job)
		description string
	}{
		{
			name:   "folder job: no branch enrichment",
			folder: "",
			resp: jsonJobList{Jobs: []jsonJob{{
				Class: "com.cloudbees.hudson.plugins.folder.Folder",
				Name:  "team",
				Color: "",
			}}},
			wantLen: 1,
			check: func(t *testing.T, jobs []jmodel.Job) {
				j := jobs[0]
				if j.Type != jmodel.JobTypeFolder {
					t.Errorf("Type = %v, want Folder", j.Type)
				}
				if j.RunningCount != 0 {
					t.Errorf("RunningCount = %d, want 0", j.RunningCount)
				}
				if j.LastAnyBuild != nil {
					t.Errorf("LastAnyBuild = %v, want nil", j.LastAnyBuild)
				}
			},
		},
		{
			name:   "single-branch pipeline: running color sets count",
			folder: "team",
			resp: jsonJobList{Jobs: []jsonJob{{
				Class:     "org.jenkinsci.plugins.workflow.job.WorkflowJob",
				Name:      "build",
				Color:     "blue_anime",
				LastBuild: branchRef(5, 1000),
			}}},
			wantLen: 1,
			check: func(t *testing.T, jobs []jmodel.Job) {
				j := jobs[0]
				if j.Type != jmodel.JobTypePipeline {
					t.Errorf("Type = %v, want Pipeline", j.Type)
				}
				if j.RunningCount != 1 {
					t.Errorf("RunningCount = %d, want 1", j.RunningCount)
				}
				if j.LastAnyBuild == nil || j.LastAnyBuild.Number != 5 {
					t.Errorf("LastAnyBuild = %v, want #5", j.LastAnyBuild)
				}
				if j.LastAnyColor != "blue_anime" {
					t.Errorf("LastAnyColor = %q, want blue_anime", j.LastAnyColor)
				}
				if j.FullPath != "team/build" {
					t.Errorf("FullPath = %q, want team/build", j.FullPath)
				}
			},
		},
		{
			name:   "single-branch idle: no running count",
			folder: "",
			resp: jsonJobList{Jobs: []jsonJob{{
				Class:     "hudson.model.FreeStyleProject",
				Name:      "legacy",
				Color:     "blue",
				LastBuild: branchRef(9, 2000),
			}}},
			wantLen: 1,
			check: func(t *testing.T, jobs []jmodel.Job) {
				if jobs[0].RunningCount != 0 {
					t.Errorf("RunningCount = %d, want 0", jobs[0].RunningCount)
				}
			},
		},
		{
			name:   "multibranch: primary picked, running counted, latest cross-branch",
			folder: "team",
			resp: jsonJobList{Jobs: []jsonJob{{
				Class: "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
				Name:  "svc",
				Color: "notbuilt",
				Jobs: []jsonJob{
					{Name: "main", Color: "blue", LastBuild: branchRef(10, 5000)},
					{Name: "feature-x", Color: "red_anime", LastBuild: branchRef(3, 9000)},
					{Name: "develop", Color: "yellow_anime", LastBuild: branchRef(7, 7000)},
				},
			}}},
			wantLen: 1,
			check: func(t *testing.T, jobs []jmodel.Job) {
				j := jobs[0]
				if j.Type != jmodel.JobTypeMultiBranch {
					t.Fatalf("Type = %v, want MultiBranch", j.Type)
				}
				// primary should be main (well-known)
				if j.Color != "blue" {
					t.Errorf("primary Color = %q, want blue", j.Color)
				}
				if j.LastBuild == nil || j.LastBuild.Number != 10 {
					t.Errorf("primary LastBuild = %v, want #10", j.LastBuild)
				}
				// two _anime branches running
				if j.RunningCount != 2 {
					t.Errorf("RunningCount = %d, want 2", j.RunningCount)
				}
				// latest timestamp = 9000 → feature-x
				if j.LastAnyBuild == nil || j.LastAnyBuild.Number != 3 {
					t.Errorf("LastAnyBuild = %v, want feature-x #3", j.LastAnyBuild)
				}
				if j.LastAnyColor != "red_anime" {
					t.Errorf("LastAnyColor = %q, want red_anime", j.LastAnyColor)
				}
			},
		},
		{
			name:   "multibranch with no built branches: falls back to primary",
			folder: "",
			resp: jsonJobList{Jobs: []jsonJob{{
				Class: "jenkins.branch.MultiBranchProject",
				Name:  "empty",
				Color: "notbuilt",
				Jobs: []jsonJob{
					{Name: "feature-a", Color: "notbuilt"},
					{Name: "feature-b", Color: "notbuilt"},
				},
			}}},
			wantLen: 1,
			check: func(t *testing.T, jobs []jmodel.Job) {
				j := jobs[0]
				if j.RunningCount != 0 {
					t.Errorf("RunningCount = %d, want 0", j.RunningCount)
				}
				// no branch has LastBuild → LastAnyBuild aliases primary's nil LastBuild
				if j.LastAnyBuild != nil {
					t.Errorf("LastAnyBuild = %v, want nil", j.LastAnyBuild)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := parseJobList(tt.resp, tt.folder)
			if len(jobs) != tt.wantLen {
				t.Fatalf("len(jobs) = %d, want %d", len(jobs), tt.wantLen)
			}
			tt.check(t, jobs)
		})
	}
}

func TestPrimaryBranch_WellKnownOrdering(t *testing.T) {
	tests := []struct {
		name     string
		branches []jsonJob
		want     string // expected branch name; "" → nil
	}{
		{
			name:     "main wins over develop",
			branches: []jsonJob{{Name: "develop"}, {Name: "main"}},
			want:     "main",
		},
		{
			name:     "master wins over trunk",
			branches: []jsonJob{{Name: "trunk"}, {Name: "master"}},
			want:     "master",
		},
		{
			name: "fall back to highest LastBuild.Number when no well-known",
			branches: []jsonJob{
				{Name: "feature-a", LastBuild: &jsonBuildRef{Number: 3}},
				{Name: "feature-b", LastBuild: &jsonBuildRef{Number: 8}},
				{Name: "feature-c", LastBuild: &jsonBuildRef{Number: 1}},
			},
			want: "feature-b",
		},
		{
			name:     "first branch when nothing else works",
			branches: []jsonJob{{Name: "only-one"}},
			want:     "only-one",
		},
		{
			name:     "empty list returns nil",
			branches: nil,
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := primaryBranch(tt.branches)
			if tt.want == "" {
				if got != nil {
					t.Errorf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want %q", tt.want)
			}
			if got.Name != tt.want {
				t.Errorf("got %q, want %q", got.Name, tt.want)
			}
		})
	}
}

func TestCountRunningBranches(t *testing.T) {
	branches := []jsonJob{
		{Color: "blue"},
		{Color: "red_anime"},
		{Color: "yellow_anime"},
		{Color: "notbuilt"},
	}
	if got := countRunningBranches(branches); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}

func TestLatestBuiltBranch(t *testing.T) {
	t.Run("picks highest timestamp", func(t *testing.T) {
		branches := []jsonJob{
			{Name: "a", LastBuild: branchRef(1, 100)},
			{Name: "b", LastBuild: branchRef(2, 500)},
			{Name: "c", LastBuild: branchRef(3, 300)},
		}
		got := latestBuiltBranch(branches)
		if got == nil || got.Name != "b" {
			t.Errorf("got %+v, want b", got)
		}
	})
	t.Run("nil when no built branch", func(t *testing.T) {
		branches := []jsonJob{{Name: "a"}, {Name: "b"}}
		if got := latestBuiltBranch(branches); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})
}

func TestBranchBuildRef_Nil(t *testing.T) {
	if got := branchBuildRef(nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

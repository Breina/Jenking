package view

import (
	"testing"
)

func TestNavigationContext_JobPath(t *testing.T) {
	tests := []struct {
		name string
		nc   NavigationContext
		want string
	}{
		{
			name: "root",
			nc:   NavigationContext{Level: CtxRoot},
			want: "",
		},
		{
			name: "standalone project (no folder)",
			nc:   NavigationContext{Level: CtxProject, ProjectName: "my-pipeline"},
			want: "my-pipeline",
		},
		{
			name: "project in folder",
			nc:   NavigationContext{Level: CtxProject, FolderPath: "Code Private", ProjectName: "my-pipeline"},
			want: "Code Private/my-pipeline",
		},
		{
			name: "branch in project",
			nc:   NavigationContext{Level: CtxBranch, FolderPath: "Code Private", ProjectName: "my-project", BranchName: "main"},
			want: "Code Private/my-project/main",
		},
		{
			name: "branch with url-encoded name",
			nc:   NavigationContext{Level: CtxBranch, ProjectName: "my-project", BranchName: "feature%2Fbranch"},
			want: "my-project/feature%2Fbranch",
		},
		{
			name: "build level (no folder)",
			nc:   NavigationContext{Level: CtxBuild, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 42}},
			want: "my-project/main",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.nc.JobPath()
			if got != tt.want {
				t.Errorf("JobPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBreadcrumbFor(t *testing.T) {
	tests := []struct {
		name         string
		viewType     string
		nc           NavigationContext
		wantViewType string
		wantParts    int
	}{
		{
			name:         "builds at branch level",
			viewType:     "builds",
			nc:           NavigationContext{Level: CtxBranch, ProjectName: "my-project", BranchName: "main"},
			wantViewType: "builds",
			wantParts:    2, // project + branch
		},
		{
			name:         "stages at build level",
			viewType:     "stages",
			nc:           NavigationContext{Level: CtxBuild, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 5}},
			wantViewType: "stages",
			wantParts:    3, // project + branch + build number
		},
		{
			name:         "log at stage level",
			viewType:     "log",
			nc:           NavigationContext{Level: CtxStage, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 5}, StageName: "Build"},
			wantViewType: "log",
			wantParts:    4, // project + branch + build number + stage
		},
		{
			name:         "builds at project level (standalone)",
			viewType:     "builds",
			nc:           NavigationContext{Level: CtxProject, ProjectName: "my-pipeline"},
			wantViewType: "builds",
			wantParts:    1, // project only
		},
		{
			name:         "last build reference",
			viewType:     "stages",
			nc:           NavigationContext{Level: CtxBuild, ProjectName: "my-project", Build: NavBuildRef{IsLast: true}},
			wantViewType: "stages",
			wantParts:    2, // project + "last"
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg := BreadcrumbFor(tt.viewType, tt.nc)
			if seg.ViewType != tt.wantViewType {
				t.Errorf("ViewType = %q, want %q", seg.ViewType, tt.wantViewType)
			}
			if len(seg.Context) != tt.wantParts {
				t.Errorf("len(Context) = %d, want %d (parts: %v)", len(seg.Context), tt.wantParts, seg.Context)
			}
		})
	}
}

func TestNcFromJobPath(t *testing.T) {
	tests := []struct {
		jobPath     string
		wantFolder  string
		wantProject string
		wantBranch  string
		wantJobPath string
	}{
		{"", "", "", "", ""},
		{"my-pipeline", "", "my-pipeline", "", "my-pipeline"},
		{"project/branch", "", "project", "branch", "project/branch"},
		{"folder/project/branch", "folder", "project", "branch", "folder/project/branch"},
		{"a/b/c/d", "a/b", "c", "d", "a/b/c/d"},
	}
	for _, tt := range tests {
		t.Run(tt.jobPath, func(t *testing.T) {
			nc := ncFromJobPath(tt.jobPath)
			if nc.FolderPath != tt.wantFolder {
				t.Errorf("FolderPath = %q, want %q", nc.FolderPath, tt.wantFolder)
			}
			if nc.ProjectName != tt.wantProject {
				t.Errorf("ProjectName = %q, want %q", nc.ProjectName, tt.wantProject)
			}
			if nc.BranchName != tt.wantBranch {
				t.Errorf("BranchName = %q, want %q", nc.BranchName, tt.wantBranch)
			}
			if got := nc.JobPath(); got != tt.wantJobPath {
				t.Errorf("JobPath() = %q, want %q", got, tt.wantJobPath)
			}
		})
	}
}

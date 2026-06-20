package view

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func TestArtifactShortcutAction(t *testing.T) {
	tests := []struct {
		name string
		arts []jmodel.Artifact
		want string
	}{
		{"single shows name", []jmodel.Artifact{{DisplayPath: "trivy-report.html"}}, "trivy-report.html"},
		{"single truncates long name", []jmodel.Artifact{{DisplayPath: "a-very-long-artifact-name.html"}}, "a-very-long-artifa…"},
		{"multiple shows count", []jmodel.Artifact{{DisplayPath: "a"}, {DisplayPath: "b"}}, "artifacts [2]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := artifactShortcutAction(tt.arts); got != tt.want {
				t.Errorf("artifactShortcutAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

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
		scope        ContextLevel
		wantViewType string
		wantParts    int
	}{
		{
			name:         "builds at branch level",
			viewType:     "builds",
			nc:           NavigationContext{Level: CtxBranch, ProjectName: "my-project", BranchName: "main"},
			scope:        CtxBranch,
			wantViewType: "builds",
			wantParts:    2, // project + branch
		},
		{
			name:         "stages at build level",
			viewType:     "stages",
			nc:           NavigationContext{Level: CtxBuild, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 5}},
			scope:        CtxBuild,
			wantViewType: "stages",
			wantParts:    3, // project + branch + build number
		},
		{
			name:         "log at stage level",
			viewType:     "log",
			nc:           NavigationContext{Level: CtxStage, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 5}, StageName: "Build"},
			scope:        CtxStage,
			wantViewType: "log",
			wantParts:    4, // project + branch + build number + stage
		},
		{
			name:         "log at stage level with parent",
			viewType:     "log",
			nc:           NavigationContext{Level: CtxStage, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 5}, StageName: "Build", StageParent: "Matrix - JDK = 'jdk21'"},
			scope:        CtxStage,
			wantViewType: "log",
			wantParts:    5, // project + branch + build + parent + leaf
		},
		{
			name:         "stage parent clipped when view owns only the build",
			viewType:     "describe",
			nc:           NavigationContext{Level: CtxStage, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 5}, StageName: "Build", StageParent: "Matrix - JDK = 'jdk21'"},
			scope:        CtxBuild,
			wantViewType: "describe",
			wantParts:    3, // project + branch + build (stage + parent dropped)
		},
		{
			name:         "builds at project level (standalone)",
			viewType:     "builds",
			nc:           NavigationContext{Level: CtxProject, ProjectName: "my-pipeline"},
			scope:        CtxProject,
			wantViewType: "builds",
			wantParts:    1, // project only
		},
		{
			name:         "last build reference",
			viewType:     "stages",
			nc:           NavigationContext{Level: CtxBuild, ProjectName: "my-project", Build: NavBuildRef{IsLast: true}},
			scope:        CtxBuild,
			wantViewType: "stages",
			wantParts:    2, // project + "last"
		},
		{
			name:         "scope clips stage tail when view owns only the build",
			viewType:     "describe",
			nc:           NavigationContext{Level: CtxStage, ProjectName: "my-project", BranchName: "main", Build: NavBuildRef{Number: 5}, StageName: "Build"},
			scope:        CtxBuild,
			wantViewType: "describe",
			wantParts:    3, // project + branch + build (stage dropped)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg := BreadcrumbFor(tt.viewType, tt.nc, tt.scope)
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

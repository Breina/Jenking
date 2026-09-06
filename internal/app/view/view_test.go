package view

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
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

// partsText flattens breadcrumb parts into a comparable string, marking build
// parts the way the renderer does ("#44", or a bare display name).
func partsText(parts []component.BreadcrumbPart) string {
	var out []string
	for _, p := range parts {
		s := p.Text
		if p.IsBuildNum && !p.NoHashPrefix {
			s = "#" + s
		}
		out = append(out, s)
	}
	return strings.Join(out, "/")
}

// TestBreadcrumbAliasSplit pins the single rule: a "#last" cursor renders the
// alias in the context and everything it resolved to in the tail, split at the
// level the alias was anchored to.
func TestBreadcrumbAliasSplit(t *testing.T) {
	tests := []struct {
		name         string
		nc           NavigationContext
		wantContext  string
		wantResolved string
	}{
		{
			name: "branch-anchored alias, resolved with display name",
			nc: NavigationContext{
				Level: CtxBuild, AliasScope: CtxBranch,
				ProjectName: "s3-provisioning-operator", BranchName: "main",
				Build: NavBuildRef{IsLast: true, Number: 44, DisplayName: "Release 1.0.7"},
			},
			wantContext:  "s3-provisioning-operator/main/#last",
			wantResolved: "Release 1.0.7",
		},
		{
			name: "branch-anchored alias, resolved without display name",
			nc: NavigationContext{
				Level: CtxBuild, AliasScope: CtxBranch,
				ProjectName: "app", BranchName: "main",
				Build: NavBuildRef{IsLast: true, Number: 44},
			},
			wantContext:  "app/main/#last",
			wantResolved: "#44",
		},
		{
			name: "root-anchored alias resolves project and branch into the tail",
			nc: NavigationContext{
				Level: CtxBuild, AliasScope: CtxRoot,
				ProjectName: "app", BranchName: "main",
				Build: NavBuildRef{IsLast: true, Number: 44, DisplayName: "Release 1.0.7"},
			},
			wantContext:  "*/#last",
			wantResolved: "app/main/Release 1.0.7",
		},
		{
			name: "project-anchored alias resolves only the branch into the tail",
			nc: NavigationContext{
				Level: CtxBuild, AliasScope: CtxProject,
				ProjectName: "app", BranchName: "main",
				Build: NavBuildRef{IsLast: true, Number: 44},
			},
			wantContext:  "app/#last",
			wantResolved: "main/#44",
		},
		{
			name: "unresolved alias keeps the stage in the context",
			nc: NavigationContext{
				Level: CtxStage, AliasScope: CtxBranch,
				ProjectName: "app", BranchName: "main", StageName: "Deploy",
				Build: NavBuildRef{IsLast: true},
			},
			wantContext:  "app/main/#last/Deploy",
			wantResolved: "",
		},
		{
			name: "resolved alias carries the stage into the tail",
			nc: NavigationContext{
				Level: CtxStage, AliasScope: CtxBranch,
				ProjectName: "app", BranchName: "main", StageName: "Deploy",
				Build: NavBuildRef{IsLast: true, Number: 44},
			},
			wantContext:  "app/main/#last",
			wantResolved: "#44/Deploy",
		},
		{
			name: "no alias renders everything as context",
			nc: NavigationContext{
				Level:       CtxBuild,
				ProjectName: "app", BranchName: "main",
				Build: NavBuildRef{Number: 44, DisplayName: "Release 1.0.7"},
			},
			wantContext:  "app/main/Release 1.0.7",
			wantResolved: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg := BreadcrumbFor("stages", tt.nc, tt.nc.Level)
			if got := partsText(seg.Context); got != tt.wantContext {
				t.Errorf("Context = %q, want %q", got, tt.wantContext)
			}
			if got := partsText(seg.ResolvedParts); got != tt.wantResolved {
				t.Errorf("ResolvedParts = %q, want %q", got, tt.wantResolved)
			}
		})
	}
}

// TestBreadcrumbStableAcrossViewHops walks the navigation edges from the three
// flows that used to disagree — the same build reached three ways must produce
// one breadcrumb, whatever view renders it.
func TestBreadcrumbStableAcrossViewHops(t *testing.T) {
	branch := NavigationContext{Level: CtxBranch, ProjectName: "s3-provisioning-operator", BranchName: "main"}
	resolved := NavBuildRef{Number: 44, DisplayName: "Release 1.0.7"}

	// The scoped stages view resolves the alias.
	scoped := branch.AtLastBuild(resolved)
	// Sibling swaps (describe/log/tests/artifacts) forward the nc verbatim.
	describe := scoped
	// …and back to stages, and on into a stage log and back out again.
	stages := describe
	stageLog := stages.AtStage("Deploy")
	backToStages := stageLog.AtBuildRef(stageLog.Build)
	// Drilling in from the pinned "#last" row of the builds view.
	fromLastRow := branch.AtBuildRef(NavBuildRef{Number: 44, DisplayName: "Release 1.0.7", IsLast: true})

	want := BreadcrumbFor("stages", scoped, CtxBuild)
	for name, nc := range map[string]NavigationContext{
		"describe":                   describe,
		"stages":                     stages,
		"back to stages":             backToStages,
		"from #last row":             fromLastRow,
		"stage log clipped to build": stageLog,
	} {
		got := BreadcrumbFor("stages", nc, CtxBuild)
		if partsText(got.Context) != partsText(want.Context) ||
			partsText(got.ResolvedParts) != partsText(want.ResolvedParts) {
			t.Errorf("%s: got (%q → %q), want (%q → %q)", name,
				partsText(got.Context), partsText(got.ResolvedParts),
				partsText(want.Context), partsText(want.ResolvedParts))
		}
	}

	// Pinning to a concrete number is the one edge that drops the alias.
	pinned := BreadcrumbFor("stages", branch.AtBuild(44), CtxBuild)
	if len(pinned.ResolvedParts) != 0 || partsText(pinned.Context) != "s3-provisioning-operator/main/#44" {
		t.Errorf("AtBuild should pin, got (%q → %q)", partsText(pinned.Context), partsText(pinned.ResolvedParts))
	}
}

// TestScopedViewTargetNC checks that the resolving view writes its resolution
// into the nc — the value handed to the inner view and to every view swapped in
// from it — rather than into a breadcrumb field only it can render.
func TestScopedViewTargetNC(t *testing.T) {
	tests := []struct {
		name         string
		scope        NavigationContext
		wantContext  string
		wantResolved string
	}{
		{
			name:         "branch scope",
			scope:        NavigationContext{Level: CtxBranch, FolderPath: "folder", ProjectName: "app", BranchName: "main"},
			wantContext:  "app/main/#last",
			wantResolved: "Release 1.0.7",
		},
		{
			name:         "root scope resolves the location too",
			scope:        NavigationContext{Level: CtxRoot},
			wantContext:  "*/#last",
			wantResolved: "app/main/Release 1.0.7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := &ScopedView{cfg: ScopedViewConfig{BreadcrumbType: "stages"}}
			sv.resolver.scope = tt.scope
			sv.resolver.resolvedPath = "folder/app/main"
			sv.resolver.resolvedNum = 44
			sv.resolver.resolvedName = "Release 1.0.7"

			seg := sv.Breadcrumb()
			if got := partsText(seg.Context); got != tt.wantContext {
				t.Errorf("Context = %q, want %q", got, tt.wantContext)
			}
			if got := partsText(seg.ResolvedParts); got != tt.wantResolved {
				t.Errorf("ResolvedParts = %q, want %q", got, tt.wantResolved)
			}
			// The inner view's nc must render the identical breadcrumb.
			inner := BreadcrumbFor("stages", sv.targetNC(), CtxBuild)
			if partsText(inner.Context) != partsText(seg.Context) ||
				partsText(inner.ResolvedParts) != partsText(seg.ResolvedParts) {
				t.Errorf("inner nc diverges: (%q → %q)", partsText(inner.Context), partsText(inner.ResolvedParts))
			}
		})
	}
}

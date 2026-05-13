package view

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
)

func newFixtureStore(t *testing.T) *cache.Store {
	t.Helper()
	s := cache.NewStore(nil)
	s.Jobs.Put("", []jenkins.Job{
		{Name: "Code Private", FullPath: "Code Private", Type: jenkins.JobTypeFolder},
		{Name: "Other", FullPath: "Other", Type: jenkins.JobTypeFolder},
		{Name: "standalone", FullPath: "standalone", Type: jenkins.JobTypePipeline},
	})
	s.Jobs.Put("Code Private", []jenkins.Job{
		{Name: "webidm", FullPath: "Code Private/webidm", Type: jenkins.JobTypeMultiBranch},
		{Name: "freestyle", FullPath: "Code Private/freestyle", Type: jenkins.JobTypeFreeStyle},
	})
	s.Jobs.Put("Other", []jenkins.Job{
		{Name: "webidm", FullPath: "Other/webidm", Type: jenkins.JobTypeMultiBranch},
	})
	return s
}

func TestResolveTarget_Empty(t *testing.T) {
	current := NavigationContext{Level: CtxBranch, ProjectName: "p", BranchName: "main", Username: "u"}
	got, err := ResolveTarget(command.Target{}, newFixtureStore(t), current)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, current) {
		t.Errorf("empty target should pass current through; got %+v want %+v", got, current)
	}
}

func TestResolveTarget_UniqueProject(t *testing.T) {
	got, err := ResolveTarget(
		command.Target{ProjectSuffix: "freestyle"},
		newFixtureStore(t),
		NavigationContext{Username: "u", FriendlyName: "U"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.FolderPath != "Code Private" || got.ProjectName != "freestyle" || got.Level != CtxProject {
		t.Errorf("unexpected NC: %+v", got)
	}
	if got.Username != "u" || got.FriendlyName != "U" {
		t.Errorf("identity not inherited: %+v", got)
	}
}

func TestResolveTarget_StandaloneProject(t *testing.T) {
	got, err := ResolveTarget(
		command.Target{ProjectSuffix: "standalone"},
		newFixtureStore(t),
		NavigationContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.FolderPath != "" || got.ProjectName != "standalone" {
		t.Errorf("unexpected NC: %+v", got)
	}
}

func TestResolveTarget_AmbiguousProject(t *testing.T) {
	_, err := ResolveTarget(
		command.Target{ProjectSuffix: "webidm"},
		newFixtureStore(t),
		NavigationContext{},
	)
	var rerr *ResolveError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *ResolveError, got %T: %v", err, err)
	}
	if len(rerr.Candidates) != 2 {
		t.Errorf("want 2 candidates, got %v", rerr.Candidates)
	}
}

func TestResolveTarget_UnknownProject(t *testing.T) {
	_, err := ResolveTarget(
		command.Target{ProjectSuffix: "nope"},
		newFixtureStore(t),
		NavigationContext{},
	)
	var rerr *ResolveError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *ResolveError, got %T: %v", err, err)
	}
	if len(rerr.Candidates) != 0 {
		t.Errorf("want 0 candidates, got %v", rerr.Candidates)
	}
}

func TestResolveTarget_ProjectAndBranch(t *testing.T) {
	got, err := ResolveTarget(
		command.Target{ProjectSuffix: "freestyle", Branch: "feature/foo"},
		newFixtureStore(t),
		NavigationContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	// User-typed "feature/foo" is encoded for storage so JobPath stays a
	// single segment per Jenkins URL convention.
	if got.Level != CtxBranch || got.BranchName != "feature%2Ffoo" {
		t.Errorf("unexpected NC: %+v", got)
	}
}

func TestResolveTarget_BuildNumber(t *testing.T) {
	got, err := ResolveTarget(
		command.Target{
			ProjectSuffix: "freestyle",
			Branch:        "main",
			Build:         command.BuildRef{Number: 42, Set: true},
		},
		newFixtureStore(t),
		NavigationContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != CtxBuild || got.Build.Number != 42 || got.Build.IsLast {
		t.Errorf("unexpected NC: %+v", got)
	}
}

func TestResolveTarget_BuildLast(t *testing.T) {
	got, err := ResolveTarget(
		command.Target{
			ProjectSuffix: "freestyle",
			Branch:        "main",
			Build:         command.BuildRef{IsLast: true, Set: true},
		},
		newFixtureStore(t),
		NavigationContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != CtxBuild || !got.Build.IsLast || got.Build.Number != 0 {
		t.Errorf("unexpected NC: %+v", got)
	}
}

func TestResolveTarget_Stage(t *testing.T) {
	got, err := ResolveTarget(
		command.Target{
			ProjectSuffix: "freestyle",
			Branch:        "main",
			Build:         command.BuildRef{Number: 7, Set: true},
			Stage:         "Build & Test",
		},
		newFixtureStore(t),
		NavigationContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != CtxStage || got.StageName != "Build & Test" {
		t.Errorf("unexpected NC: %+v", got)
	}
}

func TestResolveTarget_PartialFromCurrent(t *testing.T) {
	current := NavigationContext{
		Level: CtxBranch, FolderPath: "Code Private", ProjectName: "webidm",
		BranchName: "main", Username: "u",
	}
	// `:logs #42` on a branch view = build 42 of that branch.
	got, err := ResolveTarget(
		command.Target{Build: command.BuildRef{Number: 42, Set: true}},
		newFixtureStore(t),
		current,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != CtxBuild || got.Build.Number != 42 {
		t.Errorf("unexpected NC: %+v", got)
	}
	if got.ProjectName != "webidm" || got.BranchName != "main" {
		t.Errorf("did not inherit project/branch: %+v", got)
	}
}

func TestResolveTarget_BranchOverrideClearsBuild(t *testing.T) {
	current := NavigationContext{
		Level: CtxBuild, FolderPath: "Code Private", ProjectName: "webidm",
		BranchName: "main", Build: NavBuildRef{Number: 5},
	}
	got, err := ResolveTarget(
		command.Target{Branch: "develop"},
		newFixtureStore(t),
		current,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != CtxBranch || got.BranchName != "develop" || got.Build.Number != 0 {
		t.Errorf("unexpected NC: %+v", got)
	}
}

func TestResolveTarget_EncodedProjectName(t *testing.T) {
	// A project literally named "git/cas/webidm" with %2F-encoded slashes,
	// stored as a single segment in the cache.
	s := cache.NewStore(nil)
	s.Jobs.Put("", []jenkins.Job{
		{Name: "git/cas/webidm", FullPath: "git%2Fcas%2Fwebidm", Type: jenkins.JobTypeMultiBranch},
	})
	got, err := ResolveTarget(
		command.Target{ProjectSuffix: "webidm"},
		s,
		NavigationContext{},
	)
	if err != nil {
		t.Fatalf("expected suffix to match decoded last segment, got error: %v", err)
	}
	if got.ProjectName != "git%2Fcas%2Fwebidm" {
		t.Errorf("expected ProjectName to retain encoded form, got %q", got.ProjectName)
	}
}

func TestMatchProjectSuffix(t *testing.T) {
	paths := []string{
		"Code Private/webidm",
		"Other/webidm",
		"standalone",
		"Code Private/freestyle",
		"Foo/webidmclient",
	}
	cases := map[string][]string{
		"webidm":              {"Code Private/webidm", "Other/webidm"},
		"freestyle":           {"Code Private/freestyle"},
		"standalone":          {"standalone"},
		"Code Private/webidm": {"Code Private/webidm"},
		"webidmclient":        {"Foo/webidmclient"},
		"client":              nil, // no full-segment match (substring not allowed)
		"":                    nil, // never invoked with empty in real use, but defensive
	}
	for suffix, want := range cases {
		got := matchProjectSuffix(paths, suffix)
		if len(got) != len(want) {
			t.Errorf("suffix %q: got %v, want %v", suffix, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("suffix %q: got %v, want %v", suffix, got, want)
				break
			}
		}
	}
}

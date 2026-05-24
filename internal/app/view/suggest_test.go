package view

import (
	"reflect"
	"testing"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func newSuggestStore(t *testing.T) *cache.Store {
	t.Helper()
	s := cache.NewStore(nil)
	s.Jobs.Put("", []jmodel.Job{
		{Name: "Code Private", FullPath: "Code Private", Type: jmodel.JobTypeFolder},
		{Name: "Other", FullPath: "Other", Type: jmodel.JobTypeFolder},
		{Name: "standalone", FullPath: "standalone", Type: jmodel.JobTypePipeline},
	})
	s.Jobs.Put("Code Private", []jmodel.Job{
		{Name: "webidm", FullPath: "Code Private/webidm", Type: jmodel.JobTypeMultiBranch},
		{Name: "freestyle", FullPath: "Code Private/freestyle", Type: jmodel.JobTypeFreeStyle},
	})
	s.Jobs.Put("Other", []jmodel.Job{
		{Name: "webidm", FullPath: "Other/webidm", Type: jmodel.JobTypeMultiBranch},
	})
	return s
}

func TestProjectArgSuggest(t *testing.T) {
	s := newSuggestStore(t)

	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{
			"empty surfaces all (short form, disambiguated)",
			"",
			[]string{"Code Private/webidm", "Other/webidm", "freestyle", "standalone"},
		},
		{"whitespace before space", "Code Pri", []string{"Code Private/freestyle", "Code Private/webidm"}},
		{"unique short prefix", "free", []string{"freestyle"}},
		{"unique exact suppressed", "freestyle", nil},
		{"ambiguous last segment", "web", []string{"Code Private/webidm", "Other/webidm"}},
		{"slash forces full path", "Code", []string{"Code Private/freestyle", "Code Private/webidm"}},
		{"slash partial path", "Code Private/", []string{"Code Private/freestyle", "Code Private/webidm"}},
		{"standalone match", "stand", []string{"standalone"}},
		{"no match", "zzz", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ProjectArgSuggest(s, tc.prefix)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ProjectArgSuggest(%q) = %v, want %v", tc.prefix, got, tc.want)
			}
		})
	}
}

func TestProjectArgSuggest_EncodedProjectName(t *testing.T) {
	s := cache.NewStore(nil)
	s.Jobs.Put("", []jmodel.Job{
		{Name: "git/cas/webidm", FullPath: "git%2Fcas%2Fwebidm", Type: jmodel.JobTypeMultiBranch},
	})
	got := ProjectArgSuggest(s, "web")
	if len(got) != 1 || got[0] != "webidm" {
		t.Errorf("expected [\"webidm\"], got %v", got)
	}
}

func newTargetStore(t *testing.T) *cache.Store {
	t.Helper()
	s := cache.NewStore(nil)
	// Root: the multibranch project itself.
	s.Jobs.Put("", []jmodel.Job{
		{Name: "webidm", FullPath: "webidm", Type: jmodel.JobTypeMultiBranch},
	})
	// Inside the project: the branches, populated when the user opens the
	// project's job list in the TUI.
	s.Jobs.Put("webidm", []jmodel.Job{
		{Name: "main", FullPath: "webidm/main", Type: jmodel.JobTypePipeline, BranchType: jmodel.BranchTypeBranch},
		{Name: "feature%2Ffoo", FullPath: "webidm/feature%2Ffoo", Type: jmodel.JobTypePipeline, BranchType: jmodel.BranchTypeBranch},
	})
	s.Registry.IngestBranchList("webidm/main", []jmodel.Build{{Number: 42}, {Number: 41}, {Number: 40}})
	s.Stages.Put("webidm/main:42", []jmodel.Stage{
		{Name: "Build"},
		{Name: "Test"},
		{Name: "Deploy"},
	})
	return s
}

func TestTargetArgSuggest_Branch(t *testing.T) {
	s := newTargetStore(t)
	got := view_TargetArgSuggest(s, "webidm ")
	want := []string{"webidm feature/foo", "webidm main"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTargetArgSuggest_BranchPartial(t *testing.T) {
	s := newTargetStore(t)
	got := view_TargetArgSuggest(s, "webidm fea")
	want := []string{"webidm feature/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTargetArgSuggest_BuildNumbers(t *testing.T) {
	s := newTargetStore(t)
	got := view_TargetArgSuggest(s, "webidm main #")
	// #last + sorted-desc numbers.
	want := []string{"webidm main #last", "webidm main #42", "webidm main #41", "webidm main #40"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTargetArgSuggest_BuildPartial(t *testing.T) {
	s := newTargetStore(t)
	got := view_TargetArgSuggest(s, "webidm main #4")
	want := []string{"webidm main #42", "webidm main #41", "webidm main #40"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTargetArgSuggest_StageDisabled(t *testing.T) {
	// Once a stage marker has been opened, no further suggestions appear —
	// stage autocomplete is intentionally not provided.
	s := newTargetStore(t)
	if got := view_TargetArgSuggest(s, "webidm main #42 :"); got != nil {
		t.Errorf("expected nil after `:`, got %v", got)
	}
	if got := view_TargetArgSuggest(s, "webidm main #42 :De"); got != nil {
		t.Errorf("expected nil mid-stage, got %v", got)
	}
}

func TestTargetArgSuggest_BuildAfterBranch(t *testing.T) {
	// After project + branch + space, suggest build markers immediately
	// (no need for the user to type `#` first).
	s := newTargetStore(t)
	got := view_TargetArgSuggest(s, "webidm main ")
	want := []string{"webidm main #last", "webidm main #42", "webidm main #41", "webidm main #40"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTargetArgSuggest_MissingProject(t *testing.T) {
	s := newTargetStore(t)
	// Stage marker without project context — nothing to suggest.
	if got := view_TargetArgSuggest(s, "#42"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// view_TargetArgSuggest is a local alias so the test file matches the
// package-public name without renaming everywhere.
var view_TargetArgSuggest = TargetArgSuggest

func TestProjectArgSuggest_EmptyStore(t *testing.T) {
	s := cache.NewStore(nil)
	if got := ProjectArgSuggest(s, "web"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

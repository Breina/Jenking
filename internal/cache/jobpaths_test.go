package cache

import (
	"reflect"
	"testing"

	"github.com/Breina/Jenking/internal/jenkins"
)

func TestAllProjectPaths(t *testing.T) {
	s := NewStore(nil)

	// Root: one folder "Code Private", one project "standalone".
	s.Jobs.Put("", []jenkins.Job{
		{Name: "Code Private", FullPath: "Code Private", Type: jenkins.JobTypeFolder},
		{Name: "standalone", FullPath: "standalone", Type: jenkins.JobTypePipeline},
	})
	// "Code Private": one folder "Nested", two projects.
	s.Jobs.Put("Code Private", []jenkins.Job{
		{Name: "Nested", FullPath: "Code Private/Nested", Type: jenkins.JobTypeFolder},
		{Name: "webidm", FullPath: "Code Private/webidm", Type: jenkins.JobTypeMultiBranch},
		{Name: "freestyle", FullPath: "Code Private/freestyle", Type: jenkins.JobTypeFreeStyle},
	})
	// "Code Private/Nested": one project.
	s.Jobs.Put("Code Private/Nested", []jenkins.Job{
		{Name: "deep", FullPath: "Code Private/Nested/deep", Type: jenkins.JobTypeMultiBranch},
	})

	got := AllProjectPaths(s)
	want := []string{
		"Code Private/Nested/deep",
		"Code Private/freestyle",
		"Code Private/webidm",
		"standalone",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllProjectPaths() = %v, want %v", got, want)
	}
}

func TestAllProjectPaths_Empty(t *testing.T) {
	s := NewStore(nil)
	if got := AllProjectPaths(s); got != nil {
		t.Errorf("AllProjectPaths(empty) = %v, want nil", got)
	}
}

func TestAllProjectPaths_NilStore(t *testing.T) {
	if got := AllProjectPaths(nil); got != nil {
		t.Errorf("AllProjectPaths(nil) = %v, want nil", got)
	}
}

func TestAllProjectPaths_OnlyFolders(t *testing.T) {
	s := NewStore(nil)
	s.Jobs.Put("", []jenkins.Job{
		{Name: "F", FullPath: "F", Type: jenkins.JobTypeFolder},
	})
	// No entry for "F" in cache — walk stops there.
	if got := AllProjectPaths(s); got != nil {
		t.Errorf("AllProjectPaths(folders-only, unopened) = %v, want nil", got)
	}
}

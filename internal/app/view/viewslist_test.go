package view

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

func TestJobListViewFilterCacheKey(t *testing.T) {
	th := theme.Default()
	tests := []struct {
		name string
		view *jmodel.JenkinsView
		want string
	}{
		{"unfiltered root", nil, ""},
		// The "all" view *is* the folder listing, so it shares its cache entry.
		{"all view", &jmodel.JenkinsView{Name: "all", Kind: jmodel.ViewAll}, ""},
		// A real filter must not overwrite the folder listing: cache.AllProjectPaths
		// walks the folder-keyed entries.
		{"list view", &jmodel.JenkinsView{Name: "Team Infra", Kind: jmodel.ViewList}, "view:Team Infra@"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jl := NewJobList(th, nil, nil, "", "root", false, "", nil)
			jl.viewFilter = tt.view
			if got := jl.cacheKey(); got != tt.want {
				t.Errorf("cacheKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJobListJobNCUsesFullPath(t *testing.T) {
	th := theme.Default()
	// A view lists jobs from anywhere in the tree flat, so the row's FullPath —
	// not the list's folder — decides where navigating to it goes.
	jl := NewViewJobList(th, nil, nil, jmodel.JenkinsView{Name: "Team Infra", Kind: jmodel.ViewList}, "me", nil)
	nc := jl.jobNC(jmodel.Job{Name: "svc", FullPath: "Code/team/svc"})

	if nc.FolderPath != "Code/team" || nc.ProjectName != "svc" {
		t.Errorf("jobNC = folder %q project %q, want Code/team + svc", nc.FolderPath, nc.ProjectName)
	}
	if nc.ViewName != "Team Infra" {
		t.Errorf("nc.ViewName = %q, want the active view to travel with the context", nc.ViewName)
	}
}

func TestFindView(t *testing.T) {
	views := []jmodel.JenkinsView{
		{Name: "all", Kind: jmodel.ViewAll},
		{Name: "Team Infra", Kind: jmodel.ViewList},
	}
	if v, ok := FindView(views, "team infra"); !ok || v.Name != "Team Infra" {
		t.Errorf("case-insensitive lookup failed: %+v, %v", v, ok)
	}
	if _, ok := FindView(views, "nope"); ok {
		t.Error("FindView matched a name that does not exist")
	}
}

func TestCountJobs(t *testing.T) {
	jobs := []jmodel.Job{
		{Color: "blue", LastAnyColor: "blue"},
		{Color: "red", LastAnyColor: "red"},
		// A multibranch reports its branches' state through LastAnyColor.
		{Color: "blue", LastAnyColor: "red", RunningCount: 2},
	}
	got := countJobs(jobs)
	want := viewCounts{total: 3, running: 2, failing: 2}
	if got != want {
		t.Errorf("countJobs() = %+v, want %+v", got, want)
	}
}

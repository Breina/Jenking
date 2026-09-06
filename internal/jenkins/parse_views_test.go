package jenkins

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func TestParseViewList(t *testing.T) {
	resp := jsonViewContainer{
		Views: []jsonView{
			{Class: "hudson.model.AllView", Name: "all", URL: "https://j/"},
			{Class: "hudson.model.ListView", Name: "Team Infra", URL: "https://j/view/Team%20Infra/"},
			{Class: "hudson.plugins.nested_view.NestedView", Name: "Nested", URL: "https://j/view/Nested/"},
			{Name: ""},
		},
		PrimaryView: &jsonView{Name: "Team Infra"},
	}

	views := parseViewList(resp, "Code", false)
	if len(views) != 3 {
		t.Fatalf("len(views) = %d, want 3 (unnamed entry dropped)", len(views))
	}
	if !views[0].IsAll() {
		t.Errorf("views[0].Kind = %v, want ViewAll", views[0].Kind)
	}
	if views[0].IsPrimary || !views[1].IsPrimary {
		t.Errorf("primary flag misplaced: %v, %v", views[0].IsPrimary, views[1].IsPrimary)
	}
	// An unknown plugin class must still be listed, just unclassified.
	if views[2].Kind != jmodel.ViewOther || views[2].Name != "Nested" {
		t.Errorf("plugin view = %+v, want listed as ViewOther", views[2])
	}
	for _, v := range views {
		if v.OwnerPath != "Code" || v.Personal {
			t.Errorf("owner/personal wrong on %+v", v)
		}
	}
}

func TestParseJobListURLDerivedPath(t *testing.T) {
	// A view lists jobs from anywhere in the tree flat, so folder+name would
	// be wrong; the resolver must win over the folder-join.
	resp := jsonJobList{Jobs: []jsonJob{
		{Name: "svc", Class: "org.jenkinsci.plugins.workflow.job.WorkflowJob", URL: "https://j/job/Code/job/team/job/svc/"},
		{Name: "orphan", Class: "org.jenkinsci.plugins.workflow.job.WorkflowJob", URL: "unparseable"},
	}}
	resolve := func(jobURL string) (string, bool) { return jmodel.JobPathFromURL("https://j", jobURL) }

	jobs := parseJobList(resp, "Code", resolve)
	if jobs[0].FullPath != "Code/team/svc" {
		t.Errorf("FullPath = %q, want Code/team/svc", jobs[0].FullPath)
	}
	if jobs[1].FullPath != "Code/orphan" {
		t.Errorf("unresolvable URL should fall back to folder-join, got %q", jobs[1].FullPath)
	}
}

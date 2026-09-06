package jmodel

import "testing"

func TestJobPathFromURL(t *testing.T) {
	const base = "https://jenkins.example.com"
	tests := []struct {
		name   string
		url    string
		want   string
		wantOK bool
	}{
		{"root job", base + "/job/webidm/", "webidm", true},
		{"nested folders", base + "/job/Code/job/team/job/svc/", "Code/team/svc", true},
		{"skips view marker", base + "/job/Code/view/change-requests/job/MR-3/", "Code/MR-3", true},
		{
			// A project name containing a slash arrives double-encoded; one
			// unescape leaves the canonical percent-encoded path segment.
			"slash in name stays encoded",
			base + "/job/Code/job/git%252Fdata-platform%252Fspark/job/main/",
			"Code/git%2Fdata-platform%2Fspark/main",
			true,
		},
		{"trailing build number", base + "/job/Code/job/svc/42/console", "Code/svc", true},
		{"no job segments", base + "/view/All/", "", false},
		{"foreign origin", "https://other.example.com/job/svc/", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := JobPathFromURL(base, tt.url)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("JobPathFromURL() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestWalkJobChainCapturesViewName(t *testing.T) {
	segs, err := URLPathSegments("https://j.example.com", "https://j.example.com/view/Team%20Infra/job/svc/")
	if err != nil {
		t.Fatalf("URLPathSegments: %v", err)
	}
	chain := WalkJobChain(segs)
	if !chain.HadView || chain.ViewName != "Team Infra" {
		t.Fatalf("ViewName = %q (hadView=%v), want %q", chain.ViewName, chain.HadView, "Team Infra")
	}
	if len(chain.Segs) != 1 || chain.Segs[0] != "svc" {
		t.Fatalf("Segs = %v, want [svc]", chain.Segs)
	}
}

func TestViewPathToURL(t *testing.T) {
	if got := ViewPathToURL("", "Team Infra"); got != "/view/Team%20Infra" {
		t.Errorf("root view path = %q", got)
	}
	if got := ViewPathToURL("Code/team", "Releases"); got != "/job/Code/job/team/view/Releases" {
		t.Errorf("folder view path = %q", got)
	}
}

func TestParseViewKind(t *testing.T) {
	cases := map[string]ViewKind{
		"hudson.model.AllView":                  ViewAll,
		"hudson.model.ListView":                 ViewList,
		"hudson.model.MyView":                   ViewMy,
		"hudson.plugins.nested_view.NestedView": ViewOther,
	}
	for class, want := range cases {
		if got := ParseViewKind(class); got != want {
			t.Errorf("ParseViewKind(%q) = %v, want %v", class, got, want)
		}
	}
}

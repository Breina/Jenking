package jmodel

import "testing"

func TestCanonicalSCMURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"git@github.com:Breina/Jenking.git", "github.com/breina/jenking"},
		{"https://github.com/Breina/Jenking.git", "github.com/breina/jenking"},
		{"https://github.com/Breina/Jenking", "github.com/breina/jenking"},
		{"https://github.com/Breina/Jenking/tree/main", "github.com/breina/jenking"},
		{"https://github.com/Breina/Jenking/blob/main/README.md", "github.com/breina/jenking"},
		{"https://gitlab.example.com/team/sub/repo/-/tree/main", "gitlab.example.com/team/sub/repo"},
		{"https://gitlab.example.com/git/omv/omv-master/-/merge_requests/202", "gitlab.example.com/git/omv/omv-master"},
		{"https://github.com/org/repo/pull/42", "github.com/org/repo"},
		{"ssh://git@host.example.com:22/team/repo.git", "host.example.com/team/repo"},
		{"  git@github.com:Breina/Jenking.git  ", "github.com/breina/jenking"},
		{"", ""},
	}
	for _, c := range cases {
		if got := CanonicalSCMURL(c.in); got != c.want {
			t.Errorf("CanonicalSCMURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A local git remote and a Jenkins branch objectUrl for the same repo must
// canonicalize equal — the whole point of the reverse lookup.
func TestCanonicalSCMURL_RemoteMatchesObjectURL(t *testing.T) {
	remote := CanonicalSCMURL("git@github.com:Breina/Jenking.git")
	objectURL := CanonicalSCMURL("https://github.com/Breina/Jenking/tree/main")
	if remote != objectURL {
		t.Fatalf("remote %q != objectURL %q", remote, objectURL)
	}
}

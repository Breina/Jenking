package jmodel

import "testing"

func TestFindArtifact(t *testing.T) {
	arts := []Artifact{
		{DisplayPath: "target/app.jar", URL: "u1"},
		{DisplayPath: "reports/nested/out.txt", URL: "u2"},
	}
	tests := []struct {
		name    string
		query   string
		wantURL string
		wantOK  bool
	}{
		{"exact display path", "target/app.jar", "u1", true},
		{"basename fallback", "out.txt", "u2", true},
		{"basename of top-level", "app.jar", "u1", true},
		{"miss", "nope.txt", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FindArtifact(arts, tt.query)
			if ok != tt.wantOK || got.URL != tt.wantURL {
				t.Errorf("FindArtifact(%q) = (%q,%v), want (%q,%v)", tt.query, got.URL, ok, tt.wantURL, tt.wantOK)
			}
		})
	}
}

func TestFindArtifactExactBeatsBasename(t *testing.T) {
	// An exact display-path match must win over a basename collision.
	arts := []Artifact{
		{DisplayPath: "a/dup.txt", URL: "basename"},
		{DisplayPath: "dup.txt", URL: "exact"},
	}
	got, ok := FindArtifact(arts, "dup.txt")
	if !ok || got.URL != "exact" {
		t.Errorf("got %q,%v; want exact,true", got.URL, ok)
	}
}

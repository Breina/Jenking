package usecase

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func TestMatchArtifact(t *testing.T) {
	arts := []jmodel.Artifact{
		{DisplayPath: "target/report.html", URL: "https://j/1/artifact/target/report.html"},
		{DisplayPath: "report.html", URL: "https://j/1/artifact/report.html"},
	}

	tests := []struct {
		name    string
		query   string
		wantURL string
	}{
		{"exact display path", "target/report.html", "https://j/1/artifact/target/report.html"},
		{"exact wins over base name", "report.html", "https://j/1/artifact/report.html"},
		{"case-insensitive", "TARGET/Report.HTML", "https://j/1/artifact/target/report.html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matchArtifact(arts, tt.query)
			if err != nil {
				t.Fatalf("matchArtifact(%q): %v", tt.query, err)
			}
			if got.URL != tt.wantURL {
				t.Errorf("got %q, want %q", got.URL, tt.wantURL)
			}
		})
	}

	t.Run("base name falls back to nested artifact", func(t *testing.T) {
		only := []jmodel.Artifact{arts[0]}
		got, err := matchArtifact(only, "report.html")
		if err != nil {
			t.Fatalf("matchArtifact: %v", err)
		}
		if got.URL != arts[0].URL {
			t.Errorf("got %q, want %q", got.URL, arts[0].URL)
		}
	})

	t.Run("unknown name lists what is archived", func(t *testing.T) {
		_, err := matchArtifact(arts, "missing.txt")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "target/report.html") {
			t.Errorf("error should list archived artifacts, got: %v", err)
		}
	})

	t.Run("empty name is rejected", func(t *testing.T) {
		if _, err := matchArtifact(arts, ""); err == nil {
			t.Fatal("expected an error")
		}
	})
}

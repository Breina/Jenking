package usecase

import (
	"testing"

	"github.com/Breina/Jenking/internal/cache"
)

func TestResolveJob(t *testing.T) {
	store := cache.NewStore(nil)
	// A multibranch repo with two branches, plus an unrelated repo.
	store.RepoURLs.Put("Code/omv/dev", "https://github.com/org/omv/tree/dev")
	store.RepoURLs.Put("Code/omv/main", "https://github.com/org/omv/tree/main")
	store.RepoURLs.Put("Code/other/main", "https://github.com/org/other/tree/main")
	store.RepoURLs.Put("Code/nope/main", "") // no SCM URL

	d := Deps{Store: store}

	// A local SSH remote must resolve to both branch jobs of the same repo,
	// primary branch first.
	got := d.ResolveJob("git@github.com:org/omv.git")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(got), got)
	}
	if got[0].Branch != "main" {
		t.Errorf("expected primary branch first, got %q", got[0].Branch)
	}
	if got[0].JobPath != "Code/omv/main" {
		t.Errorf("unexpected job path: %q", got[0].JobPath)
	}

	// No match returns empty.
	if got := d.ResolveJob("git@github.com:org/absent.git"); len(got) != 0 {
		t.Errorf("expected no matches, got %+v", got)
	}

	// Empty/garbage query returns empty.
	if got := d.ResolveJob(""); got != nil {
		t.Errorf("expected nil for empty query, got %+v", got)
	}
}

package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type scmFakeClient struct {
	jmodel.JenkinsClient
	calls atomic.Int32
	url   string
}

func (f *scmFakeClient) GetJobSCMURL(context.Context, string) (string, error) {
	f.calls.Add(1)
	return f.url, nil
}

func TestFillSCMURLs_SkipsAlreadyCached(t *testing.T) {
	store := cache.NewStore(nil)
	client := &scmFakeClient{url: "https://github.com/org/repo/tree/main"}

	paths := []string{"org/repo/main", "org/repo/main", "org/repo/dev"}
	FillSCMURLs(context.Background(), client, store, paths)
	// 2 distinct paths -> 2 fetches (dup within the call is skipped).
	if got := client.calls.Load(); got != 2 {
		t.Fatalf("first fill: got %d fetches, want 2", got)
	}
	if e := store.RepoURLs.Get("org/repo/main"); e == nil || e.Value == "" {
		t.Fatalf("expected main SCM URL cached, got %v", e)
	}

	// Second call: everything is cached, so no new fetches.
	FillSCMURLs(context.Background(), client, store, paths)
	if got := client.calls.Load(); got != 2 {
		t.Fatalf("second fill: got %d fetches, want 2 (all cached)", got)
	}
}

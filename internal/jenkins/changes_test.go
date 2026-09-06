package jenkins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func changesServer(t *testing.T, body string, capture *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = r.URL.RawQuery
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestGetChanges_Pipeline(t *testing.T) {
	body := `{"changeSets":[{"items":[
		{"commitId":"abc123","msg":"first line\nbody","timestamp":1700000000000,"author":{"fullName":"Jane"},"authorEmail":"j@x.io","affectedPaths":["a.go","b.go"]},
		{"commitId":"def456","msg":"second","timestamp":1700000100000,"author":{"fullName":"Bob"}}
	]}]}`
	srv := changesServer(t, body, nil)
	defer srv.Close()

	c := NewClient(srv.URL, "u", "t", false)
	changes, err := c.GetChanges(context.Background(), "app", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("want 2 changes, got %d", len(changes))
	}
	first := changes[0]
	if first.CommitID != "abc123" || first.Author != "Jane" || first.AuthorEmail != "j@x.io" {
		t.Errorf("unexpected first change: %+v", first)
	}
	if first.Message != "first line\nbody" {
		t.Errorf("message not preserved: %q", first.Message)
	}
	if !first.Timestamp.Equal(time.UnixMilli(1700000000000)) {
		t.Errorf("timestamp mismatch: %v", first.Timestamp)
	}
	if len(first.AffectedPaths) != 2 {
		t.Errorf("affected paths: %v", first.AffectedPaths)
	}
}

func TestGetChanges_Freestyle(t *testing.T) {
	// AbstractBuild exposes the singular changeSet object.
	body := `{"changeSet":{"items":[{"commitId":"aaa","msg":"m","author":{"fullName":"A"}}]}}`
	srv := changesServer(t, body, nil)
	defer srv.Close()

	c := NewClient(srv.URL, "u", "t", false)
	changes, err := c.GetChanges(context.Background(), "app", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].CommitID != "aaa" {
		t.Fatalf("unexpected changes: %+v", changes)
	}
}

func TestGetChanges_Empty(t *testing.T) {
	// First build / no SCM: neither key populated.
	srv := changesServer(t, `{}`, nil)
	defer srv.Close()
	c := NewClient(srv.URL, "u", "t", false)
	changes, err := c.GetChanges(context.Background(), "app", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("want no changes, got %+v", changes)
	}
}

func TestFindCommit(t *testing.T) {
	body := `{"builds":[
		{"number":10,"changeSets":[{"items":[{"commitId":"ABC123def"}]}]},
		{"number":9,"changeSets":[{"items":[{"commitId":"999aaa"}]}]},
		{"number":8,"changeSet":{"items":[{"commitId":"abcXXX"},{"commitId":"zzz"}]}}
	]}`
	var query string
	srv := changesServer(t, body, &query)
	defer srv.Close()

	c := NewClient(srv.URL, "u", "t", false)
	hits, err := c.FindCommit(context.Background(), "app", "abc", 25)
	if err != nil {
		t.Fatal(err)
	}
	// Case-insensitive prefix: build 10 (ABC123def) and build 8 (abcXXX); not 9.
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %+v", hits)
	}
	if hits[0].BuildNumber != 10 || hits[0].CommitID != "ABC123def" {
		t.Errorf("unexpected hit[0]: %+v", hits[0])
	}
	if hits[1].BuildNumber != 8 {
		t.Errorf("unexpected hit[1]: %+v", hits[1])
	}
	if !strings.Contains(query, "0%2C25") && !strings.Contains(query, "0,25") {
		t.Errorf("expected {0,25} range in tree query, got %q", query)
	}
}

func TestFindCommit_ClampsAndEmptyPrefix(t *testing.T) {
	var query string
	srv := changesServer(t, `{"builds":[]}`, &query)
	defer srv.Close()
	c := NewClient(srv.URL, "u", "t", false)

	// maxBuilds over the cap is clamped to 50.
	if _, err := c.FindCommit(context.Background(), "app", "x", 999); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "0%2C50") && !strings.Contains(query, "0,50") {
		t.Errorf("expected clamp to {0,50}, got %q", query)
	}

	// Empty prefix must never match.
	hits, err := c.FindCommit(context.Background(), "app", "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("empty prefix should yield no hits, got %+v", hits)
	}
}

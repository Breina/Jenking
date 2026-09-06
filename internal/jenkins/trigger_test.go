package jenkins

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseQueueItemID(t *testing.T) {
	tests := []struct {
		location string
		want     int64
	}{
		{"https://ci.example.com/queue/item/1234/", 1234},
		{"https://ci.example.com/queue/item/1234", 1234},
		{"http://ci/jenkins/queue/item/7/", 7},
		{"", 0},
		{"https://ci.example.com/job/foo/", 0},
		{"https://ci.example.com/queue/item/notanumber/", 0},
	}
	for _, tt := range tests {
		if got := parseQueueItemID(tt.location); got != tt.want {
			t.Errorf("parseQueueItemID(%q) = %d, want %d", tt.location, got, tt.want)
		}
	}
}

func TestTriggerBuildReturnsQueueID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Location", "http://ci.example.com/queue/item/555/")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "", "", false).TriggerBuild(t.Context(), "api/main", nil)
	if err != nil {
		t.Fatalf("TriggerBuild() error: %v", err)
	}
	if id != 555 {
		t.Errorf("expected queue id 555, got %d", id)
	}
}

func TestTriggerBuildWithParamsUsesBuildWithParameters(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "", "", false).TriggerBuild(t.Context(), "api/main", map[string]string{"ENV": "prod"})
	if err != nil {
		t.Fatalf("TriggerBuild() error: %v", err)
	}
	if id != 0 {
		t.Errorf("expected queue id 0 without Location header, got %d", id)
	}
	if gotPath != "/job/api/job/main/buildWithParameters" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if gotQuery != "ENV=prod" {
		t.Errorf("unexpected query: %s", gotQuery)
	}
}

func TestGetQueueItem(t *testing.T) {
	const waiting = `{"id": 42, "inQueueSince": 1700000000000, "why": "Waiting for executor",
		"blocked": false, "buildable": true, "stuck": false, "pending": false,
		"task": {"name": "main", "url": ""}, "actions": []}`
	const started = `{"id": 42, "inQueueSince": 1700000000000, "why": null,
		"blocked": false, "buildable": false, "stuck": false, "pending": false,
		"task": {"name": "main", "url": ""}, "executable": {"number": 17}, "actions": []}`

	body := waiting
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/queue/item/42/api/json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "", "", false)

	item, num, err := client.GetQueueItem(t.Context(), 42)
	if err != nil {
		t.Fatalf("GetQueueItem() error: %v", err)
	}
	if num != 0 {
		t.Errorf("expected no build number while waiting, got %d", num)
	}
	if item == nil || item.ID != 42 || item.Why != "Waiting for executor" {
		t.Errorf("unexpected item: %+v", item)
	}

	body = started
	_, num, err = client.GetQueueItem(t.Context(), 42)
	if err != nil {
		t.Fatalf("GetQueueItem() error: %v", err)
	}
	if num != 17 {
		t.Errorf("expected build number 17, got %d", num)
	}
}

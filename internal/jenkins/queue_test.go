package jenkins

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const queueFixture = `{
  "items": [
    {
      "id": 101,
      "inQueueSince": 1700000000000,
      "why": "Waiting for next available executor",
      "blocked": false,
      "buildable": true,
      "stuck": false,
      "pending": false,
      "task": {"name": "main", "url": "%[1]s/job/api/job/main/"},
      "actions": [{"causes": [{"shortDescription": "Started by alice", "userId": "alice", "userName": "Alice"}]}]
    },
    {
      "id": 102,
      "inQueueSince": 1700000005000,
      "why": "Build #5 is already in progress (ETA: 2 min)",
      "blocked": true,
      "buildable": false,
      "stuck": false,
      "pending": false,
      "task": {"name": "web", "url": "%[1]s/job/web/job/PR-42/"},
      "actions": []
    },
    {
      "id": 103,
      "inQueueSince": 1700000010000,
      "why": null,
      "blocked": false,
      "buildable": false,
      "stuck": false,
      "pending": true,
      "task": {"name": "batch", "url": "%[1]s/job/batch/"},
      "actions": []
    }
  ]
}`

func TestListQueueParsesItems(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Substitute the server URL so task URLs resolve to a job path.
		_, _ = fmt.Fprintf(w, queueFixture, srvURL)
	}))
	defer srv.Close()
	srvURL = srv.URL

	client := NewClient(srv.URL, "", "", false)
	items, err := client.ListQueue(t.Context())
	if err != nil {
		t.Fatalf("ListQueue() error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 queue items, got %d", len(items))
	}

	buildable := items[0]
	if buildable.ID != 101 || buildable.JobPath != "api/main" || !buildable.Buildable {
		t.Errorf("buildable item mismatch: %+v", buildable)
	}
	if buildable.TriggeredBy != "alice" || buildable.Why == "" {
		t.Errorf("expected cause/why on buildable item: %+v", buildable)
	}

	blocked := items[1]
	if blocked.JobPath != "web/PR-42" || !blocked.Blocked {
		t.Errorf("blocked item mismatch: %+v", blocked)
	}

	pending := items[2]
	if pending.JobPath != "batch" || !pending.Pending {
		t.Errorf("pending item mismatch: %+v", pending)
	}
	if pending.Why != "" {
		t.Errorf("expected empty why for null reason, got %q", pending.Why)
	}
}

func TestListQueueDropsHandedOffItems(t *testing.T) {
	// An item that already has an executable (number) has been handed to an
	// executor — it is now a running build, so ListQueue must drop it to avoid a
	// queued/running duplicate during the handoff.
	const fixture = `{"items":[
	  {"id":1,"inQueueSince":1700000000000,"why":"waiting","buildable":true,"task":{"name":"a","url":"%[1]s/job/a/"}},
	  {"id":2,"inQueueSince":1700000000000,"why":null,"pending":true,"executable":{"number":7},"task":{"name":"b","url":"%[1]s/job/b/"}}
	]}`
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, fixture, srvURL)
	}))
	defer srv.Close()
	srvURL = srv.URL

	items, err := NewClient(srv.URL, "", "", false).ListQueue(t.Context())
	if err != nil {
		t.Fatalf("ListQueue() error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (handed-off dropped), got %d", len(items))
	}
	if items[0].ID != 1 {
		t.Errorf("expected the still-waiting item (id 1), got id %d", items[0].ID)
	}
}

func TestQueueSubStatePriority(t *testing.T) {
	// parseTaskURL is exercised above; here verify task-URL edge cases.
	if got := parseTaskURL("http://ci", "http://ci/job/Folder/job/Sub/job/main/"); got != "Folder/Sub/main" {
		t.Errorf("nested parseTaskURL = %q, want Folder/Sub/main", got)
	}
	if got := parseTaskURL("http://ci", "http://ci/"); got != "" {
		t.Errorf("rootless parseTaskURL = %q, want empty", got)
	}
}

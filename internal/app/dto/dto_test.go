package dto

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// TestBuildDetailJSONShape locks the wire contract the CLI (`-o json`) and the
// MCP server both depend on: field names, inlining of the embedded Build, and
// omitempty behavior. If this changes, both surfaces change together.
func TestBuildDetailJSONShape(t *testing.T) {
	d := ToBuildDetail("folder/app", jmodel.BuildDetail{
		Build: jmodel.Build{
			Number:    42,
			Status:    jmodel.BuildStatus("SUCCESS"),
			Duration:  90 * time.Second,
			Timestamp: time.Unix(1700000000, 0),
			Params:    map[string]string{"TAG": "v1"},
		},
	})
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	// Embedded Build fields must inline at the top level (not nested).
	for _, want := range []string{
		`"number":42`,
		`"status":"SUCCESS"`,
		`"duration_ms":90000`,
		`"timestamp":1700000000`,
		`"job_path":"folder/app"`,
		`"parameters":{"TAG":"v1"}`,
	} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("JSON missing %s\nfull: %s", want, got)
		}
	}
	// pending_inputs is omitempty — absent when there are none.
	if bytes.Contains(raw, []byte("pending_inputs")) {
		t.Errorf("expected pending_inputs omitted, got: %s", got)
	}
}

func TestQueueItemState(t *testing.T) {
	tests := []struct {
		item jmodel.QueueItem
		want string
	}{
		{jmodel.QueueItem{Stuck: true}, "stuck"},
		{jmodel.QueueItem{Blocked: true}, "blocked"},
		{jmodel.QueueItem{Pending: true}, "pending"},
		{jmodel.QueueItem{}, "buildable"},
		{jmodel.QueueItem{Stuck: true, Blocked: true}, "stuck"}, // stuck wins
	}
	for _, tt := range tests {
		if got := ToQueueItem(tt.item).State; got != tt.want {
			t.Errorf("state = %q, want %q", got, tt.want)
		}
	}
}

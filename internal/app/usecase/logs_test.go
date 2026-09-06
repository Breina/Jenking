package usecase

import (
	"context"
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// logFakeClient serves progressive logs and stage logs from canned data.
type logFakeClient struct {
	jmodel.JenkinsClient
	chunks   map[int]*jmodel.ProgressiveLog // keyed by requested start offset
	stages   []jmodel.Stage
	nodeLogs map[int]string
}

func (f logFakeClient) GetProgressiveLog(_ context.Context, _ string, _, start int) (*jmodel.ProgressiveLog, error) {
	return f.chunks[start], nil
}
func (f logFakeClient) ListStages(context.Context, string, int) ([]jmodel.Stage, error) {
	return f.stages, nil
}
func (f logFakeClient) GetNodeLog(_ context.Context, _ string, _, nodeID int) (string, error) {
	return f.nodeLogs[nodeID], nil
}

func TestFetchLog_FullComplete(t *testing.T) {
	d := Deps{Client: logFakeClient{chunks: map[int]*jmodel.ProgressiveLog{
		0: {Text: "line1\n", MoreData: true, NextStart: 6},
		6: {Text: "line2\n", MoreData: false, NextStart: 12},
	}}}
	text, complete, err := d.FetchLog(context.Background(), "job", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "line1\nline2\n" {
		t.Errorf("text = %q", text)
	}
	if !complete {
		t.Error("expected complete=true for a finished build")
	}
}

func TestFetchLog_FullRunningCaughtUp(t *testing.T) {
	// A running build: the second fetch returns no new bytes but MoreData stays
	// set. FetchLog must stop (not spin) and report incomplete.
	d := Deps{Client: logFakeClient{chunks: map[int]*jmodel.ProgressiveLog{
		0: {Text: "partial", MoreData: true, NextStart: 7},
		7: {Text: "", MoreData: true, NextStart: 7},
	}}}
	text, complete, err := d.FetchLog(context.Background(), "job", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "partial" {
		t.Errorf("text = %q", text)
	}
	if complete {
		t.Error("expected complete=false for a running build")
	}
}

func TestFetchLog_Stage(t *testing.T) {
	d := Deps{Client: logFakeClient{
		stages: []jmodel.Stage{
			{Name: "Build", Status: jmodel.BuildStatusSuccess, NodeIDs: []int{3}},
			{Name: "Deploy", Status: jmodel.BuildStatusRunning, NodeIDs: []int{7, 8}},
		},
		nodeLogs: map[int]string{7: "deploying", 8: "done\n"},
	}}

	// Case-insensitive match; two nodes concatenated with a newline inserted
	// after the first (which lacked one).
	text, complete, err := d.FetchLog(context.Background(), "job", 1, "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if text != "deploying\ndone\n" {
		t.Errorf("stage text = %q", text)
	}
	if complete {
		t.Error("running stage should be incomplete")
	}

	if _, _, err := d.FetchLog(context.Background(), "job", 1, "nope"); err == nil {
		t.Error("expected error for unknown stage")
	}
}

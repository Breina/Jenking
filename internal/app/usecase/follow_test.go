package usecase

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// followFakeClient replays one progressive-log response per call, so a test can
// script a log that grows across polls. Once the script runs out, the last
// response repeats (a log that keeps saying "still running, nothing new").
type followFakeClient struct {
	jmodel.JenkinsClient
	script []jmodel.ProgressiveLog
	calls  int
}

func (f *followFakeClient) next() *jmodel.ProgressiveLog {
	i := f.calls
	f.calls++
	if i >= len(f.script) {
		i = len(f.script) - 1
	}
	pl := f.script[i]
	return &pl
}

func (f *followFakeClient) GetProgressiveLog(_ context.Context, _ string, _, _ int) (*jmodel.ProgressiveLog, error) {
	return f.next(), nil
}

func (f *followFakeClient) GetScanLogProgressive(_ context.Context, _ string, _ int) (*jmodel.ProgressiveLog, error) {
	return f.next(), nil
}

func followDeps(t *testing.T, c jmodel.JenkinsClient) Deps {
	t.Helper()
	disk, err := cache.NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	return Deps{Client: c, Store: cache.NewStore(disk)}
}

func TestFollowBuildLog_MatchOnLaterPoll(t *testing.T) {
	// Poll 1 sees only the first line; the pattern lands in poll 2.
	c := &followFakeClient{script: []jmodel.ProgressiveLog{
		{Text: "starting\n", MoreData: true, NextStart: 9},
		{Text: "", MoreData: true, NextStart: 9},
		{Text: "ERROR: boom\n", MoreData: true, NextStart: 21},
		{Text: "", MoreData: true, NextStart: 21},
	}}
	d := followDeps(t, c)

	res, err := d.FollowBuildLog(context.Background(), "team/app", 42, "", FollowOptions{
		Pattern:  regexp.MustCompile(`ERROR: (\w+)`),
		Poll:     time.Millisecond,
		Deadline: time.Now().Add(5 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched {
		t.Fatalf("expected a match, got %+v", res)
	}
	if res.MatchText != "ERROR: boom" {
		t.Errorf("match = %q", res.MatchText)
	}
	if res.Line != "ERROR: boom" || res.LineNumber != 2 {
		t.Errorf("line %q at %d, want %q at 2", res.Line, res.LineNumber, "ERROR: boom")
	}
	if res.OffsetBytes != len("starting\n") {
		t.Errorf("offset = %d", res.OffsetBytes)
	}
	if res.Complete || res.TimedOut {
		t.Errorf("running log should be incomplete and not timed out: %+v", res)
	}
	if res.File.Path == "" || res.File.Size == 0 {
		t.Errorf("expected the log on disk, got %+v", res.File)
	}
}

func TestFollowBuildLog_LogEndsWithoutMatch(t *testing.T) {
	c := &followFakeClient{script: []jmodel.ProgressiveLog{
		{Text: "all fine\n", MoreData: false, NextStart: 9},
	}}
	d := followDeps(t, c)

	res, err := d.FollowBuildLog(context.Background(), "team/app", 42, "", FollowOptions{
		Pattern: regexp.MustCompile(`never`),
		Poll:    time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched || res.TimedOut {
		t.Errorf("expected no match and no timeout: %+v", res)
	}
	if !res.Complete || !res.File.Complete {
		t.Errorf("expected complete=true once the log ended: %+v", res)
	}
}

func TestFollowBuildLog_TimesOut(t *testing.T) {
	// A log that keeps running and never matches: the deadline ends the call.
	c := &followFakeClient{script: []jmodel.ProgressiveLog{
		{Text: "working\n", MoreData: true, NextStart: 8},
		{Text: "", MoreData: true, NextStart: 8},
	}}
	d := followDeps(t, c)

	res, err := d.FollowBuildLog(context.Background(), "team/app", 42, "", FollowOptions{
		Pattern:  regexp.MustCompile(`done`),
		Poll:     time.Millisecond,
		Deadline: time.Now().Add(20 * time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.Matched || res.Complete {
		t.Fatalf("expected a plain timeout, got %+v", res)
	}
	if res.File.Path == "" {
		t.Error("a timed-out follow must still hand back the log file")
	}
}

func TestFollowScanLog_Match(t *testing.T) {
	c := &followFakeClient{script: []jmodel.ProgressiveLog{
		{Text: "Checking branches...\n", MoreData: true, NextStart: 21},
		{Text: "Scheduled build for branch: main\n", MoreData: false, NextStart: 53},
	}}
	d := followDeps(t, c)

	res, err := d.FollowScanLog(context.Background(), "team/app", FollowOptions{
		Pattern: regexp.MustCompile(`Scheduled build for branch: (\S+)`),
		Poll:    time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched || res.LineNumber != 2 {
		t.Fatalf("expected a match on line 2, got %+v", res)
	}
}

func TestFollowLog_RequiresPattern(t *testing.T) {
	d := followDeps(t, &followFakeClient{script: []jmodel.ProgressiveLog{{}}})
	if _, err := d.FollowScanLog(context.Background(), "team/app", FollowOptions{}); err == nil {
		t.Fatal("expected an error without a pattern")
	}
}

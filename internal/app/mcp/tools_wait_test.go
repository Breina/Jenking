package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// newBuildFake stubs the two client calls pollNewBuild makes.
type newBuildFake struct {
	jmodel.JenkinsClient
	builds []jmodel.Build
	queue  []jmodel.QueueItem
}

// ListJobs answers the existence probe in usecase.CanonicalJobPath.
func (newBuildFake) ListJobs(context.Context, string) ([]jmodel.Job, error) {
	return nil, nil
}

func (f newBuildFake) ListBuilds(context.Context, string) ([]jmodel.Build, error) {
	return f.builds, nil
}
func (f newBuildFake) ListQueue(context.Context) ([]jmodel.QueueItem, error) {
	return f.queue, nil
}

func TestPollNewBuild_NewNumberStarted(t *testing.T) {
	d := usecase.Deps{Client: newBuildFake{builds: []jmodel.Build{{Number: 43, Status: jmodel.BuildStatusRunning}}}}
	out, found, err := pollNewBuild(context.Background(), d, "team/app", 42, time.Now())
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if out.State != "started" || out.BuildNumber != 43 || !out.Done {
		t.Errorf("got state=%q num=%d done=%v", out.State, out.BuildNumber, out.Done)
	}
}

func TestPollNewBuild_QueuedWhenNoNewNumber(t *testing.T) {
	d := usecase.Deps{Client: newBuildFake{
		builds: []jmodel.Build{{Number: 42, Status: jmodel.BuildStatusSuccess}},
		queue:  []jmodel.QueueItem{{JobPath: "team/app", Why: "waiting for executor"}},
	}}
	out, found, err := pollNewBuild(context.Background(), d, "team/app", 42, time.Now())
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if out.State != "queued" || out.BuildNumber != 0 || out.Queue == nil {
		t.Errorf("got state=%q num=%d queue=%v", out.State, out.BuildNumber, out.Queue)
	}
}

func TestPollNewBuild_QueueItemForOtherJobIgnored(t *testing.T) {
	d := usecase.Deps{Client: newBuildFake{
		builds: []jmodel.Build{{Number: 42, Status: jmodel.BuildStatusSuccess}},
		queue:  []jmodel.QueueItem{{JobPath: "other/job"}},
	}}
	_, found, err := pollNewBuild(context.Background(), d, "team/app", 42, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("queue item for a different job should not count as activity")
	}
}

func TestPollNewBuild_NothingYet(t *testing.T) {
	d := usecase.Deps{Client: newBuildFake{builds: []jmodel.Build{{Number: 42}}}}
	_, found, err := pollNewBuild(context.Background(), d, "team/app", 42, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("no new build and empty queue must not report activity")
	}
}

func TestLatestBuildNumber_EmptyIsZero(t *testing.T) {
	d := usecase.Deps{Client: newBuildFake{builds: nil}}
	n, err := latestBuildNumber(context.Background(), d, "team/app")
	if err != nil || n != 0 {
		t.Errorf("got n=%d err=%v, want 0", n, err)
	}
}

func TestWaitDone(t *testing.T) {
	cases := map[jmodel.BuildStatus]bool{
		jmodel.BuildStatusRunning:     false,
		jmodel.BuildStatusQueued:      false,
		jmodel.BuildStatusUnknown:     false,
		jmodel.BuildStatusSuccess:     true,
		jmodel.BuildStatusFailed:      true,
		jmodel.BuildStatusAborted:     true,
		jmodel.BuildStatusUnstable:    true,
		jmodel.BuildStatusPausedInput: true, // needs a decision — hand control back
	}
	for status, want := range cases {
		if got := waitDone(status); got != want {
			t.Errorf("waitDone(%s) = %v, want %v", status, got, want)
		}
	}
}

func TestWaitCap_ExplicitTimeoutHonoredAsIs(t *testing.T) {
	d := jmodel.BuildDetail{}
	// No clamping: whatever the caller passes is what they get.
	if got := waitCap(1, d); got != time.Second {
		t.Errorf("tiny timeout: got %v, want 1s", got)
	}
	if got := waitCap(99999, d); got != 99999*time.Second {
		t.Errorf("huge timeout: got %v, want 99999s", got)
	}
	if got := waitCap(30, d); got != 30*time.Second {
		t.Errorf("in-range timeout: got %v, want 30s", got)
	}
}

func TestWaitCap_FromEstimate(t *testing.T) {
	// Build started 60s ago, estimated 120s total → ~60s remaining + 50% grace.
	d := jmodel.BuildDetail{Build: jmodel.Build{
		EstimatedDuration: 120 * time.Second,
		Timestamp:         time.Now().Add(-60 * time.Second),
	}}
	got := waitCap(0, d)
	// remaining ≈ 60s, grace = 60s → ≈ 120s. Allow slack for elapsed test time.
	if got < 116*time.Second || got > 122*time.Second {
		t.Errorf("estimate-derived cap = %v, want ~120s", got)
	}
}

func TestWaitCap_NoEstimateFallback(t *testing.T) {
	d := jmodel.BuildDetail{} // no estimate, no timestamp
	if got := waitCap(0, d); got != waitNoEstimateDefault {
		t.Errorf("no-estimate cap = %v, want %v", got, waitNoEstimateDefault)
	}
}

func TestWaitCap_OverrunFallsBackToDefault(t *testing.T) {
	// Build has already run past its estimate → remaining <= 0.
	d := jmodel.BuildDetail{Build: jmodel.Build{
		EstimatedDuration: 30 * time.Second,
		Timestamp:         time.Now().Add(-120 * time.Second),
	}}
	if got := waitCap(0, d); got != waitNoEstimateDefault {
		t.Errorf("overrun cap = %v, want %v", got, waitNoEstimateDefault)
	}
}

func TestBuildWaitOut_TimedOutSuggestsCheckBack(t *testing.T) {
	d := jmodel.BuildDetail{Build: jmodel.Build{
		Number:            7,
		Status:            jmodel.BuildStatusRunning,
		EstimatedDuration: 300 * time.Second,
		Timestamp:         time.Now().Add(-60 * time.Second),
	}}
	out := buildWaitOut("team/app", 7, d, time.Now().Add(-10*time.Second), true)
	if out.Done || !out.TimedOut {
		t.Fatalf("timed-out result: Done=%v TimedOut=%v", out.Done, out.TimedOut)
	}
	// ~240s remaining on the estimate.
	if out.CheckBackInSeconds < 230 || out.CheckBackInSeconds > 245 {
		t.Errorf("check-back = %ds, want ~240s", out.CheckBackInSeconds)
	}
}

func TestBuildWaitOut_DoneHasNoCheckBack(t *testing.T) {
	d := jmodel.BuildDetail{Build: jmodel.Build{Number: 7, Status: jmodel.BuildStatusSuccess}}
	out := buildWaitOut("team/app", 7, d, time.Now().Add(-5*time.Second), false)
	if !out.Done || out.TimedOut {
		t.Fatalf("done result: Done=%v TimedOut=%v", out.Done, out.TimedOut)
	}
	if out.CheckBackInSeconds != 0 {
		t.Errorf("done result should not suggest check-back, got %d", out.CheckBackInSeconds)
	}
}

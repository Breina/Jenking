package engine

import (
	"context"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// completeFake extends the running/queue fake with the calls the completion
// cascade makes, so a departed build can be finalized.
type completeFake struct {
	engFakeClient
	detail jmodel.BuildDetail
}

func (f completeFake) GetBuild(context.Context, string, int) (*jmodel.BuildDetail, error) {
	d := f.detail
	return &d, nil
}
func (completeFake) ListStages(context.Context, string, int) ([]jmodel.Stage, error) {
	return nil, nil
}
func (completeFake) GetTestReport(context.Context, string, int) (*jmodel.TestReport, error) {
	return &jmodel.TestReport{}, nil
}

// recvType drains the event channel until it finds a message of type T or times
// out, returning the message and whether it was found.
func recvType[T any](t *testing.T, ch <-chan any) (T, bool) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case msg := <-ch:
			if v, ok := msg.(T); ok {
				return v, true
			}
		case <-deadline:
			var zero T
			return zero, false
		}
	}
}

func TestEngine_SubscribeEmitsRunningUpdate(t *testing.T) {
	store := cache.NewStore(nil)
	e := New(engFakeClient{
		running: []jmodel.UserBuild{{JobPath: "team/app", Build: jmodel.Build{Number: 42, Status: jmodel.BuildStatusRunning}}},
	}, store)
	ch := e.Subscribe()

	e.pollOnce(context.Background())

	upd, ok := recvType[navmsg.RunningBuildsUpdatedMsg](t, ch)
	if !ok {
		t.Fatal("no RunningBuildsUpdatedMsg emitted")
	}
	if upd.Count != 1 {
		t.Errorf("Count = %d, want 1", upd.Count)
	}
	if len(upd.Arrived) != 1 || upd.Arrived[0] != jmodel.BuildKey("team/app", 42) {
		t.Errorf("Arrived = %v, want the build key", upd.Arrived)
	}
}

func TestEngine_PollErrorEmitsConnectionLost(t *testing.T) {
	store := cache.NewStore(nil)
	e := New(engFakeClient{err: context.DeadlineExceeded}, store)
	ch := e.Subscribe()

	e.pollOnce(context.Background())

	if _, ok := recvType[navmsg.ConnectionLostMsg](t, ch); !ok {
		t.Fatal("expected ConnectionLostMsg on poll error")
	}
}

func TestEngine_DepartureCompletesBuild(t *testing.T) {
	store := cache.NewStore(nil)
	build := jmodel.UserBuild{JobPath: "team/app", Build: jmodel.Build{Number: 42, Status: jmodel.BuildStatusRunning}}
	fake := &completeFake{detail: jmodel.BuildDetail{Build: jmodel.Build{Number: 42, Status: jmodel.BuildStatusSuccess}}}
	e := New(fake, store)
	ch := e.Subscribe()

	fake.running = []jmodel.UserBuild{build}
	e.pollOnce(context.Background()) // arrival
	fake.running = nil
	e.pollOnce(context.Background()) // departure → completion cascade goroutine

	done, ok := recvType[navmsg.BuildCompletedMsg](t, ch)
	if !ok {
		t.Fatal("expected BuildCompletedMsg after departure")
	}
	if done.Number != 42 || done.Build.Status != jmodel.BuildStatusSuccess {
		t.Errorf("completed msg = %+v, want #42 success", done)
	}
}

func TestEngine_NoSubscriberSkipsEvents(t *testing.T) {
	store := cache.NewStore(nil)
	e := New(engFakeClient{
		running: []jmodel.UserBuild{{JobPath: "team/app", Build: jmodel.Build{Number: 1, Status: jmodel.BuildStatusRunning}}},
	}, store)
	// No Subscribe(): pollOnce must still keep the registry live without panicking
	// on the nil event channel.
	e.pollOnce(context.Background())
	if store.Registry.RunningCount() != 1 {
		t.Errorf("RunningCount = %d, want 1", store.Registry.RunningCount())
	}
}

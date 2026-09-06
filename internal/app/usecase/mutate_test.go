package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type queueStep struct {
	num int
	why string
}

// mutateFake is a stateful fake for the trigger/wait, node, and input helpers.
type mutateFake struct {
	jmodel.JenkinsClient
	triggerQueueID  int64
	triggeredParams map[string]string

	queueCalls int
	queueSeq   []queueStep

	buildCalls int
	buildSeq   []jmodel.BuildDetail

	nodes         []jmodel.Node
	toggled       bool
	toggledReason string
}

func at[T any](s []T, i int) T {
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}

func (f *mutateFake) TriggerBuild(_ context.Context, _ string, params map[string]string) (int64, error) {
	f.triggeredParams = params
	return f.triggerQueueID, nil
}

func (f *mutateFake) GetQueueItem(_ context.Context, _ int64) (*jmodel.QueueItem, int, error) {
	s := at(f.queueSeq, f.queueCalls)
	f.queueCalls++
	if s.num > 0 {
		return nil, s.num, nil
	}
	return &jmodel.QueueItem{Why: s.why}, 0, nil
}

func (f *mutateFake) GetBuild(_ context.Context, _ string, _ int) (*jmodel.BuildDetail, error) {
	d := at(f.buildSeq, f.buildCalls)
	f.buildCalls++
	return &d, nil
}

func (f *mutateFake) ListNodes(context.Context) ([]jmodel.Node, error) { return f.nodes, nil }

func (f *mutateFake) ToggleNodeOffline(_ context.Context, _ string, reason string) error {
	f.toggled = true
	f.toggledReason = reason
	return nil
}

func TestTriggerNoWait(t *testing.T) {
	f := &mutateFake{triggerQueueID: 7}
	d := Deps{Client: f}
	res, err := d.Trigger(context.Background(), "app", TriggerOptions{Params: map[string]string{"ENV": "prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.QueueID != 7 || res.BuildNumber != 0 || res.Status != "" {
		t.Errorf("unexpected result %+v", res)
	}
	if f.triggeredParams["ENV"] != "prod" {
		t.Errorf("params not passed through: %v", f.triggeredParams)
	}
}

func TestTriggerWaitToCompletion(t *testing.T) {
	defer swapIntervals(time.Millisecond, time.Millisecond)()

	f := &mutateFake{
		triggerQueueID: 99,
		queueSeq:       []queueStep{{num: 0, why: "waiting for executor"}, {num: 42}},
		buildSeq: []jmodel.BuildDetail{
			{Build: jmodel.Build{Status: jmodel.BuildStatusRunning}},
			{Build: jmodel.Build{Status: jmodel.BuildStatusSuccess}},
		},
	}
	d := Deps{Client: f}

	var progress []string
	res, err := d.Trigger(context.Background(), "app", TriggerOptions{
		Wait:     true,
		Progress: func(m string) { progress = append(progress, m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildNumber != 42 || res.Status != jmodel.BuildStatusSuccess {
		t.Fatalf("got build %d status %q; want 42 SUCCESS", res.BuildNumber, res.Status)
	}
	if len(progress) < 2 || progress[0] != "queued: waiting for executor" || progress[1] != "started app #42" {
		t.Errorf("unexpected progress: %v", progress)
	}
}

func swapIntervals(q, b time.Duration) func() {
	oq, ob := queuePollInterval, buildPollInterval
	queuePollInterval, buildPollInterval = q, b
	return func() { queuePollInterval, buildPollInterval = oq, ob }
}

func TestSetNodeOffline(t *testing.T) {
	t.Run("toggles when state differs", func(t *testing.T) {
		f := &mutateFake{nodes: []jmodel.Node{{Name: "agent", Offline: false}}}
		if err := (Deps{Client: f}).SetNodeOffline(context.Background(), "agent", true, "maint"); err != nil {
			t.Fatal(err)
		}
		if !f.toggled || f.toggledReason != "maint" {
			t.Errorf("expected toggle with reason, got toggled=%v reason=%q", f.toggled, f.toggledReason)
		}
	})

	t.Run("no-op when already in state", func(t *testing.T) {
		f := &mutateFake{nodes: []jmodel.Node{{Name: "agent", Offline: true}}}
		if err := (Deps{Client: f}).SetNodeOffline(context.Background(), "agent", true, ""); err != nil {
			t.Fatal(err)
		}
		if f.toggled {
			t.Error("should not toggle a node already offline")
		}
	})

	t.Run("unknown node errors", func(t *testing.T) {
		f := &mutateFake{nodes: []jmodel.Node{{Name: "agent"}}}
		if err := (Deps{Client: f}).SetNodeOffline(context.Background(), "ghost", true, ""); err == nil {
			t.Error("expected error for unknown node")
		}
	})
}

func TestResolveInputID(t *testing.T) {
	build := func(ids ...string) *mutateFake {
		ins := make([]jmodel.PendingInput, len(ids))
		for i, id := range ids {
			ins[i] = jmodel.PendingInput{ID: id}
		}
		return &mutateFake{buildSeq: []jmodel.BuildDetail{{PendingInputs: ins}}}
	}
	ctx := context.Background()

	t.Run("single auto-selected", func(t *testing.T) {
		id, err := (Deps{Client: build("Deploy")}).ResolveInputID(ctx, "app", 1, "")
		if err != nil || id != "Deploy" {
			t.Fatalf("got %q,%v", id, err)
		}
	})
	t.Run("multiple needs id", func(t *testing.T) {
		if _, err := (Deps{Client: build("A", "B")}).ResolveInputID(ctx, "app", 1, ""); err == nil {
			t.Error("expected ambiguity error")
		}
	})
	t.Run("explicit match", func(t *testing.T) {
		id, err := (Deps{Client: build("A", "B")}).ResolveInputID(ctx, "app", 1, "B")
		if err != nil || id != "B" {
			t.Fatalf("got %q,%v", id, err)
		}
	})
	t.Run("explicit miss", func(t *testing.T) {
		if _, err := (Deps{Client: build("A")}).ResolveInputID(ctx, "app", 1, "Z"); err == nil {
			t.Error("expected not-pending error")
		}
	})
	t.Run("none pending", func(t *testing.T) {
		if _, err := (Deps{Client: build()}).ResolveInputID(ctx, "app", 1, ""); err == nil {
			t.Error("expected no-inputs error")
		}
	})
}

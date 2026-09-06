package view

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/app/engine"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// seedDashSample records one engine sample from the current registry/queue,
// mirroring what the engine's poll loop does, so the dashboard has a data point.
func seedDashSample(eng *engine.Engine, store *cache.Store, now time.Time) {
	items := store.Queue.Query(buildregistry.Filter{}, jmodel.QueueKindBuild)
	wr := eng.Queue().Observe(now, items)
	eng.Sampler().Add(now, store.Registry.RunningCount(), len(items), wr)
}

func TestDashboardViewRenders(t *testing.T) {
	now := time.Now()
	store := cache.NewStore(nil)

	// A running build (feeds concurrency + the running sample).
	running := jmodel.UserBuild{
		JobPath: "folder/proj/main",
		Build:   jmodel.Build{Number: 42, Status: jmodel.BuildStatusRunning, Timestamp: now.Add(-2 * time.Minute)},
	}
	store.Registry.IngestRunningSnapshot([]jmodel.UserBuild{running}, now)

	// A completed build that overran its estimate, within the window.
	k := buildregistry.Key{JobPath: "folder/proj/main", Number: 41}
	store.Registry.IngestScan([]jmodel.UserBuild{{
		JobPath: k.JobPath,
		Build:   jmodel.Build{Number: 41, Status: jmodel.BuildStatusRunning, Timestamp: now.Add(-20 * time.Minute)},
	}})
	store.Registry.ApplyCompletion(k, jmodel.Build{
		Number: 41, Status: jmodel.BuildStatusSuccess,
		Timestamp: now.Add(-20 * time.Minute), Duration: 12 * time.Minute, EstimatedDuration: 6 * time.Minute,
	})

	// A queued item (feeds queue-reason + wait panes).
	store.Queue.Replace([]jmodel.QueueItem{
		{ID: 1, JobPath: "folder/proj/dev", Buildable: true, Why: "waiting for executor", InQueueSince: now.Add(-3 * time.Minute)},
	})

	eng := engine.New(nil, store)
	dv := NewDashboardView(theme.Default(), nil, store, eng)
	dv.nodes = []jmodel.Node{
		{Name: "built-in", NumExecutors: 4, BusyExecutors: 2},
		{Name: "agent-1", Offline: true, NumExecutors: 2},
	}
	seedDashSample(eng, store, now) // one point so the plot has data

	const w, h = 120, 44
	dv.SetSize(w, h)
	out := dv.View()

	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("got %d lines, want %d", len(lines), h)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != w {
			t.Fatalf("line %d width = %d, want %d", i, got, w)
		}
	}
	if !strings.Contains(out, "#41") {
		t.Errorf("expected completed build in a duration pane, output:\n%s", out)
	}
	if !strings.Contains(out, "offline") {
		t.Errorf("expected offline node in node pane")
	}
}

func TestDashboardViewZeroSize(t *testing.T) {
	store := cache.NewStore(nil)
	dv := NewDashboardView(theme.Default(), nil, store, engine.New(nil, store))
	if out := dv.View(); out != "" {
		t.Errorf("want empty output before SetSize, got %q", out)
	}
}

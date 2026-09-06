package view

import (
	"os"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/app/engine"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// TestDashboardDump writes a rendered dashboard to $DASH_DUMP for manual eyeballing.
func TestDashboardDump(t *testing.T) {
	path := os.Getenv("DASH_DUMP")
	if path == "" {
		t.Skip("set DASH_DUMP to a file path to dump")
	}
	now := time.Now()
	store := cache.NewStore(nil)

	// Running builds across a couple projects.
	store.Registry.IngestRunningSnapshot([]jmodel.UserBuild{
		{JobPath: "Code%2FPrivate/api/main", Build: jmodel.Build{Number: 12, Status: jmodel.BuildStatusRunning, Timestamp: now.Add(-3 * time.Minute)}},
		{JobPath: "Code%2FPrivate/web/PR-8", Build: jmodel.Build{Number: 4, Status: jmodel.BuildStatusRunning, Timestamp: now.Add(-1 * time.Minute)}},
	}, now)

	// Completed builds within the window.
	for i, spec := range []struct {
		path            string
		num             int
		dur, est        time.Duration
		status          jmodel.BuildStatus
		completedMinsGo int
	}{
		{"Code%2FPrivate/api/main", 11, 12 * time.Minute, 6 * time.Minute, jmodel.BuildStatusSuccess, 8},
		{"Code%2FPrivate/web/main", 40, 3 * time.Minute, 4 * time.Minute, jmodel.BuildStatusFailed, 20},
		{"infra/deploy", 99, 22 * time.Minute, 20 * time.Minute, jmodel.BuildStatusSuccess, 35},
	} {
		k := buildregistry.Key{JobPath: spec.path, Number: spec.num}
		ts := now.Add(-time.Duration(spec.completedMinsGo)*time.Minute - spec.dur)
		store.Registry.IngestScan([]jmodel.UserBuild{{JobPath: spec.path, Build: jmodel.Build{Number: spec.num, Status: jmodel.BuildStatusRunning, Timestamp: ts}}})
		store.Registry.ApplyCompletion(k, jmodel.Build{Number: spec.num, Status: spec.status, Timestamp: ts, Duration: spec.dur, EstimatedDuration: spec.est})
		_ = i
	}

	store.Queue.Replace([]jmodel.QueueItem{
		{ID: 1, JobPath: "infra/deploy", Buildable: true, InQueueSince: now.Add(-90 * time.Second)},
		{ID: 2, JobPath: "Code%2FPrivate/api/dev", Blocked: true, InQueueSince: now.Add(-6 * time.Minute)},
	})

	dv := NewDashboardView(theme.Default(), nil, store, engine.New(nil, store))
	dv.nodes = []jmodel.Node{
		{Name: "built-in", NumExecutors: 4, BusyExecutors: 2, FreeDiskBytes: 48318382080, FreeMemBytes: 3221225472, ResponseMs: 12},
		{Name: "linux-agent-1", NumExecutors: 2, BusyExecutors: 1, FreeDiskBytes: 12884901888, FreeMemBytes: 900000000, ResponseMs: 40},
		{Name: "win-agent-2", Offline: true, OfflineCause: "Disconnected by admin"},
	}
	// A few samples so the activity chart and queue tiles have data.
	for i := 30; i >= 0; i-- {
		var wr [engine.WaitBinCount][engine.ReasonCount]int
		wr[3][engine.ReasonBlocked] = i % 2   // 1-2m blocked
		wr[6][engine.ReasonBuildable] = i % 3 // 10-20m buildable
		dv.sampler.Add(now.Add(-time.Duration(i)*time.Minute), i%4, (i%3)+(i%2), wr)
	}

	dv.SetSize(120, 48)
	if err := os.WriteFile(path, []byte(dv.View()), 0o644); err != nil {
		t.Fatal(err)
	}
}

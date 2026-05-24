package view

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

func newTestBuildsView(nc NavigationContext, builds []jmodel.Build) (*BuildsView, *BranchBuildsProvider) {
	t := theme.Default()
	provider := NewBranchBuildsProvider(nil, nil, nc)
	provider.builds = builds
	bv := NewBuildsView(t, nil, nil, nc, provider)
	bv.populateTable()
	return bv, provider
}

func makeBuilds() []jmodel.Build {
	return []jmodel.Build{
		{Number: 1, Status: jmodel.BuildStatusSuccess, Timestamp: time.Now(), TriggeredBy: "alice"},
		{Number: 2, Status: jmodel.BuildStatusRunning, Timestamp: time.Now(), TriggeredBy: "bob"},
		{Number: 3, Status: jmodel.BuildStatusFailed, Timestamp: time.Now(), TriggeredBy: "alice"},
		{Number: 4, Status: jmodel.BuildStatusRunning, Timestamp: time.Now(), TriggeredBy: "alice"},
	}
}

func TestBuildsView_ToggleRunning(t *testing.T) {
	bv, provider := newTestBuildsView(NavigationContext{}, makeBuilds())

	if bv.ItemCount() != 4 {
		t.Fatalf("expected 4 builds initially, got %d", bv.ItemCount())
	}

	bv.ToggleRunning()
	if !bv.filters.Running {
		t.Error("expected Running filter to be true after ToggleRunning")
	}
	if bv.ItemCount() != 2 {
		t.Errorf("expected 2 running builds, got %d", bv.ItemCount())
	}
	builds := provider.Builds()
	for i := 0; i < bv.ItemCount(); i++ {
		di := bv.dataIndex(i)
		if builds[di].Status != jmodel.BuildStatusRunning {
			t.Errorf("build at filtered index %d is not running: %v", i, builds[di].Status)
		}
	}

	bv.ToggleRunning()
	if bv.filters.Running {
		t.Error("expected Running filter to be false after second ToggleRunning")
	}
	if bv.ItemCount() != 4 {
		t.Errorf("expected 4 builds after toggling off, got %d", bv.ItemCount())
	}
}

func TestBuildsView_ToggleMine_WithUsername(t *testing.T) {
	nc := NavigationContext{Username: "alice"}
	bv, provider := newTestBuildsView(nc, makeBuilds())

	bv.ToggleMine()
	if !bv.filters.Mine {
		t.Error("expected Mine filter to be true after ToggleMine")
	}
	// alice has builds 1, 3, 4
	if bv.ItemCount() != 3 {
		t.Errorf("expected 3 builds by alice, got %d", bv.ItemCount())
	}
	builds := provider.Builds()
	for i := 0; i < bv.ItemCount(); i++ {
		di := bv.dataIndex(i)
		if builds[di].TriggeredBy != "alice" {
			t.Errorf("build at filtered index %d not triggered by alice: %q", i, builds[di].TriggeredBy)
		}
	}

	bv.ToggleMine()
	if bv.filters.Mine {
		t.Error("expected Mine filter to be false after second ToggleMine")
	}
	if bv.ItemCount() != 4 {
		t.Errorf("expected 4 builds after toggling off, got %d", bv.ItemCount())
	}
}

func TestBuildsView_ToggleMine_WithoutUsername(t *testing.T) {
	nc := NavigationContext{Username: ""}
	bv, _ := newTestBuildsView(nc, makeBuilds())

	bv.ToggleMine()
	if bv.ItemCount() != 4 {
		t.Errorf("expected 4 builds (mine filter no-op when no username), got %d", bv.ItemCount())
	}
}

func TestBuildsView_CombinedFilters(t *testing.T) {
	nc := NavigationContext{Username: "alice"}
	bv, provider := newTestBuildsView(nc, makeBuilds())

	bv.ToggleRunning()
	bv.ToggleMine()

	// Only running builds by alice: build 4
	if bv.ItemCount() != 1 {
		t.Errorf("expected 1 build (running + mine), got %d", bv.ItemCount())
	}
	builds := provider.Builds()
	di := bv.dataIndex(0)
	if builds[di].Number != 4 {
		t.Errorf("expected build #4, got #%d", builds[di].Number)
	}
}

func TestBuildsView_ActiveFilters(t *testing.T) {
	bv, _ := newTestBuildsView(NavigationContext{}, makeBuilds())

	f := bv.ActiveFilters()
	if f.Running || f.Mine {
		t.Error("expected no active filters initially")
	}

	bv.ToggleRunning()
	f = bv.ActiveFilters()
	if !f.Running {
		t.Error("expected Running filter active")
	}
	if f.Mine {
		t.Error("expected Mine filter inactive")
	}
}

func TestBuildsView_Breadcrumb_FilterAnnotation(t *testing.T) {
	bv, _ := newTestBuildsView(NavigationContext{}, makeBuilds())

	seg := bv.Breadcrumb()
	if seg.ViewType != "builds" {
		t.Errorf("expected viewType %q, got %q", "builds", seg.ViewType)
	}
	if seg.Running || seg.Mine {
		t.Error("expected no filters initially")
	}

	bv.ToggleRunning()
	seg = bv.Breadcrumb()
	if seg.ViewType != "builds" {
		t.Errorf("expected viewType %q, got %q", "builds", seg.ViewType)
	}
	if !seg.Running {
		t.Error("expected Running filter active")
	}
	if seg.Mine {
		t.Error("expected Mine filter inactive")
	}

	bv.ToggleMine()
	seg = bv.Breadcrumb()
	if seg.ViewType != "builds" {
		t.Errorf("expected viewType %q, got %q", "builds", seg.ViewType)
	}
	if !seg.Running || !seg.Mine {
		t.Error("expected both Running and Mine filters active")
	}
}

func TestBranchBuildsProvider_HandleMsg_BuildsMsg(t *testing.T) {
	nc := NavigationContext{Username: "alice"}
	provider := NewBranchBuildsProvider(nil, nil, nc)

	builds := makeBuilds()
	handled, cmds := provider.HandleMsg(BuildsMsg{Builds: builds})
	if !handled {
		t.Fatal("expected BuildsMsg to be handled")
	}
	// Should schedule refresh (at minimum)
	if len(cmds) == 0 {
		t.Error("expected at least one command (refresh)")
	}
	// Builds should be set and sorted descending by number
	got := provider.Builds()
	if len(got) != 4 {
		t.Fatalf("expected 4 builds, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Number > got[i-1].Number {
			t.Errorf("builds not sorted descending: #%d before #%d", got[i-1].Number, got[i].Number)
		}
	}
}

func TestBranchBuildsProvider_HandleMsg_Error(t *testing.T) {
	provider := NewBranchBuildsProvider(nil, nil, NavigationContext{})
	testErr := &mockError{"fetch failed"}

	handled, cmds := provider.HandleMsg(BuildsMsg{Err: testErr})
	if !handled {
		t.Fatal("expected error BuildsMsg to be handled")
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 cmd (error dispatch), got %d", len(cmds))
	}
	msg := cmds[0]()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("expected ErrorMsg, got %T", msg)
	}
	if errMsg.Err != testErr {
		t.Errorf("unexpected error: %v", errMsg.Err)
	}
}

func TestBranchBuildsProvider_HandleMsg_TestReport(t *testing.T) {
	provider := NewBranchBuildsProvider(nil, nil, NavigationContext{})
	report := &jmodel.TestReport{Passed: 5, Failed: 1}

	handled, _ := provider.HandleMsg(TestReportMsg{BuildNum: 42, Report: report})
	if !handled {
		t.Fatal("expected TestReportMsg to be handled")
	}
	if provider.tt.get("", 42) != report {
		t.Error("expected test report to be stored")
	}
}

func makeProjectBuilds() []jmodel.ProjectBuild {
	return []jmodel.ProjectBuild{
		{
			Build:      jmodel.Build{Number: 1, Status: jmodel.BuildStatusSuccess, Timestamp: time.Now(), TriggeredBy: "alice"},
			BranchName: "main",
			BranchPath: "myproject/main",
		},
		{
			Build:      jmodel.Build{Number: 2, Status: jmodel.BuildStatusRunning, Timestamp: time.Now(), TriggeredBy: "bob"},
			BranchName: "feature-x",
			BranchPath: "myproject/feature-x",
		},
		{
			Build:      jmodel.Build{Number: 3, Status: jmodel.BuildStatusFailed, Timestamp: time.Now(), TriggeredBy: "alice"},
			BranchName: "main",
			BranchPath: "myproject/main",
		},
	}
}

func TestProjectBuildsProvider_HandleMsg_Builds(t *testing.T) {
	store := cache.NewStore(nil)
	nc := NavigationContext{Level: CtxProject, ProjectName: "myproject"}
	provider := NewProjectBuildsProvider(nil, store, nc)

	builds := makeProjectBuilds()
	handled, cmds := provider.HandleMsg(projectBuildsResultMsg{builds: builds})
	if !handled {
		t.Fatal("expected projectBuildsResultMsg to be handled")
	}
	if len(cmds) == 0 {
		t.Error("expected at least one command (refresh)")
	}
	got := provider.Builds()
	if len(got) != len(builds) {
		t.Fatalf("expected %d builds, got %d", len(builds), len(got))
	}
	// verify registry ingest
	if len(store.Registry.QueryProject("myproject")) != len(builds) {
		t.Errorf("expected %d records in registry, got %d", len(builds), len(store.Registry.QueryProject("myproject")))
	}
}

func TestProjectBuildsProvider_HandleMsg_Error(t *testing.T) {
	provider := NewProjectBuildsProvider(nil, nil, NavigationContext{})
	testErr := &mockError{"fetch failed"}

	handled, cmds := provider.HandleMsg(projectBuildsResultMsg{err: testErr})
	if !handled {
		t.Fatal("expected error msg to be handled")
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 cmd (error dispatch), got %d", len(cmds))
	}
	msg := cmds[0]()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("expected ErrorMsg, got %T", msg)
	}
	if errMsg.Err != testErr {
		t.Errorf("unexpected error: %v", errMsg.Err)
	}
}

func TestProjectBuildsProvider_Builds_BranchName(t *testing.T) {
	provider := NewProjectBuildsProvider(nil, nil, NavigationContext{})
	pb := makeProjectBuilds()
	provider.builds = pb

	got := provider.Builds()
	for i, b := range got {
		if b.BranchName != pb[i].BranchName {
			t.Errorf("build[%d]: BranchName got %q, want %q", i, b.BranchName, pb[i].BranchName)
		}
		if b.JobPath != pb[i].BranchPath {
			t.Errorf("build[%d]: JobPath got %q, want %q", i, b.JobPath, pb[i].BranchPath)
		}
		if b.Number != pb[i].Number {
			t.Errorf("build[%d]: Number got %d, want %d", i, b.Number, pb[i].Number)
		}
	}
}

func makeUserBuilds() []jmodel.UserBuild {
	return []jmodel.UserBuild{
		{
			JobPath: "folder/project/main",
			Build:   jmodel.Build{Number: 1, Status: jmodel.BuildStatusSuccess, Timestamp: time.Now(), TriggeredBy: "alice", Cause: "Started by user alice"},
		},
		{
			JobPath: "folder/project/feature",
			Build:   jmodel.Build{Number: 2, Status: jmodel.BuildStatusRunning, Timestamp: time.Now(), TriggeredBy: "bob", Cause: "Started by user bob"},
		},
	}
}

func TestAllBuildsProvider_Init_PrePopulatesFromRegistry(t *testing.T) {
	store := cache.NewStore(nil)
	builds := makeUserBuilds()
	// Seed registry directly (in prod this happens via LoadFromDisk in NewStore).
	store.Registry.IngestScan(builds)

	p := NewAllBuildsProvider(nil, store, "alice", time.Minute, "")
	_ = p.Init() // ignore returned cmd (would call nil client)

	got := p.Builds()
	if len(got) != len(builds) {
		t.Fatalf("expected %d builds from registry, got %d", len(builds), len(got))
	}
}

func TestAllBuildsProvider_HandleMsg_Running(t *testing.T) {
	store := cache.NewStore(nil)
	p := NewAllBuildsProvider(nil, store, "alice", time.Minute, "")
	builds := makeUserBuilds()

	// Seed the registry's live running set so Query returns Running for bob's build.
	runningBuilds := []jmodel.UserBuild{builds[1]}
	store.Registry.IngestRunningSnapshot(runningBuilds, time.Now())

	handled, cmds := p.HandleMsg(RunningBuildsUpdatedMsg{Builds: runningBuilds})
	if !handled {
		t.Fatal("expected RunningBuildsUpdatedMsg to be handled")
	}
	// Visual tick should be scheduled because the registry reports a running build.
	if len(cmds) != 1 {
		t.Errorf("expected 1 cmd (visual tick), got %d", len(cmds))
	}
}

func TestAllBuildsProvider_HandleMsg_Full(t *testing.T) {
	store := cache.NewStore(nil)
	p := NewAllBuildsProvider(nil, store, "alice", time.Minute, "")
	builds := makeUserBuilds()

	handled, cmds := p.HandleMsg(allBuildsFullMsg{builds: builds})
	if !handled {
		t.Fatal("expected allBuildsFullMsg to be handled")
	}
	if len(cmds) != 1 {
		t.Errorf("expected 1 cmd (slow tick), got %d", len(cmds))
	}
	// Registry should now contain the scanned builds (visible via Query, with
	// invariant 2 applied — running entries without live confirmation downgrade).
	got := store.Registry.Snapshot()
	if len(got) != len(builds) {
		t.Errorf("expected %d records in registry, got %d", len(builds), len(got))
	}
}

func TestAllBuildsProvider_Builds_TerminalSticky(t *testing.T) {
	store := cache.NewStore(nil)
	p := NewAllBuildsProvider(nil, store, "alice", time.Minute, "")
	ub := makeUserBuilds()[1] // bob's running build

	// Apply completion: build finished Success.
	store.Registry.ApplyCompletion(
		buildregistry.Key{JobPath: ub.JobPath, Number: ub.Number},
		jmodel.Build{Number: ub.Number, Status: jmodel.BuildStatusSuccess, Timestamp: ub.Timestamp},
	)
	// Now feed a stale scan that still claims Running.
	store.Registry.IngestScan([]jmodel.UserBuild{ub})

	got := p.Builds()
	if len(got) != 1 {
		t.Fatalf("expected 1 build, got %d", len(got))
	}
	if got[0].Status != jmodel.BuildStatusSuccess {
		t.Errorf("expected sticky Success, got %v", got[0].Status)
	}
}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

// TestBranchBuildsProvider_InitReFetches verifies that calling Init() a
// second time (the pop-back path: app.go re-Inits the popped-to view to
// revive its polling chain after messages were dropped while a child was
// active) returns a non-nil fetch command. Without this, popping back to
// a BuildsView whose tick chain died would leave it showing stale data
// indefinitely.
func TestBranchBuildsProvider_InitReFetches(t *testing.T) {
	provider := NewBranchBuildsProvider(nil, nil, NavigationContext{
		Level: CtxBranch, ProjectName: "p", BranchName: "b",
	})

	first := provider.Init()
	if first == nil {
		t.Fatal("first Init() returned nil cmd; expected fetchBuilds")
	}

	second := provider.Init()
	if second == nil {
		t.Fatal("second Init() returned nil cmd; expected re-fetch on re-entry (pop-back path)")
	}
}

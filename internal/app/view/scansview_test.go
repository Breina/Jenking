package view

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

func scanStore() *cache.Store {
	store := cache.NewStore(nil)
	store.Queue.Replace([]jmodel.QueueItem{
		{ID: 1, Kind: jmodel.QueueKindScan, JobPath: "Bodem/codelijst-kleur",
			Blocked: true, Why: "At maximum indexing capacity", InQueueSince: time.Now().Add(-12 * time.Minute)},
		{ID: 2, Kind: jmodel.QueueKindScan, JobPath: "Other/project", Buildable: true},
		{ID: 3, Kind: jmodel.QueueKindBuild, JobPath: "Bodem/codelijst-kleur/main", Buildable: true},
	})
	return store
}

func TestScansViewScoping(t *testing.T) {
	store := scanStore()
	th := theme.Default()

	// Root scope sees every scan, and never a build.
	root := NewScansView(th, nil, store, NavigationContext{})
	if root.ItemCount() != 2 {
		t.Errorf("root scan count = %d, want 2", root.ItemCount())
	}

	// Folder scope narrows by prefix.
	folder := NewScansView(th, nil, store, NavigationContext{Level: CtxFolder, FolderPath: "Bodem"})
	if folder.ItemCount() != 1 {
		t.Fatalf("folder scan count = %d, want 1", folder.ItemCount())
	}

	// Project scope matches the project's own scan — the case that matters,
	// since a scan's job path IS the container, never a branch beneath it.
	project := NewScansView(th, nil, store, NavigationContext{
		Level: CtxProject, FolderPath: "Bodem", ProjectName: "codelijst-kleur",
	})
	if project.ItemCount() != 1 {
		t.Fatalf("project scan count = %d, want 1", project.ItemCount())
	}
	if got := project.View(); !strings.Contains(got, "At maximum indexing capacity") {
		t.Errorf("expected the waiting reason in the rendered view, got:\n%s", got)
	}
}

func TestScansViewEmptyWithoutScans(t *testing.T) {
	store := cache.NewStore(nil)
	store.Queue.Replace([]jmodel.QueueItem{
		{ID: 1, Kind: jmodel.QueueKindBuild, JobPath: "Bodem/app/main", Buildable: true},
	})
	sv := NewScansView(theme.Default(), nil, store, NavigationContext{})
	if sv.ItemCount() != 0 {
		t.Errorf("scan count = %d, want 0 (queued builds are not scans)", sv.ItemCount())
	}
}

func TestJobListShowsScanGlyphOnContainerRows(t *testing.T) {
	// The glyph must survive a container that is simultaneously "building"
	// (its branches are) — that is the case the old single-cell design hid.
	store := scanStore()
	jl := NewJobList(theme.Default(), nil, store, "Bodem", "Bodem", false, "", nil)
	jl.jobs = []jmodel.Job{
		{Name: "codelijst-kleur", FullPath: "Bodem/codelijst-kleur", Type: jmodel.JobTypeMultiBranch, RunningCount: 1, Color: "blue_anime"},
		{Name: "quiet", FullPath: "Bodem/quiet", Type: jmodel.JobTypeMultiBranch},
		{Name: "leaf", FullPath: "Bodem/leaf", Type: jmodel.JobTypePipeline},
	}

	scanning := jl.jobNameCell(jl.jobs[0])
	if !strings.Contains(scanning, "⧗") {
		t.Errorf("container with a queued scan should carry the glyph, got %q", scanning)
	}
	if got := jl.jobNameCell(jl.jobs[1]); strings.Contains(got, "⧗") {
		t.Errorf("container without a queued scan should have no glyph, got %q", got)
	}
	// A pipeline row has no scan of its own even if a path prefix matches.
	if got := jl.jobNameCell(jl.jobs[2]); strings.Contains(got, "⧗") {
		t.Errorf("job row should never carry a scan glyph, got %q", got)
	}
}

// scanLogFake serves a two-chunk scan log and records a stop request.
type scanLogFake struct {
	jmodel.JenkinsClient
	chunks  []jmodel.ProgressiveLog
	calls   int
	stopped string
}

func (f *scanLogFake) GetScanLogProgressive(_ context.Context, _ string, _ int) (*jmodel.ProgressiveLog, error) {
	c := f.chunks[min(f.calls, len(f.chunks)-1)]
	f.calls++
	return &c, nil
}

func (f *scanLogFake) StopScan(_ context.Context, jobPath string) error {
	f.stopped = jobPath
	return nil
}

func TestScanLogViewStreamsUntilComplete(t *testing.T) {
	fake := &scanLogFake{chunks: []jmodel.ProgressiveLog{
		{Text: "Starting branch indexing...\n", MoreData: true, NextStart: 28},
		{Text: "Finished: SUCCESS\n", MoreData: false, NextStart: 46},
	}}
	sv := NewScanLogView(theme.Default(), fake, nil, NavigationContext{}, "Bodem/codelijst-kleur")
	sv.SetSize(80, 20)

	// While Jenkins reports more data the view keeps a fetch in flight; when it
	// stops, the view is done — the only running/finished signal a scan has.
	msg := progressiveFetch(context.Background(), sv.source(), 0, 0)().(consoleChunkMsg)
	if _, cmd := sv.Update(msg); cmd == nil {
		t.Fatal("expected a follow-up fetch while more data is pending")
	}
	if sv.done {
		t.Error("view marked done while the scan was still writing")
	}
	if _, cmd := sv.Update(consoleChunkMsg{lines: []string{"Finished: SUCCESS"}, moreData: false}); cmd != nil {
		t.Error("expected no follow-up fetch once the scan finished")
	}
	if !sv.done {
		t.Error("view should be done once Jenkins reports no more data")
	}
}

func TestScanLogViewStopOnlyWhileRunning(t *testing.T) {
	fake := &scanLogFake{chunks: []jmodel.ProgressiveLog{{Text: "scanning\n", MoreData: true, NextStart: 9}}}
	sv := NewScanLogView(theme.Default(), fake, nil, NavigationContext{}, "Bodem/codelijst-kleur")

	// Running: x opens the confirm, and confirming posts the stop.
	sv.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !sv.HasPopup() {
		t.Fatal("x should open the stop-scan confirm while the scan is running")
	}
	_, cmd := sv.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("confirming should issue the stop command")
	}
	cmd()
	if fake.stopped != "Bodem/codelijst-kleur" {
		t.Errorf("StopScan called with %q, want the container path", fake.stopped)
	}

	// Finished: no x, and no shortcut advertising one.
	sv.done = true
	sv.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if sv.HasPopup() {
		t.Error("x must not offer to stop a scan that already finished")
	}
	for _, s := range sv.Shortcuts() {
		if s.Key == "x" {
			t.Error("finished scan should not advertise the stop shortcut")
		}
	}
}

// TestJobListContainerKeysNavigate guards the l/s pair on container rows: the
// job-row flavours of those keys are registered first and must not swallow
// them when their gate (single job) fails.
func TestJobListContainerKeysNavigate(t *testing.T) {
	store := scanStore()
	jl := NewJobList(theme.Default(), nil, store, "Bodem", "Bodem", false, "", nil)
	jl.jobs = []jmodel.Job{
		{Name: "codelijst-kleur", FullPath: "Bodem/codelijst-kleur", Type: jmodel.JobTypeMultiBranch},
	}
	jl.populateTable()

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"s", "*view.ScansView"},
		{"l", "*view.ScanLogView"},
	} {
		_, cmd := jl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
		if cmd == nil {
			t.Fatalf("key %q on a container row produced no command", tc.key)
		}
		push, ok := cmd().(PushViewMsg)
		if !ok {
			t.Fatalf("key %q: got %T, want PushViewMsg", tc.key, cmd())
		}
		if got := fmt.Sprintf("%T", push.View); got != tc.want {
			t.Errorf("key %q pushed %s, want %s", tc.key, got, tc.want)
		}
	}
}

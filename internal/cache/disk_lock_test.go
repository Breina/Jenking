package cache

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// TestDiskStore_ConcurrentRMW_NoLostKeys verifies that two DiskStore instances
// rooted at the same directory (simulating the TUI and MCP server) can write
// disjoint keys concurrently without losing entries — the cross-process flock
// serializes each read-modify-write cycle.
func TestDiskStore_ConcurrentRMW_NoLostKeys(t *testing.T) {
	dir := t.TempDir()
	a, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("NewDiskStore a: %v", err)
	}
	b, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("NewDiskStore b: %v", err)
	}

	const n = 40
	var wg sync.WaitGroup
	wg.Add(2)
	writer := func(d *DiskStore, base int) {
		defer wg.Done()
		for i := 0; i < n; i++ {
			key := filepath.Join("job", "b") + ":" + itoa(base+i)
			if err := d.SaveStages(key, []jmodel.Stage{{Name: key}}); err != nil {
				t.Errorf("SaveStages %s: %v", key, err)
			}
		}
	}
	go writer(a, 0)
	go writer(b, 1000)
	wg.Wait()

	m, err := a.loadAllStages()
	if err != nil {
		t.Fatalf("loadAllStages: %v", err)
	}
	if len(m) != 2*n {
		t.Fatalf("expected %d keys after concurrent writes, got %d (lost updates)", 2*n, len(m))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// TestDiskStore_SaveRegistryMerged_UnionsWithDisk verifies a merged save keeps
// records another process already wrote and lets a terminal status win over a
// concurrent stale running record for the same key.
func TestDiskStore_SaveRegistryMerged_UnionsWithDisk(t *testing.T) {
	d := newTestDiskStore(t)

	// Process A persists build #1 running and build #2 terminal.
	if err := d.SaveRegistry([]buildregistry.Record{
		{JobPath: "job", Build: jmodel.Build{Number: 1, Status: jmodel.BuildStatusRunning}},
		{JobPath: "job", Terminal: true, Build: jmodel.Build{Number: 2, Status: jmodel.BuildStatusSuccess}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	// Process B only knows build #1, now completed, and a fresh build #3.
	if err := d.SaveRegistryMerged([]buildregistry.Record{
		{JobPath: "job", Terminal: true, Build: jmodel.Build{Number: 1, Status: jmodel.BuildStatusFailed}, UpdatedAt: time.Unix(100, 0)},
		{JobPath: "job", Build: jmodel.Build{Number: 3, Status: jmodel.BuildStatusRunning}},
	}); err != nil {
		t.Fatalf("SaveRegistryMerged: %v", err)
	}

	got, err := d.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	byNum := map[int]buildregistry.Record{}
	for _, r := range got {
		byNum[r.Build.Number] = r
	}
	if len(byNum) != 3 {
		t.Fatalf("expected builds 1,2,3 preserved, got %d: %+v", len(byNum), got)
	}
	if s := byNum[1].Build.Status; s != jmodel.BuildStatusFailed {
		t.Errorf("build #1 should be finalized to Failed, got %v", s)
	}
	if s := byNum[2].Build.Status; s != jmodel.BuildStatusSuccess {
		t.Errorf("build #2 (only on disk) lost: got %v", s)
	}
}

// TestDiskStore_SaveDashSamples_Union verifies samples union by timestamp across
// writes rather than overwriting, and that identical timestamps take the latest.
func TestDiskStore_SaveDashSamples_Union(t *testing.T) {
	d := newTestDiskStore(t)

	t0 := time.Unix(1700000000, 0)
	if err := d.SaveDashSamples([]DashSample{
		{T: t0, Running: 1},
		{T: t0.Add(time.Second), Running: 2},
	}); err != nil {
		t.Fatalf("SaveDashSamples first: %v", err)
	}
	// Second writer overlaps t0+1s (updated Running) and adds t0+2s.
	if err := d.SaveDashSamples([]DashSample{
		{T: t0.Add(time.Second), Running: 9},
		{T: t0.Add(2 * time.Second), Running: 3},
	}); err != nil {
		t.Fatalf("SaveDashSamples second: %v", err)
	}

	got, err := d.LoadDashSamples()
	if err != nil {
		t.Fatalf("LoadDashSamples: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 unioned samples, got %d: %+v", len(got), got)
	}
	// Sorted ascending; the overlapping timestamp keeps the later write's value.
	if got[0].Running != 1 || got[1].Running != 9 || got[2].Running != 3 {
		t.Errorf("union/ordering wrong: %+v", got)
	}
}

// TestMergeDashSamples_Cap verifies the merged buffer is trimmed to the newest
// maxDashSamples points.
func TestMergeDashSamples_Cap(t *testing.T) {
	base := time.Unix(0, 0)
	existing := make([]DashSample, maxDashSamples)
	for i := range existing {
		existing[i] = DashSample{T: base.Add(time.Duration(i) * time.Second), Running: i}
	}
	incoming := []DashSample{{T: base.Add(time.Duration(maxDashSamples) * time.Second), Running: 999}}

	out := mergeDashSamples(existing, incoming)
	if len(out) != maxDashSamples {
		t.Fatalf("expected cap %d, got %d", maxDashSamples, len(out))
	}
	if out[len(out)-1].Running != 999 {
		t.Errorf("newest sample should be retained, got %+v", out[len(out)-1])
	}
	if out[0].Running != 1 {
		t.Errorf("oldest sample should have been trimmed, got %+v", out[0])
	}
}

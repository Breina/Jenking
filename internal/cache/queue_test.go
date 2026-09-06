package cache

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func TestQueueStoreFiltersByKind(t *testing.T) {
	q := NewQueueStore()
	q.Replace([]jmodel.QueueItem{
		{ID: 1, Kind: jmodel.QueueKindBuild, JobPath: "Bodem/app/main"},
		{ID: 2, Kind: jmodel.QueueKindScan, JobPath: "Bodem/codelijst-kleur"},
		{ID: 3, Kind: jmodel.QueueKindBuild, JobPath: "Other/app/main"},
		// No kind set: hand-built items must keep behaving as builds rather
		// than vanishing from every filtered read.
		{ID: 4, JobPath: "Bodem/legacy/main"},
	})

	builds := q.Query(buildregistry.Filter{FolderPrefix: "Bodem"}, jmodel.QueueKindBuild)
	if len(builds) != 2 {
		t.Fatalf("build query returned %d items, want 2 (ids 1 and 4): %+v", len(builds), builds)
	}
	scans := q.Query(buildregistry.Filter{FolderPrefix: "Bodem"}, jmodel.QueueKindScan)
	if len(scans) != 1 || scans[0].ID != 2 {
		t.Fatalf("scan query = %+v, want only id 2", scans)
	}
	if got := q.CountVisible(nil, jmodel.QueueKindBuild); got != 3 {
		t.Errorf("CountVisible(build) = %d, want 3", got)
	}
	if got := q.CountVisible(nil, jmodel.QueueKindScan); got != 1 {
		t.Errorf("CountVisible(scan) = %d, want 1", got)
	}

	// A job that is already running folds into its running row and is not
	// counted twice — unchanged by the kind split.
	running := func(jobPath string) bool { return jobPath == "Bodem/app/main" }
	if got := q.CountVisible(running, jmodel.QueueKindBuild); got != 2 {
		t.Errorf("CountVisible(build, running) = %d, want 2", got)
	}
}

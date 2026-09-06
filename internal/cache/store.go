package cache

import (
	"sync"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

// NodeLogSnapshot holds the progressive log state for a single flow node.
type NodeLogSnapshot struct {
	Text      string
	NextStart int
	MoreData  bool
}

// StageLogKey uniquely identifies a node log within a build.
type StageLogKey struct {
	JobPath     string
	BuildNumber int
	NodeID      int
}

// Store is the app-wide cache container shared by all views.
//
// Build-status state lives entirely in Registry — the single source of truth.
// The remaining Cache fields persist non-build-status data (jobs, stages,
// test reports, artifacts) that has its own immutable-after-completion semantics.
type Store struct {
	Jobs          *Cache[string, []jmodel.Job]            // key: folderPath
	Stages        *Cache[string, []jmodel.Stage]          // key: "jobPath:buildNum"
	NodeLogs      *Cache[StageLogKey, NodeLogSnapshot]    // LRU(200)
	WhenSkipped   *Cache[string, map[string][]bool]       // key: "jobPath:buildNum"
	TestReports   *Cache[string, *jmodel.TestReport]      // key: "jobPath:buildNum"
	Artifacts     *Cache[string, []jmodel.Artifact]       // key: "jobPath:buildNum"
	BuildDetail   *Cache[string, jmodel.Build]            // key: "jobPath:buildNum"
	PendingInputs *Cache[string, []jmodel.PendingInput]   // key: "jobPath:buildNum"
	Symbols       *Cache[string, *pipelinesyntax.Symbols] // key: "jobPath#buildNum"
	RepoURLs      *Cache[string, string]                  // key: jobPath; "" = no SCM URL (unlimited: reverse SCM index)

	// Registry is the single source of truth for build status.
	Registry *buildregistry.Registry

	// Queue holds the latest build-queue snapshot (items waiting to run).
	Queue *QueueStore

	Disk        *DiskStore // nil when disk persistence is disabled
	dirtyMu     sync.Mutex
	dirtyJobs   map[string]bool // folderPaths whose Jobs cache is stale
	dirtyBuilds map[string]bool // jobPaths whose Builds cache is stale
}

// NewStore creates a Store with sensible defaults. disk may be nil to disable persistence.
func NewStore(disk *DiskStore) *Store {
	s := &Store{
		Jobs:          New[string, []jmodel.Job](0),
		Stages:        New[string, []jmodel.Stage](0),
		NodeLogs:      New[StageLogKey, NodeLogSnapshot](200),
		WhenSkipped:   New[string, map[string][]bool](0),
		TestReports:   New[string, *jmodel.TestReport](100),
		Artifacts:     New[string, []jmodel.Artifact](100),
		BuildDetail:   New[string, jmodel.Build](100),
		PendingInputs: New[string, []jmodel.PendingInput](100),
		Symbols:       New[string, *pipelinesyntax.Symbols](200),
		RepoURLs:      New[string, string](0),
		Queue:         NewQueueStore(),
		Disk:          disk,
		dirtyJobs:     make(map[string]bool),
		dirtyBuilds:   make(map[string]bool),
	}
	// Registry: persistent build-status truth. Reconcile is wired by the app
	// (which owns the JenkinsClient) via Registry.SetReconcile.
	// The persist callback runs on the mutating goroutine — including the UI
	// thread when the all-builds scan ingests. Route it through an async
	// coalescing writer so a flock'd registry read-modify-write never blocks the
	// caller (and never freezes the TUI event loop).
	var persist buildregistry.PersistFn
	if disk != nil {
		ap := newAsyncPersister(disk.SaveRegistryMerged)
		persist = ap.persist
	}
	s.Registry = buildregistry.New(buildregistry.Config{Persist: persist})
	if disk != nil {
		_ = disk.RemoveLegacyFiles()
		disk.populate(s.Jobs, s.Stages, s.TestReports, s.Artifacts, s.Symbols, s.RepoURLs)
		if records, err := disk.LoadRegistry(); err == nil && len(records) > 0 {
			s.Registry.LoadFromDisk(records)
		}
	}
	return s
}

// TotalEntries returns the sum of all cache sizes across the store, including
// the Registry's record count.
func (s *Store) TotalEntries() int {
	regSize := 0
	if s.Registry != nil {
		regSize = len(s.Registry.Snapshot())
	}
	return s.Jobs.Size() +
		s.Stages.Size() +
		s.NodeLogs.Size() +
		s.WhenSkipped.Size() +
		s.TestReports.Size() +
		s.Artifacts.Size() +
		s.BuildDetail.Size() +
		s.PendingInputs.Size() +
		s.Symbols.Size() +
		regSize
}

// MarkJobsDirty marks the Jobs cache for folderPath as stale.
func (s *Store) MarkJobsDirty(folderPath string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	s.dirtyJobs[folderPath] = true
}

// MarkBuildsDirty marks the Builds cache for jobPath as stale.
func (s *Store) MarkBuildsDirty(jobPath string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	s.dirtyBuilds[jobPath] = true
}

// IsDirtyJobs reports whether the Jobs cache for folderPath is stale.
func (s *Store) IsDirtyJobs(folderPath string) bool {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	return s.dirtyJobs[folderPath]
}

// IsDirtyBuilds reports whether the Builds cache for jobPath is stale.
func (s *Store) IsDirtyBuilds(jobPath string) bool {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	return s.dirtyBuilds[jobPath]
}

// ClearDirtyJobs clears the dirty flag for folderPath.
func (s *Store) ClearDirtyJobs(folderPath string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	delete(s.dirtyJobs, folderPath)
}

// ClearDirtyBuilds clears the dirty flag for jobPath.
func (s *Store) ClearDirtyBuilds(jobPath string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	delete(s.dirtyBuilds, jobPath)
}

package cache

import (
	"sync"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/jenkins"
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
	Jobs        *Cache[string, []jenkins.Job]        // key: folderPath
	Stages      *Cache[string, []jenkins.Stage]      // key: "jobPath:buildNum"
	NodeLogs    *Cache[StageLogKey, NodeLogSnapshot] // LRU(200)
	WhenSkipped *Cache[string, map[string][]bool]    // key: "jobPath:buildNum"
	TestReports *Cache[string, *jenkins.TestReport]  // key: "jobPath:buildNum"
	Artifacts   *Cache[string, []jenkins.Artifact]   // key: "jobPath:buildNum"
	BuildDetail *Cache[string, jenkins.Build]        // key: "jobPath:buildNum"

	// Registry is the single source of truth for build status.
	Registry *buildregistry.Registry

	Disk        *DiskStore // nil when disk persistence is disabled
	dirtyMu     sync.Mutex
	dirtyJobs   map[string]bool // folderPaths whose Jobs cache is stale
	dirtyBuilds map[string]bool // jobPaths whose Builds cache is stale
}

// NewStore creates a Store with sensible defaults. disk may be nil to disable persistence.
func NewStore(disk *DiskStore) *Store {
	s := &Store{
		Jobs:        New[string, []jenkins.Job](0),
		Stages:      New[string, []jenkins.Stage](0),
		NodeLogs:    New[StageLogKey, NodeLogSnapshot](200),
		WhenSkipped: New[string, map[string][]bool](0),
		TestReports: New[string, *jenkins.TestReport](100),
		Artifacts:   New[string, []jenkins.Artifact](100),
		BuildDetail: New[string, jenkins.Build](100),
		Disk:        disk,
		dirtyJobs:   make(map[string]bool),
		dirtyBuilds: make(map[string]bool),
	}
	// Registry: persistent build-status truth. Reconcile is wired by the app
	// (which owns the JenkinsClient) via Registry.SetReconcile.
	var persist buildregistry.PersistFn
	if disk != nil {
		persist = func(records []buildregistry.Record) {
			_ = disk.SaveRegistry(records)
		}
	}
	s.Registry = buildregistry.New(buildregistry.Config{Persist: persist})
	if disk != nil {
		_ = disk.RemoveLegacyFiles()
		disk.populate(s.Jobs, s.Stages, s.TestReports, s.Artifacts)
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

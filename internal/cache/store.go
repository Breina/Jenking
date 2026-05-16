package cache

import (
	"sync"

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
type Store struct {
	Jobs          *Cache[string, []jenkins.Job]          // key: folderPath
	Builds        *Cache[string, []jenkins.Build]        // key: jobPath
	ProjectBuilds *Cache[string, []jenkins.ProjectBuild] // key: projectPath
	Stages        *Cache[string, []jenkins.Stage]        // key: "jobPath:buildNum"
	NodeLogs      *Cache[StageLogKey, NodeLogSnapshot]   // LRU(200)
	RunningBuilds *Cache[string, []jenkins.UserBuild]    // singleton key ""
	AllBuilds     *Cache[string, []jenkins.UserBuild]    // singleton key ""; from ScanAllBuilds
	WhenSkipped   *Cache[string, map[string][]bool]      // key: "jobPath:buildNum"
	TestReports   *Cache[string, *jenkins.TestReport]    // key: "jobPath:buildNum"
	Artifacts     *Cache[string, []jenkins.Artifact]     // key: "jobPath:buildNum"
	BuildDetail   *Cache[string, jenkins.Build]          // key: "jobPath:buildNum"

	Disk        *DiskStore // nil when disk persistence is disabled
	dirtyMu     sync.Mutex
	dirtyJobs   map[string]bool // folderPaths whose Jobs cache is stale
	dirtyBuilds map[string]bool // jobPaths whose Builds cache is stale
}

// NewStore creates a Store with sensible defaults. disk may be nil to disable persistence.
func NewStore(disk *DiskStore) *Store {
	s := &Store{
		Jobs:          New[string, []jenkins.Job](0),
		Builds:        New[string, []jenkins.Build](0),
		ProjectBuilds: New[string, []jenkins.ProjectBuild](0),
		Stages:        New[string, []jenkins.Stage](0),
		NodeLogs:      New[StageLogKey, NodeLogSnapshot](200),
		RunningBuilds: New[string, []jenkins.UserBuild](0),
		AllBuilds:     New[string, []jenkins.UserBuild](0),
		WhenSkipped:   New[string, map[string][]bool](0),
		TestReports:   New[string, *jenkins.TestReport](100),
		Artifacts:     New[string, []jenkins.Artifact](100),
		BuildDetail:   New[string, jenkins.Build](100),
		Disk:          disk,
		dirtyJobs:     make(map[string]bool),
		dirtyBuilds:   make(map[string]bool),
	}
	if disk != nil {
		disk.populate(s.Jobs, s.AllBuilds, s.Stages, s.TestReports, s.Artifacts)
	}
	return s
}

// TotalEntries returns the sum of all cache sizes across the store.
func (s *Store) TotalEntries() int {
	return s.Jobs.Size() +
		s.Builds.Size() +
		s.ProjectBuilds.Size() +
		s.Stages.Size() +
		s.NodeLogs.Size() +
		s.RunningBuilds.Size() +
		s.AllBuilds.Size() +
		s.WhenSkipped.Size() +
		s.TestReports.Size() +
		s.Artifacts.Size() +
		s.BuildDetail.Size()
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

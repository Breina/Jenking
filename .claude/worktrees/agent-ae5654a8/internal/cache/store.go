package cache

import (
	"sync"

	"github.com/brecht/jenkins-tui/internal/jenkins"
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
	Jobs          *Cache[string, []jenkins.Job]        // key: folderPath
	Builds        *Cache[string, []jenkins.Build]      // key: jobPath
	Stages        *Cache[string, []jenkins.Stage]      // key: "jobPath:buildNum"
	NodeLogs      *Cache[StageLogKey, NodeLogSnapshot] // LRU(200)
	RunningBuilds *Cache[string, []jenkins.UserBuild]  // singleton key ""
	WhenSkipped   *Cache[string, map[string][]bool]    // key: "jobPath:buildNum"

	dirtyMu     sync.Mutex
	dirtyJobs   map[string]bool
	dirtyBuilds map[string]bool
}

// NewStore creates a Store with sensible defaults.
func NewStore() *Store {
	return &Store{
		Jobs:          New[string, []jenkins.Job](0),
		Builds:        New[string, []jenkins.Build](0),
		Stages:        New[string, []jenkins.Stage](0),
		NodeLogs:      New[StageLogKey, NodeLogSnapshot](200),
		RunningBuilds: New[string, []jenkins.UserBuild](0),
		WhenSkipped:   New[string, map[string][]bool](0),
		dirtyJobs:     make(map[string]bool),
		dirtyBuilds:   make(map[string]bool),
	}
}

// MarkJobsDirty marks the given job keys as dirty.
func (s *Store) MarkJobsDirty(keys ...string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	for _, k := range keys {
		s.dirtyJobs[k] = true
	}
}

// IsDirtyJobs reports whether the given key is dirty.
func (s *Store) IsDirtyJobs(key string) bool {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	return s.dirtyJobs[key]
}

// ClearDirtyJobs clears the dirty flag for the given key.
func (s *Store) ClearDirtyJobs(key string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	delete(s.dirtyJobs, key)
}

// MarkBuildsDirty marks the given build keys as dirty.
func (s *Store) MarkBuildsDirty(keys ...string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	for _, k := range keys {
		s.dirtyBuilds[k] = true
	}
}

// IsDirtyBuilds reports whether the given key is dirty.
func (s *Store) IsDirtyBuilds(key string) bool {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	return s.dirtyBuilds[key]
}

// ClearDirtyBuilds clears the dirty flag for the given key.
func (s *Store) ClearDirtyBuilds(key string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	delete(s.dirtyBuilds, key)
}

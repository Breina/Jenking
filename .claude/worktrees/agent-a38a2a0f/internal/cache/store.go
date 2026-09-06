package cache

import "github.com/brecht/jenkins-tui/internal/jenkins"

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
	}
}

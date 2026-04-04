package view

import (
	"strings"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// HasParent is optionally implemented by views that know their canonical parent.
// ESC navigates to the returned view rather than popping a history stack,
// making back-navigation deterministic regardless of how the view was reached.
type HasParent interface {
	ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View
}

// folderParentJobList returns the non-branch JobList for the folder that
// CONTAINS the item at childPath (a slash-joined path like "Code/webidm").
// Returns nil when childPath has no "/" — the parent is the root Dashboard.
func folderParentJobList(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store, childPath, username string) View {
	idx := strings.LastIndex(childPath, "/")
	if idx < 0 {
		return nil // parent is root/Dashboard
	}
	parentPath := childPath[:idx]
	// Title of the parent folder = its own last segment
	title := parentPath
	if i := strings.LastIndex(parentPath, "/"); i >= 0 {
		title = parentPath[i+1:]
	}
	return NewJobList(t, c, s, parentPath, title, false, username)
}

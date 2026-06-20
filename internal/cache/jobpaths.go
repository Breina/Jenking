package cache

import (
	"sort"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// AllProjectPaths walks the Jobs cache starting from the root folder and
// returns the FullPath of every cached non-folder job (i.e. multibranch
// projects, freestyle/pipeline jobs). Folders are descended into but not
// returned. Order is deterministic (lexicographic).
//
// This reads only what is already in the cache — it does not trigger any
// network fetches. Subtrees that have not been opened yet by the user are
// invisible to the walk.
func AllProjectPaths(s *Store) []string {
	if s == nil || s.Jobs == nil {
		return nil
	}
	var out []string
	walkJobs(s, "", &out)
	sort.Strings(out)
	return out
}

// LookupJob returns the cached job whose FullPath equals fullPath, searching
// the entry for folderPath. The boolean reports whether it was found. It reads
// only the cache and never triggers a network fetch.
func LookupJob(s *Store, folderPath, fullPath string) (jmodel.Job, bool) {
	if s == nil || s.Jobs == nil {
		return jmodel.Job{}, false
	}
	entry := s.Jobs.Get(folderPath)
	if entry == nil {
		return jmodel.Job{}, false
	}
	for _, j := range entry.Value {
		if j.FullPath == fullPath {
			return j, true
		}
	}
	return jmodel.Job{}, false
}

func walkJobs(s *Store, folderPath string, out *[]string) {
	entry := s.Jobs.Get(folderPath)
	if entry == nil {
		return
	}
	for _, j := range entry.Value {
		if j.Type == jmodel.JobTypeFolder {
			walkJobs(s, j.FullPath, out)
			continue
		}
		*out = append(*out, j.FullPath)
	}
}

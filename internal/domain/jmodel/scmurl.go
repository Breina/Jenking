package jmodel

import "strings"

// JobSCM pairs a job path with its SCM project/branch web URL
// (ObjectMetadataAction.objectUrl). Produced by a controller-wide SCM scan and
// used to build the reverse (SCM URL -> job) lookup.
type JobSCM struct {
	JobPath string
	SCMURL  string
}

// JobSCMMatch is a job whose SCM URL matched a resolve query. Branch is the
// job's leaf path segment (decoded), empty for a non-multibranch job.
type JobSCMMatch struct {
	JobPath string
	SCMURL  string
	Branch  string
}

// CanonicalSCMURL reduces any SCM URL form to a comparable "host/path" key
// (lowercased), so that a local git remote and a Jenkins objectUrl for the same
// repository collapse to the same value. It handles:
//
//	git@github.com:Breina/Jenking.git          -> github.com/breina/jenking
//	https://github.com/Breina/Jenking(.git)    -> github.com/breina/jenking
//	https://github.com/Breina/Jenking/tree/main-> github.com/breina/jenking
//	https://gitlab.example.com/g/sub/repo/-/tree/main -> gitlab.example.com/g/sub/repo
//	ssh://git@host:22/team/repo.git            -> host/team/repo
//
// It never assumes a fixed org/repo segment count, so GitLab subgroups survive.
// An empty or unparseable input yields "".
func CanonicalSCMURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Strip scheme (scheme://) if present.
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}

	// Drop userinfo (user@ or git@) that precedes the host.
	if at := strings.Index(s, "@"); at >= 0 {
		s = s[at+1:]
	}

	// SCP-like form "host:team/repo" -> "host/team/repo". Only treat the first
	// ":" as a separator when what follows is not a bare port+path we already
	// normalized; converting ":" to "/" is safe for both "host:team/repo" and
	// "host:22/team/repo" once we strip a leading numeric port below.
	s = strings.Replace(s, ":", "/", 1)

	// Remove a leading "host/<port>/" numeric port segment (from ssh://host:22/…).
	// After the ":"→"/" swap this looks like "host/22/team/repo".
	s = stripNumericPortSegment(s)

	// Strip web-view suffixes that point inside a repo at a branch, blob, MR/PR,
	// or commit, so every branch/MR objectUrl collapses to the repo root.
	// GitLab uses the "/-/" infix; GitHub does not.
	for _, marker := range []string{
		"/-/tree/", "/-/blob/", "/-/merge_requests/", "/-/commits/", "/-/commit/",
		"/tree/", "/blob/", "/pull/", "/commit/", "/commits/",
	} {
		if i := strings.Index(s, marker); i >= 0 {
			s = s[:i]
			break
		}
	}

	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	return strings.ToLower(s)
}

// stripNumericPortSegment removes a "host/<digits>/rest" port segment, returning
// "host/rest". Inputs without such a segment are returned unchanged.
func stripNumericPortSegment(s string) string {
	first := strings.IndexByte(s, '/')
	if first < 0 {
		return s
	}
	rest := s[first+1:]
	second := strings.IndexByte(rest, '/')
	if second < 0 {
		return s
	}
	seg := rest[:second]
	if seg == "" || !isAllDigits(seg) {
		return s
	}
	return s[:first] + "/" + rest[second+1:]
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

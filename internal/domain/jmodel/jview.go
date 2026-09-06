package jmodel

import (
	"errors"
	"net/url"
	"strings"
)

// ViewKind classifies a Jenkins view. Only the core kinds are distinguished;
// anything contributed by a plugin (NestedView, Dashboard View, Delivery
// Pipeline, …) lands on ViewOther and is still listed and still enumerable
// through its jobs[] — no plugin-specific field is ever read.
type ViewKind int

const (
	// ViewOther is any view kind we do not special-case.
	ViewOther ViewKind = iota
	// ViewAll is the container's built-in "all jobs" view (hudson.model.AllView).
	ViewAll
	// ViewList is a user-defined list view (hudson.model.ListView).
	ViewList
	// ViewMy is the "My View" kind, which auto-selects jobs the user can configure.
	ViewMy
)

// ParseViewKind maps a Jenkins _class to a ViewKind.
func ParseViewKind(class string) ViewKind {
	switch {
	case strings.HasSuffix(class, ".AllView"):
		return ViewAll
	case strings.HasSuffix(class, ".ListView"):
		return ViewList
	case strings.HasSuffix(class, ".MyView"):
		return ViewMy
	default:
		return ViewOther
	}
}

// JenkinsView is a server-side saved job filter hanging off a container (the
// root or a folder), or off a user's personal my-views collection.
//
// Distinct from the TUI's own "view" concept in internal/app/view: this is the
// Jenkins domain object, always spelled JenkinsView to keep the two apart.
type JenkinsView struct {
	Name      string // raw name, as Jenkins stores it
	URL       string // absolute URL reported by the API
	Kind      ViewKind
	OwnerPath string // container job path; "" = root
	Personal  bool   // lives under /user/<id>/my-views/
	IsPrimary bool   // the container's primaryView
}

// DisplayName returns the name for UI rendering.
func (v JenkinsView) DisplayName() string { return v.Name }

// IsAll reports whether this view shows every job in its container, in which
// case callers can list the container directly instead of going through the
// view endpoint.
func (v JenkinsView) IsAll() bool { return v.Kind == ViewAll }

// ViewPathToURL builds the server-relative path of a view on the given
// container. ownerPath is a job path ("" for the root).
func ViewPathToURL(ownerPath, viewName string) string {
	return JobPathToURL(ownerPath) + "/view/" + url.PathEscape(viewName)
}

// MyViewsPathToURL builds the server-relative path of a personal view.
func MyViewsPathToURL(userID, viewName string) string {
	return "/user/" + url.PathEscape(userID) + "/my-views/view/" + url.PathEscape(viewName)
}

// URLPathSegments validates rawURL against baseURL and returns the path's
// slash-separated segments (origin, query, fragment and surrounding slashes
// stripped). It is the shared front half of every Jenkins-URL parser.
func URLPathSegments(baseURL, rawURL string) ([]string, error) {
	if baseURL == "" || rawURL == "" {
		return nil, errors.New("empty URL")
	}
	rawURL = strings.TrimSpace(rawURL)
	base := strings.TrimRight(baseURL, "/")
	if rawURL != base && !strings.HasPrefix(rawURL, base+"/") {
		return nil, errors.New("URL does not match current Jenkins context")
	}
	path := strings.TrimPrefix(rawURL, base)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return nil, errors.New("URL has no path")
	}
	return strings.Split(path, "/"), nil
}

// JobChain is the result of walking the /job/ pairs at the head of a Jenkins
// URL path.
type JobChain struct {
	// Segs are the job path segments, each unescaped exactly once so a name
	// containing a slash stays in its canonical percent-encoded form.
	Segs []string
	// ViewName is the last /view/<name>/ marker seen while walking, decoded.
	ViewName string
	// HadView reports whether any /view/ marker was skipped. In the Jenkins UI
	// this marker sits between a multibranch project and its branch, so it
	// doubles as a hint that the leaf segment is a branch.
	HadView bool
	// Trailing is everything after the job chain.
	Trailing []string
}

// WalkJobChain consumes /job/<name>/ pairs from the head of segs, skipping
// (and recording) /view/<name>/ pairs.
func WalkJobChain(segs []string) JobChain {
	var c JobChain
	i := 0
	for i < len(segs) {
		seg := segs[i]
		switch {
		case seg == "job" && i+1 < len(segs):
			c.Segs = append(c.Segs, unescapeOnce(segs[i+1]))
			i += 2
		case seg == "view" && i+1 < len(segs):
			c.HadView = true
			c.ViewName = unescapeOnce(segs[i+1])
			i += 2
		default:
			c.Trailing = segs[i:]
			return c
		}
	}
	return c
}

// JobPathFromURL converts an absolute Jenkins job URL into the canonical job
// path. Returns ok=false when the URL is not on this server or carries no
// /job/ segments.
//
// This is the authority for a job's identity whenever the job arrives from an
// endpoint that does not imply its location — most importantly a view listing,
// which renders jobs from anywhere in the folder tree as a flat list.
func JobPathFromURL(baseURL, rawURL string) (string, bool) {
	segs, err := URLPathSegments(baseURL, rawURL)
	if err != nil {
		return "", false
	}
	chain := WalkJobChain(segs)
	if len(chain.Segs) == 0 {
		return "", false
	}
	return strings.Join(chain.Segs, "/"), true
}

// unescapeOnce percent-decodes a single URL path segment, leaving an
// inner-encoded slash (%252F → %2F) in the encoded form job paths use.
func unescapeOnce(seg string) string {
	decoded, err := url.PathUnescape(seg)
	if err != nil {
		return seg
	}
	return decoded
}

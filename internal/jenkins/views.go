package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// Views are core Jenkins: every container exposes views[] and primaryView, and
// my-views is a core user property. Nothing here depends on an optional
// plugin — an unrecognised _class is listed as jmodel.ViewOther and its jobs[]
// are read exactly like any other view's.

// viewsTree is the ?tree= selector for a container's view listing.
const viewsTree = "?tree=views[name,url,_class],primaryView[name]"

// ListViews returns the views defined on the given container (empty string for
// the root).
func (c *Client) ListViews(ctx context.Context, folder string) ([]jmodel.JenkinsView, error) {
	path := "/api/json"
	if folder != "" {
		path = jmodel.JobPathToURL(folder) + "/api/json"
	}
	data, err := c.get(ctx, path+viewsTree)
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}

	var resp jsonViewContainer
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing views response: %w", err)
	}
	return parseViewList(resp, folder, false), nil
}

// ListMyViews returns the user's personal views. A server or user without a
// my-views collection is not an error — it yields no views.
func (c *Client) ListMyViews(ctx context.Context, username string) ([]jmodel.JenkinsView, error) {
	if username == "" {
		return nil, nil
	}
	path := "/user/" + url.PathEscape(username) + "/my-views/api/json" + viewsTree
	data, err := c.get(ctx, path)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list my views: %w", err)
	}

	var resp jsonViewContainer
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing my-views response: %w", err)
	}
	return parseViewList(resp, "", true), nil
}

// ListViewJobs returns the jobs shown by a view. A view renders jobs from
// anywhere in the folder tree as a flat list, so each job's path is derived
// from the URL the API reports for it rather than from the view's container.
//
// An "all" view is listed through its container instead, which is the same
// data over the endpoint the rest of the app already caches.
func (c *Client) ListViewJobs(ctx context.Context, v jmodel.JenkinsView) ([]jmodel.Job, error) {
	if v.IsAll() && !v.Personal {
		return c.ListJobs(ctx, v.OwnerPath)
	}

	path, err := c.viewAPIPath(v)
	if err != nil {
		return nil, err
	}
	data, err := c.get(ctx, path+jobListTree)
	if err != nil {
		return nil, fmt.Errorf("list view jobs (%s): %w", v.Name, err)
	}

	var resp jsonJobList
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing view jobs response: %w", err)
	}
	resolve := func(jobURL string) (string, bool) {
		return jmodel.JobPathFromURL(c.baseURL, jobURL)
	}
	return parseJobList(resp, v.OwnerPath, resolve), nil
}

// viewAPIPath returns the server-relative api/json path for a view. The URL
// the API reported is preferred (it is authoritative for nested and personal
// views alike); the name-derived path is the fallback.
func (c *Client) viewAPIPath(v jmodel.JenkinsView) (string, error) {
	if v.URL != "" {
		base := strings.TrimRight(c.baseURL, "/")
		if strings.HasPrefix(v.URL, base+"/") {
			return strings.TrimPrefix(v.URL, base) + "api/json", nil
		}
	}
	if v.Name == "" {
		return "", errors.New("view has neither URL nor name")
	}
	return jmodel.ViewPathToURL(v.OwnerPath, v.Name) + "/api/json", nil
}

// isNotFound reports whether err carries an HTTP 404.
func isNotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.StatusCode == 404
}

package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// GetControllerStatus reads the controller's mode and quiet-down flag from the
// root API; the version comes from the X-Jenkins response header.
func (c *Client) GetControllerStatus(ctx context.Context) (jmodel.ControllerStatus, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/json?tree=mode,quietingDown", nil)
	if err != nil {
		return jmodel.ControllerStatus{}, fmt.Errorf("controller status: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		Mode         string `json:"mode"`
		QuietingDown bool   `json:"quietingDown"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return jmodel.ControllerStatus{}, fmt.Errorf("parsing controller status: %w", err)
	}
	return jmodel.ControllerStatus{
		Version:      resp.Header.Get("X-Jenkins"),
		Mode:         body.Mode,
		QuietingDown: body.QuietingDown,
	}, nil
}

// buildSCMTree asks for the revision fields SCM plugins export on their build
// action. A tree query naming fields no action has simply returns nothing, so
// this degrades to an empty result on a controller without such a plugin.
const buildSCMTree = "actions[lastBuiltRevision[SHA1,branch[name]],remoteUrls]"

// GetBuildSCM returns the source revisions a build checked out.
func (c *Client) GetBuildSCM(ctx context.Context, jobPath string, number int) ([]jmodel.SCMRevision, error) {
	path := fmt.Sprintf("%s/%d/api/json?tree=%s", jmodel.JobPathToURL(jobPath), number, url.QueryEscape(buildSCMTree))
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("get build scm: %w", err)
	}
	return parseBuildSCM(data)
}

func parseBuildSCM(data []byte) ([]jmodel.SCMRevision, error) {
	var resp struct {
		Actions []struct {
			LastBuiltRevision *struct {
				SHA1   string `json:"SHA1"`
				Branch []struct {
					Name string `json:"name"`
				} `json:"branch"`
			} `json:"lastBuiltRevision"`
			RemoteURLs []string `json:"remoteUrls"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing build scm: %w", err)
	}
	var revs []jmodel.SCMRevision
	seen := map[string]bool{}
	for _, a := range resp.Actions {
		if a.LastBuiltRevision == nil || a.LastBuiltRevision.SHA1 == "" {
			continue
		}
		// The same checkout can be recorded more than once (e.g. per stage).
		key := a.LastBuiltRevision.SHA1 + "|" + strings.Join(a.RemoteURLs, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		rev := jmodel.SCMRevision{Revision: a.LastBuiltRevision.SHA1, RemoteURLs: a.RemoteURLs}
		for _, b := range a.LastBuiltRevision.Branch {
			rev.Branches = append(rev.Branches, b.Name)
		}
		revs = append(revs, rev)
	}
	return revs, nil
}

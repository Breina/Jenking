package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type jsonArtifact struct {
	DisplayPath  string `json:"displayPath"`
	RelativePath string `json:"relativePath"`
}

type jsonArtifactResponse struct {
	URL       string         `json:"url"`
	Artifacts []jsonArtifact `json:"artifacts"`
}

// GetArtifacts fetches the list of artifacts for a build.
// Returns nil, nil when the build has no artifacts.
func (c *Client) GetArtifacts(ctx context.Context, jobPath string, buildNum int) ([]Artifact, error) {
	path := fmt.Sprintf(
		"%s/%d/api/json?tree=url,artifacts[displayPath,relativePath]",
		JobPathToURL(jobPath), buildNum,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating artifacts request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching artifacts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jenkins API error: GET %s returned %d", path, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading artifacts response: %w", err)
	}

	var jr jsonArtifactResponse
	if err := json.Unmarshal(data, &jr); err != nil {
		return nil, fmt.Errorf("parsing artifacts response: %w", err)
	}

	if len(jr.Artifacts) == 0 {
		return []Artifact{}, nil
	}

	buildURL := jr.URL
	if len(buildURL) > 0 && buildURL[len(buildURL)-1] != '/' {
		buildURL += "/"
	}

	out := make([]Artifact, len(jr.Artifacts))
	for i, a := range jr.Artifacts {
		out[i] = Artifact{
			DisplayPath: a.DisplayPath,
			URL:         buildURL + "artifact/" + a.RelativePath,
		}
	}
	return out, nil
}

package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type jsonArtifact struct {
	DisplayPath  string `json:"displayPath"`
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
}

// name returns the best available label for the artifact. Jenkins leaves
// displayPath empty on some controllers, so fall back to the archive-relative
// path and finally to the bare file name.
func (a jsonArtifact) name() string {
	for _, s := range []string{a.DisplayPath, a.RelativePath, a.FileName} {
		if s != "" {
			return s
		}
	}
	return ""
}

type jsonArtifactResponse struct {
	URL       string         `json:"url"`
	Artifacts []jsonArtifact `json:"artifacts"`
}

// GetArtifacts fetches the list of artifacts for a build.
// Returns nil, nil when the build has no artifacts.
func (c *Client) GetArtifacts(ctx context.Context, jobPath string, buildNum int) ([]jmodel.Artifact, error) {
	path := fmt.Sprintf(
		"%s/%d/api/json?tree=url,artifacts[displayPath,fileName,relativePath]",
		jmodel.JobPathToURL(jobPath), buildNum,
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
		return []jmodel.Artifact{}, nil
	}

	buildURL := jr.URL
	if len(buildURL) > 0 && buildURL[len(buildURL)-1] != '/' {
		buildURL += "/"
	}

	out := make([]jmodel.Artifact, len(jr.Artifacts))
	for i, a := range jr.Artifacts {
		out[i] = jmodel.Artifact{
			DisplayPath: a.name(),
			URL:         buildURL + "artifact/" + a.RelativePath,
		}
	}
	return out, nil
}

// GetArtifactContent downloads the raw bytes of a single artifact. artifactURL
// is the absolute URL stored on jmodel.Artifact (already includes the host), so
// it is requested directly rather than through doRequest's baseURL-relative
// path. Returns the body and the response Content-Type header.
func (c *Client) GetArtifactContent(ctx context.Context, artifactURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("creating artifact request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetching artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("jenkins API error: GET %s returned %d", artifactURL, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading artifact content: %w", err)
	}

	return string(data), resp.Header.Get("Content-Type"), nil
}

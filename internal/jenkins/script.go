package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Jenkins uses name="_.mainScript" in the replay form textarea.
var replayScriptRe = regexp.MustCompile(`(?s)<textarea[^>]+name="[^"]*mainScript"[^>]*>(.*?)</textarea>`)

// GetBuildScript fetches the Groovy script used for a specific build via the
// Jenkins replay page. This matches what the Jenkins GUI shows at /{build}/replay/.
func (c *Client) GetBuildScript(ctx context.Context, jobPath string, buildNumber int) (string, error) {
	path := fmt.Sprintf("%s/%d/replay/", JobPathToURL(jobPath), buildNumber)
	data, err := c.get(ctx, path)
	if err != nil {
		return "", fmt.Errorf("get build script: %w", err)
	}
	if m := replayScriptRe.FindStringSubmatch(string(data)); m != nil {
		return html.UnescapeString(m[1]), nil
	}
	return "", fmt.Errorf("mainScript not found in replay page")
}

// ReplayBuild re-runs a build using the given Groovy script instead of the
// version stored in source control. This mirrors Jenkins' "Replay" button.
// Parameters are inherited from the source build.
func (c *Client) ReplayBuild(ctx context.Context, jobPath string, buildNum int, script string) error {
	path := fmt.Sprintf("%s/%d/replay/", JobPathToURL(jobPath), buildNum)
	form := url.Values{"mainScript": {script}}
	reqURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("replay build: creating request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("replay build: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("replay build: HTTP %d", resp.StatusCode)
	}
	return nil
}

// GetBuildParameters fetches the actual parameter values used for a specific build.
// Uses json.RawMessage for values so boolean/numeric params are handled correctly.
func (c *Client) GetBuildParameters(ctx context.Context, jobPath string, buildNumber int) (map[string]string, error) {
	path := fmt.Sprintf("%s/%d/api/json?tree=actions[_class,parameters[name,value]]", JobPathToURL(jobPath), buildNumber)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("get build parameters: %w", err)
	}

	var resp struct {
		Actions []struct {
			Class      string `json:"_class"`
			Parameters []struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			} `json:"parameters"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing build parameters: %w", err)
	}

	params := make(map[string]string)
	for _, action := range resp.Actions {
		if !strings.Contains(action.Class, "ParametersAction") {
			continue
		}
		for _, p := range action.Parameters {
			var strVal string
			if err := json.Unmarshal(p.Value, &strVal); err != nil {
				// Non-string types (bool, number): use raw JSON representation
				strVal = strings.Trim(string(p.Value), `"`)
			}
			params[p.Name] = strVal
		}
	}
	return params, nil
}

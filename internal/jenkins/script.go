package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var (
	// Matches the replay textarea: captures field name and content.
	replayScriptRe = regexp.MustCompile(`(?s)<textarea[^>]+name="([^"]*mainScript[^"]*)"[^>]*>(.*?)</textarea>`)
	// Matches hidden inputs: captures the entire tag for per-tag attr parsing.
	hiddenInputRe = regexp.MustCompile(`(?i)<input[^>]*type=["']?hidden["']?[^>]*>`)
	// Extracts name= and value= attributes from a tag.
	attrNameRe  = regexp.MustCompile(`(?i)\bname=["']([^"']+)["']`)
	attrValueRe = regexp.MustCompile(`(?i)\bvalue=["']([^"']*)["']`)
	// Matches the form action attribute.
	formActionRe = regexp.MustCompile(`(?i)<form[^>]+action=["']([^"']+)["']`)
)

// GetBuildScript fetches the Groovy script used for a specific build via the
// Jenkins replay page. This matches what the Jenkins GUI shows at /{build}/replay/.
func (c *Client) GetBuildScript(ctx context.Context, jobPath string, buildNumber int) (string, error) {
	path := fmt.Sprintf("%s/%d/replay/", JobPathToURL(jobPath), buildNumber)
	data, err := c.get(ctx, path)
	if err != nil {
		return "", fmt.Errorf("get build script: %w", err)
	}
	if m := replayScriptRe.FindStringSubmatch(string(data)); m != nil {
		return strings.ReplaceAll(html.UnescapeString(m[2]), "\r\n", "\n"), nil
	}
	return "", fmt.Errorf("mainScript not found in replay page")
}

// crumb holds a Jenkins CSRF crumb for form submissions.
type crumb struct {
	Field string `json:"crumbRequestField"`
	Value string `json:"crumb"`
}

// getCrumb fetches a CSRF crumb from Jenkins. Returns an empty crumb (no error)
// when the Jenkins instance has CSRF protection disabled.
func (c *Client) getCrumb(ctx context.Context) (crumb, error) {
	data, err := c.get(ctx, "/crumbIssuer/api/json")
	if err != nil {
		// 404 means CSRF protection is disabled — not an error.
		if strings.Contains(err.Error(), "HTTP 404") {
			return crumb{}, nil
		}
		return crumb{}, fmt.Errorf("get crumb: %w", err)
	}
	var cr crumb
	if err := json.Unmarshal(data, &cr); err != nil {
		return crumb{}, fmt.Errorf("parse crumb: %w", err)
	}
	return cr, nil
}

// ReplayBuild re-runs a build using the given Groovy script instead of the
// version stored in source control. This mirrors Jenkins' "Replay" button.
// Parameters are inherited from the source build.
//
// Strategy: fetch the replay form page, parse all hidden fields and the real
// form action, then POST everything back with mainScript replaced. This mirrors
// exactly what a browser does and avoids guessing field names.
func (c *Client) ReplayBuild(ctx context.Context, jobPath string, buildNum int, script string) error {
	formPath := fmt.Sprintf("%s/%d/replay/", JobPathToURL(jobPath), buildNum)
	pageData, err := c.get(ctx, formPath)
	if err != nil {
		return fmt.Errorf("replay build: fetch form: %w", err)
	}
	pageHTML := string(pageData)

	// Parse the form action (relative URL like "rebuild").
	actionPath := formPath // fallback
	if m := formActionRe.FindStringSubmatch(pageHTML); m != nil {
		action := html.UnescapeString(m[1])
		if strings.HasPrefix(action, "http") {
			actionPath = action
		} else {
			// Resolve relative to the form page path.
			base := strings.TrimSuffix(c.baseURL+formPath, "/")
			actionPath = base + "/" + strings.TrimPrefix(action, "/")
		}
	} else {
		// Default: append rebuild to the form page path.
		actionPath = c.baseURL + strings.TrimSuffix(formPath, "/") + "/rebuild"
	}

	// Collect hidden input fields (there are typically none on the replay form,
	// but include them for robustness).
	fields := url.Values{}
	for _, tag := range hiddenInputRe.FindAllString(pageHTML, -1) {
		nm := attrNameRe.FindStringSubmatch(tag)
		if nm == nil || nm[1] == "json" {
			continue
		}
		val := ""
		if vm := attrValueRe.FindStringSubmatch(tag); vm != nil {
			val = html.UnescapeString(vm[1])
		}
		fields.Set(nm[1], val)
	}

	// Find the mainScript textarea field name and set our script.
	scriptField := "_.mainScript"
	if m := replayScriptRe.FindStringSubmatch(pageHTML); m != nil {
		scriptField = m[1]
	}
	fields.Set(scriptField, script)

	// Stapler requires a json= parameter as a form-submission marker.
	// The actual script is read from _.mainScript; json just needs to be a valid object.
	fields.Set("json", fmt.Sprintf(`{"mainScript":%s}`, jsonQuote(script)))

	// Fetch a CSRF crumb.
	cr, err := c.getCrumb(ctx)
	if err != nil {
		return err
	}
	if cr.Field != "" {
		fields.Set(cr.Field, cr.Value)
	}

	slog.Debug("replay build: posting form",
		"action", actionPath,
		"scriptField", scriptField,
		"scriptLen", len(script),
		"crumbField", cr.Field,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, actionPath, strings.NewReader(fields.Encode()))
	if err != nil {
		return fmt.Errorf("replay build: build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cr.Field != "" {
		req.Header.Set(cr.Field, cr.Value)
	}

	// Non-redirecting client: Jenkins returns 302 on success, 200 on rejection.
	noFollow := &http.Client{
		Transport:     c.httpClient.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return fmt.Errorf("replay build: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	slog.Debug("replay build: response",
		"status", resp.StatusCode,
		"location", resp.Header.Get("Location"),
		"bodyLen", len(body),
	)

	if resp.StatusCode == http.StatusOK || resp.StatusCode >= 400 {
		dumpFile := ""
		if f, werr := os.CreateTemp("", "jenkins-replay-*.html"); werr == nil {
			_, _ = f.Write(body)
			f.Close()
			dumpFile = f.Name()
		}
		if resp.StatusCode == http.StatusOK {
			return fmt.Errorf("replay build: form re-rendered (check %s)", dumpFile)
		}
		return fmt.Errorf("replay build: HTTP %d (action=%s, check %s)", resp.StatusCode, actionPath, dumpFile)
	}
	return nil
}

// jsonQuote returns s as a JSON string literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
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

package jenkins

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// JenkinsClient defines the API boundary for testability.
type JenkinsClient interface {
	ListJobs(ctx context.Context, folder string) ([]Job, error)
	ListBuilds(ctx context.Context, jobPath string) ([]Build, error)
	ListProjectBuilds(ctx context.Context, projectPath string) ([]ProjectBuild, error)
	ListUserBuilds(ctx context.Context, username string) ([]UserBuild, error)
	ListRunningBuilds(ctx context.Context) ([]UserBuild, error)
	ScanAllBuilds(ctx context.Context, maxPerJob int) ([]UserBuild, error)
	ListStages(ctx context.Context, jobPath string, buildNumber int) ([]Stage, error)
	GetBuild(ctx context.Context, jobPath string, number int) (*BuildDetail, error)
	GetConsoleOutput(ctx context.Context, jobPath string, number int) (io.ReadCloser, error)
	GetFullConsoleText(ctx context.Context, jobPath string, number int) (string, error)
	GetProgressiveLog(ctx context.Context, jobPath string, number, start int) (*ProgressiveLog, error)
	GetNodeLog(ctx context.Context, jobPath string, buildNumber, nodeID int) (string, error)
	GetNodeLogProgressive(ctx context.Context, jobPath string, buildNumber, nodeID, start int) (*NodeLog, error)
	GetJobParameters(ctx context.Context, jobPath string) ([]ParameterDefinition, error)
	GetBuildScript(ctx context.Context, jobPath string, buildNumber int) (string, error)
	GetBuildParameters(ctx context.Context, jobPath string, buildNumber int) (map[string]string, error)
	GetTestReport(ctx context.Context, jobPath string, buildNum int) (*TestReport, error)
	TriggerBuild(ctx context.Context, jobPath string, params map[string]string) error
	ReplayBuild(ctx context.Context, jobPath string, buildNum int, script string) error
	CancelBuild(ctx context.Context, jobPath string, number int) error
	WhoAmI(ctx context.Context) (*User, error)
}

// Compile-time interface check.
var _ JenkinsClient = (*Client)(nil)

// Client is the concrete Jenkins HTTP client.
type Client struct {
	baseURL    string
	username   string
	token      string
	httpClient *http.Client
}

// NewClient creates a new Jenkins API client.
func NewClient(baseURL, username, token string, insecure bool) *Client {
	// Re-attach Basic Auth on every redirect. Go strips the Authorization header
	// by default, which causes an infinite redirect loop when Jenkins responds
	// with a relative redirect to securityRealm/commenceLogin.
	reattachAuth := func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if u, p, ok := via[0].BasicAuth(); ok {
			req.SetBasicAuth(u, p)
		}
		return nil
	}
	httpClient := &http.Client{CheckRedirect: reattachAuth}
	if insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	return &Client{
		baseURL:    baseURL,
		username:   username,
		token:      token,
		httpClient: httpClient,
	}
}

// doRequest builds and executes an HTTP request with Basic Auth.
// The caller is responsible for closing the response body.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("authentication failed (check username and token): %s %s", method, path)
		case http.StatusForbidden:
			return nil, fmt.Errorf("access denied (insufficient permissions): %s %s", method, path)
		case http.StatusFound, http.StatusMovedPermanently:
			return nil, fmt.Errorf("unexpected redirect to %s — check server URL and credentials: %s %s", resp.Header.Get("Location"), method, path)
		default:
			return nil, fmt.Errorf("HTTP %d: %s %s", resp.StatusCode, method, path)
		}
	}

	return resp, nil
}

// get performs a GET request and returns the response body bytes.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return data, nil
}

// post performs a POST request.
func (c *Client) post(ctx context.Context, path string, body io.Reader) error {
	resp, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// WhoAmI returns the authenticated Jenkins user.
func (c *Client) WhoAmI(ctx context.Context) (*User, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/me/api/json", nil)
	if err != nil {
		return nil, fmt.Errorf("whoami: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading whoami response: %w", err)
	}

	var u jsonUser
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, fmt.Errorf("parsing whoami response: %w", err)
	}

	return &User{
		ID:             u.ID,
		FullName:       u.FullName,
		JenkinsVersion: resp.Header.Get("X-Jenkins"),
	}, nil
}

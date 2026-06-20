package jenkins

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// JenkinsClient lives in internal/domain/jmodel; this adapter implements it.
var _ jmodel.JenkinsClient = (*Client)(nil)

// Client is the concrete Jenkins HTTP client.
type Client struct {
	baseURL    string
	username   string
	token      string
	httpClient *http.Client
}

// NewClient creates a new Jenkins API client.
func NewClient(baseURL, username, token string, insecure bool) *Client {
	// Re-attach Basic Auth on same-host redirects. Go strips the Authorization
	// header by default, which causes an infinite redirect loop when Jenkins
	// responds with a relative redirect to securityRealm/commenceLogin.
	//
	// Only reattach when the redirect stays on the original host. When Jenkins
	// is configured with a "Resource Root URL", artifact requests redirect to a
	// separate domain with a signed token in the URL; that server authorizes via
	// the token and rejects requests carrying credentials with HTTP 400, so we
	// must let Go's default header-stripping stand for cross-host hops.
	reattachAuth := func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return nil
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

// HTTPError is returned for non-2xx responses that aren't auth- or
// redirect-related, carrying the status code so callers can react to it
// (e.g. enrich a 404 with context) via errors.As.
type HTTPError struct {
	StatusCode int
	Method     string
	Path       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s %s", e.StatusCode, e.Method, e.Path)
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
			return nil, &HTTPError{StatusCode: resp.StatusCode, Method: method, Path: path}
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
func (c *Client) WhoAmI(ctx context.Context) (*jmodel.User, error) {
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

	return &jmodel.User{
		ID:             u.ID,
		FullName:       u.FullName,
		JenkinsVersion: resp.Header.Get("X-Jenkins"),
	}, nil
}

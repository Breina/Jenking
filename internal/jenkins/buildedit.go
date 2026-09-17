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

// SetBuildDescription replaces a build's description (core Run/submitDescription).
func (c *Client) SetBuildDescription(ctx context.Context, jobPath string, number int, description string) error {
	path := fmt.Sprintf("%s/%d/submitDescription", jmodel.JobPathToURL(jobPath), number)
	return c.postForm(ctx, path, url.Values{"description": {description}})
}

// ConfigureBuild sets a build's display name and description through core's
// Run/configSubmit. Jenkins overwrites both fields, so callers pass the current
// description to keep it.
func (c *Client) ConfigureBuild(ctx context.Context, jobPath string, number int, displayName, description string) error {
	payload, err := json.Marshal(map[string]string{"displayName": displayName, "description": description})
	if err != nil {
		return fmt.Errorf("encode build config: %w", err)
	}
	path := fmt.Sprintf("%s/%d/configSubmit", jmodel.JobPathToURL(jobPath), number)
	return c.postForm(ctx, path, url.Values{"json": {string(payload)}})
}

// postForm POSTs form-encoded values in the request body, which (unlike query
// parameters) has no practical length limit.
func (c *Client) postForm(ctx context.Context, path string, form url.Values) error {
	resp, err := c.doRequestType(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

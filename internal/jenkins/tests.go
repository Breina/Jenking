package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GetTestReport fetches JUnit test results for the given build.
// Returns nil, nil when Jenkins reports 404 (no test results recorded).
func (c *Client) GetTestReport(ctx context.Context, jobPath string, buildNum int) (*TestReport, error) {
	path := fmt.Sprintf(
		"%s/%d/testReport/api/json?tree=duration,failCount,passCount,skipCount,suites[name,duration,cases[className,name,status,duration,errorDetails]]",
		JobPathToURL(jobPath), buildNum,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating test report request: %w", err)
	}
	req.SetBasicAuth(c.username, c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching test report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no test results recorded for this build
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jenkins API error: GET %s returned %d", path, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading test report response: %w", err)
	}

	var jr jsonTestReport
	if err := json.Unmarshal(data, &jr); err != nil {
		return nil, fmt.Errorf("parsing test report: %w", err)
	}

	return jr.toDomain(), nil
}

package jenkins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// GetFullConsoleText reads the entire build console log into a string.
func (c *Client) GetFullConsoleText(ctx context.Context, jobPath string, number int) (string, error) {
	rc, err := c.GetConsoleOutput(ctx, jobPath, number)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read console text: %w", err)
	}
	return string(data), nil
}

// GetConsoleOutput returns a streaming reader for a build's console output.
func (c *Client) GetConsoleOutput(ctx context.Context, jobPath string, number int) (io.ReadCloser, error) {
	path := fmt.Sprintf("%s/%d/consoleText", jmodel.JobPathToURL(jobPath), number)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get console output: %w", err)
	}

	return resp.Body, nil
}

// ProgressiveLog lives in internal/domain/jmodel.

// GetProgressiveLog fetches console output starting at byte offset start.
// Poll with NextStart from the returned value until MoreData is false.
func (c *Client) GetProgressiveLog(ctx context.Context, jobPath string, number, start int) (*jmodel.ProgressiveLog, error) {
	path := fmt.Sprintf("%s/%d/logText/progressiveText?start=%d", jmodel.JobPathToURL(jobPath), number, start)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get progressive log: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read progressive log: %w", err)
	}

	moreData := resp.Header.Get("X-More-Data") == "true"
	nextStart := start + len(body)
	if ts := resp.Header.Get("X-Text-Size"); ts != "" {
		if n, err := strconv.Atoi(ts); err == nil {
			nextStart = n
		}
	}

	return &jmodel.ProgressiveLog{
		Text:      string(body),
		MoreData:  moreData,
		NextStart: nextStart,
	}, nil
}

// NodeLog lives in internal/domain/jmodel.

// GetNodeLog fetches the full console output for a single flow graph node.
// Uses the progressive text API which works for both completed and running nodes.
func (c *Client) GetNodeLog(ctx context.Context, jobPath string, buildNumber, nodeID int) (string, error) {
	nl, err := c.GetNodeLogProgressive(ctx, jobPath, buildNumber, nodeID, 0)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(nl.Text, "\n"), nil
}

// GetNodeLogProgressive fetches node console output starting at byte offset start.
// Poll with NextStart from the returned value until MoreData is false.
func (c *Client) GetNodeLogProgressive(ctx context.Context, jobPath string, buildNumber, nodeID, start int) (*jmodel.NodeLog, error) {
	path := fmt.Sprintf("%s/%d/execution/node/%d/log/logText/progressiveText?start=%d",
		jmodel.JobPathToURL(jobPath), buildNumber, nodeID, start)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get node log: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read node log: %w", err)
	}

	moreData := resp.Header.Get("X-More-Data") == "true"
	nextStart := start + len(body)
	if ts := resp.Header.Get("X-Text-Size"); ts != "" {
		if n, err := strconv.Atoi(ts); err == nil {
			nextStart = n
		}
	}

	return &jmodel.NodeLog{
		Text:      string(body),
		MoreData:  moreData,
		NextStart: nextStart,
	}, nil
}

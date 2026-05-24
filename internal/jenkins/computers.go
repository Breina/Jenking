package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type jsonComputerList struct {
	Computer []jsonComputer `json:"computer"`
}

type jsonComputer struct {
	DisplayName     string         `json:"displayName"`
	Executors       []jsonExecutor `json:"executors"`
	OneOffExecutors []jsonExecutor `json:"oneOffExecutors"`
}

type jsonExecutor struct {
	CurrentExecutable *jsonRunningBuild `json:"currentExecutable"`
}

type jsonRunningBuild struct {
	Number            int          `json:"number"`
	URL               string       `json:"url"`
	Timestamp         int64        `json:"timestamp"`
	EstimatedDuration int64        `json:"estimatedDuration"`
	Building          bool         `json:"building"`
	FullDisplayName   string       `json:"fullDisplayName"`
	Actions           []jsonAction `json:"actions"`
}

const computerTreeParam = `computer[displayName,` +
	`executors[currentExecutable[number,url,timestamp,estimatedDuration,building,fullDisplayName,actions[causes[shortDescription,userName,userId]]]],` +
	`oneOffExecutors[currentExecutable[number,url,timestamp,estimatedDuration,building,fullDisplayName,actions[causes[shortDescription,userName,userId]]]]]`

// ListRunningBuilds returns all builds currently executing across all nodes.
func (c *Client) ListRunningBuilds(ctx context.Context) ([]jmodel.UserBuild, error) {
	data, err := c.get(ctx, "/computer/api/json?tree="+computerTreeParam)
	if err != nil {
		return nil, fmt.Errorf("listing running builds: %w", err)
	}

	var list jsonComputerList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parsing running builds: %w", err)
	}

	var builds []jmodel.UserBuild
	for _, computer := range list.Computer {
		executors := append(computer.Executors, computer.OneOffExecutors...)
		for _, ex := range executors {
			rb := ex.CurrentExecutable
			if rb == nil || !rb.Building {
				continue
			}
			jobPath, number, err := parseJobURL(c.baseURL, rb.URL)
			if err != nil {
				continue
			}
			builds = append(builds, jmodel.UserBuild{
				JobPath:     jobPath,
				Node:        computer.DisplayName,
				DisplayName: rb.FullDisplayName,
				Build: jmodel.Build{
					Number:            number,
					Status:            jmodel.BuildStatusRunning,
					EstimatedDuration: millisToDuration(rb.EstimatedDuration),
					Timestamp:         millisToTime(rb.Timestamp),
					TriggeredBy:       extractUserID(rb.Actions),
					TriggeredByName:   extractUserName(rb.Actions),
					Cause:             extractCause(rb.Actions),
				},
			})
		}
	}
	return builds, nil
}

// parseJobURL extracts the job path and build number from a Jenkins build URL.
// Example: "https://ci.example.com/job/FolderA/job/Pipeline/42/" → ("FolderA/Pipeline", 42, nil)
func parseJobURL(baseURL, rawURL string) (jobPath string, number int, err error) {
	path := strings.TrimPrefix(rawURL, baseURL)
	path = strings.Trim(path, "/")
	segments := strings.Split(path, "/")

	var jobParts []string
	i := 0
	for i < len(segments) {
		if segments[i] == "job" && i+1 < len(segments) {
			seg, _ := url.PathUnescape(segments[i+1])
			jobParts = append(jobParts, seg)
			i += 2
		} else {
			// last numeric segment should be the build number
			n, parseErr := strconv.Atoi(segments[i])
			if parseErr == nil {
				number = n
			}
			i++
		}
	}

	if len(jobParts) == 0 {
		return "", 0, fmt.Errorf("no job segments in URL: %s", rawURL)
	}
	if number == 0 {
		return "", 0, fmt.Errorf("no build number in URL: %s", rawURL)
	}
	return strings.Join(jobParts, "/"), number, nil
}

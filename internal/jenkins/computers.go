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

// monitorData is a Map keyed by monitor class name. Deep tree sub-paths into it
// (e.g. monitorData[hudson.node_monitors.ResponseTimeMonitor[average]]) return
// empty objects on newer Jenkins, so we request monitorData[*] — every monitor
// expanded to its primitive fields — and pick out the ones we surface. Note the
// monitor values are only populated for callers with permission to read node
// monitoring data; a lower-privileged API token yields empty monitors.
const nodeTreeParam = `computer[displayName,offline,temporarilyOffline,offlineCauseReason,` +
	`assignedLabels[name],executors[currentExecutable[building]],monitorData[*]]`

// jsonComputerNodeList decodes the node view of /computer with monitor data.
// It is separate from jsonComputerList (used by ListRunningBuilds) because the
// monitor keys contain dots and are only requested here.
type jsonComputerNodeList struct {
	Computer []jsonComputerNode `json:"computer"`
}

type jsonComputerNode struct {
	DisplayName        string          `json:"displayName"`
	Offline            bool            `json:"offline"`
	TemporarilyOffline bool            `json:"temporarilyOffline"`
	OfflineCauseReason string          `json:"offlineCauseReason"`
	AssignedLabels     []jsonLabel     `json:"assignedLabels"`
	Executors          []jsonExecutor  `json:"executors"`
	MonitorData        jsonMonitorData `json:"monitorData"`
}

type jsonLabel struct {
	Name string `json:"name"`
}

// jsonMonitorData decodes the subset of node-monitor results we surface. Each
// entry is null when the node is offline or the monitor has no reading, hence
// the pointer types.
type jsonMonitorData struct {
	Disk *struct {
		Size float64 `json:"size"`
	} `json:"hudson.node_monitors.DiskSpaceMonitor"`
	Swap *struct {
		AvailablePhysicalMemory float64 `json:"availablePhysicalMemory"`
	} `json:"hudson.node_monitors.SwapSpaceMonitor"`
	Response *struct {
		Average float64 `json:"average"`
	} `json:"hudson.node_monitors.ResponseTimeMonitor"`
}

// ListNodes returns per-node executor utilization and health from
// /computer/api/json. Capacity (NumExecutors) is the count of the node's
// standard executors; busy count is those whose currentExecutable is actively
// building. One-off executors (flyweight, spawned per pipeline `node{}` block)
// are excluded so the ratio reflects real executor slots. Free disk/memory and
// response time come from the built-in node monitors. Core endpoint — no plugin
// required.
func (c *Client) ListNodes(ctx context.Context) ([]jmodel.Node, error) {
	data, err := c.get(ctx, "/computer/api/json?tree="+nodeTreeParam)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var list jsonComputerNodeList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parsing nodes: %w", err)
	}

	nodes := make([]jmodel.Node, 0, len(list.Computer))
	for _, comp := range list.Computer {
		busy := 0
		for _, ex := range comp.Executors {
			if ex.CurrentExecutable != nil && ex.CurrentExecutable.Building {
				busy++
			}
		}
		var labels []string
		for _, l := range comp.AssignedLabels {
			if l.Name != "" {
				labels = append(labels, l.Name)
			}
		}
		n := jmodel.Node{
			Name:          comp.DisplayName,
			Offline:       comp.Offline || comp.TemporarilyOffline,
			OfflineCause:  comp.OfflineCauseReason,
			NumExecutors:  len(comp.Executors),
			BusyExecutors: busy,
			Labels:        labels,
		}
		if md := comp.MonitorData; md.Disk != nil {
			n.FreeDiskBytes = int64(md.Disk.Size)
		}
		if comp.MonitorData.Swap != nil {
			n.FreeMemBytes = int64(comp.MonitorData.Swap.AvailablePhysicalMemory)
		}
		if comp.MonitorData.Response != nil {
			n.ResponseMs = int64(comp.MonitorData.Response.Average)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
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

// ToggleNodeOffline flips a node's temporarily-offline state. The reason is
// recorded as the offline message (ignored by Jenkins when bringing a node
// back online). The built-in node is addressed as "(built-in)".
func (c *Client) ToggleNodeOffline(ctx context.Context, name, reason string) error {
	path := fmt.Sprintf("/computer/%s/toggleOffline?offlineMessage=%s", url.PathEscape(name), url.QueryEscape(reason))
	return c.post(ctx, path, nil)
}

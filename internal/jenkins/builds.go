package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// ListBuilds returns the most recent builds for a job.
func (c *Client) ListBuilds(ctx context.Context, jobPath string) ([]jmodel.Build, error) {
	path := jmodel.JobPathToURL(jobPath) + "/api/json?tree=builds[number,url,result,building,duration,estimatedDuration,timestamp,displayName,description,_class,actions[causes[userId,userName,shortDescription]]]{0,25}"

	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list builds: %w", err)
	}

	var resp jsonBuildList
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing builds response: %w", err)
	}

	builds := make([]jmodel.Build, len(resp.Builds))
	for i, b := range resp.Builds {
		builds[i] = b.toDomain()
	}
	return builds, nil
}

// jsonProjectJob is the JSON response shape for a branch job inside a multibranch project.
type jsonProjectJob struct {
	Name     string      `json:"name"`
	FullName string      `json:"fullName"`
	Builds   []jsonBuild `json:"builds"`
}

// jsonProjectJobList holds the branch jobs for a multibranch project.
type jsonProjectJobList struct {
	Jobs []jsonProjectJob `json:"jobs"`
}

// ListProjectBuilds returns all recent builds across all branches of a multibranch project,
// sorted by Timestamp descending (most recent first).
func (c *Client) ListProjectBuilds(ctx context.Context, projectPath string) ([]jmodel.ProjectBuild, error) {
	path := jmodel.JobPathToURL(projectPath) + "/api/json?tree=jobs[name,fullName,builds[number,result,building,duration,estimatedDuration,timestamp,displayName,description,actions[causes[userId,userName,shortDescription]]]]"

	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list project builds: %w", err)
	}

	var resp jsonProjectJobList
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing project builds response: %w", err)
	}

	var builds []jmodel.ProjectBuild
	for _, job := range resp.Jobs {
		for _, b := range job.Builds {
			pb := jmodel.ProjectBuild{
				Build:      b.toDomain(),
				BranchName: job.Name,
				BranchPath: job.FullName,
			}
			builds = append(builds, pb)
		}
	}

	sort.Slice(builds, func(i, j int) bool {
		return builds[i].Timestamp.After(builds[j].Timestamp)
	})

	return builds, nil
}

// ListUserBuilds returns recent builds triggered by the given user.
func (c *Client) ListUserBuilds(ctx context.Context, username string) ([]jmodel.UserBuild, error) {
	// Uses Jenkins views API with a user cause filter
	path := "/api/json?tree=jobs[name,url,builds[number,result,building,duration,estimatedDuration,timestamp]]{0,10}"
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list user builds: %w", err)
	}

	// TODO: implement proper user build filtering
	// For now, return empty — full implementation requires iterating builds and checking causes
	_ = data
	return nil, nil
}

// buildDetailTree is the tree query for GetBuild. Includes everything
// jsonBuildDetail.toDomain reads, plus the InputAction.executions branch so
// PendingInputs is populated when the pipeline is paused.
const buildDetailTree = "number,result,building,duration,estimatedDuration,timestamp,url," +
	"actions[_class," +
	"parameters[name,value]," +
	"causes[_class,shortDescription,userId,userName]," +
	"executions[id,displayName,settled," +
	"input[message,ok,cancel,submitter,submitterParameter," +
	"parameters[_class,name,description,choices,defaultParameterValue[value]]]]]"

// GetBuild returns detailed build information including stages.
func (c *Client) GetBuild(ctx context.Context, jobPath string, number int) (*jmodel.BuildDetail, error) {
	basePath := fmt.Sprintf("%s/%d", jmodel.JobPathToURL(jobPath), number)

	data, err := c.get(ctx, basePath+"/api/json?tree="+url.QueryEscape(buildDetailTree))
	if err != nil {
		return nil, fmt.Errorf("get build: %w", err)
	}

	var resp jsonBuildDetail
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing build response: %w", err)
	}

	detail := resp.toDomain()
	return &detail, nil
}

// ListStages returns the pipeline stages for a specific build.
// It uses the flowGraph AJAX endpoint (same as Jenkins GUI refresh)
// which returns only the table fragment (~48KB) instead of the full
// page (~157KB).
func (c *Client) ListStages(ctx context.Context, jobPath string, buildNumber int) ([]jmodel.Stage, error) {
	path := fmt.Sprintf("%s/%d/flowGraphTable/flowGraph/ajax", jmodel.JobPathToURL(jobPath), buildNumber)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list stages: %w", err)
	}
	return parseFlowGraphTable(string(data))
}

var (
	flowGraphRowRe      = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	flowGraphCellRe     = regexp.MustCompile(`(?s)<td([^>]*)>(.*?)</td>`)
	flowGraphTagRe      = regexp.MustCompile(`<[^>]+>`)
	flowGraphDurationRe = regexp.MustCompile(`\((.+?) in block\)`)
	flowGraphNodeIDRe   = regexp.MustCompile(`tooltip="ID: (\d+)"`)
	flowGraphLogRe      = regexp.MustCompile(`href="[^"]*log[^"]*"`)
	flowGraphPaddingRe  = regexp.MustCompile(`padding-left:\s*calc\([^*]*\*\s*(\d+)\)`)

	stageEnterRe   = regexp.MustCompile(`^\[Pipeline\] \{ \((.+)\)$`)
	stageSkippedRe = regexp.MustCompile(`Stage "(.+)" skipped due to (when conditional|earlier failure)`)

	// ansiHiddenBlockRe strips ANSI hidden text blocks: \x1b[8m...content...\x1b[0m.
	// Jenkins embeds base64 metadata inside these blocks in progressive log output.
	ansiHiddenBlockRe = regexp.MustCompile("\x1b\\[8m[^\x1b]*\x1b\\[0m")
	// ansiEscRe matches remaining ANSI escape sequences (colors, cursor, etc).
	ansiEscRe = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")
)

// flowGraphRow holds parsed data from one flowGraphTable <tr>.
type flowGraphRow struct {
	step     string // e.g. "stage", "stage block (X)", "sh", "echo"
	name     string // TD1 content (stage name or arguments)
	status   string // TD3 content
	nodeID   int    // flow node ID
	hasLog   bool   // whether this row has a log link
	dur      time.Duration
	cssDepth int // padding-left multiplier from CSS
}

// stageEndIdx finds the end of a stage's scope: the first row after startIdx
// whose cssDepth <= the stage's cssDepth, skipping only the stage block row
// (which sits at cssDepth+1 and is part of the stage, not a boundary).
func stageEndIdx(rows []flowGraphRow, startIdx, cssDepth int) int {
	for ri := startIdx + 1; ri < len(rows); ri++ {
		if rows[ri].cssDepth <= cssDepth {
			return ri
		}
	}
	return len(rows)
}

// isWorseStatus returns true if a is worse than b in severity ordering.
func isWorseStatus(a, b jmodel.BuildStatus) bool {
	return statusSeverity(a) > statusSeverity(b)
}

func statusSeverity(s jmodel.BuildStatus) int {
	switch s {
	case jmodel.BuildStatusSuccess:
		return 0
	case jmodel.BuildStatusSkipped:
		return 1
	case jmodel.BuildStatusNotBuilt:
		return 2
	case jmodel.BuildStatusUnknown:
		return 3
	case jmodel.BuildStatusRunning:
		return 4
	case jmodel.BuildStatusUnstable:
		return 5
	case jmodel.BuildStatusAborted:
		return 6
	case jmodel.BuildStatusFailed:
		return 7
	default:
		return 0
	}
}

func stripTags(s string) string {
	return html.UnescapeString(strings.TrimSpace(flowGraphTagRe.ReplaceAllString(s, "")))
}

func parseFlowGraphStatus(s string) jmodel.BuildStatus {
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "success"):
		return jmodel.BuildStatusSuccess
	case strings.Contains(lower, "failure"), strings.Contains(lower, "failed"):
		return jmodel.BuildStatusFailed
	case strings.Contains(lower, "aborted"):
		return jmodel.BuildStatusAborted
	case strings.Contains(lower, "unstable"):
		return jmodel.BuildStatusUnstable
	case strings.Contains(lower, "not"):
		return jmodel.BuildStatusNotBuilt
	case strings.Contains(lower, "progress"), strings.Contains(lower, "paused"):
		return jmodel.BuildStatusRunning
	default:
		return jmodel.BuildStatusUnknown
	}
}

var (
	durHourRe    = regexp.MustCompile(`(\d+)\s*hr`)
	durMinRe     = regexp.MustCompile(`(\d+)\s*min`)
	durSecRe     = regexp.MustCompile(`([\d.]+)\s*sec`)
	durMilliRe   = regexp.MustCompile(`(\d+)\s*ms`)
	durDecimalRe = regexp.MustCompile(`\.(\d+)$`)
)

// parseDurationText parses strings like "4 min 13 sec", "6.4 sec", "39 ms".
//
// Jenkins renders these with Util.getTimeSpanString, which truncates (integer
// division) rather than rounding, and drops units below the two most
// significant ones — "12 min" can be anything from 12m00s to 12m59s. Taking the
// text at face value therefore biases every duration low, which shows up as
// progress bars that reliably overrun by ~1s. We correct by returning the
// midpoint of the interval the text represents: the parsed value plus half the
// quantum of its least significant unit.
func parseDurationText(s string) time.Duration {
	s = strings.TrimSpace(s)
	var total, quantum time.Duration

	// Match hours
	if m := durHourRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		total += time.Duration(n) * time.Hour
		quantum = time.Hour
	}
	// Match minutes
	if m := durMinRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		total += time.Duration(n) * time.Minute
		quantum = time.Minute
	}
	// Match seconds (may be float like "6.4 sec", in which case the decimals
	// tighten the quantum: "6.4 sec" resolves to a tenth, not a whole second)
	if m := durSecRe.FindStringSubmatch(s); m != nil {
		f, _ := strconv.ParseFloat(m[1], 64)
		total += time.Duration(f * float64(time.Second))
		quantum = time.Second
		if d := durDecimalRe.FindStringSubmatch(m[1]); d != nil {
			for range d[1] {
				quantum /= 10
			}
		}
	}
	// Match milliseconds
	if m := durMilliRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		total += time.Duration(n) * time.Millisecond
		quantum = time.Millisecond
	}
	if quantum == 0 {
		return 0
	}
	return total + quantum/2
}

// cleanLogLine strips Jenkins ANSI hidden text blocks and remaining escape codes.
// The /logText/progressiveText endpoint embeds base64 metadata inside ANSI hidden
// blocks (\x1b[8m...\x1b[0m) which must be fully removed for regex matching.
func cleanLogLine(s string) string {
	s = ansiHiddenBlockRe.ReplaceAllString(s, "")
	s = ansiEscRe.ReplaceAllString(s, "")
	return s
}

// ParseSkippedStages extracts per-occurrence skip status for each stage name.
// It detects both when-conditional skips and earlier-failure skips from the
// console log. Returns a map from stage name to a slice of bools (one per
// occurrence in log order): true = that occurrence was skipped.
func ParseSkippedStages(logText string) map[string][]bool {
	result := map[string][]bool{}
	var currentStage string
	var currentIdx int
	for _, line := range strings.Split(logText, "\n") {
		line = strings.TrimRight(line, "\r")
		line = cleanLogLine(line)
		if m := stageEnterRe.FindStringSubmatch(line); m != nil {
			currentStage = m[1]
			currentIdx = len(result[currentStage])
			result[currentStage] = append(result[currentStage], false)
		} else if m := stageSkippedRe.FindStringSubmatch(line); m != nil && currentStage != "" {
			// Always mark the current stage as skipped.
			result[currentStage][currentIdx] = true
			// If the skip line names a different stage (e.g. parallel children
			// whose skip lines appear after all entries), also mark the named
			// stage's most recent occurrence.
			namedStage := m[1]
			if namedStage != currentStage {
				if occs, ok := result[namedStage]; ok && len(occs) > 0 {
					result[namedStage][len(occs)-1] = true
				}
			}
		}
	}
	return result
}

// MarkSkipped marks SUCCESS stages as SKIPPED based on per-occurrence skip data.
// It matches stages to log occurrences by name in order, so duplicate stage names
// in parallel branches are handled correctly. Never overrides FAILED or ABORTED.
func MarkSkipped(stages []jmodel.Stage, skippedOccs map[string][]bool) {
	occCounts := map[string]int{}
	for i := range stages {
		name := stages[i].Name
		occ := occCounts[name]
		occCounts[name]++
		occs := skippedOccs[name]
		if occ < len(occs) && occs[occ] && stages[i].Status == jmodel.BuildStatusSuccess {
			stages[i].Status = jmodel.BuildStatusSkipped
			stages[i].NodeIDs = nil
		}
	}
}

// TriggerBuild starts a new build for the given job. It returns the queue
// item id parsed from the Location response header, or 0 when the server
// does not report one.
func (c *Client) TriggerBuild(ctx context.Context, jobPath string, params map[string]string) (int64, error) {
	basePath := jmodel.JobPathToURL(jobPath)
	path := basePath + "/build"
	if len(params) > 0 {
		form := url.Values{}
		for k, v := range params {
			form.Set(k, v)
		}
		path = basePath + "/buildWithParameters?" + form.Encode()
	}
	loc, err := c.postForLocation(ctx, path)
	if err != nil {
		return 0, err
	}
	return parseQueueItemID(loc), nil
}

var queueItemIDRe = regexp.MustCompile(`/queue/item/(\d+)/?$`)

// parseQueueItemID extracts the queue item id from a Location header like
// "https://ci/queue/item/1234/". Returns 0 when the URL carries none.
func parseQueueItemID(location string) int64 {
	m := queueItemIDRe.FindStringSubmatch(location)
	if m == nil {
		return 0
	}
	id, _ := strconv.ParseInt(m[1], 10, 64)
	return id
}

// CancelBuild stops a running build.
func (c *Client) CancelBuild(ctx context.Context, jobPath string, number int) error {
	path := fmt.Sprintf("%s/%d/stop", jmodel.JobPathToURL(jobPath), number)
	return c.post(ctx, path, nil)
}

// ProceedInput approves a pipeline `input` step. params is nil/empty for a
// confirm-only input; otherwise the values are form-encoded into the JSON
// payload Jenkins expects on /submit.
func (c *Client) ProceedInput(ctx context.Context, jobPath string, buildNumber int, inputID string, params map[string]string) error {
	if inputID == "" {
		return fmt.Errorf("proceed input: empty input id")
	}
	base := fmt.Sprintf("%s/%d/input/%s", jmodel.JobPathToURL(jobPath), buildNumber, url.PathEscape(inputID))
	if len(params) == 0 {
		return c.post(ctx, base+"/proceedEmpty", nil)
	}
	jsonArr := make([]map[string]string, 0, len(params))
	for k, v := range params {
		jsonArr = append(jsonArr, map[string]string{"name": k, "value": v})
	}
	body, err := json.Marshal(map[string]interface{}{"parameter": jsonArr})
	if err != nil {
		return fmt.Errorf("encode input params: %w", err)
	}
	form := url.Values{}
	form.Set("json", string(body))
	form.Set("proceed", "Proceed")
	return c.post(ctx, base+"/submit?"+form.Encode(), nil)
}

// AbortInput rejects a pipeline `input` step.
func (c *Client) AbortInput(ctx context.Context, jobPath string, buildNumber int, inputID string) error {
	if inputID == "" {
		return fmt.Errorf("abort input: empty input id")
	}
	path := fmt.Sprintf("%s/%d/input/%s/abort", jmodel.JobPathToURL(jobPath), buildNumber, url.PathEscape(inputID))
	return c.post(ctx, path, nil)
}

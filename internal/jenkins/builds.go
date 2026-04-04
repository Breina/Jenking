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
)

// ListBuilds returns the most recent builds for a job.
func (c *Client) ListBuilds(ctx context.Context, jobPath string) ([]Build, error) {
	path := JobPathToURL(jobPath) + "/api/json?tree=builds[number,url,result,building,duration,estimatedDuration,timestamp,_class,actions[causes[userId,userName,shortDescription]]]{0,25}"

	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list builds: %w", err)
	}

	var resp jsonBuildList
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing builds response: %w", err)
	}

	builds := make([]Build, len(resp.Builds))
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
func (c *Client) ListProjectBuilds(ctx context.Context, projectPath string) ([]ProjectBuild, error) {
	path := JobPathToURL(projectPath) + "/api/json?tree=jobs[name,fullName,builds[number,result,building,duration,estimatedDuration,timestamp,actions[causes[userId,userName,shortDescription]]]]"

	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list project builds: %w", err)
	}

	var resp jsonProjectJobList
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing project builds response: %w", err)
	}

	var builds []ProjectBuild
	for _, job := range resp.Jobs {
		for _, b := range job.Builds {
			pb := ProjectBuild{
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
func (c *Client) ListUserBuilds(ctx context.Context, username string) ([]UserBuild, error) {
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

// GetBuild returns detailed build information including stages.
func (c *Client) GetBuild(ctx context.Context, jobPath string, number int) (*BuildDetail, error) {
	basePath := fmt.Sprintf("%s/%d", JobPathToURL(jobPath), number)

	// Get build details
	data, err := c.get(ctx, basePath+"/api/json")
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
func (c *Client) ListStages(ctx context.Context, jobPath string, buildNumber int) ([]Stage, error) {
	path := fmt.Sprintf("%s/%d/flowGraphTable/flowGraph/ajax", JobPathToURL(jobPath), buildNumber)
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

// TODO overly complex method
// parseFlowGraphTable extracts pipeline stages from Jenkins flowGraphTable HTML.
// It uses CSS padding-left multipliers to determine nesting depth, collects
// child node IDs per stage, propagates child failure status, and detects
// parallel execution.
func parseFlowGraphTable(rawHTML string) ([]Stage, error) {
	// First pass: parse all rows with their CSS depth.
	var rows []flowGraphRow
	for _, rowMatch := range flowGraphRowRe.FindAllStringSubmatch(rawHTML, -1) {
		row := rowMatch[1]
		cells := flowGraphCellRe.FindAllStringSubmatch(row, -1)
		if len(cells) < 4 {
			continue
		}
		// cells[i] = [fullMatch, tdAttrs, tdContent]
		tdAttrs := cells[0][1]
		raw0 := cells[0][2]
		stepCell := stripTags(raw0)

		var nodeID int
		if m := flowGraphNodeIDRe.FindStringSubmatch(raw0); m != nil {
			nodeID, _ = strconv.Atoi(m[1])
		}

		var dur time.Duration
		if m := flowGraphDurationRe.FindStringSubmatch(stepCell); m != nil {
			dur = parseDurationText(m[1])
		}

		var cssDepth int
		if m := flowGraphPaddingRe.FindStringSubmatch(tdAttrs); m != nil {
			cssDepth, _ = strconv.Atoi(m[1])
		}

		rows = append(rows, flowGraphRow{
			step:     stepCell,
			name:     strings.TrimSpace(stripTags(cells[1][2])),
			status:   strings.TrimSpace(stripTags(cells[3][2])),
			nodeID:   nodeID,
			hasLog:   flowGraphLogRe.MatchString(cells[2][2]),
			dur:      dur,
			cssDepth: cssDepth,
		})
	}

	// Second pass: identify stages, build tree with nesting + parallel detection.
	// Each stage row has a cssDepth. Rows between this stage and the next stage
	// at the same or lower depth are children.
	type stageInfo struct {
		rowIdx   int
		name     string
		dur      time.Duration
		cssDepth int
	}

	var stageRows []stageInfo
	for i, r := range rows {
		if strings.HasPrefix(r.step, "stage ") && !strings.Contains(r.step, "stage block") {
			stageRows = append(stageRows, stageInfo{
				rowIdx:   i,
				name:     r.name,
				dur:      r.dur,
				cssDepth: r.cssDepth,
			})
		}
	}

	// Find the minimum stage cssDepth to compute relative depth 0.
	minDepth := 0
	if len(stageRows) > 0 {
		minDepth = stageRows[0].cssDepth
		for _, sr := range stageRows[1:] {
			if sr.cssDepth < minDepth {
				minDepth = sr.cssDepth
			}
		}
	}

	// For each stage, find the end of its children: the next row whose
	// cssDepth <= this stage's cssDepth (meaning a sibling or ancestor).
	// We check ALL rows (not just stage rows) because non-stage rows like
	// withEnv at the same cssDepth indicate we've left the stage's scope.
	stages := make([]Stage, len(stageRows))
	for si, sr := range stageRows {
		endIdx := stageEndIdx(rows, sr.rowIdx, sr.cssDepth)

		// Collect node IDs and propagate worst status from all children.
		status := BuildStatusSuccess
		var nodeIDs []int
		for _, r := range rows[sr.rowIdx:endIdx] {
			if r.hasLog && r.nodeID > 0 {
				nodeIDs = append(nodeIDs, r.nodeID)
			}
			childStatus := parseFlowGraphStatus(r.status)
			if isWorseStatus(childStatus, status) {
				status = childStatus
			}
		}

		// Detect parallel/matrix: check if this stage's block contains
		// a direct "parallel" child row. The stage block sits at
		// cssDepth+1, so a direct parallel child is at cssDepth+2.
		// Only the parent stage is marked, not the children themselves.
		parallel := false
		for _, r := range rows[sr.rowIdx+1 : endIdx] {
			if r.cssDepth == sr.cssDepth+2 &&
				strings.HasPrefix(r.step, "parallel") &&
				!strings.HasPrefix(r.step, "parallel block") {
				parallel = true
				break
			}
		}

		// Relative depth: how many "levels" of stage nesting.
		// Count how many prior stages with lower cssDepth have a scope
		// that contains this stage (i.e. are ancestors).
		relDepth := 0
		for _, other := range stageRows[:si] {
			if other.cssDepth < sr.cssDepth {
				ancestorEnd := stageEndIdx(rows, other.rowIdx, other.cssDepth)
				if sr.rowIdx < ancestorEnd {
					relDepth++
				}
			}
		}

		stages[si] = Stage{
			Name:     sr.name,
			Status:   status,
			Duration: sr.dur,
			NodeIDs:  nodeIDs,
			Depth:    relDepth,
			Parallel: parallel,
		}
	}
	return stages, nil
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
func isWorseStatus(a, b BuildStatus) bool {
	return statusSeverity(a) > statusSeverity(b)
}

func statusSeverity(s BuildStatus) int {
	switch s {
	case BuildStatusSuccess:
		return 0
	case BuildStatusSkipped:
		return 1
	case BuildStatusNotBuilt:
		return 2
	case BuildStatusUnknown:
		return 3
	case BuildStatusRunning:
		return 4
	case BuildStatusUnstable:
		return 5
	case BuildStatusAborted:
		return 6
	case BuildStatusFailed:
		return 7
	default:
		return 0
	}
}

func stripTags(s string) string {
	return html.UnescapeString(strings.TrimSpace(flowGraphTagRe.ReplaceAllString(s, "")))
}

func parseFlowGraphStatus(s string) BuildStatus {
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "success"):
		return BuildStatusSuccess
	case strings.Contains(lower, "failure"), strings.Contains(lower, "failed"):
		return BuildStatusFailed
	case strings.Contains(lower, "aborted"):
		return BuildStatusAborted
	case strings.Contains(lower, "unstable"):
		return BuildStatusUnstable
	case strings.Contains(lower, "not"):
		return BuildStatusNotBuilt
	case strings.Contains(lower, "progress"), strings.Contains(lower, "paused"):
		return BuildStatusRunning
	default:
		return BuildStatusUnknown
	}
}

// parseDurationText parses strings like "4 min 13 sec", "6.4 sec", "39 ms".
func parseDurationText(s string) time.Duration {
	s = strings.TrimSpace(s)
	var total time.Duration

	// Match hours
	if m := regexp.MustCompile(`(\d+)\s*hr`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		total += time.Duration(n) * time.Hour
	}
	// Match minutes
	if m := regexp.MustCompile(`(\d+)\s*min`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		total += time.Duration(n) * time.Minute
	}
	// Match seconds (may be float like "6.4 sec")
	if m := regexp.MustCompile(`([\d.]+)\s*sec`).FindStringSubmatch(s); m != nil {
		f, _ := strconv.ParseFloat(m[1], 64)
		total += time.Duration(f * float64(time.Second))
	}
	// Match milliseconds
	if m := regexp.MustCompile(`(\d+)\s*ms`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		total += time.Duration(n) * time.Millisecond
	}
	return total
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
func MarkSkipped(stages []Stage, skippedOccs map[string][]bool) {
	occCounts := map[string]int{}
	for i := range stages {
		name := stages[i].Name
		occ := occCounts[name]
		occCounts[name]++
		occs := skippedOccs[name]
		if occ < len(occs) && occs[occ] && stages[i].Status == BuildStatusSuccess {
			stages[i].Status = BuildStatusSkipped
			stages[i].NodeIDs = nil
		}
	}
}

// TriggerBuild starts a new build for the given job.
func (c *Client) TriggerBuild(ctx context.Context, jobPath string, params map[string]string) error {
	basePath := JobPathToURL(jobPath)
	if len(params) == 0 {
		return c.post(ctx, basePath+"/build", nil)
	}

	// Build with parameters
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	return c.post(ctx, basePath+"/buildWithParameters?"+form.Encode(), nil)
}

// CancelBuild stops a running build.
func (c *Client) CancelBuild(ctx context.Context, jobPath string, number int) error {
	path := fmt.Sprintf("%s/%d/stop", JobPathToURL(jobPath), number)
	return c.post(ctx, path, nil)
}

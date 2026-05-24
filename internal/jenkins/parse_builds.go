package jenkins

import (
	"strconv"
	"strings"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// Pure parsers for the Jenkins flowGraphTable HTML fragment.
// These functions are deliberately HTTP-free so they can be unit-tested
// in isolation (architecture.md §6 adapter responsibility).

// parseFlowGraphRows is the first pass: turn each <tr> into a flowGraphRow.
func parseFlowGraphRows(rawHTML string) []flowGraphRow {
	var rows []flowGraphRow
	for _, rowMatch := range flowGraphRowRe.FindAllStringSubmatch(rawHTML, -1) {
		row, ok := parseFlowGraphRow(rowMatch[1])
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// parseFlowGraphRow extracts a single row's fields. Returns ok=false when
// the row has fewer than four cells (header rows, separators, etc.).
func parseFlowGraphRow(row string) (flowGraphRow, bool) {
	cells := flowGraphCellRe.FindAllStringSubmatch(row, -1)
	if len(cells) < 4 {
		return flowGraphRow{}, false
	}
	// cells[i] = [fullMatch, tdAttrs, tdContent]
	tdAttrs := cells[0][1]
	raw0 := cells[0][2]
	stepCell := stripTags(raw0)

	return flowGraphRow{
		step:     stepCell,
		name:     strings.TrimSpace(stripTags(cells[1][2])),
		status:   strings.TrimSpace(stripTags(cells[3][2])),
		nodeID:   parseNodeID(raw0),
		hasLog:   flowGraphLogRe.MatchString(cells[2][2]),
		dur:      parseStepDuration(stepCell),
		cssDepth: parseCSSDepth(tdAttrs),
	}, true
}

func parseNodeID(raw string) int {
	m := flowGraphNodeIDRe.FindStringSubmatch(raw)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func parseStepDuration(stepCell string) time.Duration {
	m := flowGraphDurationRe.FindStringSubmatch(stepCell)
	if m == nil {
		return 0
	}
	return parseDurationText(m[1])
}

func parseCSSDepth(tdAttrs string) int {
	m := flowGraphPaddingRe.FindStringSubmatch(tdAttrs)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// stageInfo is the index of a stage row inside the parsed rows slice.
type stageInfo struct {
	rowIdx   int
	name     string
	dur      time.Duration
	cssDepth int
}

// collectStageInfos finds every "stage" row (not "stage block") and
// remembers its position so later passes can resolve child scopes.
func collectStageInfos(rows []flowGraphRow) []stageInfo {
	var out []stageInfo
	for i, r := range rows {
		if !isStageRow(r) {
			continue
		}
		out = append(out, stageInfo{
			rowIdx:   i,
			name:     r.name,
			dur:      r.dur,
			cssDepth: r.cssDepth,
		})
	}
	return out
}

func isStageRow(r flowGraphRow) bool {
	return strings.HasPrefix(r.step, "stage ") && !strings.Contains(r.step, "stage block")
}

// childRollup is the aggregate of a stage's child rows.
type childRollup struct {
	status   jmodel.BuildStatus
	nodeIDs  []int
	parallel bool
}

// rollupStageChildren walks the children of a stage and aggregates their
// status (worst wins), collects logged node IDs, and detects parallel
// execution.
func rollupStageChildren(rows []flowGraphRow, sr stageInfo) childRollup {
	endIdx := stageEndIdx(rows, sr.rowIdx, sr.cssDepth)
	r := childRollup{status: jmodel.BuildStatusSuccess}

	for _, row := range rows[sr.rowIdx:endIdx] {
		if row.hasLog && row.nodeID > 0 {
			r.nodeIDs = append(r.nodeIDs, row.nodeID)
		}
		childStatus := parseFlowGraphStatus(row.status)
		if isWorseStatus(childStatus, r.status) {
			r.status = childStatus
		}
	}

	// Parallel/matrix: a "parallel" row sits at cssDepth+2 (the stage
	// block is +1, its direct children are +2). The "parallel block"
	// per-branch row also matches HasPrefix("parallel ") so it is
	// excluded explicitly.
	for _, row := range rows[sr.rowIdx+1 : endIdx] {
		if row.cssDepth == sr.cssDepth+2 &&
			strings.HasPrefix(row.step, "parallel") &&
			!strings.HasPrefix(row.step, "parallel block") {
			r.parallel = true
			break
		}
	}
	return r
}

// stageRelativeDepth counts the number of prior stages whose scope still
// contains sr — i.e. how many ancestor stage levels nest above this stage.
func stageRelativeDepth(rows []flowGraphRow, stageRows []stageInfo, si int) int {
	sr := stageRows[si]
	depth := 0
	for _, other := range stageRows[:si] {
		if other.cssDepth >= sr.cssDepth {
			continue
		}
		if sr.rowIdx < stageEndIdx(rows, other.rowIdx, other.cssDepth) {
			depth++
		}
	}
	return depth
}

// parseFlowGraphTable extracts pipeline stages from Jenkins flowGraphTable HTML.
// It uses CSS padding-left multipliers to determine nesting depth, collects
// child node IDs per stage, propagates child failure status, and detects
// parallel execution.
func parseFlowGraphTable(rawHTML string) ([]jmodel.Stage, error) {
	rows := parseFlowGraphRows(rawHTML)
	stageRows := collectStageInfos(rows)

	stages := make([]jmodel.Stage, len(stageRows))
	for si, sr := range stageRows {
		roll := rollupStageChildren(rows, sr)
		stages[si] = jmodel.Stage{
			Name:     sr.name,
			Status:   roll.status,
			Duration: sr.dur,
			NodeIDs:  roll.nodeIDs,
			Depth:    stageRelativeDepth(rows, stageRows, si),
			Parallel: roll.parallel,
		}
	}
	return stages, nil
}

package jenkins

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// row is a tiny constructor that keeps the table-test rows readable.
func row(step, name, status string, depth, nodeID int, hasLog bool) flowGraphRow {
	return flowGraphRow{
		step: step, name: name, status: status,
		cssDepth: depth, nodeID: nodeID, hasLog: hasLog,
	}
}

func TestParseFlowGraphRow(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOK   bool
		wantStep string
		wantName string
		wantNode int
		wantDur  time.Duration
		wantDep  int
	}{
		{
			name: "stage row with all attrs",
			input: `<td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 25">stage - (20 sec in block)</a></td>` +
				`<td>Build Maven</td>` +
				`<td><a href="/node/25/log/">log</a></td>` +
				`<td>Success</td>`,
			wantOK:   true,
			wantStep: "stage - (20 sec in block)",
			wantName: "Build Maven",
			wantNode: 25,
			wantDur:  20 * time.Second,
			wantDep:  9,
		},
		{
			name: "row without node id or duration is still ok",
			input: `<td><a>parallel block</a></td>` +
				`<td></td>` +
				`<td></td>` +
				`<td>Success</td>`,
			wantOK:   true,
			wantStep: "parallel block",
			wantDur:  0,
			wantDep:  0,
		},
		{
			name:   "skip rows with < 4 cells",
			input:  `<td>just one cell</td>`,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseFlowGraphRow(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.step != tt.wantStep {
				t.Errorf("step = %q, want %q", got.step, tt.wantStep)
			}
			if got.name != tt.wantName {
				t.Errorf("name = %q, want %q", got.name, tt.wantName)
			}
			if got.nodeID != tt.wantNode {
				t.Errorf("nodeID = %d, want %d", got.nodeID, tt.wantNode)
			}
			if got.dur != tt.wantDur {
				t.Errorf("dur = %v, want %v", got.dur, tt.wantDur)
			}
			if got.cssDepth != tt.wantDep {
				t.Errorf("cssDepth = %d, want %d", got.cssDepth, tt.wantDep)
			}
		})
	}
}

func TestIsStageRow(t *testing.T) {
	tests := []struct {
		step string
		want bool
	}{
		{"stage - (1 sec in block)", true},
		{"stage block (Build) - (1 sec in block)", false},
		{"parallel - (1 sec in block)", false},
		{"sh - (1 sec in self)", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			r := flowGraphRow{step: tt.step}
			if got := isStageRow(r); got != tt.want {
				t.Errorf("isStageRow(%q) = %v, want %v", tt.step, got, tt.want)
			}
		})
	}
}

func TestRollupStageChildren_WorstStatusWins(t *testing.T) {
	// stage at depth 9, block at 10, two leaves at 11 (one failed)
	rows := []flowGraphRow{
		row("stage - (1s)", "Build", "Success", 9, 25, true),
		row("stage block (Build)", "", "Success", 10, 26, false),
		row("echo - (1ms)", "hello", "Success", 11, 32, true),
		row("sh - (1s)", "go build", "Failed", 11, 33, true),
	}
	sr := stageInfo{rowIdx: 0, name: "Build", cssDepth: 9}
	got := rollupStageChildren(rows, sr)

	if got.status != jmodel.BuildStatusFailed {
		t.Errorf("status = %v, want Failed", got.status)
	}
	// Stage row has hasLog=true and node 25, plus leaves 32, 33
	if len(got.nodeIDs) != 3 {
		t.Errorf("nodeIDs = %v, want 3 entries", got.nodeIDs)
	}
	if got.parallel {
		t.Errorf("parallel = true, want false")
	}
}

func TestRollupStageChildren_ParallelDetected(t *testing.T) {
	// parent stage at 9, block at 10, "parallel" at 11 (cssDepth+2)
	rows := []flowGraphRow{
		row("stage - (1s)", "Quality", "Success", 9, 0, false),
		row("stage block (Quality)", "", "Success", 10, 0, false),
		row("parallel - (1s)", "", "Success", 11, 0, false),
		row("parallel block (Branch: A)", "", "Success", 12, 0, false),
	}
	sr := stageInfo{rowIdx: 0, name: "Quality", cssDepth: 9}
	got := rollupStageChildren(rows, sr)
	if !got.parallel {
		t.Errorf("parallel = false, want true")
	}
}

func TestRollupStageChildren_ParallelBlockRowIgnored(t *testing.T) {
	// "parallel block (...)" must NOT trigger parallel detection — only "parallel".
	rows := []flowGraphRow{
		row("stage - (1s)", "Quality", "Success", 9, 0, false),
		row("stage block (Quality)", "", "Success", 10, 0, false),
		// no real "parallel" row, only a stray "parallel block" sibling at +2
		row("parallel block (Branch: A)", "", "Success", 11, 0, false),
	}
	sr := stageInfo{rowIdx: 0, name: "Quality", cssDepth: 9}
	got := rollupStageChildren(rows, sr)
	if got.parallel {
		t.Errorf("parallel = true, want false (parallel block alone must not count)")
	}
}

func TestStageEndIdx_BoundaryAtSiblingDepth(t *testing.T) {
	rows := []flowGraphRow{
		{cssDepth: 9},  // 0: stage A
		{cssDepth: 10}, // 1: A's block
		{cssDepth: 11}, // 2: leaf
		{cssDepth: 9},  // 3: stage B — boundary
		{cssDepth: 10}, // 4: B's block
	}
	if got := stageEndIdx(rows, 0, 9); got != 3 {
		t.Errorf("stageEndIdx = %d, want 3 (next sibling at same depth)", got)
	}
	// Run-to-end when no boundary.
	if got := stageEndIdx(rows, 3, 9); got != len(rows) {
		t.Errorf("stageEndIdx = %d, want %d", got, len(rows))
	}
}

func TestStageRelativeDepth_MatrixNesting(t *testing.T) {
	// Mimic matrix: outer stage @9, parallel branch nested stage @15, inner @17.
	rows := []flowGraphRow{
		{cssDepth: 9},  // 0: stage Build envs
		{cssDepth: 10}, // 1: block
		{cssDepth: 11}, // 2
		{cssDepth: 12}, // 3
		{cssDepth: 13}, // 4: parallel
		{cssDepth: 14}, // 5
		{cssDepth: 15}, // 6: matrix stage
		{cssDepth: 16}, // 7
		{cssDepth: 17}, // 8: render manifest
	}
	stageRows := []stageInfo{
		{rowIdx: 0, cssDepth: 9},  // Build envs
		{rowIdx: 6, cssDepth: 15}, // Matrix branch
		{rowIdx: 8, cssDepth: 17}, // Render manifest
	}
	if got := stageRelativeDepth(rows, stageRows, 0); got != 0 {
		t.Errorf("Build envs depth = %d, want 0", got)
	}
	if got := stageRelativeDepth(rows, stageRows, 1); got != 1 {
		t.Errorf("Matrix branch depth = %d, want 1", got)
	}
	if got := stageRelativeDepth(rows, stageRows, 2); got != 2 {
		t.Errorf("Render manifest depth = %d, want 2", got)
	}
}

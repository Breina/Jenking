package view

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// TestFindOwningStage_ParentChildSharedNodeIDs guards against the original
// preview-prepends-parent-logs bug: a child stage's NodeIDs are always a
// subset of its parent's (parseFlowGraphTable collects every descendant row
// into the parent's NodeIDs), so naive "first overlap" matching promotes the
// child preview to its parent and prepends the parent's pre-child rows.
// The helper must pick the child (smallest overlapping NodeIDs set).
func TestFindOwningStage_ParentChildSharedNodeIDs(t *testing.T) {
	stages := []jmodel.Stage{
		{Name: "Parent", Depth: 0, NodeIDs: []int{10, 11, 12, 20, 21, 30, 31}},
		{Name: "Child", Depth: 1, NodeIDs: []int{20, 21}},
	}
	// We were previewing "Child" — its NodeIDs are [20, 21].
	idx, ok := findOwningStage(stages, "Child", []int{20, 21})
	if !ok {
		t.Fatal("expected to find owning stage")
	}
	if stages[idx].Name != "Child" {
		t.Errorf("expected Child to win over Parent, got %q", stages[idx].Name)
	}
}

// TestFindOwningStage_PrefersNameMatch verifies that when two stages have
// overlapping NodeIDs, a matching Name wins over smaller set size.
func TestFindOwningStage_PrefersNameMatch(t *testing.T) {
	stages := []jmodel.Stage{
		{Name: "Outer", NodeIDs: []int{5}},        // smaller, overlaps, wrong name
		{Name: "Target", NodeIDs: []int{5, 6, 7}}, // larger, overlaps, right name
	}
	idx, ok := findOwningStage(stages, "Target", []int{5})
	if !ok || stages[idx].Name != "Target" {
		t.Errorf("expected name match to win, got ok=%v name=%q", ok, stages[idx].Name)
	}
}

// TestFindOwningStage_SameName verifies disambiguation by NodeID overlap when
// two stages share the same name (e.g. nested "Test" stages at different
// depths). The one whose NodeIDs overlap most tightly wins.
func TestFindOwningStage_SameName(t *testing.T) {
	stages := []jmodel.Stage{
		{Name: "Test", Depth: 0, NodeIDs: []int{10, 11, 12, 20, 21}},
		{Name: "Test", Depth: 1, NodeIDs: []int{20, 21}},
	}
	idx, ok := findOwningStage(stages, "Test", []int{20, 21})
	if !ok {
		t.Fatal("expected to find owning stage")
	}
	if stages[idx].Depth != 1 {
		t.Errorf("expected nested Test (depth=1) to win, got depth=%d", stages[idx].Depth)
	}
}

// TestFindOwningStage_FallbackToOverlap verifies that when no candidate has
// a matching name (e.g. stage was renamed between refreshes), we still match
// by overlap — preferring the smallest set.
func TestFindOwningStage_FallbackToOverlap(t *testing.T) {
	stages := []jmodel.Stage{
		{Name: "A", NodeIDs: []int{1, 2, 3, 4}},
		{Name: "B", NodeIDs: []int{3, 4}},
	}
	idx, ok := findOwningStage(stages, "no-match", []int{3, 4})
	if !ok {
		t.Fatal("expected fallback to overlap-based match")
	}
	if stages[idx].Name != "B" {
		t.Errorf("expected smallest overlapping set to win (B), got %q", stages[idx].Name)
	}
}

// TestFindOwningStage_NoOverlap verifies ok=false when the old NodeIDs have
// no intersection with any stage (e.g. the previewed stage disappeared).
func TestFindOwningStage_NoOverlap(t *testing.T) {
	stages := []jmodel.Stage{
		{Name: "A", NodeIDs: []int{1, 2}},
		{Name: "B", NodeIDs: []int{3, 4}},
	}
	if _, ok := findOwningStage(stages, "A", []int{99}); ok {
		t.Error("expected ok=false when no stage overlaps with oldNodeIDs")
	}
}

// TestFindOwningStage_Empty guards trivial degenerate inputs.
func TestFindOwningStage_Empty(t *testing.T) {
	if _, ok := findOwningStage(nil, "x", []int{1}); ok {
		t.Error("expected ok=false for empty stages")
	}
	if _, ok := findOwningStage([]jmodel.Stage{{Name: "x", NodeIDs: []int{1}}}, "x", nil); ok {
		t.Error("expected ok=false for empty oldNodeIDs")
	}
}

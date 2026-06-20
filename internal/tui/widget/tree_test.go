package widget

import (
	"testing"

	"github.com/Breina/Jenking/internal/tui/theme"
)

func sampleRoot(deep bool) TreeNode {
	lastBuildKids := []TreeNode{
		{Key: "_class", Value: "WorkflowRun"},
		{Key: "number", Value: "1"},
	}
	if deep {
		// A deeper refetch reveals a container child under lastBuild.
		lastBuildKids = append(lastBuildKids, TreeNode{
			Key: "actions", Container: true,
			Children: []TreeNode{{Key: "[0]", Container: true, Children: []TreeNode{{Key: "url", Value: "u"}}}},
		})
	}
	return TreeNode{Container: true, Children: []TreeNode{
		{Key: "name", Value: "proj"},
		{Key: "lastBuild", Container: true, Children: lastBuildKids},
		{Key: "actions", Container: true, Children: []TreeNode{
			{Key: "[0]", Container: true, Children: []TreeNode{{Key: "objectUrl", Value: "https://x"}}},
		}},
	}}
}

func newTestTree(root TreeNode) *Tree {
	tr := NewTree(theme.Theme{})
	tr.SetSize(80, 20)
	tr.SetRoot(root)
	return &tr
}

func TestTreeExpandSignalsTruncation(t *testing.T) {
	tr := newTestTree(sampleRoot(false))
	if got := tr.TotalRows(); got != 3 {
		t.Fatalf("top-level rows = %d, want 3", got)
	}

	// lastBuild (leaf-only children) ⇒ expanded, no container child → caller fetches deeper.
	tr.MoveDown() // cursor → lastBuild
	expanded, hasContainer := tr.Expand()
	if !expanded || hasContainer {
		t.Fatalf("lastBuild expand = (%v,%v), want (true,false)", expanded, hasContainer)
	}
	if got := tr.TotalRows(); got != 5 { // name, lastBuild, _class, number, actions
		t.Fatalf("rows after expand = %d, want 5", got)
	}

	// actions has a container child ⇒ no deeper fetch needed.
	tr.End() // cursor → actions
	expanded, hasContainer = tr.Expand()
	if !expanded || !hasContainer {
		t.Fatalf("actions expand = (%v,%v), want (true,true)", expanded, hasContainer)
	}
}

func TestTreeSetRootPreservesExpansion(t *testing.T) {
	tr := newTestTree(sampleRoot(false))
	tr.MoveDown() // lastBuild
	tr.Expand()
	rowsBefore := tr.TotalRows()

	// Simulate a deeper refetch: lastBuild stays expanded and now has a container child.
	tr.SetRoot(sampleRoot(true))
	if tr.TotalRows() <= rowsBefore {
		t.Fatalf("deeper SetRoot did not keep lastBuild expanded (rows %d ≤ %d)", tr.TotalRows(), rowsBefore)
	}
	// Cursor should still sit on lastBuild.
	if v, ok := tr.SelectedValue(); ok {
		t.Fatalf("cursor on a leaf after refetch (%q); expected container lastBuild", v)
	}
}

func TestTreeSearchFiltersToMatchesAndAncestors(t *testing.T) {
	tr := newTestTree(sampleRoot(false))
	tr.ApplySearch(CompileSearchRegex("objectUrl"))
	// Should show actions → [0] → objectUrl only (3 rows).
	if got := tr.TotalRows(); got != 3 {
		t.Fatalf("filtered rows = %d, want 3 (actions/[0]/objectUrl)", got)
	}
	tr.ApplySearch(nil)
	if got := tr.TotalRows(); got != 3 {
		t.Fatalf("rows after clearing search = %d, want 3 top-level", got)
	}
}

package widget

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// TestRecompute_WrappedBlockKeepsScrollPosition regresses a snap-to-top bug:
// a single raw line that soft-wraps taller than the viewport must keep its
// scroll position across a rebuild (e.g. a stagelog live SetRawLines poll).
// Before the fix the anchor only remembered the raw-line index, so the rebuild
// landed on the line's first wrap chunk instead of the chunk the user was on.
func TestRecompute_WrappedBlockKeepsScrollPosition(t *testing.T) {
	lv := NewLogViewer(theme.Theme{})
	lv.wrap = true
	lv.SetSize(10, 5)

	// 100 cols / 10 width => 10 display rows for one raw line; maxOffset = 5.
	long := strings.Repeat("x", 100)
	lv.SetRawLines([]string{long})

	// Starts pinned to bottom (offset 5); scroll up into the middle of the block.
	lv.ScrollByLines(-2)
	if got := lv.ScrollInfo().Offset; got != 3 {
		t.Fatalf("setup offset = %d, want 3", got)
	}

	// Simulate a live poll re-setting the identical buffer.
	lv.SetRawLines([]string{long})
	if got := lv.ScrollInfo().Offset; got != 3 {
		t.Errorf("offset after recompute = %d, want 3 (snapped to block top)", got)
	}
}

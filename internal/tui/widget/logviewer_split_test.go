package widget

import "testing"

// TestSplitLogLines_InputStepHiddenBlocks regresses the Jenkins pipeline
// `input` step log line:
//
//	\x1b[8m<ha:////base64==>\x1b[0mProceed or \x1b[8m<ha:////base64==>\x1b[0mAbort
//
// Without ansiHiddenBlockRe the generic ANSI strip exposes the base64 blob
// inline; xstreamRe then matches the alphanumeric "Proceed"/"Abort" suffix
// as part of the same base64 token and consumes both words.
func TestSplitLogLines_InputStepHiddenBlocks(t *testing.T) {
	raw := "\x1b[8mha:////4C5pBnlEYhbkAAA==\x1b[0mProceed or \x1b[8mha:////4O2wVEtrTD2FfQA==\x1b[0mAbort"
	lines := SplitLogLines(raw + "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1: %#v", len(lines), lines)
	}
	if lines[0] != "Proceed or Abort" {
		t.Errorf("line = %q, want %q", lines[0], "Proceed or Abort")
	}
}

func TestSplitLogLines_PlainXStreamLineStripped(t *testing.T) {
	lines := SplitLogLines("ha:////4C5pBnlEYhbkAAA==\n")
	if len(lines) != 0 {
		t.Errorf("xstream-only line should be dropped, got %#v", lines)
	}
}

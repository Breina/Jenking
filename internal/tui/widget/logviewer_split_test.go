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

// TestSplitLogLines_EmbeddedCarriageReturns regresses apt/dpkg/Docker build
// progress lines that overwrite themselves in place with bare '\r'. The final
// visible text is what the terminal would show after all overwrites.
func TestSplitLogLines_EmbeddedCarriageReturns(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "later redraw is longer",
			raw:  "(Reading database ... 30%\r(Reading database ... 65%\r(Reading database ... 100%",
			want: "(Reading database ... 100%",
		},
		{
			name: "partial overwrite leaves tail",
			raw:  "Progress: 100%\rDone",
			want: "Doneress: 100%",
		},
		{
			name: "trailing carriage return dropped",
			raw:  "building...\r",
			want: "building...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := SplitLogLines(tt.raw + "\n")
			if len(lines) != 1 {
				t.Fatalf("lines = %d, want 1: %#v", len(lines), lines)
			}
			if lines[0] != tt.want {
				t.Errorf("line = %q, want %q", lines[0], tt.want)
			}
		})
	}
}

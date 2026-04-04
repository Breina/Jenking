package theme

import (
	"strconv"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// transformColorHex is a helper that calls TransformColor and returns the hex string.
func transformColorHex(s string, t ColorblindnessType) string {
	return string(TransformColor(lipgloss.Color(s), t))
}

// TestTransformColorNone verifies that ColorblindnessNone is a no-op.
func TestTransformColorNone(t *testing.T) {
	cases := []string{"71", "167", "#00FF00", "#FF0000"}
	for _, c := range cases {
		got := transformColorHex(c, ColorblindnessNone)
		if got != c {
			t.Errorf("None: TransformColor(%q) = %q, want %q", c, got, c)
		}
	}
}

// TestTransformColorPassthrough verifies that unrecognised formats are returned as-is.
func TestTransformColorPassthrough(t *testing.T) {
	cases := []string{"", "red", "auto", "#GGGGGG", "256", "-1"}
	for _, c := range cases {
		got := transformColorHex(c, ColorblindnessDeuteranopia)
		if got != c {
			t.Errorf("passthrough: TransformColor(%q) = %q, want %q", c, got, c)
		}
	}
}

// TestTransformColorDeuteranopia_Green spot-checks xterm green "71" under deuteranopia.
// xterm 71 = (1,3,1) in the 6×6×6 cube → sRGB(95,175,95).
// After daltonization the blue channel should increase noticeably.
func TestTransformColorDeuteranopia_Green(t *testing.T) {
	got := transformColorHex("71", ColorblindnessDeuteranopia)
	if got == "71" {
		t.Error("Deuteranopia: green '71' should be transformed but was returned unchanged")
	}
	idx, err := strconv.Atoi(got)
	if err != nil || idx < 0 || idx > 255 {
		t.Errorf("Deuteranopia: expected xterm index 0-255, got %q", got)
	}
}

// TestTransformColorDeuteranopia_Red spot-checks xterm red "167" under deuteranopia.
// xterm 167 = (4,1,1) in the 6×6×6 cube → sRGB(215,95,95).
func TestTransformColorDeuteranopia_Red(t *testing.T) {
	got := transformColorHex("167", ColorblindnessDeuteranopia)
	if got == "167" {
		t.Error("Deuteranopia: red '167' should be transformed but was returned unchanged")
	}
	idx, err := strconv.Atoi(got)
	if err != nil || idx < 0 || idx > 255 {
		t.Errorf("Deuteranopia: expected xterm index 0-255, got %q", got)
	}
}

// TestTransformColorProtanopia spot-checks "71" and "167".
func TestTransformColorProtanopia(t *testing.T) {
	for _, c := range []string{"71", "167"} {
		got := transformColorHex(c, ColorblindnessProtanopia)
		if got == c {
			t.Errorf("Protanopia: %q should be transformed", c)
		}
		idx, err := strconv.Atoi(got)
		if err != nil || idx < 0 || idx > 255 {
			t.Errorf("Protanopia: expected xterm index 0-255 for %q, got %q", c, got)
		}
	}
}

// TestTransformColorTritanopia spot-checks "71" and "167".
// "71" (green) is strongly shifted; "167" (red) may round to the same index after quantization.
func TestTransformColorTritanopia(t *testing.T) {
	got71 := transformColorHex("71", ColorblindnessTritanopia)
	if got71 == "71" {
		t.Error("Tritanopia: green '71' should be transformed")
	}
	for _, c := range []string{"71", "167"} {
		got := transformColorHex(c, ColorblindnessTritanopia)
		idx, err := strconv.Atoi(got)
		if err != nil || idx < 0 || idx > 255 {
			t.Errorf("Tritanopia: expected xterm index 0-255 for %q, got %q", c, got)
		}
	}
}

// TestTransformColorAchromatopsia verifies that saturated colors become grey.
// After achromatopsia the nearest xterm index must map to equal R/G/B channels.
func TestTransformColorAchromatopsia(t *testing.T) {
	colors := []string{"71", "167", "#FF0000", "#00FF00", "#0000FF"}
	for _, c := range colors {
		got := transformColorHex(c, ColorblindnessAchromatopsia)
		idx, err := strconv.Atoi(got)
		if err != nil || idx < 0 || idx > 255 {
			t.Fatalf("Achromatopsia: expected xterm index 0-255 for %q, got %q", c, got)
		}
		r, g, b := xtermToSRGB(idx)
		if r != g || g != b {
			t.Errorf("Achromatopsia: %q → index %d (sRGB %d,%d,%d), channels should be equal",
				c, idx, r, g, b)
		}
	}
}

// TestTransformColorHex verifies hex input is handled correctly.
func TestTransformColorHex(t *testing.T) {
	got := transformColorHex("#00FF00", ColorblindnessDeuteranopia)
	idx, err := strconv.Atoi(got)
	if err != nil || idx < 0 || idx > 255 {
		t.Errorf("hex input: expected xterm index 0-255, got %q", got)
	}
}

// TestApplyColorblindFilterNone verifies the filter is a no-op for None.
func TestApplyColorblindFilterNone(t *testing.T) {
	base := Default()
	filtered := ApplyColorblindFilter(base, ColorblindnessNone)
	// spot-check: Success style foreground should be unchanged
	baseFg := extractColor(base.BuildStatus.Success.GetForeground())
	filteredFg := extractColor(filtered.BuildStatus.Success.GetForeground())
	if baseFg != filteredFg {
		t.Errorf("None filter changed Success foreground: %q → %q", baseFg, filteredFg)
	}
}

// TestApplyColorblindFilterChangesColors verifies that non-None filters actually
// change at least one color in the theme.
func TestApplyColorblindFilterChangesColors(t *testing.T) {
	base := Default()
	types := []ColorblindnessType{
		ColorblindnessDeuteranopia,
		ColorblindnessProtanopia,
		ColorblindnessTritanopia,
		ColorblindnessAchromatopsia,
	}
	for _, ct := range types {
		filtered := ApplyColorblindFilter(base, ct)
		baseFg := extractColor(base.BuildStatus.Success.GetForeground())
		filteredFg := extractColor(filtered.BuildStatus.Success.GetForeground())
		if baseFg == filteredFg {
			t.Errorf("%s filter did not change Success foreground (%q)", ct, baseFg)
		}
	}
}

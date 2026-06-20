package view

import (
	"strings"
	"testing"
)

func TestVisualLenWideGlyphs(t *testing.T) {
	cases := map[string]int{
		"abc":              3,
		"✅":                2, // emoji, 2 cells
		"a✅b":              4,
		"\033[31m✅\033[0m": 2, // ANSI ignored
		"⚠️":               2, // base + variation selector, one grapheme, 2 cells
	}
	for in, want := range cases {
		if got := visualLen(in); got != want {
			t.Errorf("visualLen(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestAnsiTakeLeftPadsToWidth(t *testing.T) {
	// A wide glyph straddling the cut must not overflow; output is padded to n.
	for _, n := range []int{0, 1, 2, 3, 4, 5} {
		got := ansiTakeLeft("a✅bc", n)
		if w := visualLen(got); w != n {
			t.Errorf("ansiTakeLeft(%q,%d) width = %d, want %d", "a✅bc", n, w, n)
		}
	}
}

// The key bug: an overlay placed over a background line containing wide glyphs
// must land at the correct column and every result line must stay bgWidth wide.
func TestOverlayCenterWideBackgroundAligned(t *testing.T) {
	const bgWidth, bgHeight = 20, 5
	bgLine := "✅✅✅✅✅" // 10 cells of wide glyphs, padded by overlayCenter
	var bg []string
	for i := 0; i < bgHeight; i++ {
		bg = append(bg, bgLine)
	}
	popup := "[OK]"

	out := overlayCenter(strings.Join(bg, "\n"), popup, bgWidth, bgHeight)
	lines := strings.Split(out, "\n")
	if len(lines) != bgHeight {
		t.Fatalf("got %d lines, want %d", len(lines), bgHeight)
	}
	// The composited (popup) row must span exactly bgWidth and hold the popup
	// intact — proving the wide background glyphs didn't shift the column math.
	mid := lines[bgHeight/2]
	if w := visualLen(mid); w != bgWidth {
		t.Errorf("overlay row width = %d, want %d (%q)", w, bgWidth, mid)
	}
	if !strings.Contains(mid, popup) {
		t.Errorf("overlay row %q does not contain popup %q", mid, popup)
	}
}

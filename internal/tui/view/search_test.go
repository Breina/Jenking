package view

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestCompileSearchRegex(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantNil bool
	}{
		{"empty pattern", "", true},
		{"valid pattern", "foo", false},
		{"case insensitive", "FOO", false},
		{"regex pattern", "foo.*bar", false},
		{"invalid regex falls back to literal", "[unclosed", false},
		{"single char", "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := compileSearchRegex(tt.pattern)
			if (re == nil) != tt.wantNil {
				t.Errorf("compileSearchRegex(%q) nil=%v, want nil=%v", tt.pattern, re == nil, tt.wantNil)
			}
		})
	}
}

func TestCompileSearchRegex_CaseInsensitive(t *testing.T) {
	re := compileSearchRegex("hello")
	if re == nil {
		t.Fatal("expected non-nil regex")
	}
	if !re.MatchString("Hello World") {
		t.Error("expected case-insensitive match")
	}
	if !re.MatchString("HELLO") {
		t.Error("expected case-insensitive match for uppercase")
	}
}

func TestHighlightMatches(t *testing.T) {
	match := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	re := compileSearchRegex("world")
	if re == nil {
		t.Fatal("expected non-nil regex")
	}

	result := highlightMatches("hello world!", re, match, normal)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Should contain both match and normal styled portions
	matchRendered := match.Render("world")
	normalRendered := normal.Render("hello ")
	if !contains(result, matchRendered) {
		t.Errorf("expected result to contain match-styled 'world'")
	}
	if !contains(result, normalRendered) {
		t.Errorf("expected result to contain normal-styled 'hello '")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexString(s, substr) >= 0)
}

func indexString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestHighlightMatches_NoMatch(t *testing.T) {
	match := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	re := compileSearchRegex("xyz")
	result := highlightMatches("hello world", re, match, normal)
	expected := normal.Render("hello world")
	if result != expected {
		t.Errorf("expected normal render for no match, got %q", result)
	}
}

func TestHighlightMatches_NilRegex(t *testing.T) {
	match := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	result := highlightMatches("hello world", nil, match, normal)
	expected := normal.Render("hello world")
	if result != expected {
		t.Errorf("expected normal render for nil regex, got %q", result)
	}
}

func TestHighlightMatches_MultipleMatches(t *testing.T) {
	match := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	re := compileSearchRegex("o")
	result := highlightMatches("foo bar boo", re, match, normal)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Should contain match-styled "o" portions
	matchRendered := match.Render("o")
	if !contains(result, matchRendered) {
		t.Error("expected styled match portions in output")
	}
}

package component

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

func TestProgressBar_ExactWidth(t *testing.T) {
	th := theme.Default()
	pb := NewProgressBar(th)

	tests := []struct {
		name      string
		width     int
		elapsed   time.Duration
		estimated time.Duration
	}{
		{"0%", 20, 0, 10 * time.Minute},
		{"50%", 20, 5 * time.Minute, 10 * time.Minute},
		{"100%", 20, 10 * time.Minute, 10 * time.Minute},
		{"150% overrun", 20, 15 * time.Minute, 10 * time.Minute},
		{"narrow", 5, 3 * time.Minute, 10 * time.Minute},
		{"wide", 60, 3 * time.Minute, 10 * time.Minute},
		{"zero estimate", 20, 5 * time.Minute, 0},
		{"width 1", 1, 5 * time.Minute, 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pb.Render(tt.width, tt.elapsed, tt.estimated)
			got := lipgloss.Width(result)
			if got != tt.width {
				t.Errorf("Render width = %d, want %d", got, tt.width)
			}
		})
	}
}

func TestProgressBar_RenderWithText_ExactWidth(t *testing.T) {
	th := theme.Default()
	pb := NewProgressBar(th)

	tests := []struct {
		name      string
		width     int
		elapsed   time.Duration
		estimated time.Duration
	}{
		{"50%", 20, 5 * time.Minute, 10 * time.Minute},
		{"overrun", 20, 15 * time.Minute, 10 * time.Minute},
		{"narrow", 8, 3 * time.Minute, 10 * time.Minute},
		{"wide", 40, 3 * time.Minute, 10 * time.Minute},
		{"zero estimate", 14, 5 * time.Minute, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pb.RenderWithText(tt.width, tt.elapsed, tt.estimated)
			got := lipgloss.Width(result)
			if got != tt.width {
				t.Errorf("RenderWithText width = %d, want %d", got, tt.width)
			}
		})
	}
}

func TestProgressBar_ZeroWidth(t *testing.T) {
	pb := NewProgressBar(theme.Default())
	if result := pb.Render(0, time.Minute, 2*time.Minute); result != "" {
		t.Errorf("expected empty string for width 0, got %q", result)
	}
}

func TestFormatBarDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{67 * time.Second, "1m07s"},
		{3600*time.Second + 240*time.Second, "1h04m"},
		{0, "0s"},
	}
	for _, tt := range tests {
		got := formatBarDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatBarDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

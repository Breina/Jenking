package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// InputMode represents the current input mode.
type InputMode int

const (
	ModeNormal InputMode = iota
	ModeCommand
	ModeSearch
)

// StatusBar manages command/search input and error display.
type StatusBar struct {
	theme      theme.Theme
	mode       InputMode
	input      string
	suggestion string
	error      string
	width      int
}

// NewStatusBar creates a new status bar.
func NewStatusBar(t theme.Theme) StatusBar {
	return StatusBar{
		theme: t,
		width: 80,
	}
}

// SetTheme updates the theme used for rendering.
func (s *StatusBar) SetTheme(t theme.Theme) {
	s.theme = t
}

// SetWidth updates the status bar width.
func (s *StatusBar) SetWidth(w int) {
	s.width = w
}

// Mode returns the current input mode.
func (s *StatusBar) Mode() InputMode {
	return s.mode
}

// SetMode updates the input mode.
func (s *StatusBar) SetMode(mode InputMode) {
	s.mode = mode
	if mode == ModeNormal {
		s.input = ""
	}
}

// SetInput updates the command/search input text.
func (s *StatusBar) SetInput(input string) {
	s.input = input
}

// SetSuggestion updates the inline autocomplete suggestion.
func (s *StatusBar) SetSuggestion(suggestion string) {
	s.suggestion = suggestion
}

// HasError returns whether an error is currently displayed.
func (s *StatusBar) HasError() bool {
	return s.error != ""
}

// SetError sets an error message to display.
func (s *StatusBar) SetError(err string) {
	s.error = err
}

// ClearError clears the error message.
func (s *StatusBar) ClearError() {
	s.error = ""
}

// CommandView renders the command input panel (shown at top when active).
func (s StatusBar) CommandView() string {
	if s.mode == ModeNormal && s.error == "" {
		return ""
	}

	var content string
	switch {
	case s.error != "":
		content = s.theme.StatusBar.Error.Render(" " + s.error + " (press any key to dismiss)")
	case s.mode == ModeCommand:
		ghost := ""
		if s.suggestion != "" && strings.HasPrefix(s.suggestion, s.input) {
			ghost = s.theme.StatusBar.Suggestion.Render(s.suggestion[len(s.input):])
		}
		content = s.theme.StatusBar.Command.Render(fmt.Sprintf(" :%s", s.input)) + ghost
	case s.mode == ModeSearch:
		content = s.theme.StatusBar.Command.Render(fmt.Sprintf(" /%s", s.input))
	}

	return lipgloss.NewStyle().
		Border(s.theme.Border).
		BorderForeground(s.theme.PanelBorder.GetForeground().(lipgloss.Color)).
		Width(s.width - 2).
		Render(content)
}

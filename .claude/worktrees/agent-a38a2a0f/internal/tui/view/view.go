package view

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/command"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

// View is implemented by each screen (dashboard, job, build, etc.)
type View interface {
	tea.Model

	// Title returns the breadcrumb segment for this view.
	Title() string

	// ItemCount returns the number of items in the current view.
	ItemCount() int

	// Commands returns commands available in this view's context.
	Commands() []command.Command

	// Shortcuts returns context-sensitive key binding hints for the header.
	// Return nil if the view has no view-specific shortcuts.
	Shortcuts() []component.Shortcut

	// SetSize updates the view dimensions.
	SetSize(width, height int)

	// Close is called when the view is popped from the navigation stack.
	// Views with background work (streaming, polling) should cancel it here.
	Close() error
}

// Searchable is optionally implemented by views that support regex filtering.
type Searchable interface {
	ApplySearch(pattern string) error
	SearchQuery() string
}

// PopupLayer is optionally implemented by views that can show overlaid popups
// (confirm dialogs, param forms, etc.). The app checks this before routing ESC
// to the navigation stack so that ESC always closes the topmost popup first.
type PopupLayer interface {
	HasPopup() bool
}

// BreadcrumbSegment describes a single k9s-style breadcrumb: viewType(context)[count].
type BreadcrumbSegment struct {
	ViewType string                  // "jobs", "builds", "stages", "log", "running", "preview"
	Context  []component.BreadcrumbPart // parts joined by "/" (or ":" for the last separator)
}

// BreadcrumbProvider is optionally implemented by views that supply k9s-style breadcrumbs.
type BreadcrumbProvider interface {
	Breadcrumb() BreadcrumbSegment
}

// shortName returns the part after the last "/" in a decoded name,
// stripping folder prefixes. If there are no slashes, returns the name as-is.
func shortName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// jobRefParts builds breadcrumb context parts from a project name and optional branch.
// projectName is always included; branchName is added as a separate part if non-empty.
func jobRefParts(projectName, branchName string) []component.BreadcrumbPart {
	parts := []component.BreadcrumbPart{{Text: shortName(decodeName(projectName))}}
	if branchName != "" {
		parts = append(parts, component.BreadcrumbPart{Text: decodeName(branchName)})
	}
	return parts
}

// statusIcon returns a single icon for a build status.
func statusIcon(s jenkins.BuildStatus) string {
	switch s {
	case jenkins.BuildStatusRunning:
		return "●"
	case jenkins.BuildStatusSuccess:
		return "✔"
	case jenkins.BuildStatusFailed:
		return "✖"
	case jenkins.BuildStatusAborted:
		return "◼"
	case jenkins.BuildStatusUnstable:
		return "⚠"
	case jenkins.BuildStatusSkipped:
		return "◇"
	case jenkins.BuildStatusNotBuilt:
		return "◻"
	default:
		return "○"
	}
}

// statusLabel returns a human-readable, capitalized label for a build status.
func statusLabel(s jenkins.BuildStatus) string {
	switch s {
	case jenkins.BuildStatusSkipped:
		return "Skipped"
	case jenkins.BuildStatusNotBuilt:
		return "Not Built"
	default:
		str := string(s)
		if len(str) == 0 {
			return str
		}
		return strings.ToUpper(str[:1]) + str[1:]
	}
}

// renderStatus returns a pre-colored status string for display.
func renderStatus(t theme.Theme, s jenkins.BuildStatus) string {
	text := statusIcon(s) + " " + statusLabel(s)
	switch s {
	case jenkins.BuildStatusRunning:
		return t.BuildStatus.Running.Render(text)
	case jenkins.BuildStatusSuccess:
		return t.BuildStatus.Success.Render(text)
	case jenkins.BuildStatusFailed:
		return t.BuildStatus.Failed.Render(text)
	case jenkins.BuildStatusAborted:
		return t.BuildStatus.Aborted.Render(text)
	case jenkins.BuildStatusUnstable:
		return t.BuildStatus.Unstable.Render(text)
	case jenkins.BuildStatusSkipped:
		return t.BuildStatus.Aborted.Render(text)
	case jenkins.BuildStatusNotBuilt:
		return t.BuildStatus.Aborted.Render(text)
	default:
		return text
	}
}

// renderRunningStatus renders a progress bar when an estimate is available,
// falling back to the plain Running status badge when it is not.
func renderRunningStatus(t theme.Theme, pb component.ProgressBar, width int, elapsed, estimated time.Duration) string {
	if estimated > 0 {
		return pb.DualRenderWithText(width, elapsed, estimated)
	}
	return renderStatus(t, jenkins.BuildStatusRunning)
}

// PreviewProvider is optionally implemented by views that want a separate
// bordered panel below their main content to show a live preview.
type PreviewProvider interface {
	// PreviewView renders the preview panel content.
	PreviewView() string
	// SetPreviewSize sets the preview panel's inner dimensions.
	SetPreviewSize(width, height int)
	// PreviewBreadcrumb returns the breadcrumb segment for the preview panel.
	PreviewBreadcrumb() BreadcrumbSegment
	// PreviewItemCount returns the number of items in the preview panel.
	PreviewItemCount() int
}

package view

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// ContextLevel describes where in the job hierarchy a view is anchored.
type ContextLevel int

const (
	CtxRoot    ContextLevel = iota // "*" — global scope
	CtxFolder                      // a folder job
	CtxProject                     // a multibranch project (or standalone job)
	CtxBranch                      // a branch/MR within a multibranch project
	CtxBuild                       // a specific build
	CtxStage                       // a specific stage
)

// NavBuildRef is a navigation cursor for a build: either a concrete number or
// the "#last" moving reference. Distinct from jenkins.BuildRef (API ref).
type NavBuildRef struct {
	Number int  // 0 = unset
	IsLast bool // true = "#last" moving reference (Number should be 0)
}

// NavigationContext is the unified navigation state passed through view constructors.
// It replaces the ad-hoc (jobPath, jobName, branchName) tuple.
type NavigationContext struct {
	Level        ContextLevel
	FolderPath   string // slash-separated folder prefix, e.g. "Code Private"
	ProjectName  string // multibranch project or standalone job name
	BranchName   string // branch/MR name (URL-encoded as Jenkins stores it)
	Build        NavBuildRef
	StageName    string
	Username     string   // authenticated user (for mine filter); set at root
	GitUsernames []string // additional names to match in trigger descriptions (e.g. GitLab push)
	FriendlyName string   // display name for the user (e.g. "Brecht Derwael")
}

// AtBuild returns a CtxBuild NC for the given build number, inheriting all
// location fields and Username from the receiver.
func (nc NavigationContext) AtBuild(number int) NavigationContext {
	nc.Level = CtxBuild
	nc.Build = NavBuildRef{Number: number}
	nc.StageName = ""
	return nc
}

// AtStage returns a CtxStage NC for the given stage name, inheriting all
// location fields (including build) and Username from the receiver.
func (nc NavigationContext) AtStage(stageName string) NavigationContext {
	nc.Level = CtxStage
	nc.StageName = stageName
	return nc
}

// AtBranch returns a CtxBranch NC for the given branch name, inheriting
// folder/project and Username from the receiver.
func (nc NavigationContext) AtBranch(branchName string) NavigationContext {
	nc.Level = CtxBranch
	nc.BranchName = branchName
	nc.Build = NavBuildRef{}
	nc.StageName = ""
	return nc
}

// NavigationContextProvider is implemented by views that carry a NavigationContext.
// Used by app.go to determine the current scope for cross-cutting commands.
type NavigationContextProvider interface {
	NC() NavigationContext
}

// AtScope strips build- and stage-level detail from the NavigationContext,
// returning the highest scope level implied by the populated fields.
// CtxBuild/CtxStage → CtxBranch (if branch set), else CtxProject, etc.
func (nc NavigationContext) AtScope() NavigationContext {
	nc.Build = NavBuildRef{}
	nc.StageName = ""
	if nc.BranchName != "" {
		nc.Level = CtxBranch
		return nc
	}
	nc.BranchName = ""
	if nc.ProjectName != "" {
		nc.Level = CtxProject
		return nc
	}
	nc.ProjectName = ""
	if nc.FolderPath != "" {
		nc.Level = CtxFolder
		return nc
	}
	nc.FolderPath = ""
	nc.Level = CtxRoot
	return nc
}

// matchesUser reports whether a build was triggered by the given user.
// It checks the Jenkins userId first, then falls back to substring-matching
// the cause description against any configured git usernames (e.g.
// "Brecht Derwael" matches "Started by GitLab push by Brecht Derwael").
func matchesUser(b jenkins.Build, username string, gitUsernames []string) bool {
	if b.TriggeredBy == username {
		return true
	}
	for _, name := range gitUsernames {
		if name != "" && strings.Contains(b.Cause, name) {
			return true
		}
	}
	return false
}

// JobPath reconstructs the slash-joined API path from the context fields.
func (nc NavigationContext) JobPath() string {
	var parts []string
	if nc.FolderPath != "" {
		parts = append(parts, nc.FolderPath)
	}
	if nc.ProjectName != "" {
		parts = append(parts, nc.ProjectName)
	}
	if nc.BranchName != "" {
		parts = append(parts, nc.BranchName)
	}
	return strings.Join(parts, "/")
}

// Filters holds the active filter state for views that support mine/running filters.
type Filters struct {
	Running bool
	Mine    bool
}

// Filterable is optionally implemented by views that support mine/running filters.
type Filterable interface {
	ActiveFilters() Filters
	ToggleMine()
	ToggleRunning()
}

// BreadcrumbFor builds a BreadcrumbSegment from a view type name and a NavigationContext.
// This is the single source of truth for breadcrumb construction.
func BreadcrumbFor(viewType string, nc NavigationContext) BreadcrumbSegment {
	return BreadcrumbSegment{ViewType: viewType, Context: contextParts(nc)}
}

// contextParts builds the []component.BreadcrumbPart chain from the NavigationContext.
// It includes: * (global scope) → project → branch → build → stage, based on what fields are set.
func contextParts(nc NavigationContext) []component.BreadcrumbPart {
	var parts []component.BreadcrumbPart
	switch {
	case nc.Level == CtxFolder:
		// Folder-scoped view: show the folder's short name.
		parts = append(parts, component.BreadcrumbPart{Text: shortName(decodeName(nc.FolderPath))})
	case nc.ProjectName == "" && nc.BranchName == "":
		parts = append(parts, component.BreadcrumbPart{Text: "*"})
	}
	if nc.ProjectName != "" {
		parts = append(parts, component.BreadcrumbPart{Text: shortName(decodeName(nc.ProjectName))})
	}
	if nc.BranchName != "" {
		parts = append(parts, component.BreadcrumbPart{
			Text:      decodeName(nc.BranchName),
			Separator: branchIcon(nc.BranchName),
		})
	}
	if nc.Build.IsLast {
		parts = append(parts, component.BreadcrumbPart{Text: "last", IsBuildNum: true, Separator: " "})
	} else if nc.Build.Number > 0 {
		parts = append(parts, component.BreadcrumbPart{Text: fmt.Sprintf("%d", nc.Build.Number), IsBuildNum: true, Separator: " "})
	}
	if nc.StageName != "" {
		parts = append(parts, component.BreadcrumbPart{Text: nc.StageName, Separator: ":"})
	}
	return parts
}

// ncFromJobPath reconstructs a NavigationContext from a raw Jenkins job path.
// The last two segments are treated as project+branch (or project only if just one).
// Used by views that receive a raw path string (e.g. RunningBuildsView).
func ncFromJobPath(jobPath string) NavigationContext {
	parts := strings.Split(jobPath, "/")
	switch len(parts) {
	case 0:
		return NavigationContext{Level: CtxRoot}
	case 1:
		return NavigationContext{Level: CtxProject, ProjectName: parts[0]}
	case 2:
		return NavigationContext{Level: CtxBranch, ProjectName: parts[0], BranchName: parts[1]}
	default:
		return NavigationContext{
			Level:       CtxBranch,
			FolderPath:  strings.Join(parts[:len(parts)-2], "/"),
			ProjectName: parts[len(parts)-2],
			BranchName:  parts[len(parts)-1],
		}
	}
}

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
// PopupView returns the rendered popup box (unpositioned); the app overlays it
// at full terminal dimensions so it is never clipped by panel borders.
type PopupLayer interface {
	HasPopup() bool
	PopupView() string
}

// BreadcrumbSegment describes a single k9s-style breadcrumb: viewType(context)[count].
type BreadcrumbSegment struct {
	ViewType      string                     // "jobs", "builds", "stages", "log", "running", "preview"
	Context       []component.BreadcrumbPart // parts joined by "/" (or ":" for the last separator)
	Running       bool                       // filter active — rendered as "running " prefix
	Mine          bool                       // filter active — rendered as "my " prefix
	Failed        bool                       // filter active — rendered as "failed " prefix
	ResolvedParts []component.BreadcrumbPart // resolved #last info, shown after " → " arrow
	// NavTag, when non-empty, overrides ViewType for the bottom-left nav tag only.
	// Use when the breadcrumb label and the nav position label should differ.
	NavTag string
	// NoTail, when set on a PreviewBreadcrumb, suppresses [tail] and shows [count] instead.
	NoTail bool
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

// branchIcon returns an icon for a branch/MR job based on its name.
// Names like "PR-123" or "MR-123" are treated as merge/pull requests.
func branchIcon(name string) string {
	if strings.HasPrefix(name, "PR-") || strings.HasPrefix(name, "MR-") {
		return " ⊕"
	}
	return " ↳"
}

// iconOr returns override if non-empty, otherwise fallback.
func iconOr(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// statusIcon returns a single icon for a build status, respecting theme overrides.
func statusIcon(t theme.Theme, s jenkins.BuildStatus) string {
	switch s {
	case jenkins.BuildStatusRunning:
		return iconOr(t.Icons.StatusRunning, "●")
	case jenkins.BuildStatusSuccess:
		return iconOr(t.Icons.StatusSuccess, "✔")
	case jenkins.BuildStatusFailed:
		return iconOr(t.Icons.StatusFailed, "✖")
	case jenkins.BuildStatusAborted:
		return iconOr(t.Icons.StatusAborted, "◼")
	case jenkins.BuildStatusUnstable:
		return iconOr(t.Icons.StatusUnstable, "⚠")
	case jenkins.BuildStatusSkipped:
		return iconOr(t.Icons.StatusSkipped, "◇")
	case jenkins.BuildStatusNotBuilt:
		return iconOr(t.Icons.StatusNotBuilt, "◻")
	default:
		return iconOr(t.Icons.StatusUnknown, "○")
	}
}

// StatusLabel returns a human-readable, capitalized label for a build status.
func StatusLabel(s jenkins.BuildStatus) string {
	return statusLabel(s)
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

// vs15 (U+FE0E VARIATION SELECTOR-15) forces text (non-emoji, 1-cell) presentation
// for the preceding character. Without it many terminals render ☀/⛅/⛈ as 2-wide emoji,
// which misaligns table columns.
const vs15 = "\uFE0E"

// renderWeatherIcon renders a single Jenkins-idiomatic weather glyph for the
// MAIN column: ☀ success (yellow), ⛅ unstable (orange), ⛈ failed (blue).
// Returns "" for jobs with no meaningful build history.
// Themes can override the glyphs via Icons.Weather*.
func renderWeatherIcon(t theme.Theme, s jenkins.BuildStatus) string {
	switch s {
	case jenkins.BuildStatusSuccess:
		icon := iconOr(t.Icons.WeatherSun, "☀"+vs15)
		return t.Weather.Sun.Render(icon)
	case jenkins.BuildStatusUnstable:
		icon := iconOr(t.Icons.WeatherUnstable, "⛅"+vs15)
		return t.Weather.Unstable.Render(icon)
	case jenkins.BuildStatusFailed:
		icon := iconOr(t.Icons.WeatherStorm, "⛈"+vs15)
		return t.Weather.Storm.Render(icon)
	default:
		return ""
	}
}

// renderStatus returns a pre-colored status string for display.
func renderStatus(t theme.Theme, s jenkins.BuildStatus) string {
	text := statusIcon(t, s) + " " + statusLabel(s)
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

// renderTestBadge renders a compact badge for a test report, omitting zero counts.
// "✔54 ✖0 ~0" → "✔54", "✔2 ✖3 ~0" → "✔2 ✖3".
// Returns "" when r is nil (no tests or not yet loaded).
func renderTestBadge(t theme.Theme, r *jenkins.TestReport) string {
	if r == nil {
		return ""
	}
	passIcon := iconOr(t.Icons.StatusSuccess, "✔")
	failIcon := iconOr(t.Icons.StatusFailed, "✖")
	var parts []string
	if r.Passed > 0 {
		parts = append(parts, t.BuildStatus.Success.Render(fmt.Sprintf("%s%d", passIcon, r.Passed)))
	}
	if r.Failed > 0 {
		parts = append(parts, t.BuildStatus.Failed.Render(fmt.Sprintf("%s%d", failIcon, r.Failed)))
	}
	if r.Skipped > 0 {
		parts = append(parts, t.BuildStatus.Aborted.Render(fmt.Sprintf("~%d", r.Skipped)))
	}
	return strings.Join(parts, " ")
}

func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("xdg-open", url).Start()
		return nil
	}
}

// artifactShortcutAction returns the Action string for the <A> shortcut.
// With a single artifact it shows the display name (truncated); otherwise "artifacts: N".
func artifactShortcutAction(artifacts []jenkins.Artifact) string {
	if len(artifacts) == 1 {
		name := artifacts[0].DisplayPath
		if len(name) > 20 {
			name = name[:19] + "…"
		}
		return name
	}
	return fmt.Sprintf("artifacts: %d", len(artifacts))
}

// FullScreen is optionally implemented by views that bypass all app chrome
// (header, borders, breadcrumb, nav tags) and render to the full terminal.
type FullScreen interface {
	IsFullScreen() bool
}

// RunningLogView is optionally implemented by log views that can report whether
// their build is still actively running.
type RunningLogView interface {
	IsBuildRunning() bool
}

// HasBadge is optionally implemented by views that want a badge shown
// right-aligned in the top border of the content panel.
type HasBadge interface {
	Badge() string
}

// HasPreviewBadge is optionally implemented by views that also provide
// a badge for the preview panel's top border.
type HasPreviewBadge interface {
	PreviewBadge() string
}

// ContentHeightHint is optionally implemented by views that want a fixed-height
// main panel when paired with a PreviewProvider. The returned value is the
// desired number of content rows; the preview panel takes the remaining space.
type ContentHeightHint interface {
	ContentHeightHint() int
}

// ScrollMarkerKind identifies the type of a scrollbar marker.
type ScrollMarkerKind uint8

const (
	ScrollMarkerSearch  ScrollMarkerKind = iota // search match
	ScrollMarkerWarning                         // warning line
	ScrollMarkerError                           // error line
)

// ScrollMarker marks a notable display-line position on the scrollbar.
type ScrollMarker struct {
	Line int // display line index
	Kind ScrollMarkerKind
}

// ScrollInfo describes the scroll position of a scrollable view.
type ScrollInfo struct {
	Offset     int            // index of first visible display line
	TotalLines int            // total number of display lines
	ViewHeight int            // number of visible lines (viewport height)
	Markers    []ScrollMarker // optional: error/warning/search positions for the scrollbar gutter
}

// HasScrollInfo is optionally implemented by views that support vertical scrolling.
// App uses this to render a scrollbar indicator on the right border of the content panel.
type HasScrollInfo interface {
	ScrollInfo() ScrollInfo
}

// HasPreviewScrollInfo is optionally implemented by views that support vertical
// scrolling in their preview panel. App uses this to render a scrollbar indicator
// on the right border of the preview panel.
type HasPreviewScrollInfo interface {
	PreviewScrollInfo() ScrollInfo
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

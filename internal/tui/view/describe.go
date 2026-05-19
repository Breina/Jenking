package view

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// describeDataMsg carries the fetched script and parameters for a build.
type describeDataMsg struct {
	script string
	params map[string]string
	err    error
}

// editorDoneMsg is sent when the external editor process exits.
type editorDoneMsg struct {
	tmpFile string
	err     error
}

// describeTriggerReadyMsg carries the latest build number fetched before triggering from describe.
type describeTriggerReadyMsg struct{ latestBuild int }

// DescribeView shows build parameters (top pane) and the pipeline Groovy script
// (bottom pane). Implements PreviewProvider so app.go renders both as bordered panels.
type DescribeView struct {
	theme  theme.Theme
	client jenkins.JenkinsClient
	store  *cache.Store
	nc     NavigationContext
	build  jenkins.Build

	script      string            // in-memory Groovy script (editable)
	params      map[string]string // build parameters (nil = still loading)
	dataLoading bool              // true while the initial fetch is in flight

	// Parameters pane (main / top).
	paramLines  []string
	paramOffset int
	width       int
	height      int

	// Script pane (preview / bottom): scrolling, wrapping, and search via LogViewer.
	scriptLV LogViewer

	trigger triggerMixin
	host    behaviorHost
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewDescribeView creates a DescribeView for the given build.
// Script and parameters are always fetched fresh from the Jenkins API.
func NewDescribeView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext, build jenkins.Build) *DescribeView {
	ctx, cancel := context.WithCancel(context.Background())
	dv := &DescribeView{
		theme:       t,
		client:      client,
		store:       store,
		nc:          nc,
		build:       build,
		dataLoading: true,
		scriptLV:    LogViewer{theme: t, renderFn: renderGroovyLogLine},
		trigger:     newTriggerMixin(t, client, nc),
		ctx:         ctx,
		cancel:      cancel,
	}
	access := fixedBuildAccessor(&dv.nc, &dv.build)
	storeFn := func() *cache.Store { return dv.store }
	dv.host.Add(newTestReportBehavior(t, client, storeFn, access, swapTo))
	dv.host.Add(newArtifactBehavior(t, client, storeFn, access, swapTo))
	dv.host.Add(newTriggerBehavior(&dv.trigger))
	return dv
}

func (dv *DescribeView) Init() tea.Cmd {
	dv.buildParamLines()
	dv.buildScriptLines()
	return dv.fetchDataCmd()
}

func (dv *DescribeView) fetchDataCmd() tea.Cmd {
	ctx := dv.ctx
	client := dv.client
	jobPath := dv.nc.JobPath()
	buildNum := dv.build.Number
	return func() tea.Msg {
		script, scriptErr := client.GetBuildScript(ctx, jobPath, buildNum)
		if ctx.Err() != nil {
			return nil
		}
		params, _ := client.GetBuildParameters(ctx, jobPath, buildNum)
		if ctx.Err() != nil {
			return nil
		}
		if params == nil {
			params = map[string]string{}
		}
		return describeDataMsg{script: script, params: params, err: scriptErr}
	}
}

// buildParamLines regenerates the parameters pane display lines.
func (dv *DescribeView) buildParamLines() {
	t := dv.theme
	var lines []string
	switch {
	case dv.dataLoading:
		lines = append(lines, "  "+t.Log.Dim.Render("Loading…"))
	case len(dv.params) == 0:
		lines = append(lines, "  "+t.Log.Dim.Render("(none)"))
	default:
		keys := make([]string, 0, len(dv.params))
		for k := range dv.params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, fmt.Sprintf("  %s  %s",
				t.Popup.Label.Render(k+":"),
				t.Log.Normal.Render(dv.params[k]),
			))
		}
	}
	dv.paramLines = lines
	if maxOff := max(0, len(dv.paramLines)-dv.height); dv.paramOffset > maxOff {
		dv.paramOffset = maxOff
	}
}

// buildScriptLines populates the script LogViewer from the current script text.
func (dv *DescribeView) buildScriptLines() {
	var rawLines []string
	if dv.script != "" {
		rawLines = strings.Split(dv.script, "\n")
	}
	dv.scriptLV.rawLines = rawLines
	dv.scriptLV.recomputeLines()
}

// hasActivePreview reports whether the params panel should be shown.
// Returns true while loading (params unknown) or when params are present.
func (dv *DescribeView) hasActivePreview() bool {
	return dv.dataLoading || len(dv.params) > 0
}

// HasActivePreview implements the conditionalPreview interface checked by app.go.
func (dv *DescribeView) HasActivePreview() bool {
	return dv.hasActivePreview()
}

func (dv *DescribeView) ApplySearch(pattern string) tea.Cmd {
	return dv.scriptLV.ApplySearch(pattern)
}

func (dv *DescribeView) HandleSearchResult(msg SearchResultMsg) tea.Cmd {
	dv.scriptLV.applySearchResult(msg)
	return nil
}

func (dv *DescribeView) SearchQuery() string {
	return dv.scriptLV.SearchQueryWithCount()
}

func (dv *DescribeView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := dv.host.HandleMsg(msg); handled {
		return dv, cmd
	}

	switch msg := msg.(type) {
	case ThemeChangedMsg:
		dv.theme = msg.Theme
		dv.host.SetTheme(msg.Theme)
		dv.scriptLV.theme = msg.Theme
		dv.buildParamLines()
		dv.buildScriptLines()
		return dv, nil

	case describeDataMsg:
		dv.dataLoading = false
		dv.script = msg.script
		dv.params = msg.params
		dv.buildParamLines()
		dv.buildScriptLines()
		if msg.err != nil {
			return dv, func() tea.Msg { return ErrorMsg{Err: msg.err} }
		}
		return dv, nil

	case editorDoneMsg:
		if msg.err == nil && msg.tmpFile != "" {
			if data, err := os.ReadFile(msg.tmpFile); err == nil {
				script := strings.TrimSuffix(string(data), "\n// vim: set filetype=groovy:")
				dv.script = strings.TrimRight(script, "\n")
				dv.buildScriptLines()
			}
		}
		if msg.tmpFile != "" {
			os.Remove(msg.tmpFile)
		}
		return dv, nil

	case describeTriggerReadyMsg:
		if dv.script != "" {
			return dv, dv.trigger.startReplay(msg.latestBuild, dv.build.Number, dv.script)
		}
		return dv, dv.trigger.startTrigger(msg.latestBuild)

	case tea.KeyMsg:
		if handled, cmd := dv.host.HandleKey(msg); handled {
			return dv, cmd
		}
		maxOffset := max(0, len(dv.scriptLV.lines)-dv.scriptLV.contentHeight())
		pageSize := max(1, dv.scriptLV.height-1)
		switch msg.String() {
		case "up", "k":
			dv.scriptLV.offset = max(0, dv.scriptLV.offset-1)
		case "down", "j":
			dv.scriptLV.offset = min(maxOffset, dv.scriptLV.offset+1)
		case "pgup":
			dv.scriptLV.offset = max(0, dv.scriptLV.offset-pageSize)
		case "pgdown":
			dv.scriptLV.offset = min(maxOffset, dv.scriptLV.offset+pageSize)
		case "g", "home":
			dv.scriptLV.offset = 0
		case "G", "end":
			dv.scriptLV.offset = maxOffset
		case "left", "h":
			if !dv.scriptLV.wrap {
				dv.scriptLV.hOffset = max(0, dv.scriptLV.hOffset-8)
			}
		case "right":
			if !dv.scriptLV.wrap {
				dv.scriptLV.hOffset += 8
			}
		case "s":
			return dv, func() tea.Msg {
				return SwapViewMsg{View: NewStageView(dv.theme, dv.client, dv.store, dv.nc, dv.build)}
			}
		case "l":
			nc := dv.nc
			build := dv.build
			return dv, func() tea.Msg {
				cv := NewConsoleView(dv.theme, dv.client, nc)
				cv.build = build
				cv.store = dv.store
				return SwapViewMsg{View: cv}
			}
		case "w", "W":
			dv.scriptLV.wrap = !dv.scriptLV.wrap
			if dv.scriptLV.wrap {
				dv.scriptLV.hOffset = 0
			}
			dv.scriptLV.recomputeLines()
		case "f2", "n":
			dv.scriptLV.nextHighlight(true)
		case "N":
			dv.scriptLV.nextHighlight(false)
		case "t":
			return dv, dv.startTriggerCmd()
		case "e":
			return dv, dv.openEditorCmd()
		}
	}
	return dv, nil
}

// startTriggerCmd fetches the current latest build number, then either
// starts a replay (if a script is loaded) or a regular trigger.
func (dv *DescribeView) startTriggerCmd() tea.Cmd {
	ctx := dv.ctx
	client := dv.client
	jobPath := dv.nc.JobPath()
	return func() tea.Msg {
		builds, _ := client.ListBuilds(ctx, jobPath)
		latest := 0
		if len(builds) > 0 {
			latest = builds[0].Number
		}
		return describeTriggerReadyMsg{latestBuild: latest}
	}
}

// openEditorCmd writes the current script to a temp file and returns a
// tea.ExecProcess command that suspends the TUI, opens the editor, and
// sends an editorDoneMsg when the editor exits.
func (dv *DescribeView) openEditorCmd() tea.Cmd {
	if dv.script == "" {
		return nil
	}
	tmpFile, err := os.CreateTemp("", "jenkins-*.groovy")
	if err != nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("create temp file: %w", err)} }
	}
	content := dv.script + "\n// vim: set filetype=groovy:"
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("write temp file: %w", err)} }
	}
	tmpFile.Close()
	tmpPath := tmpFile.Name()
	cmd := editorCommand(tmpPath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{tmpFile: tmpPath, err: err}
	})
}

func (dv *DescribeView) View() string {
	if dv.height <= 0 {
		return ""
	}
	if !dv.hasActivePreview() {
		// No params panel: script fills the full main panel.
		if dv.script == "" {
			rows := make([]string, dv.height)
			rows[0] = "  " + dv.theme.Log.Dim.Render("Loading…")
			return strings.Join(rows, "\n")
		}
		rows := dv.scriptLV.renderRows()
		return strings.Join(rows, "\n")
	}
	end := min(dv.paramOffset+dv.height, len(dv.paramLines))
	visible := dv.paramLines[dv.paramOffset:end]
	rows := make([]string, dv.height)
	copy(rows, visible)
	return strings.Join(rows, "\n")
}

// ScrollInfo implements HasScrollInfo for when the script is rendered as the main panel.
func (dv *DescribeView) ScrollInfo() ScrollInfo {
	if dv.hasActivePreview() {
		return ScrollInfo{}
	}
	return dv.scriptLV.ScrollInfo()
}

// PreviewScrollInfo implements HasPreviewScrollInfo for the script preview panel.
func (dv *DescribeView) PreviewScrollInfo() ScrollInfo {
	return dv.scriptLV.ScrollInfo()
}

// PreviewView implements PreviewProvider — renders the script pane.
func (dv *DescribeView) PreviewView() string {
	if dv.scriptLV.height <= 0 {
		return ""
	}
	if dv.script == "" {
		rows := make([]string, dv.scriptLV.height)
		rows[0] = "  " + dv.theme.Log.Dim.Render("Loading…")
		return strings.Join(rows, "\n")
	}
	rows := dv.scriptLV.renderRows()
	return strings.Join(rows, "\n")
}

func (dv *DescribeView) PopupView() string {
	return dv.host.PopupView()
}

// SetPreviewSize implements PreviewProvider.
func (dv *DescribeView) SetPreviewSize(w, h int) {
	dv.scriptLV.SetSize(w, h)
}

// PreviewBreadcrumb implements PreviewProvider.
func (dv *DescribeView) PreviewBreadcrumb() BreadcrumbSegment {
	seg := BreadcrumbFor("script", dv.nc)
	seg.NoTail = true
	return seg
}

// PreviewItemCount implements PreviewProvider.
func (dv *DescribeView) PreviewItemCount() int {
	return dv.scriptLV.ItemCount()
}

func (dv *DescribeView) Title() string {
	return fmt.Sprintf("Build #%d", dv.build.Number)
}

func (dv *DescribeView) Breadcrumb() BreadcrumbSegment {
	label := "parameters"
	if !dv.hasActivePreview() {
		label = "script"
	}
	seg := BreadcrumbFor(label, dv.nc)
	seg.NavTag = "describe"
	return seg
}

func (dv *DescribeView) ItemCount() int {
	if !dv.hasActivePreview() {
		return dv.scriptLV.ItemCount()
	}
	return len(dv.paramLines)
}

func (dv *DescribeView) ContentHeightHint() int {
	return len(dv.paramLines)
}

func (dv *DescribeView) Commands() []command.Command {
	return nil
}

func (dv *DescribeView) Shortcuts() []component.Shortcut {
	shortcuts := []component.Shortcut{
		component.Nav("esc", "back"),
		component.Filter("/", "search", false),
		component.Filter("w", "wrap", dv.scriptLV.wrap),
		component.Action("e", "edit"),
	}
	shortcuts = append(shortcuts, detailViewTabs("d")...)
	shortcuts = dv.host.AppendShortcuts(shortcuts)
	if dv.scriptLV.searchRe != nil {
		shortcuts = append(shortcuts, component.Nav("n/N", "next/prev match"))
	}
	return shortcuts
}

func (dv *DescribeView) SetSize(w, h int) {
	dv.width = w
	dv.height = h
	dv.host.SetSize(w, h-6)
	if maxOff := max(0, len(dv.paramLines)-dv.height); dv.paramOffset > maxOff {
		dv.paramOffset = maxOff
	}
	if !dv.hasActivePreview() {
		dv.scriptLV.SetSize(w, h)
	}
}

func (dv *DescribeView) Close() error {
	if dv.cancel != nil {
		dv.cancel()
	}
	return nil
}

func (dv *DescribeView) NC() NavigationContext { return dv.nc }

func (dv *DescribeView) HasPopup() bool {
	return dv.host.HasPopup()
}

func (dv *DescribeView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	nc := dv.nc.AtBranch(dv.nc.BranchName)
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

// editorCommand builds an *exec.Cmd for the user's preferred editor.
// It respects $VISUAL, then $EDITOR, falling back to "vi".
// Editor values like "vim -u NONE" are handled by splitting on whitespace.
func editorCommand(filePath string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	args := append(parts[1:], filePath)
	return exec.Command(parts[0], args...)
}

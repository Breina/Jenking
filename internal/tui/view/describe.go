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

	// Script pane (preview / bottom).
	scriptLines   []string
	scriptOffset  int
	previewWidth  int
	previewHeight int

	trigger triggerMixin
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewDescribeView creates a DescribeView for the given build.
// Script and parameters are always fetched fresh from the Jenkins API.
func NewDescribeView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, nc NavigationContext, build jenkins.Build) *DescribeView {
	ctx, cancel := context.WithCancel(context.Background())
	return &DescribeView{
		theme:       t,
		client:      client,
		store:       store,
		nc:          nc,
		build:       build,
		dataLoading: true,
		trigger:     newTriggerMixin(t, client, nc),
		ctx:         ctx,
		cancel:      cancel,
	}
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

// buildScriptLines regenerates the script pane display lines.
func (dv *DescribeView) buildScriptLines() {
	t := dv.theme
	var lines []string
	if dv.script == "" {
		lines = append(lines, "  "+t.Log.Dim.Render("Loading…"))
	} else {
		for _, raw := range strings.Split(dv.script, "\n") {
			lines = append(lines, renderGroovyLine(raw, t))
		}
	}
	dv.scriptLines = lines
	if maxOff := max(0, len(dv.scriptLines)-dv.previewHeight); dv.scriptOffset > maxOff {
		dv.scriptOffset = maxOff
	}
}

func (dv *DescribeView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := dv.trigger.handleMsg(msg); handled {
		return dv, cmd
	}

	switch msg := msg.(type) {
	case ThemeChangedMsg:
		dv.theme = msg.Theme
		dv.trigger.setTheme(msg.Theme)
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
				dv.script = string(data)
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
		if handled, cmd := dv.trigger.handleKey(msg); handled {
			return dv, cmd
		}
		maxOffset := max(0, len(dv.scriptLines)-dv.previewHeight)
		pageSize := max(1, dv.previewHeight-1)
		switch msg.String() {
		case "up", "k":
			dv.scriptOffset = max(0, dv.scriptOffset-1)
		case "down", "j":
			dv.scriptOffset = min(maxOffset, dv.scriptOffset+1)
		case "pgup":
			dv.scriptOffset = max(0, dv.scriptOffset-pageSize)
		case "pgdown":
			dv.scriptOffset = min(maxOffset, dv.scriptOffset+pageSize)
		case "g", "home":
			dv.scriptOffset = 0
		case "G", "end":
			dv.scriptOffset = maxOffset
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
	if _, err := tmpFile.WriteString(dv.script); err != nil {
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
	end := min(dv.paramOffset+dv.height, len(dv.paramLines))
	visible := dv.paramLines[dv.paramOffset:end]
	rows := make([]string, dv.height)
	copy(rows, visible)
	content := strings.Join(rows, "\n")
	return content
}

// PreviewView implements PreviewProvider — renders the script pane.
func (dv *DescribeView) PreviewView() string {
	if dv.previewHeight <= 0 {
		return ""
	}
	end := min(dv.scriptOffset+dv.previewHeight, len(dv.scriptLines))
	visible := dv.scriptLines[dv.scriptOffset:end]
	rows := make([]string, dv.previewHeight)
	copy(rows, visible)
	content := strings.Join(rows, "\n")
	return dv.trigger.overlay(content, dv.previewWidth, dv.previewHeight)
}

// SetPreviewSize implements PreviewProvider.
func (dv *DescribeView) SetPreviewSize(w, h int) {
	dv.previewWidth = w
	dv.previewHeight = h
	if maxOff := max(0, len(dv.scriptLines)-dv.previewHeight); dv.scriptOffset > maxOff {
		dv.scriptOffset = maxOff
	}
}

// PreviewBreadcrumb implements PreviewProvider.
func (dv *DescribeView) PreviewBreadcrumb() BreadcrumbSegment {
	return BreadcrumbFor("script", dv.nc)
}

// PreviewItemCount implements PreviewProvider.
func (dv *DescribeView) PreviewItemCount() int {
	return len(dv.scriptLines)
}

func (dv *DescribeView) Title() string {
	return fmt.Sprintf("Build #%d", dv.build.Number)
}

func (dv *DescribeView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbFor("describe", dv.nc)
}

func (dv *DescribeView) ItemCount() int {
	return len(dv.paramLines)
}

func (dv *DescribeView) ContentHeightHint() int {
	return len(dv.paramLines)
}

func (dv *DescribeView) Commands() []command.Command {
	return nil
}

func (dv *DescribeView) Shortcuts() []component.Shortcut {
	return []component.Shortcut{
		{Key: "esc", Action: "back"},
		{Key: "e", Action: "edit"},
		{Key: "t", Action: "trigger"},
		{Key: "g/G", Action: "top/bottom"},
	}
}

func (dv *DescribeView) SetSize(w, h int) {
	dv.width = w
	dv.height = h
	dv.trigger.setMaxHeight(h - 6)
	if maxOff := max(0, len(dv.paramLines)-dv.height); dv.paramOffset > maxOff {
		dv.paramOffset = maxOff
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
	return dv.trigger.hasPopup()
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

package view

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// artifactContentMsg carries the result of downloading an artifact's bytes.
type artifactContentMsg struct {
	text        string
	contentType string
	err         error
}

// ArtifactFileView shows a single text artifact in the shared LogViewer widget,
// giving it search, error highlighting, scrolling, wrap and copy. It is to
// ArtifactView what StageLogView is to StageView. Artifacts are static, so it
// fetches once and never polls.
type ArtifactFileView struct {
	BaseView
	lv              widget.LogViewer
	artifact        jmodel.Artifact
	build           jmodel.Build
	parentArtifacts []jmodel.Artifact // for ParentView reconstruction
	trigger         triggerMixin
	host            widget.BehaviorHost
	done            bool
	binary          bool // content sniffed as non-text; show notice instead
	copyLogFlash    bool
	copySelFlash    bool
	// scopedParent overrides ParentView to return a fresh MyBuildsView when this
	// view was opened from a scoped (wildcard) view. Wired by the app on push.
	hasScopedParent      bool
	scopedParentScope    NavigationContext
	scopedParentInterval time.Duration
}

// NewArtifactFileView creates a viewer for a single artifact. parentArtifacts is
// the full list from the originating ArtifactView (used to rebuild it on ESC).
func NewArtifactFileView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, artifact jmodel.Artifact, build jmodel.Build, parentArtifacts []jmodel.Artifact) *ArtifactFileView {
	af := &ArtifactFileView{
		BaseView:        NewBaseView(t, client, store, nc, CtxBuild),
		lv:              widget.NewLogViewer(t),
		artifact:        artifact,
		build:           build,
		parentArtifacts: parentArtifacts,
		trigger:         newTriggerMixin(t, client, nc),
	}
	af.SeedBuildIdentity(build)
	// Sideways navigation to sibling detail views (l/s/d/T) and build actions
	// (x/t) via popSwapTo, mirroring StageLogView: navigating away replaces the
	// pushed artifact list parent, clearing the ":file" breadcrumb tail.
	addFixedBuildActions(&af.host, t, client, &af.nc, &af.build, &af.store, &af.trigger, popSwapTo)
	return af
}

// SetScopedParent implements ScopedParentTarget.
func (af *ArtifactFileView) SetScopedParent(scope NavigationContext, slowInterval time.Duration) {
	af.hasScopedParent = true
	af.scopedParentScope = scope
	af.scopedParentInterval = slowInterval
}

func (af *ArtifactFileView) ApplySearch(pattern string) tea.Cmd { return af.lv.ApplySearch(pattern) }
func (af *ArtifactFileView) HandleSearchResult(msg widget.SearchResultMsg) tea.Cmd {
	af.lv.ApplySearchResult(msg)
	return nil
}
func (af *ArtifactFileView) SearchQuery() string       { return af.lv.SearchQuery() }
func (af *ArtifactFileView) HasActiveNavigation() bool { return af.lv.HasActiveNavigation() }
func (af *ArtifactFileView) ClearActiveNavigation()    { af.lv.ClearActiveNavigation() }

func (af *ArtifactFileView) Init() tea.Cmd {
	return tea.Batch(af.fetchContent, widget.SelectionCheckCmd())
}

func (af *ArtifactFileView) fetchContent() tea.Msg {
	text, ct, err := af.client.GetArtifactContent(af.ctx, af.artifact.URL)
	if af.ctx.Err() != nil {
		return nil
	}
	return artifactContentMsg{text: text, contentType: ct, err: err}
}

func (af *ArtifactFileView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := af.host.HandleMsg(msg); handled {
		return af, cmd
	}
	switch msg := msg.(type) {
	case widget.SelectionCheckMsg:
		af.lv.RecordSelection(msg.Text, msg.LineCount)
		return af, widget.SelectionCheckCmd()
	case widget.CopyFlashMsg:
		return af, af.handleCopyFlash(msg)
	case widget.CopyFlashDoneMsg:
		if msg.IsSel {
			af.copySelFlash = false
		} else {
			af.copyLogFlash = false
		}
		return af, nil
	case ThemeChangedMsg:
		af.theme = msg.Theme
		af.lv.SetTheme(msg.Theme)
		af.host.SetTheme(msg.Theme)
		return af, nil
	case artifactContentMsg:
		return af, af.handleContent(msg)
	case tea.KeyMsg:
		return af.handleKeyMsg(msg)
	}
	return af, nil
}

func (af *ArtifactFileView) handleContent(msg artifactContentMsg) tea.Cmd {
	if msg.err != nil {
		return func() tea.Msg { return ErrorMsg{Err: msg.err} }
	}
	af.done = true
	if looksBinary(msg.text, msg.contentType) {
		af.binary = true
		return nil
	}
	af.lv.SetRawLines(widget.SplitLogLines(msg.text))
	return nil
}

func (af *ArtifactFileView) handleCopyFlash(msg widget.CopyFlashMsg) tea.Cmd {
	if msg.IsSel {
		af.copySelFlash = true
	} else {
		af.copyLogFlash = true
	}
	return widget.CopyFlashTimer(msg.IsSel)
}

func (af *ArtifactFileView) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if handled, cmd := af.host.HandleKey(msg); handled {
		return af, cmd
	}
	// Sideways navigation and browser-open work regardless of content kind.
	switch msg.String() {
	case "o":
		return af, openURLCmd(af.artifact.URL)
	case "l":
		return af, af.openConsoleSwap()
	case "s":
		return af, af.openStageSwap()
	case "d":
		return af, af.openDescribeSwap()
	case "t":
		return af, af.trigger.startTrigger(af.build.Number)
	}
	if af.binary {
		return af, nil
	}
	if handleLogScrollKey(&af.lv, msg, false) {
		return af, nil
	}
	switch msg.String() {
	case "e":
		af.lv.ToggleHighlightErrors()
	case "f2", "n":
		af.lv.NextHighlight(true)
	case "N":
		af.lv.NextHighlight(false)
	case "w", "W":
		af.lv.ToggleWrap()
	case "c":
		return af, af.lv.CopyLogCmd()
	case "C":
		if af.lv.SelectionInLog() {
			return af, af.lv.CopySelectionCmd()
		}
	}
	return af, nil
}

func (af *ArtifactFileView) openConsoleSwap() tea.Cmd {
	nc := af.nc
	build := af.build
	store := af.store
	return func() tea.Msg {
		cv := NewConsoleView(af.theme, af.client, nc)
		cv.SetBuild(build)
		cv.store = store
		return PopSwapViewMsg{View: cv}
	}
}

func (af *ArtifactFileView) openStageSwap() tea.Cmd {
	nc := af.nc
	build := af.build
	store := af.store
	return func() tea.Msg {
		return PopSwapViewMsg{View: NewStageView(af.theme, af.client, store, nc, build)}
	}
}

func (af *ArtifactFileView) openDescribeSwap() tea.Cmd {
	nc := af.nc
	build := af.build
	return func() tea.Msg {
		return PopSwapViewMsg{View: NewDescribeView(af.theme, af.client, af.store, nc, build)}
	}
}

func (af *ArtifactFileView) View() string {
	if af.binary {
		return af.theme.Log.Dim.Render("binary content — press 'o' to open in browser")
	}
	return af.lv.RenderVisible(af.done, "--- end ---")
}

func (af *ArtifactFileView) Title() string { return af.artifact.DisplayPath }

func (af *ArtifactFileView) Breadcrumb() BreadcrumbSegment {
	seg := af.MakeBreadcrumb("artifact")
	seg.Context = append(seg.Context, component.BreadcrumbPart{Text: af.artifact.DisplayPath, Separator: ":"})
	return seg
}

func (af *ArtifactFileView) ItemCount() int { return af.lv.ItemCount() }

func (af *ArtifactFileView) Commands() []command.Command { return nil }

func (af *ArtifactFileView) Shortcuts() []component.Shortcut {
	shortcuts := []component.Shortcut{
		component.Nav("esc", "artifacts"),
		component.Nav("o", "browser"),
	}
	if !af.binary {
		shortcuts = append(shortcuts,
			component.Filter("/", "search", false),
			component.Filter("w", "wrap", af.lv.Wrap()),
			component.Shortcut{Key: "c", Action: af.lv.LogLabel(), Active: af.copyLogFlash, Group: component.GroupAction, Rank: rankActionCopy},
		)
	}
	shortcuts = append(shortcuts, detailViewTabs("")...)
	shortcuts = af.host.AppendShortcuts(shortcuts) // adds T, A, x, t
	if af.binary {
		return shortcuts
	}
	if af.lv.SelectionInLog() {
		shortcuts = append(shortcuts, component.Shortcut{Key: "C", Action: af.lv.SelLabel(), Active: af.copySelFlash, Group: component.GroupAction, Rank: rankActionCopy})
	}
	shortcuts = append(shortcuts, component.Filter("e", "errors", af.lv.HighlightErrors()))
	if af.lv.HasSearch() || af.lv.ErrorNavActive() {
		posInfo := af.lv.NavigationPositionInfo()
		nextLabel := "next"
		if posInfo != "" {
			nextLabel = "next " + posInfo
		}
		shortcuts = append(shortcuts, component.Nav("n/N", nextLabel))
	}
	return shortcuts
}

func (af *ArtifactFileView) SetSize(w, h int) {
	af.BaseView.SetSize(w, h)
	af.lv.SetSize(w, h)
	af.host.SetSize(w, h-6)
}

func (af *ArtifactFileView) PopupView() string { return af.host.PopupView() }

func (af *ArtifactFileView) HasPopup() bool { return af.host.HasPopup() }

func (af *ArtifactFileView) Badge() string { return af.lv.Badge() }

func (af *ArtifactFileView) ScrollInfo() widget.ScrollInfo { return af.lv.ScrollInfo() }

func (af *ArtifactFileView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	if af.hasScopedParent {
		return NewMyBuildsView(t, c, s, af.scopedParentScope, af.scopedParentInterval)
	}
	return NewArtifactView(t, af.parentArtifacts, af.nc, af.build, c, s)
}

// looksBinary reports whether downloaded artifact bytes are unsuitable for the
// text viewer: an obviously binary Content-Type, or a NUL byte in the head of
// the content (the reliable signal for binaries served as octet-stream).
func looksBinary(text, contentType string) bool {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "image/"),
		strings.HasPrefix(ct, "audio/"),
		strings.HasPrefix(ct, "video/"),
		strings.Contains(ct, "pdf"),
		strings.Contains(ct, "zip"),
		strings.Contains(ct, "gzip"),
		strings.Contains(ct, "msword"),
		strings.Contains(ct, "octet") && false: // octet-stream is ambiguous; rely on NUL sniff
		return true
	}
	n := len(text)
	if n > 8000 {
		n = 8000
	}
	return strings.IndexByte(text[:n], 0) >= 0
}

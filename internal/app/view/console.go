package view

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

type consoleChunkMsg struct {
	lines     []string
	nextStart int
	moreData  bool
}

type consoleAbortMsg struct{}

// ConsoleView streams a build's console output using Jenkins' progressive log API.
type ConsoleView struct {
	BaseView
	lv           widget.LogViewer
	done         bool
	fetchStart   int // byte offset to begin the first progressive fetch (non-zero when seeded)
	build        jmodel.Build
	trigger      triggerMixin
	host         widget.BehaviorHost
	copyLogFlash bool
	copySelFlash bool
	// scopedParent, when set, overrides ParentView to return a fresh MyBuildsView
	// using the stored scope. Set when this view is opened from a scoped view.
	hasScopedParent      bool
	scopedParentScope    NavigationContext
	scopedParentInterval time.Duration
}

// SetScopedParent implements ScopedParentTarget. When set, ESC returns to a
// fresh MyBuildsView for the given scope rather than the default StageView.
func (cv *ConsoleView) SetScopedParent(scope NavigationContext, slowInterval time.Duration) {
	cv.hasScopedParent = true
	cv.scopedParentScope = scope
	cv.scopedParentInterval = slowInterval
}

func NewConsoleView(t theme.Theme, client jmodel.JenkinsClient, nc NavigationContext) *ConsoleView {
	cv := &ConsoleView{
		BaseView: NewBaseView(t, client, nil, nc, CtxBuild),
		lv:       widget.NewLogViewer(t, widget.WithInternalLineCheck(widget.IsInternalLine)),
		trigger:  newTriggerMixin(t, client, nc),
	}
	// build is set lazily by callers; fixedBuildAccessor falls back to nc.Build.Number.
	access := fixedBuildAccessor(&cv.nc, &cv.build)
	storeFn := func() *cache.Store { return cv.store }
	cv.host.Add(newTestReportBehavior(t, client, storeFn, access, swapTo))
	cv.host.Add(newArtifactBehavior(t, client, storeFn, access, swapTo))
	cv.host.Add(newTriggerBehavior(&cv.trigger))
	return cv
}

// NewConsoleViewSeeded creates a ConsoleView pre-populated with lines already
// fetched by the preview panel, so the first rendered frame is not blank.
// nextStart is the byte offset from which to continue fetching; done signals
// no further fetching is needed.
func NewConsoleViewSeeded(t theme.Theme, client jmodel.JenkinsClient, nc NavigationContext, seedLines []string, nextStart int, done bool) *ConsoleView {
	cv := NewConsoleView(t, client, nc)
	cv.lv.AppendRawLines(seedLines)
	cv.fetchStart = nextStart
	cv.done = done
	return cv
}

func (cv *ConsoleView) IsBuildRunning() bool { return !cv.done }

func (cv *ConsoleView) ApplySearch(pattern string) tea.Cmd {
	return cv.lv.ApplySearch(pattern)
}

func (cv *ConsoleView) HandleSearchResult(msg widget.SearchResultMsg) tea.Cmd {
	cv.lv.ApplySearchResult(msg)
	return nil
}

func (cv *ConsoleView) SearchQuery() string {
	return cv.lv.SearchQuery()
}

func (cv *ConsoleView) HasActiveNavigation() bool { return cv.lv.HasActiveNavigation() }
func (cv *ConsoleView) ClearActiveNavigation()    { cv.lv.ClearActiveNavigation() }

func (cv *ConsoleView) Init() tea.Cmd {
	if cv.done {
		return widget.SelectionCheckCmd()
	}
	return tea.Batch(
		consoleFetch(cv.ctx, cv.client, cv.nc.JobPath(), cv.nc.Build.Number, cv.fetchStart, 0),
		widget.SelectionCheckCmd(),
	)
}

func consoleFetch(ctx context.Context, client jmodel.JenkinsClient, jobPath string, buildNumber, start int, delay time.Duration) tea.Cmd {
	return progressiveFetch(ctx, func(start int) (*jmodel.ProgressiveLog, error) {
		return client.GetProgressiveLog(ctx, jobPath, buildNumber, start)
	}, start, delay)
}

// logSource reads one chunk of a run's log from a byte offset. It is what makes
// the streaming loop indifferent to *which* kind of run it is following — a
// build's console or a container's scan log, which Jenkins serves through the
// same progressive-text protocol at a different URL.
type logSource func(start int) (*jmodel.ProgressiveLog, error)

func progressiveFetch(ctx context.Context, src logSource, start int, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return consoleAbortMsg{}
			case <-time.After(delay):
			}
		}
		chunk, err := src(start)
		if err != nil {
			if ctx.Err() != nil {
				return consoleAbortMsg{}
			}
			return ErrorMsg{Err: err}
		}
		return consoleChunkMsg{
			lines:     widget.SplitLogLines(chunk.Text),
			nextStart: chunk.NextStart,
			moreData:  chunk.MoreData,
		}
	}
}

// appendRawLines adds new raw lines and updates display lines incrementally.
func (cv *ConsoleView) appendRawLines(newLines []string) {
	cv.lv.AppendRawLines(newLines)
}

func (cv *ConsoleView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := cv.host.HandleMsg(msg); handled {
		return cv, cmd
	}

	switch msg := msg.(type) {
	case widget.SelectionCheckMsg:
		cv.lv.RecordSelection(msg.Text, msg.LineCount)
		return cv, widget.SelectionCheckCmd()
	case widget.CopyFlashMsg:
		return cv, cv.handleCopyFlash(msg)
	case widget.CopyFlashDoneMsg:
		cv.handleCopyFlashDone(msg)
		return cv, nil
	case ThemeChangedMsg:
		cv.handleThemeChanged(msg)
		return cv, nil
	case consoleChunkMsg:
		return cv, cv.handleConsoleChunk(msg)
	case ConnectionRestoredMsg:
		return cv, cv.handleConnectionRestored()
	case consoleAbortMsg:
		return cv, nil
	case tea.KeyMsg:
		return cv.handleKeyMsg(msg)
	}
	return cv, nil
}

func (cv *ConsoleView) handleCopyFlash(msg widget.CopyFlashMsg) tea.Cmd {
	if msg.IsSel {
		cv.copySelFlash = true
	} else {
		cv.copyLogFlash = true
	}
	return widget.CopyFlashTimer(msg.IsSel)
}

func (cv *ConsoleView) handleCopyFlashDone(msg widget.CopyFlashDoneMsg) {
	if msg.IsSel {
		cv.copySelFlash = false
	} else {
		cv.copyLogFlash = false
	}
}

func (cv *ConsoleView) handleThemeChanged(msg ThemeChangedMsg) {
	cv.theme = msg.Theme
	cv.lv.SetTheme(msg.Theme)
	cv.host.SetTheme(msg.Theme)
}

func (cv *ConsoleView) handleConsoleChunk(msg consoleChunkMsg) tea.Cmd {
	cv.fetchStart = msg.nextStart
	pinned := cv.lv.IsPinnedToBottom()
	cv.appendRawLines(msg.lines)
	if pinned {
		cv.lv.ScrollToBottom()
	}
	if msg.moreData {
		return consoleFetch(cv.ctx, cv.client, cv.nc.JobPath(), cv.nc.Build.Number, msg.nextStart, time.Second)
	}
	cv.done = true
	return nil
}

func (cv *ConsoleView) handleConnectionRestored() tea.Cmd {
	if !cv.done {
		return consoleFetch(cv.ctx, cv.client, cv.nc.JobPath(), cv.nc.Build.Number, cv.fetchStart, 0)
	}
	return nil
}

func (cv *ConsoleView) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if handled, cmd := cv.host.HandleKey(msg); handled {
		return cv, cmd
	}
	if handleLogScrollKey(&cv.lv, msg, true) {
		return cv, nil
	}
	switch msg.String() {
	case "e":
		cv.lv.ToggleHighlightErrors()
	case "f2", "n":
		cv.lv.NextHighlight(true)
	case "N":
		cv.lv.NextHighlight(false)
	case "w", "W":
		cv.lv.ToggleWrap()
	case "p", "P":
		cv.lv.ToggleShowInternal()
	case "s":
		return cv, cv.openStageSwap()
	case "d":
		return cv, cv.openDescribeSwap()
	case "t":
		return cv, cv.trigger.startTrigger(cv.effectiveBuildNumber())
	case "c":
		return cv, cv.lv.CopyLogCmd()
	case "C":
		if cv.lv.SelectionInLog() {
			return cv, cv.lv.CopySelectionCmd()
		}
	}
	return cv, nil
}

func (cv *ConsoleView) effectiveBuildNumber() int {
	if cv.build.Number != 0 {
		return cv.build.Number
	}
	return cv.nc.Build.Number
}

func (cv *ConsoleView) openStageSwap() tea.Cmd {
	nc := cv.nc
	build := cv.build
	if build.Number == 0 {
		build.Number = nc.Build.Number
	}
	return func() tea.Msg {
		return SwapViewMsg{View: NewStageView(cv.theme, cv.client, cv.store, nc, build)}
	}
}

func (cv *ConsoleView) openDescribeSwap() tea.Cmd {
	nc := cv.nc
	build := cv.build
	if build.Number == 0 {
		build.Number = nc.Build.Number
	}
	return func() tea.Msg {
		return SwapViewMsg{View: NewDescribeView(cv.theme, cv.client, cv.store, nc, build)}
	}
}

func (cv *ConsoleView) View() string {
	return cv.lv.RenderVisible(cv.done, "─── end ───")
}

func (cv *ConsoleView) PopupView() string {
	return cv.host.PopupView()
}

func (cv *ConsoleView) Title() string {
	return fmt.Sprintf("Build #%d", cv.nc.Build.Number)
}

// SetBuild attaches the build this console belongs to. Callers assign it after
// construction; going through a setter keeps the nc's build identity in step,
// so the console's breadcrumb matches the view it was swapped in from.
func (cv *ConsoleView) SetBuild(b jmodel.Build) {
	cv.build = b
	cv.SeedBuildIdentity(b)
}

func (cv *ConsoleView) Breadcrumb() BreadcrumbSegment {
	return cv.MakeBreadcrumb("log")
}

func (cv *ConsoleView) ItemCount() int {
	return cv.lv.ItemCount()
}

func (cv *ConsoleView) SetSize(w, h int) {
	cv.lv.SetSize(w, h)
	cv.host.SetSize(w, h-6)
}

func (cv *ConsoleView) Commands() []command.Command {
	return nil
}

func (cv *ConsoleView) Shortcuts() []component.Shortcut {
	shortcuts := []component.Shortcut{
		component.Nav("esc", "builds"),
		component.Filter("/", "search", false),
		component.Filter("w", "wrap", cv.lv.Wrap()),
		component.Filter("p", "[Pipeline]", cv.lv.ShowInternal()),
		{Key: "c", Action: cv.lv.LogLabel(), Active: cv.copyLogFlash, Group: component.GroupAction, Rank: rankActionCopy},
	}
	shortcuts = append(shortcuts, detailViewTabs("l")...)
	shortcuts = cv.host.AppendShortcuts(shortcuts)
	if cv.lv.SelectionInLog() {
		shortcuts = append(shortcuts, component.Shortcut{Key: "C", Action: cv.lv.SelLabel(), Active: cv.copySelFlash, Group: component.GroupAction, Rank: rankActionCopy})
	}
	shortcuts = append(shortcuts, component.Filter("e", "errors", cv.lv.HighlightErrors()))
	navActive := cv.lv.HasSearch() || cv.lv.ErrorNavActive()
	if navActive {
		posInfo := cv.lv.NavigationPositionInfo()
		nextLabel := "next"
		if posInfo != "" {
			nextLabel = "next " + posInfo
		}
		shortcuts = append(shortcuts, component.Nav("n/N", nextLabel))
	}
	return shortcuts
}

func (cv *ConsoleView) HasPopup() bool {
	return cv.host.HasPopup()
}

func (cv *ConsoleView) Badge() string { return cv.lv.Badge() }

func (cv *ConsoleView) ScrollInfo() widget.ScrollInfo { return cv.lv.ScrollInfo() }

func (cv *ConsoleView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	if cv.hasScopedParent {
		return NewMyBuildsView(t, c, s, cv.scopedParentScope, cv.scopedParentInterval)
	}
	nc := cv.nc.AtBranch(cv.nc.BranchName)
	if cv.nc.Level == CtxProject {
		return NewBuildsView(t, c, s, nc, NewProjectBuildsProvider(c, s, nc))
	}
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

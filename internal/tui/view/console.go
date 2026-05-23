package view

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

type consoleChunkMsg struct {
	lines     []string
	nextStart int
	moreData  bool
}

type consoleAbortMsg struct{}

// ConsoleView streams a build's console output using Jenkins' progressive log API.
type ConsoleView struct {
	lv           LogViewer
	client       jenkins.JenkinsClient
	nc           NavigationContext
	done         bool
	ctx          context.Context
	cancel       context.CancelFunc
	fetchStart   int // byte offset to begin the first progressive fetch (non-zero when seeded)
	build        jenkins.Build
	store        *cache.Store
	trigger      triggerMixin
	host         behaviorHost
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

func NewConsoleView(t theme.Theme, client jenkins.JenkinsClient, nc NavigationContext) *ConsoleView {
	ctx, cancel := context.WithCancel(context.Background())
	cv := &ConsoleView{
		lv:      newLogViewer(t),
		client:  client,
		nc:      nc,
		ctx:     ctx,
		cancel:  cancel,
		trigger: newTriggerMixin(t, client, nc),
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
func NewConsoleViewSeeded(t theme.Theme, client jenkins.JenkinsClient, nc NavigationContext, seedLines []string, nextStart int, done bool) *ConsoleView {
	cv := NewConsoleView(t, client, nc)
	cv.lv.rawLines = append(cv.lv.rawLines, seedLines...)
	cv.fetchStart = nextStart
	cv.done = done
	return cv
}

func (cv *ConsoleView) IsBuildRunning() bool { return !cv.done }

func (cv *ConsoleView) ApplySearch(pattern string) tea.Cmd {
	return cv.lv.ApplySearch(pattern)
}

func (cv *ConsoleView) HandleSearchResult(msg SearchResultMsg) tea.Cmd {
	cv.lv.applySearchResult(msg)
	return nil
}

func (cv *ConsoleView) SearchQuery() string {
	return cv.lv.SearchQuery()
}

func (cv *ConsoleView) HasActiveNavigation() bool { return cv.lv.HasActiveNavigation() }
func (cv *ConsoleView) ClearActiveNavigation()    { cv.lv.ClearActiveNavigation() }

func (cv *ConsoleView) Init() tea.Cmd {
	// Populate display lines from any seed data (no-op when rawLines is empty).
	cv.lv.recomputeLines()
	if cv.done {
		return selectionCheckCmd()
	}
	return tea.Batch(
		consoleFetch(cv.ctx, cv.client, cv.nc.JobPath(), cv.nc.Build.Number, cv.fetchStart, 0),
		selectionCheckCmd(),
	)
}

func consoleFetch(ctx context.Context, client jenkins.JenkinsClient, jobPath string, buildNumber, start int, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return consoleAbortMsg{}
			case <-time.After(delay):
			}
		}
		chunk, err := client.GetProgressiveLog(ctx, jobPath, buildNumber, start)
		if err != nil {
			if ctx.Err() != nil {
				return consoleAbortMsg{}
			}
			return ErrorMsg{Err: err}
		}
		return consoleChunkMsg{
			lines:     splitLines(chunk.Text),
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
	case selectionCheckMsg:
		cv.lv.selectionText = msg.text
		cv.lv.selectionLineCount = msg.lineCount
		cv.lv.selectionInLog = cv.lv.checkSelectionInLog(msg.text)
		return cv, selectionCheckCmd()

	case copyFlashMsg:
		if msg.isSel {
			cv.copySelFlash = true
		} else {
			cv.copyLogFlash = true
		}
		return cv, copyFlashTimer(msg.isSel)

	case copyFlashDoneMsg:
		if msg.isSel {
			cv.copySelFlash = false
		} else {
			cv.copyLogFlash = false
		}
		return cv, nil

	case ThemeChangedMsg:
		cv.lv.theme = msg.Theme
		cv.host.SetTheme(msg.Theme)
		return cv, nil

	case consoleChunkMsg:
		cv.fetchStart = msg.nextStart
		maxOffset := max(0, len(cv.lv.lines)-cv.lv.contentHeight())
		pinned := cv.lv.offset >= maxOffset
		cv.appendRawLines(msg.lines)
		if pinned {
			cv.lv.offset = max(0, len(cv.lv.lines)-cv.lv.contentHeight())
		}
		if msg.moreData {
			return cv, consoleFetch(cv.ctx, cv.client, cv.nc.JobPath(), cv.nc.Build.Number, msg.nextStart, time.Second)
		}
		cv.done = true
		return cv, nil

	case ConnectionRestoredMsg:
		if !cv.done {
			return cv, consoleFetch(cv.ctx, cv.client, cv.nc.JobPath(), cv.nc.Build.Number, cv.fetchStart, 0)
		}
		return cv, nil

	case consoleAbortMsg:
		return cv, nil

	case tea.KeyMsg:
		if handled, cmd := cv.host.HandleKey(msg); handled {
			return cv, cmd
		}
		maxOffset := max(0, len(cv.lv.lines)-cv.lv.contentHeight())
		pageSize := max(1, cv.lv.height-1)
		switch msg.String() {
		case "up", "k":
			cv.lv.offset = max(0, cv.lv.offset-1)
		case "down", "j":
			cv.lv.offset = min(maxOffset, cv.lv.offset+1)
		case "pgup":
			cv.lv.offset = max(0, cv.lv.offset-pageSize)
		case "pgdown":
			cv.lv.offset = min(maxOffset, cv.lv.offset+pageSize)
		case "g", "home":
			cv.lv.offset = 0
		case "G", "end":
			cv.lv.offset = maxOffset
		case "left", "h":
			if !cv.lv.wrap {
				cv.lv.hOffset = max(0, cv.lv.hOffset-8)
			}
		case "right", "l":
			if !cv.lv.wrap {
				cv.lv.hOffset += 8
			}
		case "e":
			cv.lv.highlightErrors = !cv.lv.highlightErrors
			cv.lv.currentNavIdx = -1
			cv.lv.recomputeLines()
		case "f2", "n":
			cv.lv.nextHighlight(true)
		case "N":
			cv.lv.nextHighlight(false)
		case "w", "W":
			cv.lv.wrap = !cv.lv.wrap
			if cv.lv.wrap {
				cv.lv.hOffset = 0
			}
			cv.lv.recomputeLines()
		case "p", "P":
			cv.lv.showInternal = !cv.lv.showInternal
			cv.lv.recomputeLines()
		case "s":
			nc := cv.nc
			build := cv.build
			if build.Number == 0 {
				build.Number = nc.Build.Number
			}
			return cv, func() tea.Msg {
				return SwapViewMsg{View: NewStageView(cv.lv.theme, cv.client, cv.store, nc, build)}
			}
		case "d":
			nc := cv.nc
			build := cv.build
			if build.Number == 0 {
				build.Number = nc.Build.Number
			}
			return cv, func() tea.Msg {
				return SwapViewMsg{View: NewDescribeView(cv.lv.theme, cv.client, cv.store, nc, build)}
			}
		case "t":
			buildNum := cv.build.Number
			if buildNum == 0 {
				buildNum = cv.nc.Build.Number
			}
			return cv, cv.trigger.startTrigger(buildNum)
		case "c":
			return cv, cv.lv.CopyLogCmd()
		case "C":
			if cv.lv.selectionInLog {
				return cv, cv.lv.CopySelectionCmd()
			}
		}
	}
	return cv, nil
}

func (cv *ConsoleView) View() string {
	if cv.lv.height <= 0 {
		return ""
	}

	ch := cv.lv.contentHeight()
	end := min(cv.lv.offset+ch, len(cv.lv.lines))
	visible := cv.lv.lines[cv.lv.offset:end]

	rows := make([]string, 0, cv.lv.height)
	for i, dl := range visible {
		rows = append(rows, cv.lv.renderLineAt(dl, cv.lv.offset+i))
	}

	if cv.done && len(rows) < cv.lv.height {
		rows = append(rows, cv.lv.theme.Log.Dim.Render("─── end ───"))
	}

	for len(rows) < cv.lv.height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

func (cv *ConsoleView) PopupView() string {
	return cv.host.PopupView()
}

func (cv *ConsoleView) Title() string {
	return fmt.Sprintf("Build #%d", cv.nc.Build.Number)
}

func (cv *ConsoleView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbFor("log", cv.nc)
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
		component.Filter("w", "wrap", cv.lv.wrap),
		component.Filter("p", "[Pipeline]", cv.lv.showInternal),
		{Key: "c", Action: cv.lv.logLabel(), Active: cv.copyLogFlash, Group: component.GroupAction},
	}
	shortcuts = append(shortcuts, detailViewTabs("l")...)
	shortcuts = cv.host.AppendShortcuts(shortcuts)
	if cv.lv.selectionInLog {
		shortcuts = append(shortcuts, component.Shortcut{Key: "C", Action: cv.lv.selLabel(), Active: cv.copySelFlash, Group: component.GroupAction})
	}
	shortcuts = append(shortcuts, component.Filter("e", "errors", cv.lv.highlightErrors))
	navActive := cv.lv.searchRe != nil || cv.lv.ErrorNavActive()
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

func (cv *ConsoleView) NC() NavigationContext { return cv.nc }

func (cv *ConsoleView) HasPopup() bool {
	return cv.host.HasPopup()
}

func (cv *ConsoleView) Close() error {
	if cv.cancel != nil {
		cv.cancel()
	}
	return nil
}

func (cv *ConsoleView) Badge() string { return cv.lv.Badge() }

func (cv *ConsoleView) ScrollInfo() ScrollInfo { return cv.lv.ScrollInfo() }

func (cv *ConsoleView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	if cv.hasScopedParent {
		return NewMyBuildsView(t, c, s, cv.scopedParentScope, cv.scopedParentInterval)
	}
	nc := cv.nc.AtBranch(cv.nc.BranchName)
	if cv.nc.Level == CtxProject {
		return NewBuildsView(t, c, s, nc, NewProjectBuildsProvider(c, s, nc))
	}
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

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
	return &ConsoleView{
		lv:      LogViewer{theme: t},
		client:  client,
		nc:      nc,
		ctx:     ctx,
		cancel:  cancel,
		trigger: newTriggerMixin(t, client, nc),
	}
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

func (cv *ConsoleView) ApplySearch(pattern string) error {
	return cv.lv.ApplySearch(pattern)
}

func (cv *ConsoleView) SearchQuery() string {
	return cv.lv.SearchQueryWithCount()
}

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
	if handled, cmd := cv.trigger.handleMsg(msg); handled {
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
		cv.trigger.setTheme(msg.Theme)
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
		if handled, cmd := cv.trigger.handleKey(msg); handled {
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
		case "T":
			if cv.store != nil {
				key := fmt.Sprintf("%s:%d", cv.nc.JobPath(), cv.nc.Build.Number)
				if entry := cv.store.TestReports.Get(key); entry != nil && entry.Value != nil && len(entry.Value.Suites) > 0 {
					child := NewTestReportView(cv.lv.theme, *entry.Value, cv.nc, cv.build, cv.client, cv.store)
					return cv, func() tea.Msg { return SwapViewMsg{View: child} }
				}
			}
		case "A":
			if cv.store != nil {
				key := fmt.Sprintf("%s:%d", cv.nc.JobPath(), cv.nc.Build.Number)
				if entry := cv.store.Artifacts.Get(key); entry != nil && len(entry.Value) > 0 {
					child := NewArtifactView(cv.lv.theme, entry.Value, cv.nc, cv.build, cv.client, cv.store)
					return cv, func() tea.Msg { return SwapViewMsg{View: child} }
				}
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
	return cv.trigger.popupView()
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
	cv.trigger.setSize(w, h-6)
}

func (cv *ConsoleView) Commands() []command.Command {
	return nil
}

func (cv *ConsoleView) Shortcuts() []component.Shortcut {
	wrapLabel := "wrap"
	if cv.lv.wrap {
		wrapLabel = "no wrap"
	}
	pipelineLabel := "[Pipeline] show"
	if cv.lv.showInternal {
		pipelineLabel = "[Pipeline] hide"
	}
	// esc first for stable grid positioning
	shortcuts := []component.Shortcut{
		{Key: "esc", Action: "builds"},
		{Key: "/", Action: "search"},
		{Key: "w", Action: wrapLabel},
		{Key: "s", Action: "stages"},
		{Key: "d", Action: "describe"},
	}
	if cv.store != nil {
		key := fmt.Sprintf("%s:%d", cv.nc.JobPath(), cv.nc.Build.Number)
		if entry := cv.store.TestReports.Get(key); entry != nil && entry.Value != nil && len(entry.Value.Suites) > 0 {
			badge := renderTestBadge(cv.lv.theme, entry.Value)
			shortcuts = append(shortcuts, component.Shortcut{Key: "T", Action: "tests: " + badge})
		}
		if entry := cv.store.Artifacts.Get(key); entry != nil && len(entry.Value) > 0 {
			shortcuts = append(shortcuts, component.Shortcut{Key: "A", Action: fmt.Sprintf("artifacts: %d", len(entry.Value))})
		}
	}
	shortcuts = append(shortcuts,
		component.Shortcut{Key: "p", Action: pipelineLabel},
		component.Shortcut{Key: "c", Action: cv.lv.logLabel(), Active: cv.copyLogFlash},
		component.Shortcut{Key: "t", Action: "trigger"},
	)
	if cv.lv.selectionInLog {
		shortcuts = append(shortcuts, component.Shortcut{Key: "C", Action: cv.lv.selLabel(), Active: cv.copySelFlash})
	}
	if !cv.lv.wrap {
		shortcuts = append(shortcuts, component.Shortcut{Key: "←/→", Action: "scroll"})
	}
	if cv.lv.searchRe != nil {
		shortcuts = append(shortcuts, component.Shortcut{Key: "n/N", Action: "next/prev match"})
	}
	return shortcuts
}

func (cv *ConsoleView) NC() NavigationContext { return cv.nc }

func (cv *ConsoleView) HasPopup() bool {
	return cv.trigger.hasPopup()
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

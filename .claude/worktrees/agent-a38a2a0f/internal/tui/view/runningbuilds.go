package view

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brecht/jenkins-tui/internal/cache"
	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/command"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

type runningBuildsTickMsg struct{}
type runningBuildsVisualTickMsg struct{}

const (
	colRBDurationWidth  = 14
	colRBTriggeredWidth = 30
	// 3 cols × 2 padding each
	colRBFixedTotal = colRBDurationWidth + colRBTriggeredWidth + 3*2
)

// RunningBuildsView shows all builds currently executing on the Jenkins instance.
type RunningBuildsView struct {
	theme             theme.Theme
	client            jenkins.JenkinsClient
	store             *cache.Store
	table             component.Table
	builds            []jenkins.UserBuild
	confirmCancel     bool
	confirmYes        bool
	confirmCancelPath string
	confirmCancelNum  int
	width             int
	height            int
	progressBar       component.ProgressBar
	ctx               context.Context
	cancel            context.CancelFunc
	searchQuery       string
	searchRe          *regexp.Regexp
	filteredBuilds    []int
}

func NewRunningBuildsView(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store) *RunningBuildsView {
	columns := []component.Column{
		{Title: "JOB", Width: 40},
		{Title: "DURATION", Width: colRBDurationWidth},
		{Title: "TRIGGERED BY", Width: colRBTriggeredWidth},
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RunningBuildsView{
		theme:       t,
		client:      client,
		store:       store,
		table:       component.NewTable(t, columns),
		progressBar: component.NewProgressBar(t),
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (rv *RunningBuildsView) ApplySearch(pattern string) error {
	rv.searchQuery = pattern
	rv.searchRe = compileSearchRegex(pattern)
	rv.populateTable()
	rv.table.SetCursor(0)
	return nil
}

func (rv *RunningBuildsView) SearchQuery() string {
	return rv.searchQuery
}

func (rv *RunningBuildsView) dataIndex(tableIdx int) int {
	if rv.filteredBuilds != nil && tableIdx >= 0 && tableIdx < len(rv.filteredBuilds) {
		return rv.filteredBuilds[tableIdx]
	}
	return tableIdx
}

func (rv *RunningBuildsView) Init() tea.Cmd {
	return rv.fetchBuilds
}

func (rv *RunningBuildsView) fetchBuilds() tea.Msg {
	builds, err := rv.client.ListRunningBuilds(context.Background())
	return RunningBuildsMsg{Builds: builds, Err: err}
}

func (rv *RunningBuildsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		rv.theme = msg.Theme
		rv.table.SetTheme(msg.Theme)
		rv.progressBar.SetTheme(msg.Theme)
		rv.populateTable()
		return rv, nil

	case RunningBuildsMsg:
		if msg.Err != nil {
			return rv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		cursorIdx := rv.table.Cursor()
		rv.builds = msg.Builds
		if rv.store != nil {
			rv.store.RunningBuilds.Put("", msg.Builds)
		}
		// Sort by longest elapsed time first (oldest start = most urgent)
		sort.Slice(rv.builds, func(i, j int) bool {
			return rv.builds[i].Timestamp.Before(rv.builds[j].Timestamp)
		})
		rv.populateTable()
		rv.table.SetCursor(cursorIdx)
		cmds := []tea.Cmd{tea.Tick(10*time.Second, func(time.Time) tea.Msg {
			return runningBuildsTickMsg{}
		})}
		if len(rv.builds) > 0 {
			cmds = append(cmds, rv.scheduleVisualTick())
		}
		return rv, tea.Batch(cmds...)

	case runningBuildsTickMsg:
		return rv, rv.fetchBuilds

	case runningBuildsVisualTickMsg:
		if len(rv.builds) > 0 {
			rv.populateTable()
			return rv, rv.scheduleVisualTick()
		}

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return rv, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		return rv, rv.fetchBuilds

	case tea.KeyMsg:
		if rv.confirmCancel {
			switch msg.String() {
			case "left", "right", "h":
				rv.confirmYes = !rv.confirmYes
			case "y":
				rv.confirmCancel = false
				jobPath, number := rv.confirmCancelPath, rv.confirmCancelNum
				return rv, func() tea.Msg {
					err := rv.client.CancelBuild(context.Background(), jobPath, number)
					return CancelBuildResultMsg{Err: err}
				}
			case "enter":
				if rv.confirmYes {
					rv.confirmCancel = false
					jobPath, number := rv.confirmCancelPath, rv.confirmCancelNum
					return rv, func() tea.Msg {
						err := rv.client.CancelBuild(context.Background(), jobPath, number)
						return CancelBuildResultMsg{Err: err}
					}
				}
				rv.confirmCancel = false
			default:
				rv.confirmCancel = false
			}
			return rv, nil
		}

		switch msg.String() {
		case "up", "k":
			rv.table.MoveUp()
		case "down", "j":
			rv.table.MoveDown()
		case "pgup":
			rv.table.PageUp()
		case "pgdown":
			rv.table.PageDown()
		case "home":
			rv.table.Home()
		case "end":
			rv.table.End()
		case "enter":
			di := rv.dataIndex(rv.table.Cursor())
			if di >= 0 && di < len(rv.builds) {
				selected := rv.builds[di]
				jobName, branchName := extractJobAndBranch(selected.JobPath)
				build := jenkins.Build{
					Number:            selected.Number,
					Status:            jenkins.BuildStatusRunning,
					Timestamp:         selected.Timestamp,
					EstimatedDuration: selected.EstimatedDuration,
				}
				bl := NewBuildList(rv.theme, rv.client, rv.store, selected.JobPath, jobName, branchName)
				sv := NewStageView(rv.theme, rv.client, rv.store, selected.JobPath, build, jobName, branchName)
				return rv, func() tea.Msg { return PushTwoViewsMsg{First: bl, Second: sv} }
			}
		case "l":
			di := rv.dataIndex(rv.table.Cursor())
			if di >= 0 && di < len(rv.builds) {
				selected := rv.builds[di]
				jobName, branchName := extractJobAndBranch(selected.JobPath)
				bl := NewBuildList(rv.theme, rv.client, rv.store, selected.JobPath, jobName, branchName)
				cv := NewConsoleView(rv.theme, rv.client, selected.JobPath, selected.Number, jobName, branchName)
				return rv, func() tea.Msg { return PushTwoViewsMsg{First: bl, Second: cv} }
			}
		case "x":
			di := rv.dataIndex(rv.table.Cursor())
			if di >= 0 && di < len(rv.builds) {
				selected := rv.builds[di]
				rv.confirmCancel = true
				rv.confirmYes = false
				rv.confirmCancelPath = selected.JobPath
				rv.confirmCancelNum = selected.Number
			}
		}
	}
	return rv, nil
}

func (rv *RunningBuildsView) scheduleVisualTick() tea.Cmd {
	ctx := rv.ctx
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(1 * time.Second):
		}
		if ctx.Err() != nil {
			return nil
		}
		return runningBuildsVisualTickMsg{}
	}
}

func (rv *RunningBuildsView) populateTable() {
	rv.filteredBuilds = nil
	var rows []component.Row
	for i, b := range rv.builds {
		if rv.searchRe != nil && !rv.searchRe.MatchString(b.DisplayName) {
			continue
		}
		rv.filteredBuilds = append(rv.filteredBuilds, i)
		elapsed := time.Since(b.Timestamp)
		durationStr := renderRunningStatus(rv.theme, rv.progressBar, colRBDurationWidth, elapsed, b.EstimatedDuration)
		jobName, branchName := extractJobAndBranch(b.JobPath)
		ref := renderFullJobRef(rv.theme, jobName, branchName, b.Number)
		rows = append(rows, component.Row{
			ref,
			durationStr,
			b.Cause,
		})
	}
	rv.table.SetRows(rows)
}

// renderFullJobRef renders a full job reference with breadcrumb colors:
// shortName/branch/#N with colored parts.
func renderFullJobRef(t theme.Theme, jobName, branchName string, buildNumber int) string {
	short := shortName(decodeName(jobName))
	ref := t.Breadcrumb.Context.Render(short)
	if branchName != "" {
		ref += t.Breadcrumb.Paren.Render("/") +
			t.Breadcrumb.Context.Render(decodeName(branchName))
	}
	ref += t.Breadcrumb.Paren.Render("/") +
		t.Breadcrumb.BuildNum.Render(fmt.Sprintf("#%d", buildNumber))
	return ref
}

func (rv *RunningBuildsView) View() string {
	tableView := rv.table.View()
	if rv.confirmCancel {
		return renderConfirmDialog(rv.theme, tableView, rv.width, rv.height,
			"Cancel Build",
			fmt.Sprintf("Stop build #%d?", rv.confirmCancelNum),
			rv.confirmYes,
		)
	}
	return tableView
}

func (rv *RunningBuildsView) Title() string {
	return "Running Builds"
}

func (rv *RunningBuildsView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{ViewType: "running"}
}

func (rv *RunningBuildsView) ItemCount() int {
	if rv.filteredBuilds != nil {
		return len(rv.filteredBuilds)
	}
	return len(rv.builds)
}

func (rv *RunningBuildsView) Commands() []command.Command {
	return nil
}

func (rv *RunningBuildsView) Shortcuts() []component.Shortcut {
	return []component.Shortcut{
		{Key: "/", Action: "search"},
		{Key: "enter", Action: "stages"},
		{Key: "l", Action: "log"},
		{Key: "x", Action: "cancel"},
	}
}

// extractJobAndBranch derives jobName and branchName from a Jenkins JobPath.
// JobPath is slash-joined decoded segments like "folder/project/branch".
// If the path has 2+ segments, the second-to-last is the project and the
// last is the branch. With only 1 segment, it's a standalone project.
func extractJobAndBranch(jobPath string) (string, string) {
	parts := strings.Split(jobPath, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return "", ""
}

func (rv *RunningBuildsView) SetSize(width, height int) {
	rv.width = width
	rv.height = height
	jobWidth := width - colRBFixedTotal
	if jobWidth < 10 {
		jobWidth = 10
	}
	rv.table.SetColumnWidth(0, jobWidth)
	rv.table.SetSize(width, height)
}

func (rv *RunningBuildsView) HasPopup() bool {
	return rv.confirmCancel
}

func (rv *RunningBuildsView) Close() error {
	if rv.cancel != nil {
		rv.cancel()
	}
	return nil
}

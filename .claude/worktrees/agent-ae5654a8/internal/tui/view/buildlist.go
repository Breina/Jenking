package view

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brecht/jenkins-tui/internal/cache"
	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/command"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

const (
	colBuildWidth    = 10
	colStatusBWidth  = 14
	colDurationWidth = 12
	colStartedWidth  = 15
)

type buildListTickMsg struct{}
type buildListVisualTickMsg struct{}

// BuildList is a view that lists builds for a given job.
type BuildList struct {
	theme         theme.Theme
	table         component.Table
	client        jenkins.JenkinsClient
	store         *cache.Store
	jobPath       string
	jobName       string
	branchName    string
	builds        []jenkins.Build
	confirmCancel bool
	confirmYes    bool
	confirmBuild  jenkins.Build
	// Trigger build state
	confirmTrigger bool // show confirm dialog for non-parameterized jobs
	triggerYes     bool
	paramForm      *component.ParamForm // non-nil when showing parameter form
	width          int
	height         int
	ctx            context.Context
	cancel         context.CancelFunc
	progressBar    component.ProgressBar
	searchQuery    string
	searchRe       *regexp.Regexp
	filteredBuilds []int
}

func NewBuildList(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, jobPath, jobName, branchName string) *BuildList {
	ctx, cancel := context.WithCancel(context.Background())
	columns := []component.Column{
		{Title: "BUILD", Width: colBuildWidth},
		{Title: "STATUS", Width: colStatusBWidth},
		{Title: "DURATION", Width: colDurationWidth},
		{Title: "STARTED", Width: colStartedWidth},
	}
	return &BuildList{
		theme:       t,
		table:       component.NewTable(t, columns),
		client:      client,
		store:       store,
		jobPath:     jobPath,
		jobName:     jobName,
		branchName:  branchName,
		ctx:         ctx,
		cancel:      cancel,
		progressBar: component.NewProgressBar(t),
	}
}

func (bl *BuildList) ApplySearch(pattern string) error {
	bl.searchQuery = pattern
	bl.searchRe = compileSearchRegex(pattern)
	bl.populateTable()
	bl.table.SetCursor(0)
	return nil
}

func (bl *BuildList) SearchQuery() string {
	return bl.searchQuery
}

func (bl *BuildList) dataIndex(tableIdx int) int {
	if bl.filteredBuilds != nil && tableIdx >= 0 && tableIdx < len(bl.filteredBuilds) {
		return bl.filteredBuilds[tableIdx]
	}
	return tableIdx
}

func (bl *BuildList) Init() tea.Cmd {
	if bl.store != nil {
		if e := bl.store.Builds.Get(bl.jobPath); e != nil {
			bl.builds = e.Value
			bl.populateTable()
		}
	}
	return bl.fetchBuilds
}

func (bl *BuildList) fetchBuilds() tea.Msg {
	builds, err := bl.client.ListBuilds(bl.ctx, bl.jobPath)
	if bl.ctx.Err() != nil {
		return nil
	}
	return BuildsMsg{Builds: builds, Err: err}
}

func (bl *BuildList) refreshInterval() time.Duration {
	return 10 * time.Second
}

func (bl *BuildList) scheduleRefresh() tea.Cmd {
	ctx := bl.ctx
	interval := bl.refreshInterval()
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
		if ctx.Err() != nil {
			return nil
		}
		return buildListTickMsg{}
	}
}

func (bl *BuildList) scheduleVisualTick() tea.Cmd {
	ctx := bl.ctx
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(1 * time.Second):
		}
		if ctx.Err() != nil {
			return nil
		}
		return buildListVisualTickMsg{}
	}
}

func (bl *BuildList) hasRunning() bool {
	for _, b := range bl.builds {
		if b.Status == jenkins.BuildStatusRunning {
			return true
		}
	}
	return false
}

func (bl *BuildList) populateTable() {
	bl.filteredBuilds = nil
	var rows []component.Row
	for i, b := range bl.builds {
		buildLabel := fmt.Sprintf("#%d", b.Number)
		if bl.searchRe != nil && !bl.searchRe.MatchString(buildLabel) {
			continue
		}
		bl.filteredBuilds = append(bl.filteredBuilds, i)
		styledBuildLabel := bl.theme.Breadcrumb.BuildNum.Render(buildLabel)
		var statusStr string
		var durationStr string
		if b.Status == jenkins.BuildStatusRunning {
			elapsed := time.Since(b.Timestamp)
			statusStr = renderRunningStatus(bl.theme, bl.progressBar, colStatusBWidth, elapsed, b.EstimatedDuration)
			durationStr = "~" + formatDuration(elapsed)
		} else {
			statusStr = renderStatus(bl.theme, b.Status)
			durationStr = formatDuration(b.Duration)
		}
		rows = append(rows, component.Row{
			styledBuildLabel,
			statusStr,
			durationStr,
			relativeTime(b.Timestamp),
		})
	}
	bl.table.SetRows(rows)
}

func (bl *BuildList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		bl.theme = msg.Theme
		bl.table.SetTheme(msg.Theme)
		bl.progressBar.SetTheme(msg.Theme)
		bl.populateTable()
		return bl, nil

	case buildListTickMsg:
		return bl, bl.fetchBuilds

	case buildListVisualTickMsg:
		if bl.hasRunning() {
			bl.populateTable()
			return bl, bl.scheduleVisualTick()
		}

	case BuildsMsg:
		if msg.Err != nil {
			return bl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		cursorIdx := bl.table.Cursor()
		bl.builds = msg.Builds
		if bl.store != nil {
			bl.store.Builds.Put(bl.jobPath, msg.Builds)
		}
		sort.Slice(bl.builds, func(i, j int) bool {
			return bl.builds[i].Number > bl.builds[j].Number
		})
		bl.populateTable()
		bl.table.SetCursor(cursorIdx)
		cmds := []tea.Cmd{bl.scheduleRefresh()}
		if bl.hasRunning() {
			cmds = append(cmds, bl.scheduleVisualTick())
		}
		return bl, tea.Batch(cmds...)

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return bl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		return bl, bl.fetchBuilds

	case JobParamsMsg:
		if msg.Err != nil {
			return bl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		if len(msg.Params) == 0 {
			bl.confirmTrigger = true
			bl.triggerYes = false
			return bl, nil
		}
		form := component.NewParamForm(msg.Params)
		form.SetMaxHeight(bl.height - 6)
		bl.paramForm = &form
		return bl, nil

	case TriggerBuildResultMsg:
		if msg.Err != nil {
			return bl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		lastKnown := 0
		if len(bl.builds) > 0 {
			lastKnown = bl.builds[0].Number
		}
		sv := NewPendingStageView(bl.theme, bl.client, bl.store, bl.jobPath, lastKnown, bl.jobName, bl.branchName)
		return bl, func() tea.Msg { return PushViewMsg{View: sv} }

	case tea.KeyMsg:
		if bl.paramForm != nil {
			result := bl.paramForm.Update(msg)
			switch result.Status {
			case component.ParamFormDone:
				bl.paramForm = nil
				return bl, triggerBuild(bl.client, bl.jobPath, result.Values)
			case component.ParamFormCancelled:
				bl.paramForm = nil
			}
			return bl, nil
		}

		if bl.confirmTrigger {
			switch msg.String() {
			case "left", "right", "h":
				bl.triggerYes = !bl.triggerYes
			case "y":
				bl.confirmTrigger = false
				return bl, triggerBuild(bl.client, bl.jobPath, nil)
			case "enter":
				if bl.triggerYes {
					bl.confirmTrigger = false
					return bl, triggerBuild(bl.client, bl.jobPath, nil)
				}
				bl.confirmTrigger = false
			default:
				bl.confirmTrigger = false
			}
			return bl, nil
		}

		if bl.confirmCancel {
			switch msg.String() {
			case "left", "right", "h":
				bl.confirmYes = !bl.confirmYes
			case "y":
				bl.confirmCancel = false
				jobPath, number := bl.jobPath, bl.confirmBuild.Number
				return bl, func() tea.Msg {
					err := bl.client.CancelBuild(context.Background(), jobPath, number)
					return CancelBuildResultMsg{Err: err}
				}
			case "enter":
				if bl.confirmYes {
					bl.confirmCancel = false
					jobPath, number := bl.jobPath, bl.confirmBuild.Number
					return bl, func() tea.Msg {
						err := bl.client.CancelBuild(context.Background(), jobPath, number)
						return CancelBuildResultMsg{Err: err}
					}
				}
				bl.confirmCancel = false
			default:
				bl.confirmCancel = false
			}
			return bl, nil
		}

		switch msg.String() {
		case "up", "k":
			bl.table.MoveUp()
		case "down", "j":
			bl.table.MoveDown()
		case "pgup":
			bl.table.PageUp()
		case "pgdown":
			bl.table.PageDown()
		case "home":
			bl.table.Home()
		case "end":
			bl.table.End()
		case "enter":
			di := bl.dataIndex(bl.table.Cursor())
			if di >= 0 && di < len(bl.builds) {
				selected := bl.builds[di]
				child := NewStageView(bl.theme, bl.client, bl.store, bl.jobPath, selected, bl.jobName, bl.branchName)
				return bl, func() tea.Msg { return PushViewMsg{View: child} }
			}
		case "l":
			di := bl.dataIndex(bl.table.Cursor())
			if di >= 0 && di < len(bl.builds) {
				selected := bl.builds[di]
				child := NewConsoleView(bl.theme, bl.client, bl.jobPath, selected.Number, bl.jobName, bl.branchName)
				return bl, func() tea.Msg { return PushViewMsg{View: child} }
			}
		case "f":
			di := bl.dataIndex(bl.table.Cursor())
			if di >= 0 && di < len(bl.builds) {
				selected := bl.builds[di]
				if selected.Status == jenkins.BuildStatusFailed {
					return bl, fetchFailedStage(bl.client, bl.jobPath, bl.jobName, bl.branchName, selected)
				}
			}
		case "t":
			return bl, fetchJobParams(bl.client, bl.jobPath, bl.jobName, bl.branchName)
		case "x":
			di := bl.dataIndex(bl.table.Cursor())
			if di >= 0 && di < len(bl.builds) {
				if bl.builds[di].Status == jenkins.BuildStatusRunning {
					bl.confirmCancel = true
					bl.confirmYes = false
					bl.confirmBuild = bl.builds[di]
				}
			}
		}
	}
	return bl, nil
}

func (bl *BuildList) View() string {
	tableView := bl.table.View()
	if bl.paramForm != nil {
		return overlayCenter(tableView, bl.paramForm.View(), bl.width, bl.height)
	}
	if bl.confirmTrigger {
		return renderConfirmDialog(bl.theme, tableView, bl.width, bl.height,
			"Trigger Build",
			fmt.Sprintf("Start a new build of %s?", decodeName(bl.jobName)),
			bl.triggerYes,
		)
	}
	if bl.confirmCancel {
		return renderConfirmDialog(bl.theme, tableView, bl.width, bl.height,
			"Cancel Build",
			fmt.Sprintf("Stop build #%d?", bl.confirmBuild.Number),
			bl.confirmYes,
		)
	}
	return tableView
}

func (bl *BuildList) Title() string {
	return decodeName(bl.jobName)
}

func (bl *BuildList) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{
		ViewType: "builds",
		Context:  jobRefParts(bl.jobName, bl.branchName),
	}
}

func (bl *BuildList) ItemCount() int {
	if bl.filteredBuilds != nil {
		return len(bl.filteredBuilds)
	}
	return len(bl.builds)
}

func (bl *BuildList) Commands() []command.Command {
	return nil
}

func (bl *BuildList) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{
		{Key: "/", Action: "search"},
		{Key: "t", Action: "trigger"},
	}
	if len(bl.builds) == 0 {
		return sc
	}
	sc = append(sc,
		component.Shortcut{Key: "enter", Action: "stages"},
		component.Shortcut{Key: "l", Action: "log"},
	)
	di := bl.dataIndex(bl.table.Cursor())
	if di >= 0 && di < len(bl.builds) {
		if bl.builds[di].Status == jenkins.BuildStatusFailed {
			sc = append(sc, component.Shortcut{Key: "f", Action: "failed stage"})
		}
		if bl.builds[di].Status == jenkins.BuildStatusRunning {
			sc = append(sc, component.Shortcut{Key: "x", Action: "cancel"})
		}
	}
	return sc
}

func (bl *BuildList) HasPopup() bool {
	return bl.confirmTrigger || bl.confirmCancel || bl.paramForm != nil
}

func (bl *BuildList) Close() error {
	if bl.cancel != nil {
		bl.cancel()
	}
	return nil
}

func (bl *BuildList) SetSize(width, height int) {
	bl.width = width
	bl.height = height
	bl.table.SetSize(width, height)
}

// formatDuration formats a duration as "45s", "2m 13s", or "1h 04m".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// relativeTime formats a time as a human-readable relative string.
func relativeTime(t time.Time) string {
	elapsed := time.Since(t)
	switch {
	case elapsed < 30*time.Second:
		return "just now"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	}
}

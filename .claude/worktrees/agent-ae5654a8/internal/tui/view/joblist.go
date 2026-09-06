package view

import (
	"context"
	"fmt"
	"net/url"
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

// JobsMsg carries fetched jobs to the view.
type JobsMsg struct {
	Jobs []jenkins.Job
	Err  error
}

// typeIcon returns a single icon character for the job type.
func typeIcon(t jenkins.JobType) string {
	switch t {
	case jenkins.JobTypeFolder:
		return "▸"
	case jenkins.JobTypePipeline:
		return "⚙"
	case jenkins.JobTypeMultiBranch:
		return "⎇"
	case jenkins.JobTypeFreeStyle:
		return "⊙"
	default:
		return "?"
	}
}

// isContainer returns true for job types that don't have their own build status or last build.
func isContainer(t jenkins.JobType) bool {
	return t == jenkins.JobTypeFolder || t == jenkins.JobTypeMultiBranch
}

type jobListTickMsg struct{}
type jobListVisualTickMsg struct{}

// JobList is a view that lists jobs under a given folder path.
type JobList struct {
	theme             theme.Theme
	table             component.Table
	client            jenkins.JenkinsClient
	store             *cache.Store
	folderPath        string
	title             string
	jobs              []jenkins.Job
	width             int
	height            int
	branchContext     bool // true when listing branches/MRs inside a MultiBranch project
	confirmTrigger    bool // show confirm dialog for non-parameterized jobs
	triggerYes        bool
	triggerJobPath    string
	triggerJobName    string
	triggerBranchName string
	paramForm         *component.ParamForm // non-nil when showing parameter form
	confirmCancel     bool
	confirmCancelYes  bool
	confirmCancelPath string
	confirmCancelNum  int
	ctx               context.Context
	cancel            context.CancelFunc
	progressBar       component.ProgressBar
	searchQuery       string
	searchRe          *regexp.Regexp
	filteredJobs      []int // maps table row index → jl.jobs index
}

// fixed column widths (content area, excluding padding).
const (
	colStatusWidth    = 12
	colLastBuildWidth = 10
	// each column has padding(0,1) = 2 chars; 3 columns × 2 = 6 total padding overhead
	colFixedTotal = colStatusWidth + colLastBuildWidth + 3*2
)

// NewJobList creates a job list view. folderPath="" means root. title is the breadcrumb label.
// branchContext=true renders branch/MR icons instead of type icons (used inside MultiBranch projects).
func NewJobList(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, folderPath, title string, branchContext bool) *JobList {
	ctx, cancel := context.WithCancel(context.Background())
	columns := []component.Column{
		{Title: "NAME", Width: 30},
		{Title: "STATUS", Width: colStatusWidth},
		{Title: "LAST RUN", Width: colLastBuildWidth},
	}
	return &JobList{
		theme:         t,
		table:         component.NewTable(t, columns),
		client:        client,
		store:         store,
		folderPath:    folderPath,
		title:         title,
		branchContext: branchContext,
		ctx:           ctx,
		cancel:        cancel,
		progressBar:   component.NewProgressBar(t),
	}
}

// branchIcon returns an icon for a branch/MR job based on its name.
// Names like "PR-123" or "MR-123" are treated as merge/pull requests.
func branchIcon(name string) string {
	if strings.HasPrefix(name, "PR-") || strings.HasPrefix(name, "MR-") {
		return "⊕"
	}
	return "↳"
}

func (jl *JobList) ApplySearch(pattern string) error {
	jl.searchQuery = pattern
	jl.searchRe = compileSearchRegex(pattern)
	jl.populateTable()
	jl.table.SetCursor(0)
	return nil
}

func (jl *JobList) SearchQuery() string {
	return jl.searchQuery
}

// dataIndex maps a table row index to the actual jl.jobs index.
func (jl *JobList) dataIndex(tableIdx int) int {
	if jl.filteredJobs != nil && tableIdx >= 0 && tableIdx < len(jl.filteredJobs) {
		return jl.filteredJobs[tableIdx]
	}
	return tableIdx
}

func (jl *JobList) Init() tea.Cmd {
	if jl.store != nil {
		if e := jl.store.Jobs.Get(jl.folderPath); e != nil {
			jl.jobs = e.Value
			jl.populateTable()
		}
	}
	return jl.fetchJobs
}

func (jl *JobList) fetchJobs() tea.Msg {
	jobs, err := jl.client.ListJobs(jl.ctx, jl.folderPath)
	if jl.ctx.Err() != nil {
		return nil
	}
	return JobsMsg{Jobs: jobs, Err: err}
}

func (jl *JobList) refreshInterval() time.Duration {
	return 15 * time.Second
}

func (jl *JobList) scheduleRefresh() tea.Cmd {
	ctx := jl.ctx
	interval := jl.refreshInterval()
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
		if ctx.Err() != nil {
			return nil
		}
		return jobListTickMsg{}
	}
}

func (jl *JobList) scheduleVisualTick() tea.Cmd {
	ctx := jl.ctx
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(1 * time.Second):
		}
		if ctx.Err() != nil {
			return nil
		}
		return jobListVisualTickMsg{}
	}
}

func (jl *JobList) hasRunning() bool {
	for _, j := range jl.jobs {
		if jenkins.ColorToBuildStatus(j.Color) == jenkins.BuildStatusRunning {
			return true
		}
	}
	return false
}

func (jl *JobList) populateTable() {
	jl.filteredJobs = nil
	var rows []component.Row
	for i, j := range jl.jobs {
		decoded := decodeName(j.Name)
		if jl.searchRe != nil && !jl.searchRe.MatchString(decoded) {
			continue
		}
		jl.filteredJobs = append(jl.filteredJobs, i)
		var icon string
		if jl.branchContext {
			icon = branchIcon(decoded)
		} else {
			icon = typeIcon(j.Type)
		}
		name := icon + " " + decoded
		var status, lastBuild string
		if isContainer(j.Type) && j.Color == "" {
			status = "-"
			lastBuild = "-"
		} else {
			bs := jenkins.ColorToBuildStatus(j.Color)
			if bs == jenkins.BuildStatusRunning && j.LastBuild != nil {
				elapsed := time.Since(j.LastBuild.Timestamp)
				status = renderRunningStatus(jl.theme, jl.progressBar, colStatusWidth, elapsed, j.LastBuild.EstimatedDuration)
			} else {
				status = renderStatus(jl.theme, bs)
			}
			if j.LastBuild != nil {
				lastBuild = relativeTime(j.LastBuild.Timestamp)
			} else {
				lastBuild = "-"
			}
		}
		rows = append(rows, component.Row{name, status, lastBuild})
	}
	jl.table.SetRows(rows)
}

func (jl *JobList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		jl.theme = msg.Theme
		jl.table.SetTheme(msg.Theme)
		jl.progressBar.SetTheme(msg.Theme)
		jl.populateTable()
		return jl, nil

	case jobListTickMsg:
		return jl, jl.fetchJobs

	case jobListVisualTickMsg:
		if jl.hasRunning() {
			jl.populateTable()
			return jl, jl.scheduleVisualTick()
		}

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return jl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		return jl, jl.fetchJobs

	case JobsMsg:
		if msg.Err != nil {
			return jl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		cursorIdx := jl.table.Cursor()
		jl.jobs = msg.Jobs
		if jl.store != nil {
			jl.store.Jobs.Put(jl.folderPath, msg.Jobs)
		}
		sort.Slice(jl.jobs, func(i, j int) bool {
			ti := jl.jobs[i].LastBuild
			tj := jl.jobs[j].LastBuild
			if ti == nil && tj == nil {
				return false
			}
			if ti == nil {
				return false
			}
			if tj == nil {
				return true
			}
			return ti.Timestamp.After(tj.Timestamp)
		})
		jl.populateTable()
		jl.table.SetCursor(cursorIdx)
		cmds := []tea.Cmd{jl.scheduleRefresh()}
		if jl.hasRunning() {
			cmds = append(cmds, jl.scheduleVisualTick())
		}
		return jl, tea.Batch(cmds...)

	case JobParamsMsg:
		if msg.Err != nil {
			return jl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		jl.triggerJobPath = msg.JobPath
		jl.triggerJobName = msg.JobName
		jl.triggerBranchName = msg.BranchName
		if len(msg.Params) == 0 {
			jl.confirmTrigger = true
			jl.triggerYes = false
			return jl, nil
		}
		form := component.NewParamForm(msg.Params)
		form.SetMaxHeight(jl.height - 6)
		jl.paramForm = &form
		return jl, nil

	case TriggerBuildResultMsg:
		if msg.Err != nil {
			return jl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		lastKnown := 0
		for _, j := range jl.jobs {
			if j.FullPath == msg.JobPath && j.LastBuild != nil {
				lastKnown = j.LastBuild.Number
				break
			}
		}
		triggerJobName := jl.triggerJobName
		triggerBranchName := jl.triggerBranchName
		return jl, func() tea.Msg {
			return OpenTriggeredBuildMsg{
				JobPath:        msg.JobPath,
				JobName:        triggerJobName,
				BranchName:     triggerBranchName,
				LastKnownBuild: lastKnown,
			}
		}

	case tea.KeyMsg:
		if jl.paramForm != nil {
			result := jl.paramForm.Update(msg)
			switch result.Status {
			case component.ParamFormDone:
				jobPath := jl.triggerJobPath
				jl.paramForm = nil
				return jl, triggerBuild(jl.client, jobPath, result.Values)
			case component.ParamFormCancelled:
				jl.paramForm = nil
			}
			return jl, nil
		}

		if jl.confirmTrigger {
			switch msg.String() {
			case "left", "right", "h":
				jl.triggerYes = !jl.triggerYes
			case "y":
				jl.confirmTrigger = false
				return jl, triggerBuild(jl.client, jl.triggerJobPath, nil)
			case "enter":
				if jl.triggerYes {
					jl.confirmTrigger = false
					return jl, triggerBuild(jl.client, jl.triggerJobPath, nil)
				}
				jl.confirmTrigger = false
			default:
				jl.confirmTrigger = false
			}
			return jl, nil
		}

		if jl.confirmCancel {
			switch msg.String() {
			case "left", "right", "h":
				jl.confirmCancelYes = !jl.confirmCancelYes
			case "y":
				jl.confirmCancel = false
				jobPath, number := jl.confirmCancelPath, jl.confirmCancelNum
				return jl, func() tea.Msg {
					err := jl.client.CancelBuild(context.Background(), jobPath, number)
					return CancelBuildResultMsg{Err: err}
				}
			case "enter":
				if jl.confirmCancelYes {
					jl.confirmCancel = false
					jobPath, number := jl.confirmCancelPath, jl.confirmCancelNum
					return jl, func() tea.Msg {
						err := jl.client.CancelBuild(context.Background(), jobPath, number)
						return CancelBuildResultMsg{Err: err}
					}
				}
				jl.confirmCancel = false
			default:
				jl.confirmCancel = false
			}
			return jl, nil
		}

		switch msg.String() {
		case "up", "k":
			jl.table.MoveUp()
		case "down", "j":
			jl.table.MoveDown()
		case "pgup":
			jl.table.PageUp()
		case "pgdown":
			jl.table.PageDown()
		case "home":
			jl.table.Home()
		case "end":
			jl.table.End()
		case "enter":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				switch selected.Type {
				case jenkins.JobTypeFolder:
					child := NewJobList(jl.theme, jl.client, jl.store, selected.FullPath, selected.Name, false)
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				case jenkins.JobTypeMultiBranch:
					child := NewJobList(jl.theme, jl.client, jl.store, selected.FullPath, selected.Name, true)
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				default:
					jobName := selected.Name
					branchName := ""
					if jl.branchContext {
						jobName = jl.title
						branchName = selected.Name
					}
					child := NewBuildList(jl.theme, jl.client, jl.store, selected.FullPath, jobName, branchName)
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				}
			}
		case "l":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) && selected.LastBuild != nil {
					jobName := selected.Name
					branchName := ""
					if jl.branchContext {
						jobName = jl.title
						branchName = selected.Name
					}
					child := NewConsoleView(jl.theme, jl.client, selected.FullPath, selected.LastBuild.Number, jobName, branchName)
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				}
			}
		case "t":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) {
					jobName := selected.Name
					branchName := ""
					if jl.branchContext {
						jobName = jl.title
						branchName = selected.Name
					}
					return jl, fetchJobParams(jl.client, selected.FullPath, jobName, branchName)
				}
			}
		case "f":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) && selected.LastBuild != nil {
					bs := jenkins.ColorToBuildStatus(selected.Color)
					if bs == jenkins.BuildStatusFailed {
						jobName := selected.Name
						branchName := ""
						if jl.branchContext {
							jobName = jl.title
							branchName = selected.Name
						}
						build := jenkins.Build{
							Number:    selected.LastBuild.Number,
							Status:    bs,
							Timestamp: selected.LastBuild.Timestamp,
						}
						return jl, fetchFailedStage(jl.client, selected.FullPath, jobName, branchName, build)
					}
				}
			}
		case "x":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) && selected.LastBuild != nil {
					bs := jenkins.ColorToBuildStatus(selected.Color)
					if bs == jenkins.BuildStatusRunning {
						jl.confirmCancel = true
						jl.confirmCancelYes = false
						jl.confirmCancelPath = selected.FullPath
						jl.confirmCancelNum = selected.LastBuild.Number
					}
				}
			}
		}
	}
	return jl, nil
}

func (jl *JobList) View() string {
	tableView := jl.table.View()
	if jl.paramForm != nil {
		return overlayCenter(tableView, jl.paramForm.View(), jl.width, jl.height)
	}
	if jl.confirmTrigger {
		return renderConfirmDialog(jl.theme, tableView, jl.width, jl.height,
			"Trigger Build",
			"Start a new build of "+decodeName(jl.triggerJobName)+"?",
			jl.triggerYes,
		)
	}
	if jl.confirmCancel {
		return renderConfirmDialog(jl.theme, tableView, jl.width, jl.height,
			"Cancel Build",
			fmt.Sprintf("Stop build #%d?", jl.confirmCancelNum),
			jl.confirmCancelYes,
		)
	}
	return tableView
}

func (jl *JobList) Title() string {
	return decodeName(jl.title)
}

func (jl *JobList) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{
		ViewType: "jobs",
		Context:  []BreadcrumbPart{{Text: shortName(decodeName(jl.title))}},
	}
}

func (jl *JobList) ItemCount() int {
	if jl.filteredJobs != nil {
		return len(jl.filteredJobs)
	}
	return len(jl.jobs)
}

func (jl *JobList) Commands() []command.Command {
	return nil
}

func (jl *JobList) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{{Key: "/", Action: "search"}}
	di := jl.dataIndex(jl.table.Cursor())
	if di < 0 || di >= len(jl.jobs) {
		return sc
	}
	selected := jl.jobs[di]
	if isContainer(selected.Type) {
		return sc
	}
	sc = append(sc, component.Shortcut{Key: "t", Action: "trigger"})
	if selected.LastBuild != nil {
		sc = append(sc, component.Shortcut{Key: "l", Action: "log"})
		bs := jenkins.ColorToBuildStatus(selected.Color)
		if bs == jenkins.BuildStatusFailed {
			sc = append(sc, component.Shortcut{Key: "f", Action: "failed stage"})
		}
		if bs == jenkins.BuildStatusRunning {
			sc = append(sc, component.Shortcut{Key: "x", Action: "cancel"})
		}
	}
	return sc
}

func (jl *JobList) HasPopup() bool {
	return jl.confirmTrigger || jl.confirmCancel || jl.paramForm != nil
}

func (jl *JobList) Close() error {
	if jl.cancel != nil {
		jl.cancel()
	}
	return nil
}

func (jl *JobList) SetSize(width, height int) {
	jl.width = width
	jl.height = height
	nameWidth := width - colFixedTotal
	if nameWidth < 10 {
		nameWidth = 10
	}
	jl.table.SetColumnWidth(0, nameWidth)
	jl.table.SetSize(width, height)
}

// decodeName URL-decodes a job name (branch names like "feature%2Fbranch" → "feature/branch").
func decodeName(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}

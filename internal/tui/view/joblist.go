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

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// JobsMsg carries fetched jobs to the view.
type JobsMsg struct {
	Jobs []jenkins.Job
	Err  error
}

// typeIcon returns a single icon character for the job type, respecting theme overrides.
func typeIcon(th theme.Theme, t jenkins.JobType) string {
	switch t {
	case jenkins.JobTypeFolder:
		return iconOr(th.Icons.TypeFolder, "▸")
	case jenkins.JobTypePipeline:
		return iconOr(th.Icons.TypePipeline, "⚙")
	case jenkins.JobTypeMultiBranch:
		return iconOr(th.Icons.TypeMultiBranch, "⎇")
	case jenkins.JobTypeFreeStyle:
		return iconOr(th.Icons.TypeFreeStyle, "⊙")
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
// Cross-cutting concerns (trigger, cancel, test report, artifact) are owned by
// behaviors registered on host with a row-aware access closure.
type JobList struct {
	theme            theme.Theme
	table            component.Table
	client           jenkins.JenkinsClient
	store            *cache.Store
	folderPath       string
	title            string
	jobs             []jenkins.Job
	width            int
	height           int
	username         string   // authenticated user ID (propagated to child NavigationContexts)
	gitUsernames     []string // git display names for mine-filter matching (propagated to child NavigationContexts)
	branchContext    bool     // true when listing branches/MRs inside a MultiBranch project
	ctx              context.Context
	cancel           context.CancelFunc
	progressBar      component.ProgressBar
	searchQuery      string
	searchRe         *regexp.Regexp
	filteredJobs     []int // maps table row index → jl.jobs index
	visualTickActive bool  // true while a visual tick chain is in flight
	lastFetchedKey   string
	trigger          triggerMixin
	host             behaviorHost
}

// fixed column widths (content area, excluding padding).
const (
	colMainWidth      = 4
	colLastBuildWidth = 10
	colSepWidth       = 2 // blank spacer column
	colStatusWidth    = 12

	// Normal view: JOB | MAIN | sep | LAST BUILD | sep | STATUS  (6 cols × 2 pad)
	colFixedTotalNormal = colMainWidth + colSepWidth + colLastBuildWidth + colSepWidth + colStatusWidth + 6*2
	// Branch view: JOB | LAST BUILD | sep | STATUS               (4 cols × 2 pad)
	colFixedTotalBranch = colLastBuildWidth + colSepWidth + colStatusWidth + 4*2
)

// NewJobList creates a job list view. folderPath="" means root. title is the breadcrumb label.
// branchContext=true renders branch/MR icons instead of type icons (used inside MultiBranch projects).
// username is the authenticated user ID (for mine filter propagation); pass "" if unknown.
// gitUsernames are the git display names used for mine-filter matching in child builds views.
func NewJobList(t theme.Theme, client jenkins.JenkinsClient, store *cache.Store, folderPath, title string, branchContext bool, username string, gitUsernames []string) *JobList {
	ctx, cancel := context.WithCancel(context.Background())
	var columns []component.Column
	if branchContext {
		columns = []component.Column{
			{Title: "JOB", Width: 30},
			{Title: "LAST BUILD", Width: colLastBuildWidth},
			{Title: "", Width: colSepWidth},
			{Title: "STATUS", Width: colStatusWidth},
		}
	} else {
		columns = []component.Column{
			{Title: "JOB", Width: 30},
			{Title: "MAIN", Width: colMainWidth},
			{Title: "", Width: colSepWidth},
			{Title: "LAST BUILD", Width: colLastBuildWidth},
			{Title: "", Width: colSepWidth},
			{Title: "STATUS", Width: colStatusWidth},
		}
	}
	jl := &JobList{
		theme:         t,
		table:         component.NewTable(t, columns),
		client:        client,
		store:         store,
		folderPath:    folderPath,
		title:         title,
		username:      username,
		gitUsernames:  gitUsernames,
		branchContext: branchContext,
		ctx:           ctx,
		cancel:        cancel,
		progressBar:   component.NewProgressBar(t),
	}
	// selectedJob returns the highlighted job, or ok=false for empty rows,
	// containers, or rows without a last build. Used by all behaviors here.
	selectedJob := func() (jenkins.Job, bool) {
		di := jl.dataIndex(jl.table.Cursor())
		if di < 0 || di >= len(jl.jobs) {
			return jenkins.Job{}, false
		}
		j := jl.jobs[di]
		if isContainer(j.Type) || j.LastBuild == nil {
			return jenkins.Job{}, false
		}
		return j, true
	}
	access := func() (NavigationContext, jenkins.Build, bool) {
		j, ok := selectedJob()
		if !ok {
			return NavigationContext{}, jenkins.Build{}, false
		}
		nc := jl.jobNC(j).AtBuild(j.LastBuild.Number)
		return nc, jenkins.Build{
			Number: j.LastBuild.Number,
			Status: jenkins.ColorToBuildStatus(j.Color),
		}, true
	}
	// navigate pushes the test/artifact child on top of a freshly-constructed
	// BuildsView for the selected job so ESC lands the user on the build list
	// they conceptually drilled into.
	navigate := func(child View) tea.Cmd {
		j, ok := selectedJob()
		if !ok {
			return pushTo(child)
		}
		branchNC := jl.jobNC(j)
		bv := NewBuildsView(jl.theme, jl.client, jl.store, branchNC, NewBranchBuildsProvider(jl.client, jl.store, branchNC))
		return func() tea.Msg { return PushViewsMsg{Views: []View{bv, child}} }
	}
	storeFn := func() *cache.Store { return jl.store }
	jl.trigger = newTriggerMixin(t, client, NavigationContext{})
	jl.host.Add(newTestReportBehavior(t, client, storeFn, access, navigate))
	jl.host.Add(newArtifactBehavior(t, client, storeFn, access, navigate))
	jl.host.Add(newCancelBehavior(t, client, access))
	jl.host.Add(newTriggerBehavior(&jl.trigger).WithShortcutGate(func() bool {
		_, ok := selectedJob()
		return ok
	}))
	return jl
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

// rowForJobPath returns the table row index of the job with the given FullPath,
// or 0 when not found or the key is empty. Used to preserve cursor selection
// by identity across refreshes and re-sorts.
func (jl *JobList) rowForJobPath(fullPath string) int {
	if fullPath == "" {
		return 0
	}
	if jl.filteredJobs != nil {
		for row, di := range jl.filteredJobs {
			if di >= 0 && di < len(jl.jobs) && jl.jobs[di].FullPath == fullPath {
				return row
			}
		}
		return 0
	}
	for i, j := range jl.jobs {
		if j.FullPath == fullPath {
			return i
		}
	}
	return 0
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
		if j.RunningCount > 0 {
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
			icon = typeIcon(jl.theme, j.Type)
		}
		name := icon + " " + decoded
		var lastBuild, status string
		if isContainer(j.Type) && j.Color == "" {
			lastBuild = "-"
			status = "-"
		} else {
			// Last build: most recent build across all branches.
			if j.LastAnyBuild != nil {
				lastBuild = relativeTime(j.LastAnyBuild.Timestamp)
			} else {
				lastBuild = "-"
			}

			// Status: live running indicator, or status of the most recently built branch.
			switch {
			case j.RunningCount > 1:
				status = jl.theme.BuildStatus.Running.Render(
					fmt.Sprintf("%s %d running", iconOr(jl.theme.Icons.StatusRunning, "●"), j.RunningCount))
			case j.RunningCount == 1:
				// Show progress bar only when the primary branch is the running one.
				if jenkins.ColorToBuildStatus(j.Color) == jenkins.BuildStatusRunning && j.LastBuild != nil {
					elapsed := time.Since(j.LastBuild.Timestamp)
					status = renderRunningStatus(jl.theme, jl.progressBar, colStatusWidth, elapsed, j.LastBuild.EstimatedDuration)
				} else {
					status = renderStatus(jl.theme, jenkins.BuildStatusRunning)
				}
			default:
				// Use the most-recently-built branch's completed status.
				status = renderStatus(jl.theme, jenkins.ColorToLastCompletedStatus(j.LastAnyColor))
			}
		}

		if jl.branchContext {
			rows = append(rows, component.Row{name, lastBuild, "", status})
		} else {
			main := renderWeatherIcon(jl.theme, jenkins.ColorToLastCompletedStatus(j.Color))
			if isContainer(j.Type) && j.Color == "" {
				main = "-"
			}
			rows = append(rows, component.Row{name, main, "", lastBuild, "", status})
		}
	}
	jl.table.SetRows(rows)
}

// countRunningForJob counts how many builds in the running list belong to a given job.
// For container types (folder, multibranch), child paths are counted as well.
func countRunningForJob(builds []jenkins.UserBuild, jobFullPath string, jobType jenkins.JobType) int {
	count := 0
	prefix := jobFullPath + "/"
	for _, b := range builds {
		if b.JobPath == jobFullPath {
			count++
		} else if (jobType == jenkins.JobTypeMultiBranch || jobType == jenkins.JobTypeFolder) &&
			strings.HasPrefix(b.JobPath, prefix) {
			count++
		}
	}
	return count
}

// maybeFetchSelected fires test-report and artifact fetches for the selected
// non-container job's last build if not already cached.
func (jl *JobList) maybeFetchSelected() tea.Cmd {
	if jl.store == nil {
		return nil
	}
	di := jl.dataIndex(jl.table.Cursor())
	if di < 0 || di >= len(jl.jobs) {
		return nil
	}
	selected := jl.jobs[di]
	if isContainer(selected.Type) || selected.LastBuild == nil {
		return nil
	}
	key := fmt.Sprintf("%s:%d", selected.FullPath, selected.LastBuild.Number)
	if key == jl.lastFetchedKey {
		return nil
	}
	jl.lastFetchedKey = key
	var cmds []tea.Cmd
	if jl.store.TestReports.Get(key) == nil {
		cmds = append(cmds, fetchTestReport(jl.client, jl.store, selected.FullPath, selected.LastBuild.Number))
	}
	if jl.store.Artifacts.Get(key) == nil {
		cmds = append(cmds, fetchArtifacts(jl.client, jl.store, selected.FullPath, selected.LastBuild.Number))
	}
	return tea.Batch(cmds...)
}

func (jl *JobList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := jl.host.HandleMsg(msg); handled {
		return jl, cmd
	}
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		jl.theme = msg.Theme
		jl.table.SetTheme(msg.Theme)
		jl.progressBar.SetTheme(msg.Theme)
		jl.host.SetTheme(msg.Theme)
		jl.populateTable()
		return jl, nil

	case RunningBuildsUpdatedMsg:
		// Update RunningCount for every job inline — no API call needed.
		for i := range jl.jobs {
			jl.jobs[i].RunningCount = countRunningForJob(msg.Builds, jl.jobs[i].FullPath, jl.jobs[i].Type)
		}
		jl.populateTable()
		var cmds []tea.Cmd
		// If a new (unknown) job appeared in this folder, re-fetch the listing.
		if jl.store != nil && jl.store.IsDirtyJobs(jl.folderPath) {
			jl.store.ClearDirtyJobs(jl.folderPath)
			cmds = append(cmds, jl.fetchJobs)
		}
		if jl.hasRunning() && !jl.visualTickActive {
			jl.visualTickActive = true
			cmds = append(cmds, jl.scheduleVisualTick())
		}
		return jl, tea.Batch(cmds...)

	case jobListTickMsg:
		return jl, jl.fetchJobs

	case jobListVisualTickMsg:
		if jl.hasRunning() {
			jl.populateTable()
			return jl, jl.scheduleVisualTick()
		}
		jl.visualTickActive = false

	case CancelBuildResultMsg:
		if msg.Err != nil {
			return jl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		return jl, jl.fetchJobs

	case JobsMsg:
		if msg.Err != nil {
			return jl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		// Preserve selection by identity so a refresh/resort doesn't drift
		// the cursor onto a different job. Exception: when the cursor is
		// pinned at the top row, keep it at row 0 so newly arriving items
		// don't push it downward.
		prevKey := ""
		prevCursor := jl.table.Cursor()
		if prevCursor > 0 {
			if di := jl.dataIndex(prevCursor); di >= 0 && di < len(jl.jobs) {
				prevKey = jl.jobs[di].FullPath
			}
		}
		jl.jobs = msg.Jobs
		if jl.store != nil {
			jl.store.Jobs.Put(jl.folderPath, msg.Jobs)
			jl.store.ClearDirtyJobs(jl.folderPath)
		}
		sort.Slice(jl.jobs, func(i, j int) bool {
			ti := jl.jobs[i].LastAnyBuild
			tj := jl.jobs[j].LastAnyBuild
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
		if prevCursor <= 0 {
			jl.table.SetCursor(0)
		} else {
			jl.table.SetCursor(jl.rowForJobPath(prevKey))
		}
		cmds := []tea.Cmd{jl.scheduleRefresh(), jl.maybeFetchSelected()}
		if jl.hasRunning() && !jl.visualTickActive {
			jl.visualTickActive = true
			cmds = append(cmds, jl.scheduleVisualTick())
		}
		if jl.store != nil && jl.store.Disk != nil {
			fp := jl.folderPath
			jobs := msg.Jobs
			disk := jl.store.Disk
			cmds = append(cmds, func() tea.Msg {
				_ = disk.SaveJobs(fp, jobs)
				return nil
			})
		}
		return jl, tea.Batch(cmds...)

	case tea.KeyMsg:
		if handled, cmd := jl.host.HandleKey(msg); handled {
			return jl, cmd
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
					child := NewJobList(jl.theme, jl.client, jl.store, selected.FullPath, selected.Name, false, jl.username, jl.gitUsernames)
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				case jenkins.JobTypeMultiBranch:
					child := NewJobList(jl.theme, jl.client, jl.store, selected.FullPath, selected.Name, true, jl.username, jl.gitUsernames)
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				default:
					nc := jl.jobNC(selected)
					child := NewBuildsView(jl.theme, jl.client, jl.store, nc, NewBranchBuildsProvider(jl.client, jl.store, nc))
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				}
			}
		case "l":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) && selected.LastBuild != nil {
					nc := jl.jobNC(selected)
					childNC := NavigationContext{
						Level: CtxBuild, FolderPath: nc.FolderPath,
						ProjectName: nc.ProjectName, BranchName: nc.BranchName,
						Build: NavBuildRef{Number: selected.LastBuild.Number},
					}
					bv := NewBuildsView(jl.theme, jl.client, jl.store, nc, NewBranchBuildsProvider(jl.client, jl.store, nc))
					child := NewConsoleView(jl.theme, jl.client, childNC)
					child.store = jl.store
					return jl, func() tea.Msg { return PushViewsMsg{Views: []View{bv, child}} }
				}
			}
		case "s":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) {
					nc := jl.jobNC(selected)
					return jl, func() tea.Msg { return OpenScopedStagesMsg{NC: nc} }
				}
			}
		case "b":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if selected.Type == jenkins.JobTypeMultiBranch {
					nc := jl.jobNC(selected)
					child := NewBuildsView(jl.theme, jl.client, jl.store, nc, NewProjectBuildsProvider(jl.client, jl.store, nc))
					return jl, func() tea.Msg { return PushViewMsg{View: child} }
				}
			}
		case "t":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) {
					lastKnown := 0
					if selected.LastBuild != nil {
						lastKnown = selected.LastBuild.Number
					}
					return jl, jl.trigger.startTriggerFor(jl.jobNC(selected), lastKnown)
				}
			}
		case "d":
			di := jl.dataIndex(jl.table.Cursor())
			if di >= 0 && di < len(jl.jobs) {
				selected := jl.jobs[di]
				if !isContainer(selected.Type) && selected.LastBuild != nil {
					branchNC := jl.jobNC(selected)
					nc := branchNC.AtBuild(selected.LastBuild.Number)
					build := jenkins.Build{Number: selected.LastBuild.Number}
					bv := NewBuildsView(jl.theme, jl.client, jl.store, branchNC, NewBranchBuildsProvider(jl.client, jl.store, branchNC))
					child := NewDescribeView(jl.theme, jl.client, jl.store, nc, build)
					return jl, func() tea.Msg { return PushViewsMsg{Views: []View{bv, child}} }
				}
			}
		}
	}
	return jl, jl.maybeFetchSelected()
}

func (jl *JobList) View() string {
	return jl.table.View()
}

func (jl *JobList) PopupView() string {
	return jl.host.PopupView()
}

func (jl *JobList) Title() string {
	return decodeName(jl.title)
}

func (jl *JobList) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{
		ViewType: "jobs",
		Context:  []component.BreadcrumbPart{{Text: shortName(decodeName(jl.title))}},
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
	di := jl.dataIndex(jl.table.Cursor())
	if di < 0 || di >= len(jl.jobs) {
		return []component.Shortcut{component.Filter("/", "search", false)}
	}
	selected := jl.jobs[di]
	var sc []component.Shortcut
	switch selected.Type {
	case jenkins.JobTypeFolder:
		sc = append(sc, component.Nav("enter", "jobs"))
	case jenkins.JobTypeMultiBranch:
		sc = append(sc, component.Nav("enter", "branches"))
	default:
		sc = append(sc, component.Nav("enter", "builds"))
	}
	if jl.folderPath != "" || jl.branchContext {
		sc = append(sc, component.Nav("esc", "jobs"))
	}
	sc = append(sc, component.Filter("/", "search", false))
	switch selected.Type {
	case jenkins.JobTypeMultiBranch:
		sc = append(sc, component.Nav("b", "all builds"))
	case jenkins.JobTypeFolder:
		// no extra shortcuts for folders
	default:
		if selected.LastBuild != nil {
			sc = append(sc, component.ViewSC("l", "full log", false))
		}
		sc = append(sc, component.ViewSC("s", "stages", false))
		if selected.LastBuild != nil {
			sc = append(sc, component.ViewSC("d", "describe", false))
		}
		// T/A/t/x come from behaviors; their shortcut gates already check
		// container/LastBuild/cache presence and running status.
		sc = jl.host.AppendShortcuts(sc)
	}
	return sc
}

func (jl *JobList) HasPopup() bool {
	return jl.host.HasPopup()
}

func (jl *JobList) Close() error {
	if jl.cancel != nil {
		jl.cancel()
	}
	return nil
}

func (jl *JobList) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	return folderParentJobList(t, c, s, jl.folderPath, jl.username, jl.gitUsernames)
}

func (jl *JobList) ScrollInfo() ScrollInfo {
	return ScrollInfo{Offset: jl.table.ScrollOffset(), TotalLines: jl.table.TotalRows(), ViewHeight: jl.table.ContentHeight()}
}

func (jl *JobList) SetSize(width, height int) {
	jl.width = width
	jl.height = height
	fixedTotal := colFixedTotalNormal
	if jl.branchContext {
		fixedTotal = colFixedTotalBranch
	}
	nameWidth := width - fixedTotal
	if nameWidth < 10 {
		nameWidth = 10
	}
	jl.table.SetColumnWidth(0, nameWidth)
	jl.table.SetSize(width, height)
	jl.host.SetSize(width, height-6)
}

// NC returns the NavigationContext for this job list. Used by app.go to
// determine the correct builds scope when the :builds command is run.
func (jl *JobList) NC() NavigationContext {
	if jl.branchContext {
		// folderPath = "folder/project" — split into FolderPath + ProjectName.
		folderPath := ""
		if idx := strings.LastIndex(jl.folderPath, "/"); idx >= 0 {
			folderPath = jl.folderPath[:idx]
		}
		return NavigationContext{
			Level:       CtxProject,
			FolderPath:  folderPath,
			ProjectName: jl.title,
			Username:    jl.username,
		}
	}
	if jl.folderPath == "" {
		return NavigationContext{Level: CtxRoot, Username: jl.username}
	}
	return NavigationContext{
		Level:      CtxFolder,
		FolderPath: jl.folderPath,
		Username:   jl.username,
	}
}

// jobNC constructs a NavigationContext for a job in this list.
// For branchContext lists, the parent folder path is split from jl.folderPath
// and the branch name comes from the selected job.
func (jl *JobList) jobNC(selected jenkins.Job) NavigationContext {
	if jl.branchContext {
		// jl.folderPath = "folder/project"; split into FolderPath + ProjectName.
		folderPath := ""
		if idx := strings.LastIndex(jl.folderPath, "/"); idx >= 0 {
			folderPath = jl.folderPath[:idx]
		}
		return NavigationContext{
			Level:        CtxBranch,
			FolderPath:   folderPath,
			ProjectName:  jl.title,
			BranchName:   selected.Name,
			Username:     jl.username,
			GitUsernames: jl.gitUsernames,
		}
	}
	return NavigationContext{
		Level:        CtxProject,
		FolderPath:   jl.folderPath,
		ProjectName:  selected.Name,
		Username:     jl.username,
		GitUsernames: jl.gitUsernames,
	}
}

// decodeName URL-decodes a job name (branch names like "feature%2Fbranch" → "feature/branch").
func decodeName(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}

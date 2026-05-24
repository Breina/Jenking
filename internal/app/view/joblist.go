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
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// JobsMsg carries fetched jobs to the view.
type JobsMsg struct {
	Jobs []jmodel.Job
	Err  error
}

// typeIcon returns a single icon character for the job type, respecting theme overrides.
func typeIcon(th theme.Theme, t jmodel.JobType) string {
	switch t {
	case jmodel.JobTypeFolder:
		return iconOr(th.Icons.TypeFolder, "▸")
	case jmodel.JobTypePipeline:
		return iconOr(th.Icons.TypePipeline, "⚙")
	case jmodel.JobTypeMultiBranch:
		return iconOr(th.Icons.TypeMultiBranch, "⎇")
	case jmodel.JobTypeFreeStyle:
		return iconOr(th.Icons.TypeFreeStyle, "⊙")
	default:
		return "?"
	}
}

// isContainer returns true for job types that don't have their own build status or last build.
func isContainer(t jmodel.JobType) bool {
	return t == jmodel.JobTypeFolder || t == jmodel.JobTypeMultiBranch
}

type jobListTickMsg struct{}
type jobListVisualTickMsg struct{}

// JobList is a view that lists jobs under a given folder path.
// Cross-cutting concerns (trigger, cancel, test report, artifact) are owned by
// behaviors registered on host with a row-aware access closure.
type JobList struct {
	theme            theme.Theme
	table            component.Table
	client           jmodel.JenkinsClient
	store            *cache.Store
	folderPath       string
	title            string
	jobs             []jmodel.Job
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
	showDisabled     bool  // when true, disabled jobs are shown (default: true)
	showOnlyRunning  bool  // when true, only jobs with running builds are shown
	lastFetchedKey   string
	trigger          triggerMixin
	host             widget.BehaviorHost
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
func NewJobList(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, folderPath, title string, branchContext bool, username string, gitUsernames []string) *JobList {
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
		showDisabled:  true,
	}
	// selectedJob returns the highlighted job, or ok=false for empty rows,
	// containers, or rows without a last build. Used by all behaviors here.
	selectedJob := func() (jmodel.Job, bool) {
		di := jl.dataIndex(jl.table.Cursor())
		if di < 0 || di >= len(jl.jobs) {
			return jmodel.Job{}, false
		}
		j := jl.jobs[di]
		if isContainer(j.Type) || j.LastBuild == nil {
			return jmodel.Job{}, false
		}
		return j, true
	}
	access := func() (NavigationContext, jmodel.Build, bool) {
		j, ok := selectedJob()
		if !ok {
			return NavigationContext{}, jmodel.Build{}, false
		}
		nc := jl.jobNC(j).AtBuild(j.LastBuild.Number)
		return nc, jmodel.Build{
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
	// Navigation shortcuts (l/s/d/b). Each accessor folds the selection lookup
	// + gating into its closure; host.HandleKey consumes the key before the
	// view's own switch sees it, and host.AppendShortcuts contributes the
	// header entry — gated on accessor ok so unavailable actions disappear.
	// `l` and `d` reuse `selectedJob` (requires non-container + LastBuild);
	// `s` and `b` use looser lookups (selectedNonContainer / selectedMultibranch).
	jl.host.Add(widget.NewNavBehavior("l", "full log", func() (tea.Cmd, bool) {
		j, ok := selectedJob()
		if !ok {
			return nil, false
		}
		return jl.openLogCmd(j), true
	}).WithRank(rankViewFullLog))
	jl.host.Add(widget.NewNavBehavior("d", "describe", func() (tea.Cmd, bool) {
		j, ok := selectedJob()
		if !ok {
			return nil, false
		}
		return jl.openDescribeCmd(j), true
	}).WithRank(rankViewDescribe))
	jl.host.Add(widget.NewNavBehavior("s", "stages", func() (tea.Cmd, bool) {
		j, ok := jl.selectedNonContainer()
		if !ok {
			return nil, false
		}
		nc := jl.jobNC(j)
		return func() tea.Msg { return OpenScopedStagesMsg{NC: nc} }, true
	}).WithRank(rankViewStages))
	jl.host.Add(widget.NewNavBehavior("b", "all builds", func() (tea.Cmd, bool) {
		j, ok := jl.selectedMultibranch()
		if !ok {
			return nil, false
		}
		nc := jl.jobNC(j)
		child := NewBuildsView(jl.theme, jl.client, jl.store, nc, NewProjectBuildsProvider(jl.client, jl.store, nc))
		return func() tea.Msg { return PushViewMsg{View: child} }, true
	}).WithRank(rankViewAllBuilds))
	return jl
}

// selectedNonContainer returns the job under the cursor, gated on the row
// being a real job (not a folder or multibranch project). Used by the stages
// navigation behavior which works on any pipeline/freestyle row even without
// a LastBuild — selectedJob is stricter and excludes LastBuild==nil.
func (jl *JobList) selectedNonContainer() (jmodel.Job, bool) {
	di := jl.dataIndex(jl.table.Cursor())
	if di < 0 || di >= len(jl.jobs) {
		return jmodel.Job{}, false
	}
	j := jl.jobs[di]
	if isContainer(j.Type) {
		return jmodel.Job{}, false
	}
	return j, true
}

// selectedMultibranch returns the job under the cursor when it's a multibranch
// project; used by the "all builds" (b) navigation behavior.
func (jl *JobList) selectedMultibranch() (jmodel.Job, bool) {
	di := jl.dataIndex(jl.table.Cursor())
	if di < 0 || di >= len(jl.jobs) {
		return jmodel.Job{}, false
	}
	j := jl.jobs[di]
	if j.Type != jmodel.JobTypeMultiBranch {
		return jmodel.Job{}, false
	}
	return j, true
}

// openLogCmd builds the two-view push (BuildsView + ConsoleView) so ESC lands
// on the build list the user conceptually drilled into.
func (jl *JobList) openLogCmd(j jmodel.Job) tea.Cmd {
	nc := jl.jobNC(j)
	childNC := nc.AtBuild(j.LastBuild.Number)
	bv := NewBuildsView(jl.theme, jl.client, jl.store, nc, NewBranchBuildsProvider(jl.client, jl.store, nc))
	child := NewConsoleView(jl.theme, jl.client, childNC)
	child.store = jl.store
	return func() tea.Msg { return PushViewsMsg{Views: []View{bv, child}} }
}

// openDescribeCmd builds the two-view push (BuildsView + DescribeView).
func (jl *JobList) openDescribeCmd(j jmodel.Job) tea.Cmd {
	branchNC := jl.jobNC(j)
	nc := branchNC.AtBuild(j.LastBuild.Number)
	build := jmodel.Build{Number: j.LastBuild.Number}
	bv := NewBuildsView(jl.theme, jl.client, jl.store, branchNC, NewBranchBuildsProvider(jl.client, jl.store, branchNC))
	child := NewDescribeView(jl.theme, jl.client, jl.store, nc, build)
	return func() tea.Msg { return PushViewsMsg{Views: []View{bv, child}} }
}

func (jl *JobList) ApplySearch(pattern string) tea.Cmd {
	jl.searchQuery = pattern
	jl.searchRe = widget.CompileSearchRegex(pattern)
	jl.populateTable()
	jl.table.SetCursor(0)
	return nil
}

func (jl *JobList) SearchQuery() string {
	return jl.searchQuery
}

// selectedJobPath returns the FullPath of the currently highlighted job, or "".
func (jl *JobList) selectedJobPath() string {
	di := jl.dataIndex(jl.table.Cursor())
	if di >= 0 && di < len(jl.jobs) {
		return jl.jobs[di].FullPath
	}
	return ""
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
		if !jl.jobPassesFilter(j) {
			continue
		}
		jl.filteredJobs = append(jl.filteredJobs, i)
		rows = append(rows, jl.buildJobRow(j))
	}
	jl.table.SetRows(rows)
}

// jobPassesFilter applies the disabled/running/search filters in one place.
func (jl *JobList) jobPassesFilter(j jmodel.Job) bool {
	if !jl.showDisabled && j.Disabled {
		return false
	}
	if jl.showOnlyRunning && j.RunningCount == 0 {
		return false
	}
	if jl.searchRe != nil && !jl.searchRe.MatchString(decodeName(j.Name)) {
		return false
	}
	return true
}

// jobNameCell composes the leading icon + decoded job name.
func (jl *JobList) jobNameCell(j jmodel.Job) string {
	decoded := decodeName(j.Name)
	if jl.branchContext {
		return branchIcon(decoded) + " " + decoded
	}
	return typeIcon(jl.theme, j.Type) + " " + decoded
}

// jobStatusCells returns (lastBuild, status) text for a job, accounting for
// empty containers and live running indicators.
func (jl *JobList) jobStatusCells(j jmodel.Job) (string, string) {
	if isContainer(j.Type) && j.Color == "" {
		return "-", "-"
	}
	lastBuild := "-"
	if j.LastAnyBuild != nil {
		lastBuild = relativeTime(j.LastAnyBuild.Timestamp)
	}
	return lastBuild, jl.jobStatusText(j)
}

// jobStatusText picks the appropriate status rendering based on running count.
func (jl *JobList) jobStatusText(j jmodel.Job) string {
	switch {
	case j.RunningCount > 1:
		return jl.theme.BuildStatus.Running.Render(
			fmt.Sprintf("%s %d running", iconOr(jl.theme.Icons.StatusRunning, "●"), j.RunningCount))
	case j.RunningCount == 1:
		if jenkins.ColorToBuildStatus(j.Color) == jmodel.BuildStatusRunning && j.LastBuild != nil {
			elapsed := time.Since(j.LastBuild.Timestamp)
			return renderRunningStatus(jl.theme, jl.progressBar, colStatusWidth, elapsed, j.LastBuild.EstimatedDuration)
		}
		return renderStatus(jl.theme, jmodel.BuildStatusRunning)
	default:
		return renderStatus(jl.theme, jenkins.ColorToLastCompletedStatus(j.LastAnyColor))
	}
}

// buildJobRow assembles a final table row, applying dimming for disabled jobs
// and the branchContext column shape.
func (jl *JobList) buildJobRow(j jmodel.Job) component.Row {
	name := jl.jobNameCell(j)
	lastBuild, status := jl.jobStatusCells(j)
	dim := jl.theme.Breadcrumb.Paren

	if jl.branchContext {
		if j.Disabled {
			return component.Row{dim.Render(name), dim.Render(lastBuild), "", dim.Render(status)}
		}
		return component.Row{name, lastBuild, "", status}
	}

	main := jl.jobWeatherCell(j)
	if j.Disabled {
		return component.Row{dim.Render(name), main, "", dim.Render(lastBuild), "", dim.Render(status)}
	}
	return component.Row{name, main, "", lastBuild, "", status}
}

// jobWeatherCell renders the weather/disabled icon shown in non-branch context.
func (jl *JobList) jobWeatherCell(j jmodel.Job) string {
	if j.Disabled {
		return jl.theme.Breadcrumb.Paren.Render("⊘")
	}
	if isContainer(j.Type) && j.Color == "" {
		return "-"
	}
	return renderWeatherIcon(jl.theme, jenkins.ColorToLastCompletedStatus(j.Color))
}

// countRunningForJob counts how many builds in the running list belong to a given job.
// For container types (folder, multibranch), child paths are counted as well.
func countRunningForJob(builds []jmodel.UserBuild, jobFullPath string, jobType jmodel.JobType) int {
	count := 0
	prefix := jobFullPath + "/"
	for _, b := range builds {
		if b.JobPath == jobFullPath {
			count++
		} else if (jobType == jmodel.JobTypeMultiBranch || jobType == jmodel.JobTypeFolder) &&
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
		return jl, jl.handleThemeChanged(msg)
	case RunningBuildsUpdatedMsg:
		return jl, jl.handleRunningBuildsUpdated(msg)
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
			return jl, func() tea.Msg { return ErrorMsg(msg) }
		}
		return jl, jl.fetchJobs
	case JobsMsg:
		return jl, jl.handleJobsMsg(msg)
	case tea.KeyMsg:
		if cmd, returned := jl.handleKeyMsg(msg); returned {
			return jl, cmd
		}
	}
	return jl, jl.maybeFetchSelected()
}

func (jl *JobList) handleThemeChanged(msg ThemeChangedMsg) tea.Cmd {
	jl.theme = msg.Theme
	jl.table.SetTheme(msg.Theme)
	jl.progressBar.SetTheme(msg.Theme)
	jl.host.SetTheme(msg.Theme)
	jl.populateTable()
	return nil
}

func (jl *JobList) handleRunningBuildsUpdated(msg RunningBuildsUpdatedMsg) tea.Cmd {
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
	return tea.Batch(cmds...)
}

func (jl *JobList) handleJobsMsg(msg JobsMsg) tea.Cmd {
	if msg.Err != nil {
		return func() tea.Msg { return ErrorMsg{Err: msg.Err} }
	}
	// Preserve selection by identity so a refresh/resort doesn't drift the
	// cursor onto a different job. Exception: when the cursor is pinned at
	// the top row, keep it at row 0 so newly arriving items don't push it
	// downward.
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
	sortJobsByLastBuild(jl.jobs)
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
	return tea.Batch(cmds...)
}

// sortJobsByLastBuild orders jobs by most recent build first; jobs with no
// build sink to the bottom (stable order among themselves).
func sortJobsByLastBuild(jobs []jmodel.Job) {
	sort.Slice(jobs, func(i, j int) bool {
		ti := jobs[i].LastAnyBuild
		tj := jobs[j].LastAnyBuild
		if ti == nil {
			return false
		}
		if tj == nil {
			return true
		}
		return ti.Timestamp.After(tj.Timestamp)
	})
}

// handleKeyMsg processes view-local keys after the behavior host has had its
// chance to consume the keypress. Returns (cmd, true) when the key produced a
// definitive action; (nil, false) means the key was a no-op or filter toggle
// and the caller should fall through to the post-key selected-fetch.
func (jl *JobList) handleKeyMsg(msg tea.KeyMsg) (tea.Cmd, bool) {
	if handled, cmd := jl.host.HandleKey(msg); handled {
		return cmd, true
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
		return jl.openSelectedJobUnderCursor()
	case "t":
		return jl.triggerSelectedJobUnderCursor()
	case "D":
		jl.toggleFilter(&jl.showDisabled)
	case "r":
		jl.toggleFilter(&jl.showOnlyRunning)
	}
	return nil, false
}

// openSelectedJobUnderCursor handles enter: drill into folder/multibranch/
// builds depending on the job type. Stays inline because each branch
// constructs a different child view with different constructor args — not a
// reusable navigation pattern.
func (jl *JobList) openSelectedJobUnderCursor() (tea.Cmd, bool) {
	di := jl.dataIndex(jl.table.Cursor())
	if di < 0 || di >= len(jl.jobs) {
		return nil, false
	}
	selected := jl.jobs[di]
	switch selected.Type {
	case jmodel.JobTypeFolder:
		child := NewJobList(jl.theme, jl.client, jl.store, selected.FullPath, selected.Name, false, jl.username, jl.gitUsernames)
		return func() tea.Msg { return PushViewMsg{View: child} }, true
	case jmodel.JobTypeMultiBranch:
		child := NewJobList(jl.theme, jl.client, jl.store, selected.FullPath, selected.Name, true, jl.username, jl.gitUsernames)
		return func() tea.Msg { return PushViewMsg{View: child} }, true
	default:
		nc := jl.jobNC(selected)
		child := NewBuildsView(jl.theme, jl.client, jl.store, nc, NewBranchBuildsProvider(jl.client, jl.store, nc))
		return func() tea.Msg { return PushViewMsg{View: child} }, true
	}
}

// triggerSelectedJobUnderCursor handles "t". The behavior host's
// triggerBehavior owns the popup lifecycle, but the initial entry-point with
// per-row lastKnown lookup stays view-local for now.
func (jl *JobList) triggerSelectedJobUnderCursor() (tea.Cmd, bool) {
	di := jl.dataIndex(jl.table.Cursor())
	if di < 0 || di >= len(jl.jobs) {
		return nil, false
	}
	selected := jl.jobs[di]
	if isContainer(selected.Type) {
		return nil, false
	}
	lastKnown := 0
	if selected.LastBuild != nil {
		lastKnown = selected.LastBuild.Number
	}
	return jl.trigger.startTriggerFor(jl.jobNC(selected), lastKnown), true
}

// toggleFilter flips a filter flag and restores the cursor to the previously
// selected job (or the closest surviving row if it was filtered out).
func (jl *JobList) toggleFilter(flag *bool) {
	prevKey := jl.selectedJobPath()
	*flag = !*flag
	jl.populateTable()
	jl.table.SetCursor(jl.rowForJobPath(prevKey))
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
	case jmodel.JobTypeFolder:
		sc = append(sc, component.Nav("enter", "jobs"))
	case jmodel.JobTypeMultiBranch:
		sc = append(sc, component.Nav("enter", "branches"))
	default:
		sc = append(sc, component.Nav("enter", "builds"))
	}
	if jl.folderPath != "" || jl.branchContext {
		sc = append(sc, component.Nav("esc", "jobs"))
	}
	sc = append(sc, component.Filter("/", "search", false))
	sc = append(sc, component.Filter("D", "disabled", jl.showDisabled))
	sc = append(sc, component.Filter("r", "running", jl.showOnlyRunning))
	// l/s/d/b, T/A/t/x all come from behaviors; their shortcut gates check
	// container/LastBuild/cache presence and running status per-row. Folders
	// produce an empty append because no behavior advertises for them.
	sc = jl.host.AppendShortcuts(sc)
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

func (jl *JobList) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	return folderParentJobList(t, c, s, jl.folderPath, jl.username, jl.gitUsernames)
}

func (jl *JobList) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: jl.table.ScrollOffset(), TotalLines: jl.table.TotalRows(), ViewHeight: jl.table.ContentHeight()}
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
func (jl *JobList) jobNC(selected jmodel.Job) NavigationContext {
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

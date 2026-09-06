package view

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// ViewsList is the root of the navigation: the Jenkins views defined on the
// controller, each of which opens the job list filtered by that view. The
// built-in "all" view is the unfiltered job list the app used to open on.
//
// Counts are filled in lazily, one view at a time, and only while this list is
// on screen — a controller with many views would otherwise turn its root into
// a burst of requests. Names render immediately from the single views[] call.
type ViewsList struct {
	theme        theme.Theme
	client       jmodel.JenkinsClient
	store        *cache.Store
	table        component.Table
	views        []jmodel.JenkinsView
	ownerPath    string // container whose views are listed; "" = root
	counts       map[string]viewCounts
	username     string
	gitUsernames []string
	width        int
	height       int
	searchQuery  string
	filtered     []int // table row index → vl.views index; nil = unfiltered
	// autoOpen is the view to jump straight into once the list has loaded (the
	// remembered view from the last session). Cleared after it fires, so
	// returning here with ESC stays on the list.
	autoOpen string
	ctx      context.Context
	cancel   context.CancelFunc
}

// viewCounts is the lazily-fetched summary of a view's job set.
type viewCounts struct {
	total   int
	running int
	failing int
}

// ViewsMsg carries the fetched view list.
type ViewsMsg struct {
	Views []jmodel.JenkinsView
	Err   error
}

// viewCountMsg carries one view's lazily-fetched counts, plus the index of the
// next view to enrich so the chain advances one request at a time.
type viewCountMsg struct {
	name   string
	counts viewCounts
	next   int
}

const (
	colViewCountWidth = 7
	// colViewFixedTotal: VIEW | JOBS | RUNNING | FAILING  (4 cols × 2 pad)
	colViewFixedTotal = 3*colViewCountWidth + 4*2
)

// NewViewsListAt creates the root views list, jumping straight into the named
// view once loaded. An empty or unknown name just leaves the user on the list.
func NewViewsListAt(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, username string, gitUsernames []string, autoOpen string) *ViewsList {
	vl := NewViewsList(t, client, store, username, gitUsernames)
	vl.autoOpen = autoOpen
	return vl
}

// NewFolderViewsList creates the views list of a folder. Personal views are
// root-only, so a folder list shows exactly the views that folder defines.
func NewFolderViewsList(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, folder, username string, gitUsernames []string) *ViewsList {
	vl := NewViewsList(t, client, store, username, gitUsernames)
	vl.ownerPath = folder
	return vl
}

// NewViewsList creates the root views list.
func NewViewsList(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, username string, gitUsernames []string) *ViewsList {
	ctx, cancel := context.WithCancel(context.Background())
	return &ViewsList{
		theme:  t,
		client: client,
		store:  store,
		table: component.NewTable(t, []component.Column{
			{Title: "VIEW", Width: 30},
			{Title: "JOBS", Width: colViewCountWidth},
			{Title: "RUNNING", Width: colViewCountWidth},
			{Title: "FAILING", Width: colViewCountWidth},
		}),
		counts:       map[string]viewCounts{},
		username:     username,
		gitUsernames: gitUsernames,
		ctx:          ctx,
		cancel:       cancel,
	}
}

func (vl *ViewsList) Init() tea.Cmd { return vl.fetchViews }

// fetchViews loads the controller's views followed by the user's personal
// ones. A my-views collection that does not exist is not an error.
func (vl *ViewsList) fetchViews() tea.Msg {
	views, err := vl.client.ListViews(vl.ctx, vl.ownerPath)
	if vl.ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return ViewsMsg{Err: err}
	}
	if vl.ownerPath != "" {
		return ViewsMsg{Views: views} // personal views hang off the user, not a folder
	}
	mine, myErr := vl.client.ListMyViews(vl.ctx, vl.username)
	if vl.ctx.Err() != nil {
		return nil
	}
	if myErr == nil {
		views = append(views, mine...)
	}
	return ViewsMsg{Views: views}
}

// fetchCount enriches views[i] with its job counts and schedules the next one.
func (vl *ViewsList) fetchCount(i int) tea.Cmd {
	if i < 0 || i >= len(vl.views) {
		return nil
	}
	v := vl.views[i]
	return func() tea.Msg {
		jobs, err := vl.client.ListViewJobs(vl.ctx, v)
		if vl.ctx.Err() != nil {
			return nil
		}
		// A view we cannot read still belongs in the list; it just shows no
		// counts, same as one that has not been reached yet.
		if err != nil {
			return viewCountMsg{name: v.Name, next: i + 1}
		}
		return viewCountMsg{name: v.Name, counts: countJobs(jobs), next: i + 1}
	}
}

// countJobs summarises a view's job set for the count columns.
func countJobs(jobs []jmodel.Job) viewCounts {
	c := viewCounts{total: len(jobs)}
	for _, j := range jobs {
		c.running += j.RunningCount
		color := j.Color
		if j.LastAnyColor != "" {
			color = j.LastAnyColor
		}
		if jenkins.ColorToBuildStatus(color) == jmodel.BuildStatusFailed {
			c.failing++
		}
	}
	return c
}

func (vl *ViewsList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ViewsMsg:
		if msg.Err != nil {
			return vl, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		vl.views = msg.Views
		if vl.ownerPath == "" {
			rememberViewNames(msg.Views)
		}
		vl.populateTable()
		if name := vl.autoOpen; name != "" {
			vl.autoOpen = ""
			if v, ok := vl.Find(name); ok {
				// Leaving for the job list immediately: don't start the count
				// chain, which would then run behind a screen nobody is looking at.
				vl.table.SetCursor(vl.rowForView(v.Name))
				return vl, OpenViewCmd(vl.theme, vl.client, vl.store, v, vl.username, vl.gitUsernames)
			}
		}
		return vl, vl.fetchCount(0)
	case viewCountMsg:
		vl.counts[msg.name] = msg.counts
		vl.populateTable()
		return vl, vl.fetchCount(msg.next)
	case ThemeChangedMsg:
		vl.theme = msg.Theme
		vl.table.SetTheme(msg.Theme)
		vl.populateTable()
	case tea.KeyMsg:
		return vl, vl.handleKey(msg)
	}
	return vl, nil
}

func (vl *ViewsList) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		vl.table.MoveUp()
	case "down", "j":
		vl.table.MoveDown()
	case "pgup":
		vl.table.PageUp()
	case "pgdown":
		vl.table.PageDown()
	case "home":
		vl.table.Home()
	case "end":
		vl.table.End()
	case "enter":
		if v, ok := vl.Selected(); ok {
			return OpenViewCmd(vl.theme, vl.client, vl.store, v, vl.username, vl.gitUsernames)
		}
	}
	return nil
}

// OpenViewCmd opens a view's job list and records it as the last used view for
// this Jenkins context, so the next launch starts where the user left off.
func OpenViewCmd(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store, v jmodel.JenkinsView, username string, gitUsernames []string) tea.Cmd {
	child := NewViewJobList(t, c, s, v, username, gitUsernames)
	return tea.Batch(
		func() tea.Msg { return PushViewMsg{View: child} },
		func() tea.Msg { return ViewSelectedMsg{View: v} },
	)
}

// Selected returns the view under the cursor.
func (vl *ViewsList) Selected() (jmodel.JenkinsView, bool) {
	i := vl.dataIndex(vl.table.Cursor())
	if i < 0 || i >= len(vl.views) {
		return jmodel.JenkinsView{}, false
	}
	return vl.views[i], true
}

// Find returns the view with the given name (case-insensitive on the raw
// name), once the list has loaded.
func (vl *ViewsList) Find(name string) (jmodel.JenkinsView, bool) {
	return FindView(vl.views, name)
}

// FindView returns the view with the given name, preferring an exact match and
// falling back to a case-insensitive one.
func FindView(views []jmodel.JenkinsView, name string) (jmodel.JenkinsView, bool) {
	for _, v := range views {
		if v.Name == name {
			return v, true
		}
	}
	for _, v := range views {
		if strings.EqualFold(v.Name, name) {
			return v, true
		}
	}
	return jmodel.JenkinsView{}, false
}

// rowForView returns the table row showing the named view, or 0.
func (vl *ViewsList) rowForView(name string) int {
	for row := 0; row < vl.ItemCount(); row++ {
		if i := vl.dataIndex(row); i >= 0 && i < len(vl.views) && vl.views[i].Name == name {
			return row
		}
	}
	return 0
}

func (vl *ViewsList) dataIndex(row int) int {
	if vl.filtered != nil && row >= 0 && row < len(vl.filtered) {
		return vl.filtered[row]
	}
	return row
}

func (vl *ViewsList) populateTable() {
	re := widget.CompileSearchRegex(vl.searchQuery)
	vl.filtered = nil
	rows := make([]component.Row, 0, len(vl.views))
	for i, v := range vl.views {
		if re != nil && !re.MatchString(v.DisplayName()) {
			continue
		}
		vl.filtered = append(vl.filtered, i)
		rows = append(rows, vl.buildRow(v))
	}
	vl.table.SetRows(rows)
}

func (vl *ViewsList) buildRow(v jmodel.JenkinsView) component.Row {
	c, known := vl.counts[v.Name]
	return component.Row{
		vl.viewNameCell(v),
		countCell(c.total, known),
		countCell(c.running, known && c.running > 0),
		countCell(c.failing, known && c.failing > 0),
	}
}

// viewNameCell renders the view name with its personal/primary markers.
func (vl *ViewsList) viewNameCell(v jmodel.JenkinsView) string {
	name := v.DisplayName()
	if v.Personal {
		// Personal views live under the user, not the controller; the marker
		// keeps a my-view from reading as a shared one of the same name.
		name = "@" + name
	}
	if v.IsPrimary {
		name += " " + vl.theme.Breadcrumb.Paren.Render("(primary)")
	}
	return name
}

// countCell renders a count, or a placeholder while it is still unknown.
func countCell(n int, known bool) string {
	if !known {
		return "·"
	}
	return strconv.Itoa(n)
}

func (vl *ViewsList) View() string { return vl.table.View() }

func (vl *ViewsList) Title() string { return "Views" }

func (vl *ViewsList) Breadcrumb() BreadcrumbSegment {
	seg := BreadcrumbSegment{ViewType: "views"}
	if vl.ownerPath != "" {
		seg.Context = []component.BreadcrumbPart{{Text: shortName(decodeName(vl.ownerPath))}}
	}
	return seg
}

// ParentView sends ESC from a folder's views list back to that folder's job
// list. The root views list has no parent — it is the root.
func (vl *ViewsList) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	if vl.ownerPath == "" {
		return nil
	}
	return NewJobList(t, c, s, vl.ownerPath, shortName(vl.ownerPath), false, vl.username, vl.gitUsernames)
}

func (vl *ViewsList) ItemCount() int {
	if vl.filtered != nil {
		return len(vl.filtered)
	}
	return len(vl.views)
}

func (vl *ViewsList) Commands() []command.Command { return nil }

func (vl *ViewsList) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{}
	if _, ok := vl.Selected(); ok {
		sc = append(sc, component.Nav("enter", "jobs"))
	}
	return append(sc, component.Filter("/", "search", false))
}

func (vl *ViewsList) ApplySearch(pattern string) tea.Cmd {
	vl.searchQuery = pattern
	vl.populateTable()
	vl.table.SetCursor(0)
	return nil
}

func (vl *ViewsList) SearchQuery() string { return vl.searchQuery }

func (vl *ViewsList) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: vl.table.ScrollOffset(), TotalLines: vl.table.TotalRows(), ViewHeight: vl.table.ContentHeight()}
}

func (vl *ViewsList) SetSize(width, height int) {
	vl.width, vl.height = width, height
	nameWidth := width - colViewFixedTotal
	if nameWidth < 10 {
		nameWidth = 10
	}
	vl.table.SetColumnWidth(0, nameWidth)
	vl.table.SetSize(width, height)
}

func (vl *ViewsList) Close() error {
	if vl.cancel != nil {
		vl.cancel()
	}
	return nil
}

// The known-views registry backs ":view <name>" — its argument suggestions and
// its ability to open a view without a round-trip. It is filled whenever the
// views list loads; before that first load it is simply empty, and ":view"
// falls back to resolving the name against a fresh fetch.
var (
	knownViewsMu sync.RWMutex
	knownViews   []jmodel.JenkinsView
)

func rememberViewNames(views []jmodel.JenkinsView) {
	knownViewsMu.Lock()
	defer knownViewsMu.Unlock()
	knownViews = append(knownViews[:0], views...)
}

// LookupView returns a known view by name, once the views list has loaded.
func LookupView(name string) (jmodel.JenkinsView, bool) {
	knownViewsMu.RLock()
	defer knownViewsMu.RUnlock()
	return FindView(knownViews, name)
}

// ViewNameSuggest completes view names for the ":view" command.
func ViewNameSuggest(prefix string) []string {
	knownViewsMu.RLock()
	defer knownViewsMu.RUnlock()
	var out []string
	for _, v := range knownViews {
		if v.Name != prefix && strings.HasPrefix(strings.ToLower(v.Name), strings.ToLower(prefix)) {
			out = append(out, v.Name)
		}
	}
	sort.Strings(out)
	return out
}

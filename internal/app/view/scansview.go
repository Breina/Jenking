package view

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

type scansTickMsg struct{}

// ScansView lists the branch-indexing scans waiting in the queue, scoped to the
// current navigation context. It is the scan-side sibling of the running-builds
// view: a "what is happening now" list, not a history — Jenkins keeps no record
// of past scans beyond the latest log.
//
// Rows come from the shared queue snapshot the engine already polls, so the
// view costs no requests of its own. A scan that has left the queue is running
// or done; it disappears from here, and its log stays reachable from the job
// list (l on the container row).
type ScansView struct {
	BaseView
	table component.Table
	items []jmodel.QueueItem
}

const (
	colScanStateWidth   = 12
	colScanWaitingWidth = 9
)

// NewScansView creates a scans list scoped to nc. The scope follows the same
// prefix semantics as the builds views, so a project-level scope matches that
// project's own scan (the project path *is* the scan's job path).
func NewScansView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext) *ScansView {
	sv := &ScansView{
		BaseView: NewBaseView(t, client, store, nc, nc.Level),
		table: component.NewTable(t, []component.Column{
			{Title: "PROJECT", Width: 40},
			{Title: "STATE", Width: colScanStateWidth},
			{Title: "WAITING", Width: colScanWaitingWidth},
			{Title: "WHY", Width: 40},
		}),
	}
	sv.refresh()
	return sv
}

func (sv *ScansView) Init() tea.Cmd { return sv.tick() }

func (sv *ScansView) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return scansTickMsg{} })
}

// scopePath is the container the view is scoped to: its own scan and every
// scan nested under it are listed. A branch contributes nothing of its own
// (branches never scan), so the branch segment is dropped and the enclosing
// project used instead. An empty path means the whole controller.
func (sv *ScansView) scopePath() string {
	switch {
	case sv.nc.ProjectName != "":
		return sv.nc.ClipTo(CtxProject).JobPath()
	default:
		return sv.nc.FolderPath
	}
}

func (sv *ScansView) refresh() {
	if sv.store == nil || sv.store.Queue == nil {
		return
	}
	sv.items = sv.store.Queue.ScansInScope(sv.scopePath())
	rows := make([]component.Row, 0, len(sv.items))
	for _, it := range sv.items {
		rows = append(rows, component.Row{
			decodePath(it.JobPath),
			renderQueueStatus(sv.theme, queueSubState(it)),
			relativeSince(it.InQueueSince),
			it.Why,
		})
	}
	sv.table.SetRows(rows)
}

// selected returns the scan under the cursor.
func (sv *ScansView) selected() (jmodel.QueueItem, bool) {
	i := sv.table.Cursor()
	if i < 0 || i >= len(sv.items) {
		return jmodel.QueueItem{}, false
	}
	return sv.items[i], true
}

func (sv *ScansView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case scansTickMsg:
		sv.refresh()
		return sv, sv.tick()
	case ThemeChangedMsg:
		sv.theme = msg.Theme
		sv.table.SetTheme(msg.Theme)
		sv.refresh()
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			sv.table.MoveUp()
		case "down", "j":
			sv.table.MoveDown()
		case "pgup":
			sv.table.PageUp()
		case "pgdown":
			sv.table.PageDown()
		case "home":
			sv.table.Home()
		case "end":
			sv.table.End()
		case "enter":
			// The scan under the cursor is still queued, so its log is the
			// *previous* scan's until Jenkins starts this one; the scan log view
			// waits out that gap itself rather than showing stale output.
			if it, ok := sv.selected(); ok {
				path := it.JobPath
				return sv, func() tea.Msg {
					return OpenScanLogMsg{NC: containerNC(path), JobPath: path}
				}
			}
		case "o":
			if it, ok := sv.selected(); ok {
				if url := cachedRepoURL(sv.store, it.JobPath); url != "" {
					return sv, openURLCmd(url)
				}
			}
		}
	}
	return sv, nil
}

func (sv *ScansView) View() string { return sv.table.View() }

func (sv *ScansView) Title() string { return "Scans" }

func (sv *ScansView) Breadcrumb() BreadcrumbSegment { return sv.MakeBreadcrumb("scans") }

func (sv *ScansView) ItemCount() int { return len(sv.items) }

func (sv *ScansView) Commands() []command.Command { return nil }

func (sv *ScansView) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{component.Nav("esc", "back")}
	it, ok := sv.selected()
	if !ok {
		return sc
	}
	sc = append(sc, component.Nav("enter", "scan log"))
	if cachedRepoURL(sv.store, it.JobPath) != "" {
		sc = append(sc, component.Nav("o", "open repo"))
	}
	return sc
}

// containerNC builds the navigation context of a container job path (a folder
// or multibranch project). It differs from ncFromJobPath, which reads the last
// segment as a branch — a container's last segment is the container itself.
func containerNC(jobPath string) NavigationContext {
	folder, name := "", jobPath
	if i := strings.LastIndex(jobPath, "/"); i >= 0 {
		folder, name = jobPath[:i], jobPath[i+1:]
	}
	return NavigationContext{Level: CtxProject, FolderPath: folder, ProjectName: name}
}

func (sv *ScansView) SetSize(width, height int) {
	sv.BaseView.SetSize(width, height)
	// PROJECT and WHY share what the fixed columns leave; WHY is the reason
	// Jenkins gives ("At maximum indexing capacity"), which is the whole point
	// of the view, so it gets the same room as the path.
	flex := (width - colScanStateWidth - colScanWaitingWidth - 4*2) / 2
	if flex < 15 {
		flex = 15
	}
	sv.table.SetColumnWidth(0, flex)
	sv.table.SetColumnWidth(3, flex)
	sv.table.SetSize(width, height)
}

// relativeSince renders how long a scan has been waiting, without the "ago"
// suffix the build rows use — here it reads as a duration, not a timestamp.
func relativeSince(since time.Time) string {
	if since.IsZero() {
		return ""
	}
	d := time.Since(since)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

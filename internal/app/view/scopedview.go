package view

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// ScopedViewConfig holds the per-view variation points for ScopedView.
type ScopedViewConfig struct {
	// Title is the value returned by the Title() method.
	Title string
	// BreadcrumbType is the view-type label passed to BreadcrumbFor (e.g. "stages", "log", "matrix").
	BreadcrumbType string
	// NewInner constructs the inner View for a resolved build.
	// nc is the build-level NavigationContext; build is the full resolved UserBuild.
	NewInner func(nc NavigationContext, build jmodel.UserBuild) View
	// HandleSlowFetch enables the slow-fetch loop (myBuildsFullMsg, slow tick, "r" key).
	// Set to true for builds/console views; false for matrix (running-only).
	HandleSlowFetch bool
	// AppendFilterShortcuts appends r/m shortcut hints to Shortcuts().
	AppendFilterShortcuts bool
	// FullScreenWhenActive makes IsFullScreen() return true when inner != nil.
	// Implements the FullScreen optional interface for views like MyMatrixView.
	FullScreenWhenActive bool
}

// ScopedView implements the shared "scoped last-build" wrapper pattern.
// It holds a buildResolver and an inner View, delegating optional interfaces
// via type assertions on the inner view.
type ScopedView struct {
	theme    theme.Theme
	resolver buildResolver
	inner    View
	width    int
	height   int
	cfg      ScopedViewConfig
}

// NewScopedView creates a ScopedView with the given resolver and configuration.
func NewScopedView(t theme.Theme, resolver buildResolver, cfg ScopedViewConfig) *ScopedView {
	return &ScopedView{
		theme:    t,
		resolver: resolver,
		cfg:      cfg,
	}
}

// IsFullScreen implements FullScreen. Only active when cfg.FullScreenWhenActive is set.
func (sv *ScopedView) IsFullScreen() bool {
	return sv.cfg.FullScreenWhenActive && sv.inner != nil
}

// ActiveFilters implements Filterable.
func (sv *ScopedView) ActiveFilters() Filters {
	return Filters{Running: sv.resolver.filterRunning, Mine: sv.resolver.filterMine}
}

func (sv *ScopedView) ToggleRunning() { sv.resolver.filterRunning = !sv.resolver.filterRunning }
func (sv *ScopedView) ToggleMine()    { sv.resolver.filterMine = !sv.resolver.filterMine }

func (sv *ScopedView) Init() tea.Cmd {
	dbg("ScopedView.Init: title=%q scope.Level=%v scope.JobPath=%q", sv.cfg.Title, sv.resolver.scope.Level, sv.resolver.scope.JobPath())
	sv.resolver.preloadFromCache()
	var cmds []tea.Cmd
	if cmd := sv.reEvaluate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if sv.cfg.HandleSlowFetch && !sv.resolver.filterRunning {
		dbg("ScopedView.Init: scheduling fetchSlow for jobPath=%q", sv.resolver.scope.JobPath())
		cmds = append(cmds, sv.resolver.fetchSlow)
	}
	return tea.Batch(cmds...)
}

func (sv *ScopedView) reEvaluate() tea.Cmd {
	latest := sv.resolver.bestMatch(sv.resolver.mergeBuilds())
	return sv.resolveWith(latest)
}

func (sv *ScopedView) resolveWith(latest *jmodel.UserBuild) tea.Cmd {
	if latest == nil {
		if sv.inner != nil {
			sv.inner.Close()
			sv.inner = nil
			sv.resolver.resolvedPath = ""
			sv.resolver.resolvedNum = 0
			sv.resolver.resolvedName = ""
		}
		sv.resolver.loading = false
		return nil
	}
	if latest.JobPath == sv.resolver.resolvedPath && latest.Number == sv.resolver.resolvedNum {
		sv.resolver.loading = false
		return nil
	}
	if sv.inner != nil {
		sv.inner.Close()
	}
	sv.resolver.resolvedPath = latest.JobPath
	sv.resolver.resolvedNum = latest.Number
	sv.resolver.resolvedName = latest.Name
	sv.resolver.loading = false

	sv.inner = sv.cfg.NewInner(sv.targetNC(), *latest)
	sv.inner.SetSize(sv.width, sv.height)
	return sv.inner.Init()
}

func (sv *ScopedView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		return sv.handleThemeChanged(msg)
	case RunningBuildsUpdatedMsg:
		return sv.handleRunningBuildsUpdated(msg)
	case myBuildsFullMsg:
		return sv.handleMyBuildsFull(msg)
	case myBuildsSlowTickMsg:
		return sv.handleMyBuildsSlowTick()
	case tea.KeyMsg:
		return sv.handleKeyMsg(msg)
	default:
		return sv.delegateToInner(msg)
	}
}

// delegateToInner forwards a message to the inner view if present.
func (sv *ScopedView) delegateToInner(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sv.inner != nil {
		m, cmd := sv.inner.Update(msg)
		sv.inner = m.(View)
		return sv, cmd
	}
	return sv, nil
}

func (sv *ScopedView) handleThemeChanged(msg ThemeChangedMsg) (tea.Model, tea.Cmd) {
	sv.theme = msg.Theme
	return sv.delegateToInner(msg)
}

func (sv *ScopedView) handleRunningBuildsUpdated(msg RunningBuildsUpdatedMsg) (tea.Model, tea.Cmd) {
	sv.resolver.lastRunningBuilds = msg.Builds
	return sv, sv.reEvaluate()
}

func (sv *ScopedView) handleMyBuildsFull(msg myBuildsFullMsg) (tea.Model, tea.Cmd) {
	if !sv.cfg.HandleSlowFetch {
		return sv, nil
	}
	var cmds []tea.Cmd
	if msg.err == nil {
		sv.resolver.cacheSlowBuilds(msg.builds)
		sv.resolver.lastSlowBuilds = msg.builds
		if cmd := sv.reEvaluate(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, sv.resolver.scheduleSlowTick())
	return sv, tea.Batch(cmds...)
}

func (sv *ScopedView) handleMyBuildsSlowTick() (tea.Model, tea.Cmd) {
	if !sv.cfg.HandleSlowFetch {
		return sv, nil
	}
	if !sv.resolver.filterRunning {
		return sv, sv.resolver.fetchSlow
	}
	return sv, sv.resolver.scheduleSlowTick()
}

func (sv *ScopedView) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		if sv.cfg.HandleSlowFetch {
			return sv.handleToggleRunning()
		}
		// "r" not handled here — fall through to inner below.
	case "m":
		sv.resolver.filterMine = !sv.resolver.filterMine
		return sv, sv.reEvaluate()
	}
	return sv.delegateToInner(msg)
}

func (sv *ScopedView) handleToggleRunning() (tea.Model, tea.Cmd) {
	sv.resolver.filterRunning = !sv.resolver.filterRunning
	var cmds []tea.Cmd
	if cmd := sv.reEvaluate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if !sv.resolver.filterRunning && len(sv.resolver.lastSlowBuilds) == 0 {
		cmds = append(cmds, sv.resolver.fetchSlow)
	}
	return sv, tea.Batch(cmds...)
}

func (sv *ScopedView) View() string {
	if sv.inner != nil {
		return sv.inner.View()
	}
	if sv.resolver.loading {
		return sv.theme.Breadcrumb.Paren.Render("Looking for builds…")
	}
	label := "No builds"
	if sv.resolver.filterMine {
		label = "No builds for " + sv.resolver.friendlyName
	}
	if sv.resolver.filterRunning {
		label += " (running)"
	}
	return sv.theme.Breadcrumb.Paren.Render(label)
}

func (sv *ScopedView) Title() string { return sv.cfg.Title }

// targetNC is the build-level NavigationContext this view currently resolves
// to: the scope, plus the "#last" alias anchored at that scope, plus whatever
// the alias has resolved to so far. It is handed to the inner view and used for
// this view's own breadcrumb, so both render identically — and, crucially, so
// the resolution survives into any view swapped in from here.
func (sv *ScopedView) targetNC() NavigationContext {
	scope := sv.resolver.scope
	nc := scope
	if p := sv.resolver.resolvedPath; p != "" && p != scope.JobPath() {
		// The alias resolved below its anchor (e.g. a root- or project-scoped
		// #last landing on a specific branch); adopt the resolved location.
		resolved := ncFromJobPath(p)
		nc.FolderPath = resolved.FolderPath
		nc.ProjectName = resolved.ProjectName
		nc.BranchName = resolved.BranchName
	}
	// The anchor is the scope the user navigated by, not the level the
	// resolution happens to reach — so it is taken from scope, never derived.
	nc.Level = CtxBuild
	nc.AliasScope = scope.Level
	nc.Build = NavBuildRef{
		IsLast:      true,
		Number:      sv.resolver.resolvedNum,
		DisplayName: sv.resolver.resolvedName,
	}
	nc.StageName = ""
	nc.StageParent = ""
	return nc
}

func (sv *ScopedView) Breadcrumb() BreadcrumbSegment {
	seg := BreadcrumbFor(sv.cfg.BreadcrumbType, sv.targetNC(), CtxBuild)
	seg.Running = sv.resolver.filterRunning
	seg.Mine = sv.resolver.filterMine
	return seg
}

// InspectTarget delegates to the inner view so the `:inspect` command targets
// the resolved build shown by this scoped view.
func (sv *ScopedView) InspectTarget() (NavigationContext, bool) {
	if sv.inner != nil {
		if ip, ok := sv.inner.(InspectProvider); ok {
			return ip.InspectTarget()
		}
	}
	return NavigationContext{}, false
}

// Badge delegates to inner if it implements HasBadge.
func (sv *ScopedView) Badge() string {
	if sv.inner != nil {
		if b, ok := sv.inner.(HasBadge); ok {
			return b.Badge()
		}
	}
	return ""
}

func (sv *ScopedView) Shortcuts() []component.Shortcut {
	var sc []component.Shortcut
	if sv.inner != nil {
		sc = sv.inner.Shortcuts()
	}
	if sv.cfg.AppendFilterShortcuts {
		sc = append(sc, filterShortcuts(sv.resolver.filterRunning, sv.resolver.filterMine)...)
	}
	return sc
}

func (sv *ScopedView) ItemCount() int {
	if sv.inner != nil {
		return sv.inner.ItemCount()
	}
	return 0
}

func (sv *ScopedView) Commands() []command.Command {
	if sv.inner != nil {
		return sv.inner.Commands()
	}
	return nil
}

func (sv *ScopedView) SetSize(width, height int) {
	sv.width = width
	sv.height = height
	if sv.inner != nil {
		sv.inner.SetSize(width, height)
	}
}

func (sv *ScopedView) HasPopup() bool {
	if sv.inner != nil {
		if pp, ok := sv.inner.(PopupLayer); ok {
			return pp.HasPopup()
		}
	}
	return false
}

func (sv *ScopedView) PopupView() string {
	if sv.inner != nil {
		if pp, ok := sv.inner.(PopupLayer); ok {
			return pp.PopupView()
		}
	}
	return ""
}

// NC returns the build-level context this view resolves to, so commands issued
// from a "#last" view (:artifact, :tests, :inspect) target the build on screen.
// Before the alias resolves it degrades to the scope, which is all that is known.
func (sv *ScopedView) NC() NavigationContext {
	if sv.resolver.resolvedNum == 0 {
		return sv.resolver.scope
	}
	return sv.targetNC()
}

// ScopeNC returns the scope this view resolves *within* — what a child view
// needs to rebuild an equivalent scoped view, as opposed to the build NC()
// points at.
func (sv *ScopedView) ScopeNC() NavigationContext { return sv.resolver.scope }

// ParentView implements HasParent. ESC from a scoped view returns to the
// natural parent based on the scope level: project → job list, branch →
// builds view, folder → folder job list, root → Dashboard (nil).
func (sv *ScopedView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	scope := sv.resolver.scope
	username := sv.resolver.username
	switch scope.Level {
	case CtxFolder:
		return NewJobList(t, c, s, scope.FolderPath, shortName(decodeName(scope.FolderPath)), false, username, scope.GitUsernames)
	case CtxProject:
		return NewJobList(t, c, s, scope.JobPath(), scope.ProjectName, true, username, scope.GitUsernames)
	case CtxBranch:
		return NewBuildsView(t, c, s, scope, NewBranchBuildsProvider(c, s, scope))
	}
	return nil // CtxRoot → Dashboard
}

func (sv *ScopedView) Close() error {
	dbg("ScopedView.Close: title=%q jobPath=%q", sv.cfg.Title, sv.resolver.scope.JobPath())
	sv.resolver.cancel()
	if sv.inner != nil {
		return sv.inner.Close()
	}
	return nil
}

// PreviewProvider delegation — active only when inner supports it.

// HasActivePreview reports whether the inner view currently implements PreviewProvider.
// Used by the app to decide whether to render the split-pane layout.
func (sv *ScopedView) HasActivePreview() bool {
	if sv.inner == nil {
		return false
	}
	_, ok := sv.inner.(PreviewProvider)
	return ok
}

func (sv *ScopedView) PreviewView() string {
	if sv.inner != nil {
		if pp, ok := sv.inner.(PreviewProvider); ok {
			return pp.PreviewView()
		}
	}
	return ""
}

func (sv *ScopedView) SetPreviewSize(width, height int) {
	if sv.inner != nil {
		if pp, ok := sv.inner.(PreviewProvider); ok {
			pp.SetPreviewSize(width, height)
		}
	}
}

func (sv *ScopedView) PreviewBreadcrumb() BreadcrumbSegment {
	if sv.inner != nil {
		if pp, ok := sv.inner.(PreviewProvider); ok {
			return pp.PreviewBreadcrumb()
		}
	}
	return BreadcrumbSegment{}
}

func (sv *ScopedView) PreviewItemCount() int {
	if sv.inner != nil {
		if pp, ok := sv.inner.(PreviewProvider); ok {
			return pp.PreviewItemCount()
		}
	}
	return 0
}

// Searchable delegation.

func (sv *ScopedView) ApplySearch(pattern string) tea.Cmd {
	if sv.inner != nil {
		if s, ok := sv.inner.(Searchable); ok {
			return s.ApplySearch(pattern)
		}
	}
	return nil
}

func (sv *ScopedView) HandleSearchResult(msg widget.SearchResultMsg) tea.Cmd {
	if sv.inner != nil {
		if h, ok := sv.inner.(SearchResultHandler); ok {
			return h.HandleSearchResult(msg)
		}
	}
	return nil
}

func (sv *ScopedView) SearchQuery() string {
	if sv.inner != nil {
		if s, ok := sv.inner.(Searchable); ok {
			return s.SearchQuery()
		}
	}
	return ""
}

// RunningLogView delegation.

func (sv *ScopedView) IsBuildRunning() bool {
	if sv.inner != nil {
		if r, ok := sv.inner.(RunningLogView); ok {
			return r.IsBuildRunning()
		}
	}
	return false
}

// compile-time import guard for time package
var _ time.Duration

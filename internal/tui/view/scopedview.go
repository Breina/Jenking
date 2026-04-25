package view

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// ScopedViewConfig holds the per-view variation points for ScopedView.
type ScopedViewConfig struct {
	// Title is the value returned by the Title() method.
	Title string
	// BreadcrumbType is the view-type label passed to BreadcrumbFor (e.g. "stages", "log", "matrix").
	BreadcrumbType string
	// NewInner constructs the inner View for a resolved build.
	// nc is the build-level NavigationContext; build is the full resolved UserBuild.
	NewInner func(nc NavigationContext, build jenkins.UserBuild) View
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

func (sv *ScopedView) resolveWith(latest *jenkins.UserBuild) tea.Cmd {
	if latest == nil {
		if sv.inner != nil {
			sv.inner.Close()
			sv.inner = nil
			sv.resolver.resolvedPath = ""
			sv.resolver.resolvedNum = 0
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
	sv.resolver.loading = false

	nc := ncFromJobPath(latest.JobPath)
	nc.Username = sv.resolver.username
	nc.GitUsernames = sv.resolver.gitUsernames
	nc = nc.AtBuild(latest.Number)
	sv.inner = sv.cfg.NewInner(nc, *latest)
	sv.inner.SetSize(sv.width, sv.height)
	return sv.inner.Init()
}

func (sv *ScopedView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		sv.theme = msg.Theme
		if sv.inner != nil {
			m, cmd := sv.inner.Update(msg)
			sv.inner = m.(View)
			return sv, cmd
		}
		return sv, nil

	case RunningBuildsUpdatedMsg:
		sv.resolver.lastRunningBuilds = msg.Builds
		if cmd := sv.reEvaluate(); cmd != nil {
			return sv, cmd
		}
		return sv, nil

	case myBuildsFullMsg:
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

	case myBuildsSlowTickMsg:
		if !sv.cfg.HandleSlowFetch {
			return sv, nil
		}
		if !sv.resolver.filterRunning {
			return sv, sv.resolver.fetchSlow
		}
		return sv, sv.resolver.scheduleSlowTick()

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			if sv.cfg.HandleSlowFetch {
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
			// "r" not handled here — fall through to inner below.
		case "m":
			sv.resolver.filterMine = !sv.resolver.filterMine
			if cmd := sv.reEvaluate(); cmd != nil {
				return sv, cmd
			}
			return sv, nil
		}
		if sv.inner != nil {
			m, cmd := sv.inner.Update(msg)
			sv.inner = m.(View)
			return sv, cmd
		}
		return sv, nil

	default:
		if sv.inner != nil {
			m, cmd := sv.inner.Update(msg)
			sv.inner = m.(View)
			return sv, cmd
		}
	}
	return sv, nil
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

func (sv *ScopedView) Breadcrumb() BreadcrumbSegment {
	nc := sv.resolver.scope
	nc.Build = NavBuildRef{IsLast: true}
	seg := BreadcrumbFor(sv.cfg.BreadcrumbType, nc)
	seg.Running = sv.resolver.filterRunning
	seg.Mine = sv.resolver.filterMine
	seg.ResolvedParts = resolverParts(&sv.resolver)
	return seg
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

func (sv *ScopedView) NC() NavigationContext { return sv.resolver.scope }

// ParentView implements HasParent. ESC from a scoped view returns to the
// natural parent based on the scope level: project → job list, branch →
// builds view, folder → folder job list, root → Dashboard (nil).
func (sv *ScopedView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
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

func (sv *ScopedView) ApplySearch(pattern string) error {
	if sv.inner != nil {
		if s, ok := sv.inner.(Searchable); ok {
			return s.ApplySearch(pattern)
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

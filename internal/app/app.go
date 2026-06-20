package app

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/monitor"
	"github.com/Breina/Jenking/internal/notify"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
	"github.com/Breina/Jenking/internal/updater"
	"github.com/Breina/Jenking/internal/version"
)

// wireMonitor builds a RunningBuildsMonitor and attaches a Reconciler to the
// store's Registry so background GetBuild fetches drive build-status transitions
// for builds the monitor never observed in its prev set (e.g. transient builds
// that completed between two 1s polls).
func wireMonitor(client jmodel.JenkinsClient, store *cache.Store) *monitor.RunningBuildsMonitor {
	m := monitor.NewRunningBuildsMonitor(client, store)
	if store != nil && store.Registry != nil {
		rec := buildregistry.NewReconciler(client, store.Registry, nil)
		store.Registry.SetReconcile(rec.Reconcile)
	}
	return m
}

// debugStats is shared via pointer so value-receiver methods can mutate it.
type debugStats struct {
	renderMs    int64
	updateMs    int64
	updateCount int64
}

type updateCheckResultMsg struct {
	version string // empty when no newer version exists
	err     error
}
type startUpdateMsg struct{}
type updateDoneMsg struct{ err error }

type openColorblindMenuMsg struct{}
type openThemeMenuMsg struct{}
type openContextMenuMsg struct{}
type switchContextMsg struct{ name string }
type addContextProbeResultMsg struct {
	ok  bool
	msg string
}
type openPrefsMsg struct{}

// viewKind identifies which navigation view a slash command opens.
type viewKind int

const (
	kindBuilds viewKind = iota
	kindStages
	kindJobs
	kindLogs
	kindMatrix
)

// openTargetMsg is the unified navigation message emitted by :builds, :stages,
// :jobs, :logs, and :matrix. The Target carries any positional/marker arguments
// the user supplied; an empty Target falls back to the current view's NC.
type openTargetMsg struct {
	kind   viewKind
	target command.Target
}

// openArtifactMsg is emitted by :artifact. name is the artifact to open
// (empty = list all artifacts for the current build).
type openArtifactMsg struct{ name string }

// artifactsFetchedMsg carries the artifact list fetched asynchronously for the
// current build so :artifact can resolve the requested file and open it.
type artifactsFetchedMsg struct {
	nc        view.NavigationContext
	build     jmodel.Build
	name      string
	artifacts []jmodel.Artifact
}

type openRunningBuildsMsg struct{}
type openHelpMsg struct{}
type connCheckMsg struct{}
type connProbeResultMsg struct{ ok bool }
type userInfoMsg struct{ fullName string }

// App is the root bubbletea model.
type App struct {
	theme                theme.Theme
	baseTheme            theme.Theme
	themeID              theme.ThemeID
	paywallRestoreID     theme.ThemeID // theme to restore to if Royal paywall is cancelled
	paywallRestoreDeg    bool          // degraded state of the pre-paywall theme
	showAddContextDialog bool
	addContextDialog     view.AddContextDialog
	addCtxFn             func(config.ContextConfig) error
	delCtxFn             func(string) error
	setCtxFn             func(string) error
	saveThemeFn          func(string) error
	showPrefsDialog      bool
	prefsDialog          view.PrefsDialog
	savePrefsFn          func(notifications bool, gitUsernames []string, refreshInterval, slowInterval time.Duration, maxLogLines int, logLevel string, textArtifactExtensions []string) error
	refreshInterval      time.Duration
	maxLogLines          int
	logLevel             string
	showHelp             bool
	showRoyalPaywall     bool
	royalPaywall         view.RoyalPaywall
	sponsorKey           string
	colorblindnessType   theme.ColorblindnessType
	saveFn               func(theme.ColorblindnessType) error
	keys                 KeyMap
	client               jmodel.JenkinsClient
	store                *cache.Store
	monitor              *monitor.RunningBuildsMonitor
	username             string
	friendlyName         string
	gitUsernames         []string
	contexts             []config.ContextConfig
	currentContextName   string
	diskStoreFn          func(serverURL string) *cache.DiskStore
	slowInterval         time.Duration
	registry             *command.Registry
	header               component.Header
	breadcrumb           component.Breadcrumb
	statusBar            component.StatusBar
	navTags              component.NavTags
	currentView          view.View
	initialView          view.View
	navStack             []view.View // back-stack populated on PushViewMsg
	width                int
	height               int
	cmdInput             string
	cmdSuggestions       []string
	cmdSuggestionIdx     int
	searchInput          string
	notifications        bool
	termFocused          bool        // true while the terminal window has focus
	connected            bool        // tracks live connection status shown in header
	dbg                  *debugStats // non-nil when log_level=debug
	updateVersion        string      // non-empty when a newer version is available
	showUpdateDialog     bool
	updateDialogYes      bool // which button is highlighted in the confirm dialog
	isUpdating           bool
	UpdatedTo            string // set on successful self-update; read by main after p.Run()

	pendingDeeplink *view.DeepLink // last clipboard URL that parsed for this context
}

// NewApp creates the root application model.
func NewApp(cfg AppConfig) App {
	registry := buildCommandRegistry(cfg.Store, cfg.Contexts)

	var dbg *debugStats
	if cfg.Debug {
		dbg = &debugStats{}
	}

	return App{
		theme:              cfg.Theme,
		baseTheme:          cfg.BaseTheme,
		themeID:            cfg.ThemeID,
		saveThemeFn:        cfg.SaveThemeFn,
		sponsorKey:         cfg.SponsorKey,
		colorblindnessType: cfg.ColorblindnessType,
		saveFn:             cfg.SaveColorblindnessFn,
		keys:               cfg.Keys,
		client:             cfg.Client,
		store:              cfg.Store,
		monitor:            wireMonitor(cfg.Client, cfg.Store),
		username:           cfg.Username,
		friendlyName:       cfg.FriendlyName,
		gitUsernames:       cfg.GitUsernames,
		contexts:           cfg.Contexts,
		currentContextName: cfg.CurrentContextName,
		diskStoreFn:        cfg.DiskStoreFn,
		addCtxFn:           cfg.AddContextFn,
		delCtxFn:           cfg.DeleteContextFn,
		setCtxFn:           cfg.SetContextFn,
		savePrefsFn:        cfg.SavePrefsFn,
		refreshInterval:    cfg.RefreshInterval,
		slowInterval:       cfg.SlowRefreshInterval,
		maxLogLines:        cfg.MaxLogLines,
		logLevel:           cfg.LogLevel,
		registry:           registry,
		header:             cfg.Header,
		breadcrumb:         cfg.Breadcrumb,
		statusBar:          cfg.StatusBar,
		navTags:            component.NewNavTags(cfg.Theme),
		currentView:        cfg.InitialView,
		initialView:        cfg.InitialView,
		notifications:      cfg.Notifications,
		termFocused:        true, // assume focused until a BlurMsg says otherwise
		connected:          true,
		dbg:                dbg,
	}
}

// openTargetCmd builds the Execute closure for a navigation command. It
// parses the user's args into a Target; on parse error the error is surfaced
// via the status bar without a view change.
func openTargetCmd(kind viewKind) func(args []string) tea.Cmd {
	return func(args []string) tea.Cmd {
		t, err := command.ParseTarget(args)
		if err != nil {
			return errCmd(err)
		}
		return func() tea.Msg { return openTargetMsg{kind: kind, target: t} }
	}
}

// errCmd returns a tea.Cmd that surfaces err on the status bar via ErrorMsg.
func errCmd(err error) tea.Cmd {
	return func() tea.Msg { return view.ErrorMsg{Err: err} }
}

func (a App) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tea.SetWindowTitle("Jenking"))
	cmds = append(cmds, tea.EnableReportFocus)
	if a.currentView != nil {
		cmds = append(cmds, a.currentView.Init())
	}
	cmds = append(cmds, a.monitor.Init())
	cmds = append(cmds, checkForUpdateCmd())
	cmds = append(cmds, clipboardPollCmd(a.currentContextURL(), a.store))
	return tea.Batch(cmds...)
}

// Update is the Bubble Tea entry point. It routes messages through four
// progressively-narrower stages: (1) monitor consumes its own poll/tick
// messages; (2) any open modal dialog claims key events while visible;
// (3) tea framework messages (focus/blur/resize/key) take their typed
// branches; (4) everything else is forwarded to handleTypedMessage and
// its per-concern handler methods.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.dbg != nil {
		start := time.Now()
		defer func() {
			a.dbg.updateMs = time.Since(start).Milliseconds()
			a.dbg.updateCount++
		}()
	}

	if handled, cmds := a.monitor.HandleMsg(msg); handled {
		return a, tea.Batch(cmds...)
	}

	if next, cmd, consumed := a.handleModalPrecedence(msg); consumed {
		return next, cmd
	}

	switch msg := msg.(type) {
	case tea.FocusMsg:
		a.termFocused = true
		return a, nil
	case tea.BlurMsg:
		a.termFocused = false
		return a, nil
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateLayout()
		if a.showAddContextDialog {
			a.addContextDialog.SetSize(a.width, a.height)
		}
		if a.showPrefsDialog {
			a.prefsDialog.SetSize(a.width, a.height)
		}
		return a, nil
	case tea.KeyMsg:
		return a.handleKeyMsg(msg)
	}

	return a.handleTypedMessage(msg)
}

// handleModalPrecedence checks each currently-visible modal dialog in priority
// order and lets the topmost one consume the message. Modals only react to
// key events; non-key messages fall through. Returns consumed=true when a
// modal handled the message and the caller should return immediately.
func (a App) handleModalPrecedence(msg tea.Msg) (App, tea.Cmd, bool) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return a, nil, false
	}
	if a.showAddContextDialog {
		return a.modalAddContext(keyMsg)
	}
	if a.showPrefsDialog {
		return a.modalPrefs(keyMsg)
	}
	if a.showUpdateDialog {
		return a.modalUpdateConfirm(keyMsg)
	}
	if a.showRoyalPaywall {
		return a.modalRoyalPaywall(keyMsg)
	}
	return a, nil, false
}

func (a App) modalAddContext(keyMsg tea.KeyMsg) (App, tea.Cmd, bool) {
	updated, result := a.addContextDialog.Update(keyMsg)
	a.addContextDialog = updated
	if cmd, done := a.handleAddContextResult(result); done {
		return a, cmd, true
	}
	return a, nil, true
}

func (a App) modalPrefs(keyMsg tea.KeyMsg) (App, tea.Cmd, bool) {
	updated, result := a.prefsDialog.Update(keyMsg)
	a.prefsDialog = updated
	switch result.Status {
	case view.PrefsCancelled:
		a.showPrefsDialog = false
	case view.PrefsConfirmed:
		a.showPrefsDialog = false
		p := result.Prefs
		a.notifications = p.Notifications
		a.gitUsernames = p.GitUsernames
		a.refreshInterval = p.RefreshInterval
		a.slowInterval = p.SlowRefreshInterval
		a.maxLogLines = p.MaxLogLines
		a.logLevel = p.LogLevel
		view.SetTextArtifactExtensions(p.TextArtifactExtensions)
		if a.savePrefsFn != nil {
			if err := a.savePrefsFn(p.Notifications, p.GitUsernames, p.RefreshInterval, p.SlowRefreshInterval, p.MaxLogLines, p.LogLevel, view.TextArtifactExtensionList()); err != nil {
				a.statusBar.SetError(fmt.Sprintf("save preferences: %v", err))
			}
		}
	}
	return a, nil, true
}

func (a App) modalUpdateConfirm(keyMsg tea.KeyMsg) (App, tea.Cmd, bool) {
	switch keyMsg.String() {
	case "left", "right", "h", "l":
		a.updateDialogYes = !a.updateDialogYes
	case "y", "Y":
		a.showUpdateDialog = false
		a.isUpdating = true
		return a, doUpdateCmd(a.updateVersion), true
	case "enter":
		if a.updateDialogYes {
			a.showUpdateDialog = false
			a.isUpdating = true
			return a, doUpdateCmd(a.updateVersion), true
		}
		a.showUpdateDialog = false
	case "n", "N", "esc", "q":
		a.showUpdateDialog = false
	}
	return a, nil, true
}

func (a App) modalRoyalPaywall(keyMsg tea.KeyMsg) (App, tea.Cmd, bool) {
	updated, result, closed := a.royalPaywall.Update(keyMsg)
	a.royalPaywall = updated
	if !closed {
		return a, nil, true
	}
	a.showRoyalPaywall = false
	if result == nil {
		return a, nil, true
	}
	switch *result {
	case view.PaywallResultSponsor:
		_ = exec.Command("xdg-open", view.GitHubSponsorsURL).Start()
		a.applyTheme(a.paywallRestoreID, false, a.paywallRestoreDeg)
	case view.PaywallResultDegrade:
		a.applyTheme(theme.ThemeRoyal, true, true)
	case view.PaywallResultCancel:
		a.applyTheme(a.paywallRestoreID, false, a.paywallRestoreDeg)
	}
	return a, nil, true
}

// handleKeyMsg routes a KeyMsg through mode-specific handlers and global
// shortcuts before delegating any unhandled key to the active view.
func (a App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.statusBar.HasError() {
		a.statusBar.ClearError()
		return a, nil
	}
	if a.statusBar.Mode() == component.ModeCommand {
		return a.handleCommandInput(msg)
	}
	if a.statusBar.Mode() == component.ModeSearch {
		return a.handleSearchInput(msg)
	}
	if a.showHelp {
		a.showHelp = false
		return a, nil
	}
	if next, cmd, consumed := a.handleGlobalKey(msg); consumed {
		return next, cmd
	}
	if v := a.activeView(); v != nil {
		model, cmd := v.Update(msg)
		a.currentView = model.(view.View)
		return a, cmd
	}
	return a, nil
}

// handleGlobalKey matches the message against the global key map (quit,
// command, search, back, "U" update prompt, running-builds shortcut). The
// back-key flow is in its own method because it has its own multi-level
// fallback chain (popup → active-nav → search → pop-stack → parent → dashboard).
func (a App) handleGlobalKey(msg tea.KeyMsg) (App, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit, true
	case key.Matches(msg, a.keys.Command):
		a.statusBar.SetMode(component.ModeCommand)
		a.cmdInput = ""
		return a, nil, true
	case key.Matches(msg, a.keys.Search):
		if _, ok := a.activeView().(view.Searchable); ok {
			a.statusBar.SetMode(component.ModeSearch)
			a.searchInput = ""
			a.statusBar.SetInput("")
			return a, nil, true
		}
	case key.Matches(msg, a.keys.Back):
		return a.handleBackKey(msg)
	case msg.String() == "U" && a.updateVersion != "":
		a.showUpdateDialog = true
		a.updateDialogYes = true
		return a, nil, true
	case msg.String() == "V" && a.pendingDeeplink != nil:
		dl := a.pendingDeeplink
		a.pendingDeeplink = nil
		next, cmd := a.openDeeplink(dl)
		return next.(App), cmd, true
	case key.Matches(msg, a.keys.RunningBuilds):
		if bv, ok := a.activeView().(*view.BuildsView); ok && bv.NC().Level == view.CtxRoot {
			bv.ToggleRunning()
			return a, nil, true
		}
		av := view.NewAllBuildsView(a.theme, a.client, a.store, a.username, a.gitUsernames, a.slowInterval)
		av.ToggleRunning()
		a.replaceView(av)
		a.updateBreadcrumb()
		return a, av.Init(), true
	}
	return a, nil, false
}

// handleBackKey processes Esc through a priority chain: if the view has an
// open popup let it consume the key; otherwise clear the active-nav match;
// otherwise clear pending search; otherwise pop the back-stack; otherwise
// fall back to HasParent or the initial dashboard.
func (a App) handleBackKey(msg tea.KeyMsg) (App, tea.Cmd, bool) {
	if pl, ok := a.activeView().(view.PopupLayer); ok && pl.HasPopup() {
		model, cmd := a.activeView().Update(msg)
		a.currentView = model.(view.View)
		return a, cmd, true
	}
	if nc, ok := a.activeView().(view.NavigationClearable); ok && nc.HasActiveNavigation() {
		nc.ClearActiveNavigation()
		return a, nil, true
	}
	if s, ok := a.activeView().(view.Searchable); ok && s.SearchQuery() != "" {
		s.ApplySearch("")
		return a, nil, true
	}
	var closeCmd tea.Cmd
	if cw, ok := a.currentView.(interface{ CloseCmd() tea.Cmd }); ok {
		closeCmd = cw.CloseCmd()
	}
	if a.popView() {
		a.updateBreadcrumb()
		// Re-Init the restored view so its polling chain wakes back up.
		// Messages were dropped while the child was pushed; each view's
		// Init() is required to be idempotent w.r.t. re-entry.
		return a, tea.Batch(closeCmd, a.currentView.Init()), true
	}
	if hp, ok := a.activeView().(view.HasParent); ok {
		parent := hp.ParentView(a.theme, a.client, a.store)
		a.activeView().Close()
		if parent != nil {
			a.currentView = parent
		} else {
			a.currentView = a.initialView
		}
		a.updateBreadcrumb()
		return a, a.currentView.Init(), true
	}
	if a.currentView != a.initialView {
		a.activeView().Close()
		a.currentView = a.initialView
		a.updateBreadcrumb()
		return a, a.initialView.Init(), true
	}
	return a, nil, true
}

// handleTypedMessage routes typed (non-key, non-framework) messages to
// per-concern handler methods. Each handler owns a related group of messages
// (build events, navigation, context management, dialogs, theme, connection,
// update lifecycle, error/search) and returns consumed=true if it handled
// the message. When no handler claims the message, it is delegated to the
// active view so views can subscribe to messages App doesn't care about.
func (a App) handleTypedMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, done := a.handleBuildEvents(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleViewOpen(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleViewStackOps(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleContextManagement(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleDialogOpen(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleThemeChange(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleConnection(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleUpdateLifecycle(msg); done {
		return next, cmd
	}
	if next, cmd, done := a.handleErrorAndSearch(msg); done {
		return next, cmd
	}
	if dm, ok := msg.(deeplinkCheckedMsg); ok {
		a.pendingDeeplink = dm.dl
		return a, clipboardPollCmd(a.currentContextURL(), a.store)
	}

	// Delegate non-key messages to active view.
	if v := a.activeView(); v != nil {
		model, cmd := v.Update(msg)
		a.currentView = model.(view.View)
		return a, cmd
	}
	return a, nil
}

// handleBuildEvents — RunningBuildsUpdatedMsg, BuildCompletedMsg, FailedStageMsg.
// These touch the running-builds counter, fire desktop notifications, and
// open follow-up views for failed builds.
func (a App) handleBuildEvents(msg tea.Msg) (App, tea.Cmd, bool) {
	if msg, ok := msg.(view.RunningBuildsUpdatedMsg); ok {
		a.header.SetRunningBuilds(msg.Count, "R")
		if watchPath := a.notifyJobPath(); watchPath != "" {
			for _, key := range msg.Arrived {
				jobPath, number := jmodel.ParseBuildKey(key)
				if strings.HasPrefix(jobPath, watchPath) {
					notify.Send("Build Started", fmt.Sprintf("#%d · %s", number, jobPath))
				}
			}
		}
		if v := a.activeView(); v != nil {
			model, cmd := v.Update(msg)
			a.currentView = model.(view.View)
			return a, cmd, true
		}
		return a, nil, true
	}
	if msg, ok := msg.(view.BuildCompletedMsg); ok {
		if msg.Err == nil {
			if watchPath := a.notifyJobPath(); watchPath != "" && strings.HasPrefix(msg.JobPath, watchPath) {
				notify.Send(
					fmt.Sprintf("Build #%d: %s", msg.Number, view.StatusLabel(msg.Build.Status)),
					msg.JobPath,
				)
			}
		}
		if v := a.activeView(); v != nil {
			model, cmd := v.Update(msg)
			a.currentView = model.(view.View)
			return a, cmd, true
		}
		return a, nil, true
	}
	if fsm, ok := msg.(view.FailedStageMsg); ok {
		return a.openFailedStage(fsm)
	}
	return a, nil, false
}

// openFailedStage opens the right follow-up view for a FailedStageMsg: the
// stage-log view when we know which stage failed and have its node IDs, or
// the full console log as a fallback when no failed stage is identified.
func (a App) openFailedStage(fsm view.FailedStageMsg) (App, tea.Cmd, bool) {
	if fsm.Err != nil {
		a.statusBar.SetError(fsm.Err.Error())
		return a, nil, true
	}
	buildNC := fsm.NC.AtBuild(fsm.Build.Number)
	if fsm.FailedStage != nil && len(fsm.FailedStage.NodeIDs) > 0 {
		sv := view.NewStageView(a.theme, a.client, a.store, buildNC, fsm.Build)
		sv.SetStages(fsm.Stages, fsm.FailedIdx)
		sl := view.NewStageLogViewWithBuild(a.theme, a.client, a.store, buildNC.AtStage(fsm.FailedStage.Name), fsm.FailedStage.NodeIDs, fsm.Build.Status == jmodel.BuildStatusRunning, fsm.Build)
		a.replaceView(sl)
		a.updateBreadcrumb()
		return a, sl.Init(), true
	}
	cv := view.NewConsoleView(a.theme, a.client, buildNC)
	a.replaceView(cv)
	a.updateBreadcrumb()
	return a, cv.Init(), true
}

// handleViewOpen — messages that compose a specific concrete view and put it
// active: :builds/:stages/:jobs/:logs/:matrix, :running, scoped stages, and
// the pending-build "trigger then watch" handoff.
func (a App) handleViewOpen(msg tea.Msg) (App, tea.Cmd, bool) {
	if otm, ok := msg.(openTargetMsg); ok {
		m, cmd := a.handleOpenTarget(otm)
		return m.(App), cmd, true
	}
	if oam, ok := msg.(openArtifactMsg); ok {
		m, cmd := a.handleOpenArtifact(oam)
		return m.(App), cmd, true
	}
	if afm, ok := msg.(artifactsFetchedMsg); ok {
		m, cmd := a.handleArtifactsFetched(afm)
		return m.(App), cmd, true
	}
	if _, ok := msg.(openRunningBuildsMsg); ok {
		if bv, ok := a.activeView().(*view.BuildsView); ok && bv.NC().Level == view.CtxRoot {
			bv.ToggleRunning()
			return a, nil, true
		}
		av := view.NewAllBuildsView(a.theme, a.client, a.store, a.username, a.gitUsernames, a.slowInterval)
		av.ToggleRunning()
		a.replaceView(av)
		a.updateBreadcrumb()
		return a, av.Init(), true
	}
	if osm, ok := msg.(view.OpenScopedStagesMsg); ok {
		nc := osm.NC
		nc.Username = a.username
		nc.FriendlyName = a.friendlyName
		nc.GitUsernames = a.gitUsernames
		mv := view.NewMyBuildsView(a.theme, a.client, a.store, nc, a.slowInterval)
		a.replaceView(mv)
		a.updateBreadcrumb()
		return a, mv.Init(), true
	}
	if ocm, ok := msg.(view.OpenScopedConsoleMsg); ok {
		nc := ocm.NC
		nc.Username = a.username
		nc.FriendlyName = a.friendlyName
		nc.GitUsernames = a.gitUsernames
		mv := view.NewMyConsoleView(a.theme, a.client, a.store, nc, a.slowInterval)
		a.replaceView(mv)
		a.updateBreadcrumb()
		return a, mv.Init(), true
	}
	if otb, ok := msg.(view.OpenTriggeredBuildMsg); ok {
		sv := view.NewPendingStageView(a.theme, a.client, a.store, otb.NC, otb.LastKnownBuild)
		a.replaceView(sv)
		a.updateBreadcrumb()
		return a, sv.Init(), true
	}
	return a, nil, false
}

// handleViewStackOps — push/pop/swap operations on the view stack. Views
// emit these to navigate without knowing what's underneath them; App owns
// the stack semantics.
func (a App) handleViewStackOps(msg tea.Msg) (App, tea.Cmd, bool) {
	if _, ok := msg.(view.PopViewMsg); ok {
		var closeCmd tea.Cmd
		if cw, ok := a.currentView.(interface{ CloseCmd() tea.Cmd }); ok {
			closeCmd = cw.CloseCmd()
		}
		if a.popView() {
			a.updateBreadcrumb()
			return a, tea.Batch(closeCmd, a.currentView.Init()), true
		}
		return a, closeCmd, true
	}
	if push, ok := msg.(view.PushViewMsg); ok {
		if mbv, ok := a.activeView().(*view.MyBuildsView); ok {
			if sp, ok := push.View.(view.ScopedParentTarget); ok {
				sp.SetScopedParent(mbv.NC(), a.slowInterval)
			}
		}
		a.pushView(push.View)
		a.updateBreadcrumb()
		return a, push.View.Init(), true
	}
	if swap, ok := msg.(view.SwapViewMsg); ok {
		if a.currentView != nil {
			a.currentView.Close()
		}
		a.currentView = swap.View
		a.updateBreadcrumb()
		return a, swap.View.Init(), true
	}
	if ps, ok := msg.(view.PopSwapViewMsg); ok {
		if len(a.navStack) > 0 {
			top := a.navStack[len(a.navStack)-1]
			a.navStack = a.navStack[:len(a.navStack)-1]
			top.Close()
		}
		if a.currentView != nil {
			a.currentView.Close()
		}
		a.currentView = ps.View
		a.updateBreadcrumb()
		return a, ps.View.Init(), true
	}
	if pvm, ok := msg.(view.PushViewsMsg); ok {
		if len(pvm.Views) == 0 {
			return a, nil, true
		}
		if a.currentView != nil {
			a.navStack = append(a.navStack, a.currentView)
		}
		a.navStack = append(a.navStack, pvm.Views[:len(pvm.Views)-1]...)
		a.currentView = pvm.Views[len(pvm.Views)-1]
		a.updateBreadcrumb()
		return a, a.currentView.Init(), true
	}
	return a, nil, false
}

// handleContextManagement — add/switch/delete Jenkins contexts.
func (a App) handleContextManagement(msg tea.Msg) (App, tea.Cmd, bool) {
	if apr, ok := msg.(addContextProbeResultMsg); ok {
		if a.showAddContextDialog {
			a.addContextDialog.SetConnStatus(apr.ok, apr.msg)
			if pending, ready := a.addContextDialog.ConsumePending(); ready {
				if cmd, done := a.handleAddContextResult(pending); done {
					return a, cmd, true
				}
			}
		}
		return a, nil, true
	}
	if sc, ok := msg.(switchContextMsg); ok {
		return a.switchToContext(sc.name)
	}
	if sr, ok := msg.(view.ContextSwitchRequestMsg); ok {
		return a.switchToContext(sr.Name)
	}
	if dr, ok := msg.(view.ContextDeleteRequestMsg); ok {
		return a.deleteContext(dr.Name)
	}
	if _, ok := msg.(view.OpenAddContextDialogMsg); ok {
		a.showAddContextDialog = true
		a.addContextDialog = view.NewAddContextDialog(a.theme)
		a.addContextDialog.SetSize(a.width, a.height)
		return a, nil, true
	}
	return a, nil, false
}

// deleteContext removes a named context from the in-memory list (and persists
// the change via delCtxFn). When the active context was deleted, falls back
// to the first remaining context; otherwise notifies the ContextView so it
// refreshes its list.
func (a App) deleteContext(name string) (App, tea.Cmd, bool) {
	if a.delCtxFn != nil {
		if err := a.delCtxFn(name); err != nil {
			a.statusBar.SetError(fmt.Sprintf("delete context: %v", err))
			return a, nil, true
		}
	}
	newContexts := make([]config.ContextConfig, 0, len(a.contexts))
	for _, c := range a.contexts {
		if c.Name != name {
			newContexts = append(newContexts, c)
		}
	}
	a.contexts = newContexts
	if name == a.currentContextName && len(newContexts) > 0 {
		return a.switchToContext(newContexts[0].Name)
	}
	return a, func() tea.Msg {
		return view.ContextListUpdatedMsg{Contexts: a.contexts, Current: a.currentContextName}
	}, true
}

// switchToContext wires a new Jenkins client/store from the named context and
// resets the view stack to the dashboard. Shared by switchContextMsg,
// ContextSwitchRequestMsg, and the delete-then-fallback flow.
func (a App) switchToContext(name string) (App, tea.Cmd, bool) {
	var target *config.ContextConfig
	for i := range a.contexts {
		if a.contexts[i].Name == name {
			target = &a.contexts[i]
			break
		}
	}
	if target == nil {
		a.statusBar.SetError(fmt.Sprintf("unknown context: %s", name))
		return a, nil, true
	}
	if target.Name == a.currentContextName {
		return a, nil, true
	}
	newClient := jenkins.NewClient(target.URL, target.Username, target.Token, target.Insecure)
	var newDisk *cache.DiskStore
	if a.diskStoreFn != nil {
		newDisk = a.diskStoreFn(target.URL)
	}
	newStore := cache.NewStore(newDisk)
	a.resetNavStack()
	a.activeView().Close()
	a.client = newClient
	a.store = newStore
	a.monitor = wireMonitor(newClient, newStore)
	a.username = target.Username
	a.friendlyName = ""
	a.currentContextName = target.Name
	if a.setCtxFn != nil {
		_ = a.setCtxFn(target.Name)
	}
	a.header.SetURL(target.URL)
	a.header.SetUser("")
	dashboard := view.NewJobList(a.theme, newClient, newStore, "", "Dashboard", false, target.Username, a.gitUsernames)
	a.currentView = dashboard
	a.initialView = dashboard
	a.updateBreadcrumb()
	return a, tea.Batch(dashboard.Init(), a.monitor.Init(), fetchUserInfo(newClient, target.Username)), true
}

// handleDialogOpen — :help, :context, :prefs, :colorblind, :theme.
func (a App) handleDialogOpen(msg tea.Msg) (App, tea.Cmd, bool) {
	if _, ok := msg.(openHelpMsg); ok {
		a.showHelp = true
		return a, nil, true
	}
	if _, ok := msg.(openContextMenuMsg); ok {
		cv := view.NewContextView(a.theme, a.contexts, a.currentContextName)
		return a, func() tea.Msg { return view.PushViewMsg{View: cv} }, true
	}
	if _, ok := msg.(openPrefsMsg); ok {
		a.prefsDialog = view.NewPrefsDialog(a.theme, view.PrefsValues{
			Notifications:          a.notifications,
			GitUsernames:           a.gitUsernames,
			RefreshInterval:        a.refreshInterval,
			SlowRefreshInterval:    a.slowInterval,
			MaxLogLines:            a.maxLogLines,
			LogLevel:               a.logLevel,
			TextArtifactExtensions: view.TextArtifactExtensionList(),
		})
		a.prefsDialog.SetSize(a.width, a.height)
		a.showPrefsDialog = true
		return a, nil, true
	}
	if _, ok := msg.(openColorblindMenuMsg); ok {
		cbv := view.NewColorblindView(a.theme, a.colorblindnessType)
		return a, func() tea.Msg { return view.PushViewMsg{View: cbv} }, true
	}
	if _, ok := msg.(openThemeMenuMsg); ok {
		sponsored := theme.IsSponsor(a.username, a.sponsorKey)
		tv := view.NewThemeView(a.theme, a.themeID, a.baseTheme.Peasant, sponsored)
		return a, func() tea.Msg { return view.PushViewMsg{View: tv} }, true
	}
	return a, nil, false
}

// handleThemeChange — preview/confirm theme and colorblindness updates.
func (a App) handleThemeChange(msg tea.Msg) (App, tea.Cmd, bool) {
	if cp, ok := msg.(view.ColorblindPreviewMsg); ok {
		a.applyColorblindnessType(cp.Type, false)
		return a, nil, true
	}
	if cc, ok := msg.(view.ColorblindConfirmMsg); ok {
		a.applyColorblindnessType(cc.Type, true)
		return a, nil, true
	}
	if tp, ok := msg.(view.ThemePreviewMsg); ok {
		a.applyTheme(tp.ID, false, tp.Degraded)
		return a, nil, true
	}
	if tc, ok := msg.(view.ThemeConfirmMsg); ok {
		a.applyTheme(tc.ID, true, false)
		return a, nil, true
	}
	if lr, ok := msg.(view.ThemeLockedRoyalMsg); ok {
		a.paywallRestoreID = lr.OriginalID
		a.paywallRestoreDeg = lr.OriginalDegraded
		a.showRoyalPaywall = true
		a.royalPaywall = view.NewRoyalPaywall(a.theme)
		return a, nil, true
	}
	return a, nil, false
}

// handleConnection — periodic WhoAmI probe + user-info-after-reconnect.
func (a App) handleConnection(msg tea.Msg) (App, tea.Cmd, bool) {
	if clm, ok := msg.(view.ConnectionLostMsg); ok {
		if a.connected && isConnError(clm.Err) {
			a.connected = false
			a.header.SetConnected(false)
			a.statusBar.SetError(clm.Err.Error())
			return a, scheduleConnCheck(), true
		}
		return a, nil, true
	}
	if _, ok := msg.(connCheckMsg); ok {
		if !a.connected {
			return a, probeCurrentConn(a.client), true
		}
		return a, nil, true
	}
	if pr, ok := msg.(connProbeResultMsg); ok {
		if pr.ok && !a.connected {
			a.connected = true
			a.header.SetConnected(true)
			userCmd := fetchUserInfo(a.client, a.username)
			if v := a.activeView(); v != nil {
				model, cmd := v.Update(view.ConnectionRestoredMsg{})
				a.currentView = model.(view.View)
				return a, tea.Batch(userCmd, cmd), true
			}
			return a, userCmd, true
		}
		if !pr.ok {
			return a, scheduleConnCheck(), true
		}
		return a, nil, true
	}
	if ui, ok := msg.(userInfoMsg); ok {
		a.friendlyName = ui.fullName
		a.header.SetUser(ui.fullName)
		return a, nil, true
	}
	return a, nil, false
}

// handleUpdateLifecycle — :update check/start/done.
func (a App) handleUpdateLifecycle(msg tea.Msg) (App, tea.Cmd, bool) {
	if ucr, ok := msg.(updateCheckResultMsg); ok {
		if ucr.err == nil && ucr.version != "" {
			a.updateVersion = ucr.version
			a.header.SetUpdateVersion(ucr.version)
		}
		return a, nil, true
	}
	if _, ok := msg.(startUpdateMsg); ok {
		if a.updateVersion == "" {
			a.statusBar.SetError(fmt.Sprintf("already on latest version (%s)", version.App))
			return a, nil, true
		}
		a.showUpdateDialog = true
		a.updateDialogYes = true
		return a, nil, true
	}
	if ud, ok := msg.(updateDoneMsg); ok {
		a.isUpdating = false
		if ud.err != nil {
			a.statusBar.SetError(fmt.Sprintf("update failed: %v", ud.err))
			return a, nil, true
		}
		a.UpdatedTo = a.updateVersion
		return a, tea.Quit, true
	}
	return a, nil, false
}

// handleErrorAndSearch — status-bar error display + async search results.
func (a App) handleErrorAndSearch(msg tea.Msg) (App, tea.Cmd, bool) {
	if errMsg, ok := msg.(view.ErrorMsg); ok {
		a.statusBar.SetError(errMsg.Err.Error())
		if a.connected && isConnError(errMsg.Err) {
			a.connected = false
			a.header.SetConnected(false)
			return a, scheduleConnCheck(), true
		}
		return a, nil, true
	}
	if sr, ok := msg.(widget.SearchResultMsg); ok {
		if h, ok := a.activeView().(view.SearchResultHandler); ok {
			return a, h.HandleSearchResult(sr), true
		}
		return a, nil, true
	}
	return a, nil, false
}

// notifyJobPath returns the job path to match against for desktop notifications,
// or "" if the active view should not produce notifications.
// Notifications are suppressed for the all-builds wildcard view (CtxRoot).
func (a App) notifyJobPath() string {
	if !a.notifications || a.termFocused {
		return ""
	}
	v := a.activeView()
	switch v.(type) {
	case *view.BuildsView, *view.StageView, *view.StageLogView, *view.ConsoleView, *view.ScopedView:
	default:
		return ""
	}
	nc, ok := v.(view.NavigationContextProvider)
	if !ok {
		return ""
	}
	ctx := nc.NC()
	if ctx.Level == view.CtxRoot {
		return ""
	}
	return ctx.JobPath()
}

// panelSpec describes one bordered panel: the inner content plus its
// breadcrumb title, error/warn badge, and optional scrollbar marker set.
// Both the content panel and the preview panel are rendered through this.
type panelSpec struct {
	content    string
	height     int
	width      int
	breadcrumb string
	badge      string
	hasScroll  bool
	scrollInfo widget.ScrollInfo
}

func (a App) View() string {
	if a.dbg != nil {
		renderStart := time.Now()
		defer func() { a.dbg.renderMs = time.Since(renderStart).Milliseconds() }()
	}

	// Full-screen views bypass all app chrome.
	if v := a.activeView(); v != nil {
		if fs, ok := v.(view.FullScreen); ok && fs.IsFullScreen() {
			v.SetSize(a.width, a.height)
			return v.View()
		}
	}

	a.prepareHeader()

	v := a.activeView()
	pp, hasPreview := a.resolvePreviewProvider(v)

	headerView := a.header.View()
	commandView := a.statusBar.CommandView()

	contentHeight, previewHeight, innerWidth := a.computePanelDims(headerView, commandView, hasPreview, v)

	a.updateBreadcrumb()

	searchQuery := ""
	if s, ok := v.(view.Searchable); ok {
		searchQuery = s.SearchQuery()
	}
	if hasPreview {
		a.breadcrumb.SetSearchAnnotation("")
	} else {
		a.breadcrumb.SetSearchAnnotation(searchQuery)
	}
	if v != nil {
		a.breadcrumb.SetCount(v.ItemCount())
		v.SetSize(innerWidth, contentHeight)
		if hasPreview {
			pp.SetPreviewSize(innerWidth, previewHeight)
		}
	}

	contentPanel := a.renderContentPanel(v, innerWidth, contentHeight)

	sections := []string{headerView}
	if commandView != "" {
		sections = append(sections, commandView)
	}
	sections = append(sections, contentPanel)

	if hasPreview {
		sections = append(sections, a.renderPreviewPanel(v, pp, searchQuery, innerWidth, previewHeight))
	}

	sections = append(sections, a.navTags.View())
	rendered := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return a.applyOverlays(rendered, v)
}

// prepareHeader pushes view-supplied shortcuts, debug counters, and the
// mine-filter chip into the header before it is rendered this frame.
//
// Pointer receiver is required: View() calls `a.prepareHeader()` on its local
// `a` (value receiver). A value-receiver prepareHeader would mutate a copy,
// losing the SetViewShortcuts / SetDebugCounters / SetMineFilter writes before
// View() reads `a.header` to render. With a pointer receiver, Go addresses
// View's local `a` and the mutations persist for the rest of the frame.
func (a *App) prepareHeader() {
	v := a.activeView()
	if v != nil {
		a.header.SetViewShortcuts(a.viewShortcutsForHeader(v))
	}
	if a.dbg != nil {
		viewType := fmt.Sprintf("%T", a.currentView)
		if idx := strings.LastIndex(viewType, "."); idx >= 0 {
			viewType = viewType[idx+1:]
		}
		a.header.SetDebugCounters(a.dbg.renderMs, a.dbg.updateMs, a.store.TotalEntries(), a.dbg.updateCount, viewType)
	}
	if f, ok := v.(view.Filterable); ok {
		a.header.SetMineFilter(f.ActiveFilters().Mine)
	} else {
		a.header.SetMineFilter(false)
	}
}

// viewShortcutsForHeader returns the view's shortcuts with the `/` and `esc`
// entries rewritten to reflect the current search / active-navigation state:
// `/` becomes active while searching; `esc` flips between "deselect" (active
// nav match) and "exit search" (search pending).
func (a App) viewShortcutsForHeader(v view.View) []component.Shortcut {
	sc := v.Shortcuts()
	inSearch := a.statusBar.Mode() == component.ModeSearch
	if !inSearch {
		if s, ok := v.(view.Searchable); ok && s.SearchQuery() != "" {
			inSearch = true
		}
	}
	hasActiveNav := false
	if nc, ok := v.(view.NavigationClearable); ok {
		hasActiveNav = nc.HasActiveNavigation()
	}
	if dl := a.pendingDeeplink; dl != nil {
		sc = append(sc, component.Nav("V", dl.Label))
	}
	if !inSearch && !hasActiveNav {
		return sc
	}
	for i := range sc {
		switch sc[i].Key {
		case "/":
			if inSearch {
				sc[i].Active = true
			}
		case "esc":
			if hasActiveNav {
				sc[i].Action = "deselect"
			} else {
				sc[i].Action = "exit search"
			}
		}
	}
	return sc
}

// resolvePreviewProvider returns the active view as a PreviewProvider if it
// actually wants a preview pane this frame. ScopedView always satisfies the
// type assertion via delegation but only opts in when its inner view does.
func (a App) resolvePreviewProvider(v view.View) (view.PreviewProvider, bool) {
	pp, hasPreview := v.(view.PreviewProvider)
	if hasPreview {
		type conditionalPreview interface{ HasActivePreview() bool }
		if cp, ok := v.(conditionalPreview); ok && !cp.HasActivePreview() {
			return nil, false
		}
	}
	return pp, hasPreview
}

// computePanelDims figures out the height to give the content panel, the
// preview panel (when present), and the inner width. The view's optional
// ContentHeightHint can shrink the content split when the view knows how
// many rows it actually needs.
func (a App) computePanelDims(headerView, commandView string, hasPreview bool, v view.View) (contentHeight, previewHeight, innerWidth int) {
	usedHeight := lipgloss.Height(headerView)
	if commandView != "" {
		usedHeight += lipgloss.Height(commandView)
	}
	const contentBorderOverhead = 2
	previewBorderOverhead := 0
	if hasPreview {
		previewBorderOverhead = 2
	}
	const navTagsHeight = 1
	totalAvailable := a.height - usedHeight - contentBorderOverhead - previewBorderOverhead - navTagsHeight
	if totalAvailable < 2 {
		totalAvailable = 2
	}
	if hasPreview {
		contentHeight = totalAvailable / 2
		if hint, ok := v.(view.ContentHeightHint); ok {
			if h := hint.ContentHeightHint(); h > 0 && h < totalAvailable {
				contentHeight = h
			}
		}
		previewHeight = totalAvailable - contentHeight
		if contentHeight < 1 {
			contentHeight = 1
		}
		if previewHeight < 1 {
			previewHeight = 1
		}
	} else {
		contentHeight = totalAvailable
	}
	innerWidth = a.width - 2
	return
}

// renderContentPanel builds the main bordered panel: view content + breadcrumb
// title + optional badge + optional scrollbar.
func (a App) renderContentPanel(v view.View, innerWidth, contentHeight int) string {
	var content string
	if v != nil {
		content = v.View()
	}
	spec := panelSpec{
		content:    content,
		height:     contentHeight,
		width:      innerWidth,
		breadcrumb: a.breadcrumb.View(),
	}
	if bv, ok := v.(view.HasBadge); ok {
		spec.badge = bv.Badge()
	}
	if si, ok := v.(view.HasScrollInfo); ok {
		spec.hasScroll = true
		spec.scrollInfo = si.ScrollInfo()
	}
	return a.renderPanel(spec)
}

// renderPreviewPanel builds the secondary bordered preview panel using a
// fresh breadcrumb derived from the PreviewProvider's segment.
func (a App) renderPreviewPanel(v view.View, pp view.PreviewProvider, searchQuery string, innerWidth, previewHeight int) string {
	previewBC := component.NewBreadcrumb(a.theme)
	seg := pp.PreviewBreadcrumb()
	previewBC.SetSegment(&component.BreadcrumbSegment{
		ViewType:      seg.ViewType,
		Context:       seg.Context,
		Running:       seg.Running,
		Mine:          seg.Mine,
		Failed:        seg.Failed,
		ResolvedParts: seg.ResolvedParts,
	})
	if seg.NoTail {
		previewBC.SetCount(pp.PreviewItemCount())
	} else {
		previewBC.SetTail(true)
	}
	previewBC.SetSearchAnnotation(searchQuery)

	spec := panelSpec{
		content:    pp.PreviewView(),
		height:     previewHeight,
		width:      innerWidth,
		breadcrumb: previewBC.View(),
	}
	if pbv, ok := v.(view.HasPreviewBadge); ok {
		spec.badge = pbv.PreviewBadge()
	}
	if psi, ok := v.(view.HasPreviewScrollInfo); ok {
		spec.hasScroll = true
		spec.scrollInfo = psi.PreviewScrollInfo()
	}
	return a.renderPanel(spec)
}

// renderPanel does the bordered-panel rendering both content and preview share:
// pad to height, apply panel border, inject the breadcrumb-title-in-top-border
// trick, and inject the right-edge scrollbar when ScrollInfo is provided.
func (a App) renderPanel(spec panelSpec) string {
	content := spec.content
	contentLines := lipgloss.Height(content)
	if contentLines < spec.height {
		content += strings.Repeat("\n", spec.height-contentLines)
	}
	borderColor, _ := a.theme.PanelBorder.GetForeground().(lipgloss.Color)
	panel := lipgloss.NewStyle().
		Border(a.theme.Border).
		BorderForeground(borderColor).
		Width(spec.width).
		Render(content)
	panel = injectBorderTitle(panel, spec.breadcrumb, spec.badge, borderColor, a.width)
	if spec.hasScroll {
		thumbColor, _ := a.theme.Table.Selected.GetForeground().(lipgloss.Color)
		panel = injectScrollbar(panel, spec.scrollInfo, thumbColor, a.theme)
	}
	return panel
}

// applyOverlays layers view-level popups and modal dialogs over the assembled
// frame. The order matches App.Update's modal precedence: popups < prefs <
// addContext < paywall < help < updateConfirm < updating.
func (a App) applyOverlays(rendered string, v view.View) string {
	if pl, ok := v.(view.PopupLayer); ok && pl.HasPopup() {
		if popup := pl.PopupView(); popup != "" {
			rendered = view.OverlayPopup(rendered, popup, a.width, a.height)
		}
	}
	if a.showPrefsDialog {
		rendered = a.prefsDialog.Render(rendered, a.width, a.height)
	}
	if a.showAddContextDialog {
		rendered = a.addContextDialog.Render(rendered, a.width, a.height)
	}
	if a.showRoyalPaywall {
		rendered = a.royalPaywall.Render(rendered, a.width, a.height)
	}
	if a.showHelp {
		rendered = view.RenderHelpDialog(a.theme, a.registry.ListVisible(), rendered, a.width, a.height)
	}
	if a.showUpdateDialog {
		rendered = view.RenderUpdateConfirmDialog(a.theme, rendered, a.width, a.height, version.App, a.updateVersion, a.updateDialogYes)
	}
	if a.isUpdating {
		rendered = view.RenderUpdatingDialog(a.theme, rendered, a.width, a.height)
	}
	return rendered
}

// handleAddContextResult applies an AddContextDialog result. It returns
// (cmd, done): when done is true the caller should return immediately.
func (a *App) handleAddContextResult(result view.AddContextResult) (tea.Cmd, bool) {
	switch result.Status {
	case view.AddContextCancelled:
		a.showAddContextDialog = false
		return nil, true
	case view.AddContextConfirmed:
		a.showAddContextDialog = false
		cfg := result.Config
		if a.addCtxFn != nil {
			if err := a.addCtxFn(cfg); err != nil {
				a.statusBar.SetError(fmt.Sprintf("save context: %v", err))
				return nil, true
			}
		}
		a.contexts = append(a.contexts, cfg)
		return tea.Batch(
			view.ProbeContextCmd(cfg),
			func() tea.Msg {
				return view.ContextListUpdatedMsg{Contexts: a.contexts, Current: a.currentContextName}
			},
		), true
	}
	if result.TestConn {
		return probeAddContext(a.addContextDialog.CurrentConfig()), true
	}
	return nil, false
}

func (a *App) handleCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		input := a.cmdInput
		a.statusBar.SetMode(component.ModeNormal)
		a.cmdInput = ""
		a.cmdSuggestions = nil
		a.cmdSuggestionIdx = 0
		a.statusBar.SetSuggestion("")

		cmd, err := a.registry.Execute(input)
		if err != nil {
			a.statusBar.SetError(err.Error())
			return a, nil
		}
		return a, cmd

	case "esc":
		a.statusBar.SetMode(component.ModeNormal)
		a.cmdInput = ""
		a.cmdSuggestions = nil
		a.cmdSuggestionIdx = 0
		a.statusBar.SetSuggestion("")
		return a, nil

	case "right", "tab":
		if len(a.cmdSuggestions) > 0 {
			a.cmdInput = a.cmdSuggestions[a.cmdSuggestionIdx]
			a.cmdSuggestions = nil
			a.cmdSuggestionIdx = 0
			a.statusBar.SetInput(a.cmdInput)
			a.statusBar.SetSuggestion("")
		}
		return a, nil

	case "up":
		if len(a.cmdSuggestions) > 0 {
			a.cmdSuggestionIdx = (a.cmdSuggestionIdx - 1 + len(a.cmdSuggestions)) % len(a.cmdSuggestions)
			a.statusBar.SetSuggestion(a.cmdSuggestions[a.cmdSuggestionIdx])
		}
		return a, nil

	case "down":
		if len(a.cmdSuggestions) > 0 {
			a.cmdSuggestionIdx = (a.cmdSuggestionIdx + 1) % len(a.cmdSuggestions)
			a.statusBar.SetSuggestion(a.cmdSuggestions[a.cmdSuggestionIdx])
		}
		return a, nil

	case "backspace":
		if len(a.cmdInput) > 0 {
			a.cmdInput = a.cmdInput[:len(a.cmdInput)-1]
			a.statusBar.SetInput(a.cmdInput)
		}
		a.cmdSuggestions = a.registry.Suggest(a.cmdInput)
		a.cmdSuggestionIdx = 0
		if len(a.cmdSuggestions) > 0 {
			a.statusBar.SetSuggestion(a.cmdSuggestions[0])
		} else {
			a.statusBar.SetSuggestion("")
		}
		return a, nil

	default:
		if len(msg.String()) == 1 {
			a.cmdInput += msg.String()
			a.statusBar.SetInput(a.cmdInput)
			a.cmdSuggestions = a.registry.Suggest(a.cmdInput)
			a.cmdSuggestionIdx = 0
			if len(a.cmdSuggestions) > 0 {
				a.statusBar.SetSuggestion(a.cmdSuggestions[0])
			} else {
				a.statusBar.SetSuggestion("")
			}
		}
		return a, nil
	}
}

func (a *App) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		a.statusBar.SetMode(component.ModeNormal)
		return a, nil

	case "esc":
		a.statusBar.SetMode(component.ModeNormal)
		a.searchInput = ""
		if s, ok := a.activeView().(view.Searchable); ok {
			s.ApplySearch("") // always returns nil for empty pattern
		}
		return a, nil

	case "backspace":
		if len(a.searchInput) > 0 {
			a.searchInput = a.searchInput[:len(a.searchInput)-1]
			a.statusBar.SetInput(a.searchInput)
			if s, ok := a.activeView().(view.Searchable); ok {
				return a, s.ApplySearch(a.searchInput)
			}
		}
		return a, nil

	default:
		if len(msg.String()) == 1 {
			a.searchInput += msg.String()
			a.statusBar.SetInput(a.searchInput)
			if s, ok := a.activeView().(view.Searchable); ok {
				return a, s.ApplySearch(a.searchInput)
			}
		}
		return a, nil
	}
}

func (a App) activeView() view.View {
	return a.currentView
}

// pushView records the current view on the back-stack and makes v active.
// The pushed view is NOT closed — its state (cursor, scroll, data) is
// preserved so ESC returns the user to exactly where they were.
func (a *App) pushView(v view.View) {
	if a.currentView != nil {
		a.navStack = append(a.navStack, a.currentView)
	}
	a.currentView = v
}

// popView pops the top of the back-stack and makes it the active view.
// Returns false when the stack is empty. Closes the current view.
func (a *App) popView() bool {
	if len(a.navStack) == 0 {
		return false
	}
	top := a.navStack[len(a.navStack)-1]
	a.navStack = a.navStack[:len(a.navStack)-1]
	if a.currentView != nil {
		a.currentView.Close()
	}
	a.currentView = top
	return true
}

// replaceView swaps the active view, closing the current one and discarding
// any back-stack entries. Use for command-based jumps (":builds", context
// switch, etc.) that break the push/pop chain.
func (a *App) replaceView(v view.View) {
	a.resetNavStack()
	if a.currentView != nil {
		a.currentView.Close()
	}
	a.currentView = v
}

// resetNavStack closes and discards every view on the back-stack.
// Used by command-based jumps (":builds", ESC→ParentView, context switches)
// that break the push/pop chain.
func (a *App) resetNavStack() {
	for _, v := range a.navStack {
		if v != nil {
			v.Close()
		}
	}
	a.navStack = nil
}

// handleOpenTarget resolves an openTargetMsg into a concrete view and pushes
// it. Empty targets fall back to the current view's NC, preserving today's
// no-arg slash-command behaviour.
func (a App) handleOpenTarget(otm openTargetMsg) (tea.Model, tea.Cmd) {
	current := a.currentContextNC()
	current.Username = a.username
	current.FriendlyName = a.friendlyName
	current.GitUsernames = a.gitUsernames

	nc, err := view.ResolveTarget(otm.target, a.store, current)
	if err != nil {
		return a, errCmd(err)
	}
	nc.Username = a.username
	nc.FriendlyName = a.friendlyName
	nc.GitUsernames = a.gitUsernames

	// Empty target preserves today's behaviour: the no-args slash command
	// always opens the scope-level view of the current location, never a
	// build-specific view (even when the active view is itself CtxBuild).
	if otm.target.IsEmpty() {
		nc = nc.AtScope()
	}

	switch otm.kind {
	case kindBuilds:
		return a.openView(a.buildsViewFor(nc))
	case kindStages:
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			return a.openView(view.NewStageView(a.theme, a.client, a.store, nc, jmodel.Build{Number: nc.Build.Number}))
		}
		return a.openView(view.NewMyBuildsView(a.theme, a.client, a.store, nc.AtScope(), a.slowInterval))
	case kindLogs:
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			return a.openView(view.NewConsoleView(a.theme, a.client, nc))
		}
		return a.openView(view.NewMyConsoleView(a.theme, a.client, a.store, nc.AtScope(), a.slowInterval))
	case kindJobs:
		var jl view.View
		if otm.target.IsEmpty() {
			jl = a.jobListForCurrentContext()
		} else {
			jl = a.jobListForTarget(nc)
		}
		if jl == nil {
			return a, nil
		}
		return a.openView(jl)
	case kindMatrix:
		// :matrix is gated to Matrix-themed running-log views (today's behaviour).
		// Args are accepted but only the AtScope() portion is honoured.
		if a.themeID != theme.ThemeMatrix {
			return a, nil
		}
		rlv, ok := a.currentView.(view.RunningLogView)
		if !ok || !rlv.IsBuildRunning() {
			return a, nil
		}
		return a.openView(view.NewMyMatrixView(a.theme, a.client, a.store, nc.AtScope(), a.slowInterval))
	}
	return a, nil
}

// handleOpenArtifact resolves the current build context and fetches its
// artifacts asynchronously; handleArtifactsFetched then opens the requested
// file (text → in-TUI viewer, else browser) or lists them all when no name
// was given.
func (a App) handleOpenArtifact(oam openArtifactMsg) (tea.Model, tea.Cmd) {
	nc := a.currentContextNC()
	if nc.Level != view.CtxBuild || nc.Build.Number == 0 {
		return a, errCmd(fmt.Errorf("no build in current context; navigate to a build first"))
	}
	jobPath := nc.JobPath()
	buildNum := nc.Build.Number
	client := a.client
	name := oam.name
	return a, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		arts, err := client.GetArtifacts(ctx, jobPath, buildNum)
		if err != nil {
			return view.ErrorMsg{Err: err}
		}
		return artifactsFetchedMsg{nc: nc, build: jmodel.Build{Number: buildNum}, name: name, artifacts: arts}
	}
}

func (a App) handleArtifactsFetched(afm artifactsFetchedMsg) (tea.Model, tea.Cmd) {
	if afm.name == "" {
		return a.openView(view.NewArtifactView(a.theme, afm.artifacts, afm.nc, afm.build, a.client, a.store))
	}
	art, ok := view.FindArtifact(afm.artifacts, afm.name)
	if !ok {
		return a, errCmd(fmt.Errorf("artifact %q not found in build #%d", afm.name, afm.build.Number))
	}
	if view.IsTextArtifact(art.DisplayPath) {
		v := view.NewArtifactFileView(a.theme, a.client, a.store, afm.nc, art, afm.build, afm.artifacts)
		return a.openView(v)
	}
	return a, view.OpenURLCmd(art.URL)
}

// openDeeplink navigates to the view described by a clipboard-derived
// DeepLink. The NC carries folder/project/branch/build; identity fields are
// re-stamped from the current App state so the destination view inherits the
// authenticated user.
func (a App) openDeeplink(dl *view.DeepLink) (tea.Model, tea.Cmd) {
	nc := dl.NC
	nc.Username = a.username
	nc.FriendlyName = a.friendlyName
	nc.GitUsernames = a.gitUsernames

	switch dl.Kind {
	case view.DeepLinkBuilds:
		return a.openView(a.buildsViewFor(nc))
	case view.DeepLinkStages:
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			return a.openView(view.NewStageView(a.theme, a.client, a.store, nc, jmodel.Build{Number: nc.Build.Number}))
		}
		return a.openView(view.NewMyBuildsView(a.theme, a.client, a.store, nc.AtScope(), a.slowInterval))
	case view.DeepLinkLogs:
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			return a.openView(view.NewConsoleView(a.theme, a.client, nc))
		}
		return a.openView(view.NewMyConsoleView(a.theme, a.client, a.store, nc.AtScope(), a.slowInterval))
	case view.DeepLinkJobs:
		if jl := a.jobListForTarget(nc); jl != nil {
			return a.openView(jl)
		}
		return a, nil
	}
	return a, nil
}

// openView is the "replace current view, refresh breadcrumb, kick off Init"
// trio used by every command-driven view jump. Centralising the sequence
// avoids skewed-by-one drift between paths.
func (a App) openView(v view.View) (tea.Model, tea.Cmd) {
	a.replaceView(v)
	a.updateBreadcrumb()
	return a, v.Init()
}

// buildsViewFor returns a BuildsView scoped to the given NC. The NC is
// reduced to scope-level (Build/Stage stripped) before dispatch.
func (a *App) buildsViewFor(nc view.NavigationContext) *view.BuildsView {
	nc = nc.AtScope()
	switch nc.Level {
	case view.CtxBranch:
		return view.NewBuildsView(a.theme, a.client, a.store, nc, view.NewBranchBuildsProvider(a.client, a.store, nc))
	case view.CtxProject:
		return view.NewBuildsView(a.theme, a.client, a.store, nc, view.NewProjectBuildsProvider(a.client, a.store, nc))
	case view.CtxFolder:
		return view.NewFolderBuildsView(a.theme, a.client, a.store, nc.FolderPath, a.username, a.gitUsernames, a.slowInterval)
	default:
		return view.NewAllBuildsView(a.theme, a.client, a.store, a.username, a.gitUsernames, a.slowInterval)
	}
}

// jobListForTarget returns the JobList for an explicitly-targeted NC: drills
// INTO a project (showing its branches), into a folder (showing its children),
// or to the root dashboard. Used when the user supplied target arguments.
func (a *App) jobListForTarget(nc view.NavigationContext) view.View {
	switch nc.Level {
	case view.CtxFolder:
		title := nc.FolderPath
		if idx := strings.LastIndex(nc.FolderPath, "/"); idx >= 0 {
			title = nc.FolderPath[idx+1:]
		}
		return view.NewJobList(a.theme, a.client, a.store, nc.FolderPath, title, false, a.username, a.gitUsernames)
	case view.CtxProject, view.CtxBranch, view.CtxBuild, view.CtxStage:
		pp := nc.ProjectName
		if nc.FolderPath != "" {
			pp = nc.FolderPath + "/" + nc.ProjectName
		}
		return view.NewJobList(a.theme, a.client, a.store, pp, nc.ProjectName, true, a.username, a.gitUsernames)
	default:
		return a.initialView
	}
}

// jobListForCurrentContext returns the JobList that represents "sideways"
// navigation to the jobs level for the current context. Returns nil when
// the active view is already the root JobList.
func (a *App) jobListForCurrentContext() view.View {
	nc := a.currentContextNC()
	switch nc.Level {
	case view.CtxBranch, view.CtxBuild, view.CtxStage:
		// Navigate to the project's branch listing.
		pp := nc.ProjectName
		if nc.FolderPath != "" {
			pp = nc.FolderPath + "/" + nc.ProjectName
		}
		return view.NewJobList(a.theme, a.client, a.store, pp, nc.ProjectName, true, a.username, a.gitUsernames)
	case view.CtxProject:
		// Navigate up to the parent folder's job list.
		if nc.FolderPath != "" {
			title := nc.FolderPath
			if idx := strings.LastIndex(nc.FolderPath, "/"); idx >= 0 {
				title = nc.FolderPath[idx+1:]
			}
			return view.NewJobList(a.theme, a.client, a.store, nc.FolderPath, title, false, a.username, a.gitUsernames)
		}
		return a.initialView
	case view.CtxFolder:
		title := nc.FolderPath
		if idx := strings.LastIndex(nc.FolderPath, "/"); idx >= 0 {
			title = nc.FolderPath[idx+1:]
		}
		return view.NewJobList(a.theme, a.client, a.store, nc.FolderPath, title, false, a.username, a.gitUsernames)
	default:
		// CtxRoot: if already on a JobList, go to parent; otherwise go to root.
		if jl, ok := a.currentView.(*view.JobList); ok {
			if parent := jl.ParentView(a.theme, a.client, a.store); parent != nil {
				return parent
			}
			return nil // already at root JobList
		}
		return a.initialView
	}
}

// currentContextURL returns the base URL of the currently active Jenkins
// context, or "" when none is configured. Used by the clipboard deeplink
// check to validate that a clipboard URL points at this server.
func (a *App) currentContextURL() string {
	for _, c := range a.contexts {
		if c.Name == a.currentContextName {
			return c.URL
		}
	}
	return ""
}

// currentContextNC infers the NavigationContext from the active view.
func (a *App) currentContextNC() view.NavigationContext {
	if ncp, ok := a.currentView.(view.NavigationContextProvider); ok {
		return ncp.NC()
	}
	return view.NavigationContext{Level: view.CtxRoot, Username: a.username, FriendlyName: a.friendlyName}
}

func (a *App) updateLayout() {
	a.header.SetWidth(a.width)
	a.statusBar.SetWidth(a.width)
}

// rebuildTheme recomputes a.theme from baseTheme + colorblind filter and
// broadcasts ThemeChangedMsg to all components and views.
func (a *App) rebuildTheme() {
	a.theme = theme.ApplyColorblindFilter(a.baseTheme, a.colorblindnessType)
	a.header.SetTheme(a.theme)
	a.breadcrumb.SetTheme(a.theme)
	a.statusBar.SetTheme(a.theme)
	a.navTags.SetTheme(a.theme)
	themeMsg := view.ThemeChangedMsg{Theme: a.theme}
	if a.currentView != nil {
		updated, _ := a.currentView.Update(themeMsg)
		a.currentView = updated.(view.View)
	}
	// Keep the initial view in sync so navigating back doesn't show stale colours.
	if a.initialView != nil && a.initialView != a.currentView {
		updated, _ := a.initialView.Update(themeMsg)
		a.initialView = updated.(view.View)
	}
}

// applyColorblindnessType updates the active theme and broadcasts ThemeChangedMsg
// to all views. When persist is true the selection is also saved to disk.
func (a *App) applyColorblindnessType(cbType theme.ColorblindnessType, persist bool) {
	a.colorblindnessType = cbType
	a.rebuildTheme()
	if persist && a.saveFn != nil {
		_ = a.saveFn(cbType)
	}
}

// applyTheme switches the base theme to the given ID and broadcasts ThemeChangedMsg.
// When persist is true the selection is also saved to disk.
// When degraded is true, the Royal peasant mode is activated (no crown, "Peasant" label).
func (a *App) applyTheme(id theme.ThemeID, persist bool, degraded bool) {
	a.themeID = id
	base := theme.ByID(id)
	if degraded {
		base.Peasant = true
	}
	a.baseTheme = base
	a.rebuildTheme()
	if persist && a.saveThemeFn != nil {
		_ = a.saveThemeFn(string(id))
	}
}

func (a *App) updateBreadcrumb() {
	v := a.activeView()
	if v == nil {
		a.navTags.SetViewType("jobs")
		return
	}
	if bp, ok := v.(view.BreadcrumbProvider); ok {
		seg := bp.Breadcrumb()
		a.breadcrumb.SetSegment(&component.BreadcrumbSegment{
			ViewType:      seg.ViewType,
			Context:       seg.Context,
			Running:       seg.Running,
			Mine:          seg.Mine,
			Failed:        seg.Failed,
			ResolvedParts: seg.ResolvedParts,
		})
		navTag := seg.ViewType
		if seg.NavTag != "" {
			navTag = seg.NavTag
		}
		a.navTags.SetViewType(navTag)
		a.navTags.SetRooted(len(seg.Context) > 0 && seg.Context[0].Text == "*")
	} else {
		a.breadcrumb.SetSegment(nil)
		a.breadcrumb.SetSegments([]string{v.Title()})
		a.navTags.SetViewType("jobs")
		a.navTags.SetRooted(false)
	}
}

// injectBorderTitle rebuilds the top border line with a centered title and an optional right badge.
// Uses the known rounded border characters and total width to avoid ANSI parsing issues.
func injectBorderTitle(rendered, centerTitle, rightBadge string, borderColor lipgloss.Color, totalWidth int) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}

	const (
		cornerL = "╭"
		cornerR = "╮"
		horiz   = "─"
	)

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	innerWidth := totalWidth - 2

	centerStr := " " + centerTitle + " "
	centerDispW := lipgloss.Width(centerStr)

	rightDispW := 0
	if rightBadge != "" {
		rightDispW = lipgloss.Width(" " + rightBadge + " ")
	}

	leftPad := (innerWidth - centerDispW - rightDispW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	middlePad := innerWidth - leftPad - centerDispW - rightDispW
	if middlePad < 0 {
		middlePad = 0
	}

	var topLine string
	if rightBadge == "" {
		topLine = borderStyle.Render(cornerL+strings.Repeat(horiz, leftPad)) +
			borderStyle.Render(" ") + centerTitle + borderStyle.Render(" ") +
			borderStyle.Render(strings.Repeat(horiz, middlePad)+cornerR)
	} else {
		topLine = borderStyle.Render(cornerL+strings.Repeat(horiz, leftPad)) +
			borderStyle.Render(" ") + centerTitle + borderStyle.Render(" ") +
			borderStyle.Render(strings.Repeat(horiz, middlePad)) +
			borderStyle.Render(" ") + rightBadge + borderStyle.Render(" "+cornerR)
	}

	lines[0] = topLine
	return strings.Join(lines, "\n")
}

// rightBorderRe matches the right border │ character (with surrounding ANSI codes) at end of line.
var rightBorderRe = regexp.MustCompile(`(?:\x1b\[[0-9;]*m)*│(?:\x1b\[[0-9;]*m)*$`)

// injectScrollbar colors a portion of the right border to indicate scroll position.
// Only applies when totalLines exceeds the viewport (i.e., the content is scrollable).
// Markers (errors, warnings, search matches) are painted in the gutter; the thumb takes priority.
func injectScrollbar(rendered string, scroll widget.ScrollInfo, thumbColor lipgloss.Color, t theme.Theme) string {
	if scroll.TotalLines <= scroll.ViewHeight || scroll.ViewHeight <= 0 {
		return rendered
	}

	lines := strings.Split(rendered, "\n")
	// Content lines are between the top border (index 0) and bottom border (last non-empty line).
	contentLineCount := len(lines) - 2
	if contentLineCount <= 0 {
		return rendered
	}

	thumbSize := max(1, contentLineCount*contentLineCount/scroll.TotalLines)
	maxOffset := scroll.TotalLines - scroll.ViewHeight
	thumbTop := 0
	if maxOffset > 0 {
		thumbTop = scroll.Offset * (contentLineCount - thumbSize) / maxOffset
	}
	thumbBottom := thumbTop + thumbSize

	// Build per-row marker colors. Higher-priority kinds win on overlap.
	// Priority: error(3) > warning(2) > search(1).
	type rowMark struct {
		color    lipgloss.Color
		priority int
	}
	var rowMarkers map[int]rowMark
	if len(scroll.Markers) > 0 {
		errColor, _ := t.Log.Error.GetForeground().(lipgloss.Color)
		warnColor, _ := t.Log.Warning.GetForeground().(lipgloss.Color)
		searchColor, _ := t.Search.CurrentMatch.GetBackground().(lipgloss.Color)

		rowMarkers = make(map[int]rowMark, len(scroll.Markers))
		for _, m := range scroll.Markers {
			// Use the same coordinate mapping as the thumb so that when a marked
			// line is at the top of the viewport its marker lands on thumbTop.
			row := min(m.Line, maxOffset) * (contentLineCount - thumbSize) / maxOffset
			var col lipgloss.Color
			var pri int
			switch m.Kind {
			case widget.ScrollMarkerError:
				col, pri = errColor, 3
			case widget.ScrollMarkerWarning:
				col, pri = warnColor, 2
			case widget.ScrollMarkerSearch:
				col, pri = searchColor, 1
			}
			if existing, ok := rowMarkers[row]; !ok || pri > existing.priority {
				rowMarkers[row] = rowMark{color: col, priority: pri}
			}
		}
	}

	thumbStyle := lipgloss.NewStyle().Foreground(thumbColor)
	thumb := thumbStyle.Render("│")

	for i := 1; i <= contentLineCount; i++ {
		lineIdx := i - 1
		if lineIdx >= thumbTop && lineIdx < thumbBottom {
			lines[i] = rightBorderRe.ReplaceAllLiteralString(lines[i], thumb)
		} else if rm, ok := rowMarkers[lineIdx]; ok {
			marker := lipgloss.NewStyle().Foreground(rm.color).Render("│")
			lines[i] = rightBorderRe.ReplaceAllLiteralString(lines[i], marker)
		}
	}

	return strings.Join(lines, "\n")
}

// probeAddContext fires a connection test for the add-context dialog.
// The result arrives as addContextProbeResultMsg.
func probeAddContext(ctx config.ContextConfig) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client := jenkins.NewClient(ctx.URL, ctx.Username, ctx.Token, ctx.Insecure)
		_, err := client.WhoAmI(c)
		if err != nil {
			return addContextProbeResultMsg{ok: false, msg: err.Error()}
		}
		return addContextProbeResultMsg{ok: true}
	}
}

// scheduleConnCheck returns a command that delivers connCheckMsg after 1 second.
func scheduleConnCheck() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return connCheckMsg{}
	})
}

// probeCurrentConn fires a WhoAmI against the live client and returns connProbeResultMsg.
func probeCurrentConn(client jmodel.JenkinsClient) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.WhoAmI(c)
		return connProbeResultMsg{ok: err == nil}
	}
}

// fetchUserInfo calls WhoAmI and returns userInfoMsg with the full name.
// Falls back to fallback if the request fails or returns an empty name.
func fetchUserInfo(client jmodel.JenkinsClient, fallback string) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		user, err := client.WhoAmI(c)
		if err != nil || user.FullName == "" {
			return userInfoMsg{fullName: fallback}
		}
		return userInfoMsg{fullName: user.FullName}
	}
}

// checkForUpdateCmd fetches the latest GitHub release tag in the background.
// It delivers updateCheckResultMsg with a non-empty version only when a newer
// release exists; errors are silently swallowed to avoid blocking startup.
func checkForUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		tag, err := updater.LatestVersion()
		if err != nil {
			return updateCheckResultMsg{err: err}
		}
		if updater.IsNewer(version.App, tag) {
			return updateCheckResultMsg{version: tag}
		}
		return updateCheckResultMsg{}
	}
}

// doUpdateCmd runs the self-update in the background and delivers updateDoneMsg.
func doUpdateCmd(latestTag string) tea.Cmd {
	return func() tea.Msg {
		err := updater.SelfUpdate(latestTag)
		return updateDoneMsg{err: err}
	}
}

// isConnError returns true when err indicates a network or server connectivity
// problem (as opposed to an app-level error like a missing resource).
func isConnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "executing request") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "HTTP 5")
}

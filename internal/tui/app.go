package tui

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/monitor"
	"github.com/Breina/Jenking/internal/notify"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/view"
	"github.com/Breina/Jenking/internal/updater"
	"github.com/Breina/Jenking/internal/version"
)

// wireMonitor builds a RunningBuildsMonitor and attaches a Reconciler to the
// store's Registry so background GetBuild fetches drive build-status transitions
// for builds the monitor never observed in its prev set (e.g. transient builds
// that completed between two 1s polls).
func wireMonitor(client jenkins.JenkinsClient, store *cache.Store) *monitor.RunningBuildsMonitor {
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
	savePrefsFn          func(notifications bool, gitUsernames []string, refreshInterval, slowInterval time.Duration, maxLogLines int, logLevel string) error
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
	client               jenkins.JenkinsClient
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
}

// NewApp creates the root application model.
func NewApp(t theme.Theme, baseTheme theme.Theme, themeID theme.ThemeID, cbType theme.ColorblindnessType, keys KeyMap, client jenkins.JenkinsClient, store *cache.Store, username string, friendlyName string, gitUsernames []string, refreshInterval, slowInterval time.Duration, header component.Header, breadcrumb component.Breadcrumb, statusBar component.StatusBar, initialView view.View, saveFn func(theme.ColorblindnessType) error, saveThemeFn func(string) error, debug bool, sponsorKey string, notifications bool, maxLogLines int, logLevel string, contexts []config.ContextConfig, currentContextName string, diskStoreFn func(string) *cache.DiskStore, addCtxFn func(config.ContextConfig) error, delCtxFn func(string) error, setCtxFn func(string) error, savePrefsFn func(notifications bool, gitUsernames []string, refreshInterval, slowInterval time.Duration, maxLogLines int, logLevel string) error) App {
	registry := command.NewRegistry()

	registry.Register(command.Command{
		Name:    "quit",
		Aliases: []string{"q"},
		Help:    "Exit application",
		Execute: func(args []string) tea.Cmd {
			return tea.Quit
		},
	})

	registry.Register(command.Command{
		Name:    "colorblind",
		Aliases: []string{"cb"},
		Help:    "Select colorblindness compensation type",
		Execute: func(args []string) tea.Cmd {
			if len(args) == 0 {
				return func() tea.Msg { return openColorblindMenuMsg{} }
			}
			cbType := theme.ColorblindnessType(args[0])
			for _, t := range theme.AllColorblindnessTypes {
				if t == cbType {
					return func() tea.Msg { return view.ColorblindConfirmMsg{Type: cbType} }
				}
			}
			return func() tea.Msg {
				return view.ErrorMsg{Err: fmt.Errorf("unknown colorblindness type: %s", args[0])}
			}
		},
		ArgSuggest: func(prefix string) []string {
			var result []string
			for _, t := range theme.AllColorblindnessTypes {
				s := string(t)
				if strings.HasPrefix(s, prefix) && s != prefix {
					result = append(result, s)
				}
			}
			sort.Strings(result)
			return result
		},
	})

	// Suggestion closures capture `store` at registration time. After a
	// :context switch a.store is reassigned but the captured reference is
	// not — suggestions remain tied to the original Jenkins instance until
	// the process restarts. Same caveat as the :context ArgSuggest above.
	projectSuggest := func(prefix string) []string {
		return view.TargetArgSuggest(store, prefix)
	}

	registry.Register(command.Command{
		Name:       "builds",
		Aliases:    []string{"b", "build"},
		Help:       "Show builds [<project> [<branch>]]",
		Execute:    openTargetCmd(kindBuilds),
		ArgSuggest: projectSuggest,
	})

	registry.Register(command.Command{
		Name:       "stages",
		Aliases:    []string{"s", "stage"},
		Help:       "Show stages [<project> [<branch>] [#<n>|#last]]",
		Execute:    openTargetCmd(kindStages),
		ArgSuggest: projectSuggest,
	})

	registry.Register(command.Command{
		Name:       "jobs",
		Aliases:    []string{"j", "job"},
		Help:       "Navigate to job list [<project>]",
		Execute:    openTargetCmd(kindJobs),
		ArgSuggest: projectSuggest,
	})

	registry.Register(command.Command{
		Name:       "log",
		Aliases:    []string{"l", "logs"},
		Help:       "Show console log [<project> [<branch>] [#<n>|#last]]",
		Execute:    openTargetCmd(kindLogs),
		ArgSuggest: projectSuggest,
	})

	registry.Register(command.Command{
		Name:    "running",
		Aliases: []string{"r"},
		Help:    "Show running builds",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openRunningBuildsMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "matrix",
		Help:    "The Matrix has you...",
		Hidden:  true,
		Execute: openTargetCmd(kindMatrix),
	})

	registry.Register(command.Command{
		Name:    "theme",
		Aliases: []string{"th"},
		Help:    "Select colour theme",
		Execute: func(args []string) tea.Cmd {
			if len(args) == 0 {
				return func() tea.Msg { return openThemeMenuMsg{} }
			}
			id := theme.ThemeID(args[0])
			for _, t := range theme.AllThemes {
				if t.ID == id {
					return func() tea.Msg { return view.ThemeConfirmMsg{ID: id} }
				}
			}
			return func() tea.Msg { return view.ErrorMsg{Err: fmt.Errorf("unknown theme: %s", args[0])} }
		},
		ArgSuggest: func(prefix string) []string {
			var result []string
			for _, t := range theme.AllThemes {
				s := string(t.ID)
				if strings.HasPrefix(s, prefix) && s != prefix {
					result = append(result, s)
				}
			}
			sort.Strings(result)
			return result
		},
	})

	registry.Register(command.Command{
		Name: "help",
		Help: "Show available commands",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openHelpMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "config",
		Aliases: []string{"preferences", "prefs"},
		Help:    "Edit preferences (notifications, git usernames, refresh interval)",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openPrefsMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "context",
		Aliases: []string{"ctx"},
		Help:    "Manage Jenkins contexts (switch, add, delete)",
		Execute: func(args []string) tea.Cmd {
			if len(args) == 0 {
				return func() tea.Msg { return openContextMenuMsg{} }
			}
			return func() tea.Msg { return switchContextMsg{name: args[0]} }
		},
		ArgSuggest: func(prefix string) []string {
			var result []string
			for _, ctx := range contexts {
				if strings.HasPrefix(ctx.Name, prefix) && ctx.Name != prefix {
					result = append(result, ctx.Name)
				}
			}
			sort.Strings(result)
			return result
		},
	})

	registry.Register(command.Command{
		Name:    "update",
		Aliases: []string{"upgrade"},
		Help:    "Update Jenking to the latest release",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return startUpdateMsg{} }
		},
	})

	var dbg *debugStats
	if debug {
		dbg = &debugStats{}
	}

	return App{
		theme:              t,
		baseTheme:          baseTheme,
		themeID:            themeID,
		saveThemeFn:        saveThemeFn,
		sponsorKey:         sponsorKey,
		colorblindnessType: cbType,
		saveFn:             saveFn,
		keys:               keys,
		client:             client,
		store:              store,
		monitor:            wireMonitor(client, store),
		username:           username,
		friendlyName:       friendlyName,
		gitUsernames:       gitUsernames,
		contexts:           contexts,
		currentContextName: currentContextName,
		diskStoreFn:        diskStoreFn,
		addCtxFn:           addCtxFn,
		delCtxFn:           delCtxFn,
		setCtxFn:           setCtxFn,
		savePrefsFn:        savePrefsFn,
		refreshInterval:    refreshInterval,
		slowInterval:       slowInterval,
		maxLogLines:        maxLogLines,
		logLevel:           logLevel,
		registry:           registry,
		header:             header,
		breadcrumb:         breadcrumb,
		statusBar:          statusBar,
		navTags:            component.NewNavTags(t),
		currentView:        initialView,
		initialView:        initialView,
		notifications:      notifications,
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
	return tea.Batch(cmds...)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.dbg != nil {
		start := time.Now()
		defer func() {
			a.dbg.updateMs = time.Since(start).Milliseconds()
			a.dbg.updateCount++
		}()
	}

	// Monitor consumes its own internal messages (poll result, tick).
	if handled, cmds := a.monitor.HandleMsg(msg); handled {
		return a, tea.Batch(cmds...)
	}

	// Add-context dialog intercepts all key events while open.
	if a.showAddContextDialog {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			updated, result := a.addContextDialog.Update(keyMsg)
			a.addContextDialog = updated
			if cmd, done := a.handleAddContextResult(result); done {
				return a, cmd
			}
			return a, nil
		}
	}

	// Preferences dialog intercepts all key events while open.
	if a.showPrefsDialog {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
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
				if a.savePrefsFn != nil {
					if err := a.savePrefsFn(p.Notifications, p.GitUsernames, p.RefreshInterval, p.SlowRefreshInterval, p.MaxLogLines, p.LogLevel); err != nil {
						a.statusBar.SetError(fmt.Sprintf("save preferences: %v", err))
					}
				}
			}
			return a, nil
		}
	}

	// Update confirm dialog intercepts all key events while open.
	if a.showUpdateDialog {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "left", "right", "h", "l":
				a.updateDialogYes = !a.updateDialogYes
			case "y", "Y":
				a.showUpdateDialog = false
				a.isUpdating = true
				return a, doUpdateCmd(a.updateVersion)
			case "enter":
				if a.updateDialogYes {
					a.showUpdateDialog = false
					a.isUpdating = true
					return a, doUpdateCmd(a.updateVersion)
				}
				a.showUpdateDialog = false
			case "n", "N", "esc", "q":
				a.showUpdateDialog = false
			}
			return a, nil
		}
	}

	// Royal paywall intercepts all key events while open.
	if a.showRoyalPaywall {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			updated, result, closed := a.royalPaywall.Update(keyMsg)
			a.royalPaywall = updated
			if closed {
				a.showRoyalPaywall = false
				if result != nil {
					switch *result {
					case view.PaywallResultSponsor:
						// Open browser and revert to the pre-paywall theme.
						_ = exec.Command("xdg-open", view.GitHubSponsorsURL).Start()
						a.applyTheme(a.paywallRestoreID, false, a.paywallRestoreDeg)
					case view.PaywallResultDegrade:
						a.applyTheme(theme.ThemeRoyal, true, true)
					case view.PaywallResultCancel:
						a.applyTheme(a.paywallRestoreID, false, a.paywallRestoreDeg)
					}
				}
			}
			return a, nil
		}
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
		// Error displayed — any key dismisses it
		if a.statusBar.HasError() {
			a.statusBar.ClearError()
			return a, nil
		}

		// Command mode input
		if a.statusBar.Mode() == component.ModeCommand {
			return a.handleCommandInput(msg)
		}

		// Search mode input
		if a.statusBar.Mode() == component.ModeSearch {
			return a.handleSearchInput(msg)
		}

		// Help overlay — any key closes it
		if a.showHelp {
			a.showHelp = false
			return a, nil
		}

		// Global keys
		switch {
		case key.Matches(msg, a.keys.Quit):
			return a, tea.Quit
		case key.Matches(msg, a.keys.Command):
			a.statusBar.SetMode(component.ModeCommand)
			a.cmdInput = ""
			return a, nil
		case key.Matches(msg, a.keys.Search):
			if _, ok := a.activeView().(view.Searchable); ok {
				a.statusBar.SetMode(component.ModeSearch)
				a.searchInput = ""
				a.statusBar.SetInput("")
				return a, nil
			}
		case key.Matches(msg, a.keys.Back):
			// If a popup is open, let the view consume ESC to close it first.
			if pl, ok := a.activeView().(view.PopupLayer); ok && pl.HasPopup() {
				model, cmd := a.activeView().Update(msg)
				a.currentView = model.(view.View)
				return a, cmd
			}
			// Esc clears active search before navigating back
			if s, ok := a.activeView().(view.Searchable); ok && s.SearchQuery() != "" {
				s.ApplySearch("")
				return a, nil
			}
			// Pop the back-stack first: returns the user to the view they
			// pushed from with its state (cursor, scroll, data) preserved.
			var closeCmd tea.Cmd
			if cw, ok := a.currentView.(interface{ CloseCmd() tea.Cmd }); ok {
				closeCmd = cw.CloseCmd()
			}
			if a.popView() {
				a.updateBreadcrumb()
				// Restart the popped-to view's data flow. Its tea.Cmd
				// messages were dropped by the child while pushed, so
				// its polling chain has died. Calling Init() re-fetches
				// and reschedules. Each view's Init() is required to be
				// idempotent w.r.t. re-entry.
				return a, tea.Batch(closeCmd, a.currentView.Init())
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
				return a, a.currentView.Init()
			}
			// No parent: go to Dashboard (or stay if already there)
			if a.currentView != a.initialView {
				a.activeView().Close()
				a.currentView = a.initialView
				a.updateBreadcrumb()
				return a, a.initialView.Init()
			}
			return a, nil
		case msg.String() == "U" && a.updateVersion != "":
			a.showUpdateDialog = true
			a.updateDialogYes = true
			return a, nil
		case key.Matches(msg, a.keys.RunningBuilds):
			if bv, ok := a.activeView().(*view.BuildsView); ok && bv.NC().Level == view.CtxRoot {
				// Already at builds(*) — toggle the running filter.
				bv.ToggleRunning()
				return a, nil
			}
			// Navigate to builds(*) with the running filter pre-enabled.
			av := view.NewAllBuildsView(a.theme, a.client, a.store, a.username, a.gitUsernames, a.slowInterval)
			av.ToggleRunning()
			a.replaceView(av)
			a.updateBreadcrumb()
			return a, av.Init()
		}

		// Delegate to active view
		if v := a.activeView(); v != nil {
			model, cmd := v.Update(msg)
			a.currentView = model.(view.View)
			return a, cmd
		}
	}

	// RunningBuildsUpdatedMsg — update header count, notify on new builds, forward to active view.
	if msg, ok := msg.(view.RunningBuildsUpdatedMsg); ok {
		a.header.SetRunningBuilds(msg.Count, "R")
		if watchPath := a.notifyJobPath(); watchPath != "" {
			for _, key := range msg.Arrived {
				jobPath, number := jenkins.ParseBuildKey(key)
				if strings.HasPrefix(jobPath, watchPath) {
					notify.Send("Build Started", fmt.Sprintf("#%d · %s", number, jobPath))
				}
			}
		}
		if v := a.activeView(); v != nil {
			model, cmd := v.Update(msg)
			a.currentView = model.(view.View)
			return a, cmd
		}
		return a, nil
	}

	// Unified navigation: :builds / :stages / :jobs / :logs / :matrix.
	if otm, ok := msg.(openTargetMsg); ok {
		return a.handleOpenTarget(otm)
	}

	// :running command — navigate to builds(*) with the running filter pre-enabled.
	if _, ok := msg.(openRunningBuildsMsg); ok {
		if bv, ok := a.activeView().(*view.BuildsView); ok && bv.NC().Level == view.CtxRoot {
			bv.ToggleRunning()
			return a, nil
		}
		av := view.NewAllBuildsView(a.theme, a.client, a.store, a.username, a.gitUsernames, a.slowInterval)
		av.ToggleRunning()
		a.replaceView(av)
		a.updateBreadcrumb()
		return a, av.Init()
	}

	// Connection probe result for the add-context dialog.
	if apr, ok := msg.(addContextProbeResultMsg); ok {
		if a.showAddContextDialog {
			a.addContextDialog.SetConnStatus(apr.ok, apr.msg)
			if pending, ready := a.addContextDialog.ConsumePending(); ready {
				if cmd, done := a.handleAddContextResult(pending); done {
					return a, cmd
				}
			}
		}
		return a, nil
	}

	// :context/:ctx command — switch Jenkins environment.
	if sc, ok := msg.(switchContextMsg); ok {
		var target *config.ContextConfig
		for i := range a.contexts {
			if a.contexts[i].Name == sc.name {
				target = &a.contexts[i]
				break
			}
		}
		if target == nil {
			a.statusBar.SetError(fmt.Sprintf("unknown context: %s", sc.name))
			return a, nil
		}
		if target.Name == a.currentContextName {
			return a, nil
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
		return a, tea.Batch(dashboard.Init(), a.monitor.Init(), fetchUserInfo(newClient, target.Username))
	}

	// :help command — show command list overlay.
	if _, ok := msg.(openHelpMsg); ok {
		a.showHelp = true
		return a, nil
	}

	// Open context view — push onto the nav stack. Probes fire from its Init.
	if _, ok := msg.(openContextMenuMsg); ok {
		cv := view.NewContextView(a.theme, a.contexts, a.currentContextName)
		return a, func() tea.Msg { return view.PushViewMsg{View: cv} }
	}

	// Open preferences dialog
	if _, ok := msg.(openPrefsMsg); ok {
		a.prefsDialog = view.NewPrefsDialog(a.theme, view.PrefsValues{
			Notifications:       a.notifications,
			GitUsernames:        a.gitUsernames,
			RefreshInterval:     a.refreshInterval,
			SlowRefreshInterval: a.slowInterval,
			MaxLogLines:         a.maxLogLines,
			LogLevel:            a.logLevel,
		})
		a.prefsDialog.SetSize(a.width, a.height)
		a.showPrefsDialog = true
		return a, nil
	}

	// Open colorblind view — pushed onto the nav stack.
	if _, ok := msg.(openColorblindMenuMsg); ok {
		cbv := view.NewColorblindView(a.theme, a.colorblindnessType)
		return a, func() tea.Msg { return view.PushViewMsg{View: cbv} }
	}

	// Open theme view — pushed onto the nav stack.
	if _, ok := msg.(openThemeMenuMsg); ok {
		sponsored := theme.IsSponsor(a.username, a.sponsorKey)
		tv := view.NewThemeView(a.theme, a.themeID, a.baseTheme.Peasant, sponsored)
		return a, func() tea.Msg { return view.PushViewMsg{View: tv} }
	}

	// ColorblindPreviewMsg / ColorblindConfirmMsg from ColorblindView.
	if cp, ok := msg.(view.ColorblindPreviewMsg); ok {
		a.applyColorblindnessType(cp.Type, false)
		return a, nil
	}
	if cc, ok := msg.(view.ColorblindConfirmMsg); ok {
		a.applyColorblindnessType(cc.Type, true)
		return a, nil
	}

	// Theme preview / confirm / locked-royal from ThemeView.
	if tp, ok := msg.(view.ThemePreviewMsg); ok {
		a.applyTheme(tp.ID, false, tp.Degraded)
		return a, nil
	}
	if tc, ok := msg.(view.ThemeConfirmMsg); ok {
		a.applyTheme(tc.ID, true, false)
		return a, nil
	}
	if lr, ok := msg.(view.ThemeLockedRoyalMsg); ok {
		a.paywallRestoreID = lr.OriginalID
		a.paywallRestoreDeg = lr.OriginalDegraded
		a.showRoyalPaywall = true
		a.royalPaywall = view.NewRoyalPaywall(a.theme)
		return a, nil
	}

	// Context view actions.
	if sr, ok := msg.(view.ContextSwitchRequestMsg); ok {
		return a.Update(switchContextMsg{name: sr.Name})
	}
	if dr, ok := msg.(view.ContextDeleteRequestMsg); ok {
		name := dr.Name
		if a.delCtxFn != nil {
			if err := a.delCtxFn(name); err != nil {
				a.statusBar.SetError(fmt.Sprintf("delete context: %v", err))
				return a, nil
			}
		}
		newContexts := make([]config.ContextConfig, 0, len(a.contexts))
		for _, c := range a.contexts {
			if c.Name != name {
				newContexts = append(newContexts, c)
			}
		}
		a.contexts = newContexts
		// If the active context was deleted, switch to first available.
		if name == a.currentContextName && len(newContexts) > 0 {
			return a.Update(switchContextMsg{name: newContexts[0].Name})
		}
		// Otherwise, notify the active view (ContextView) that the list changed.
		return a, func() tea.Msg {
			return view.ContextListUpdatedMsg{Contexts: a.contexts, Current: a.currentContextName}
		}
	}
	if _, ok := msg.(view.OpenAddContextDialogMsg); ok {
		a.showAddContextDialog = true
		a.addContextDialog = view.NewAddContextDialog(a.theme)
		a.addContextDialog.SetSize(a.width, a.height)
		return a, nil
	}

	// PopViewMsg — pop the top view from the nav stack.
	if _, ok := msg.(view.PopViewMsg); ok {
		var closeCmd tea.Cmd
		if cw, ok := a.currentView.(interface{ CloseCmd() tea.Cmd }); ok {
			closeCmd = cw.CloseCmd()
		}
		if a.popView() {
			a.updateBreadcrumb()
			// See pop-on-Esc above: re-Init the restored view to wake up
			// its polling chain (messages were dropped while child active).
			return a, tea.Batch(closeCmd, a.currentView.Init())
		}
		return a, closeCmd
	}

	// OpenScopedStagesMsg — open scoped last-build stage view (from JobList s shortcut).
	if osm, ok := msg.(view.OpenScopedStagesMsg); ok {
		nc := osm.NC
		nc.Username = a.username
		nc.FriendlyName = a.friendlyName
		nc.GitUsernames = a.gitUsernames
		mv := view.NewMyBuildsView(a.theme, a.client, a.store, nc, a.slowInterval)
		a.replaceView(mv)
		a.updateBreadcrumb()
		return a, mv.Init()
	}

	// Handle navigation messages
	if push, ok := msg.(view.PushViewMsg); ok {
		prev := a.activeView()
		// If pushing from a wildcard stages view, tell the child so ESC returns there.
		if mbv, ok := prev.(*view.MyBuildsView); ok {
			if sp, ok := push.View.(view.ScopedParentTarget); ok {
				sp.SetScopedParent(mbv.NC(), a.slowInterval)
			}
		}
		a.pushView(push.View)
		a.updateBreadcrumb()
		return a, push.View.Init()
	}
	if swap, ok := msg.(view.SwapViewMsg); ok {
		if a.currentView != nil {
			a.currentView.Close()
		}
		a.currentView = swap.View
		a.updateBreadcrumb()
		return a, swap.View.Init()
	}
	if pvm, ok := msg.(view.PushViewsMsg); ok {
		if len(pvm.Views) == 0 {
			return a, nil
		}
		if a.currentView != nil {
			a.navStack = append(a.navStack, a.currentView)
		}
		for _, v := range pvm.Views[:len(pvm.Views)-1] {
			a.navStack = append(a.navStack, v)
		}
		a.currentView = pvm.Views[len(pvm.Views)-1]
		a.updateBreadcrumb()
		return a, a.currentView.Init()
	}
	if otb, ok := msg.(view.OpenTriggeredBuildMsg); ok {
		sv := view.NewPendingStageView(a.theme, a.client, a.store, otb.NC, otb.LastKnownBuild)
		a.replaceView(sv)
		a.updateBreadcrumb()
		return a, sv.Init()
	}
	if errMsg, ok := msg.(view.ErrorMsg); ok {
		a.statusBar.SetError(errMsg.Err.Error())
		if a.connected && isConnError(errMsg.Err) {
			a.connected = false
			a.header.SetConnected(false)
			return a, scheduleConnCheck()
		}
		return a, nil
	}

	// Connection probe tick — fire a WhoAmI probe against the current context.
	if _, ok := msg.(connCheckMsg); ok {
		if !a.connected {
			return a, probeCurrentConn(a.client)
		}
		return a, nil
	}

	// Connection probe result.
	if pr, ok := msg.(connProbeResultMsg); ok {
		if pr.ok && !a.connected {
			a.connected = true
			a.header.SetConnected(true)
			// Re-fetch user info in case the initial fetch failed while disconnected.
			userCmd := fetchUserInfo(a.client, a.username)
			// Resume any streaming view that stopped while disconnected.
			if v := a.activeView(); v != nil {
				model, cmd := v.Update(view.ConnectionRestoredMsg{})
				a.currentView = model.(view.View)
				return a, tea.Batch(userCmd, cmd)
			}
			return a, userCmd
		} else if !pr.ok {
			// Still down — schedule next probe in 1s.
			return a, scheduleConnCheck()
		}
		return a, nil
	}
	// User info fetched after context switch — update header with friendly name.
	if ui, ok := msg.(userInfoMsg); ok {
		a.friendlyName = ui.fullName
		a.header.SetUser(ui.fullName)
		return a, nil
	}

	if fsm, ok := msg.(view.FailedStageMsg); ok {
		if fsm.Err != nil {
			a.statusBar.SetError(fsm.Err.Error())
			return a, nil
		}
		buildNC := fsm.NC.AtBuild(fsm.Build.Number)
		if fsm.FailedStage != nil && len(fsm.FailedStage.NodeIDs) > 0 {
			sv := view.NewStageView(a.theme, a.client, a.store, buildNC, fsm.Build)
			sv.SetStages(fsm.Stages, fsm.FailedIdx)
			sl := view.NewStageLogView(a.theme, a.client, a.store, buildNC.AtStage(fsm.FailedStage.Name), fsm.FailedStage.NodeIDs, fsm.Build.Status == jenkins.BuildStatusRunning)
			a.replaceView(sl)
			a.updateBreadcrumb()
			return a, sl.Init()
		}
		// No failed stage found — open full console log
		cv := view.NewConsoleView(a.theme, a.client, buildNC)
		a.replaceView(cv)
		a.updateBreadcrumb()
		return a, cv.Init()
	}

	// BuildCompletedMsg — notify on completion then forward to active view.
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
			return a, cmd
		}
		return a, nil
	}

	// Update check result — show badge if newer version is available.
	if ucr, ok := msg.(updateCheckResultMsg); ok {
		if ucr.err == nil && ucr.version != "" {
			a.updateVersion = ucr.version
			a.header.SetUpdateVersion(ucr.version)
		}
		return a, nil
	}

	// :update command — open confirm dialog (or warn if nothing to update).
	if _, ok := msg.(startUpdateMsg); ok {
		if a.updateVersion == "" {
			a.statusBar.SetError(fmt.Sprintf("already on latest version (%s)", version.App))
			return a, nil
		}
		a.showUpdateDialog = true
		a.updateDialogYes = true
		return a, nil
	}

	// Update completed.
	if ud, ok := msg.(updateDoneMsg); ok {
		a.isUpdating = false
		if ud.err != nil {
			a.statusBar.SetError(fmt.Sprintf("update failed: %v", ud.err))
			return a, nil
		}
		a.UpdatedTo = a.updateVersion
		return a, tea.Quit
	}

	// Delegate non-key messages to active view
	if v := a.activeView(); v != nil {
		model, cmd := v.Update(msg)
		a.currentView = model.(view.View)
		return a, cmd
	}

	return a, nil
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

	borderColor, _ := a.theme.PanelBorder.GetForeground().(lipgloss.Color)

	// Set view shortcuts before rendering the header so they appear this frame.
	if v := a.activeView(); v != nil {
		a.header.SetViewShortcuts(v.Shortcuts())
	}

	// Push debug counters into the header (shows stats from the previous frame).
	if a.dbg != nil {
		viewType := fmt.Sprintf("%T", a.currentView)
		if idx := strings.LastIndex(viewType, "."); idx >= 0 {
			viewType = viewType[idx+1:]
		}
		a.header.SetDebugCounters(a.dbg.renderMs, a.dbg.updateMs, a.store.TotalEntries(), a.dbg.updateCount, viewType)
	}

	// Reflect mine filter state in the header username line.
	if f, ok := a.activeView().(view.Filterable); ok {
		a.header.SetMineFilter(f.ActiveFilters().Mine)
	} else {
		a.header.SetMineFilter(false)
	}

	// Header (bordered panel with info + shortcuts)
	headerView := a.header.View()

	// Command panel (only when active or error)
	commandView := a.statusBar.CommandView()

	// Calculate content height
	usedHeight := lipgloss.Height(headerView)
	if commandView != "" {
		usedHeight += lipgloss.Height(commandView)
	}

	// Check if the active view provides a preview panel.
	v := a.activeView()
	pp, hasPreview := v.(view.PreviewProvider)
	// A ScopedView always satisfies PreviewProvider at the type level (delegation
	// methods), but only creates a real split when its inner view also implements
	// PreviewProvider. Honour that with an optional HasActivePreview check.
	if hasPreview {
		type conditionalPreview interface{ HasActivePreview() bool }
		if cp, ok := v.(conditionalPreview); ok && !cp.HasActivePreview() {
			hasPreview = false
			pp = nil
		}
	}

	contentBorderOverhead := 2 // top + bottom border
	previewBorderOverhead := 0
	if hasPreview {
		previewBorderOverhead = 2 // top + bottom border for preview panel
	}

	navTagsHeight := 1
	totalAvailable := a.height - usedHeight - contentBorderOverhead - previewBorderOverhead - navTagsHeight
	if totalAvailable < 2 {
		totalAvailable = 2
	}

	var contentHeight, previewHeight int
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

	innerWidth := a.width - 2

	// Update breadcrumb (title may change, e.g. pending build resolves).
	a.updateBreadcrumb()

	// Update breadcrumb count, search annotation, and view dimensions.
	// For views with a preview panel, search targets the preview — show the badge there.
	searchQuery := ""
	if s, ok := a.activeView().(view.Searchable); ok {
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

	// Render content
	var content string
	if v != nil {
		content = v.View()
	}

	// Pad content to fill the space
	contentLines := lipgloss.Height(content)
	if contentLines < contentHeight {
		for i := 0; i < contentHeight-contentLines; i++ {
			content += "\n"
		}
	}

	contentPanel := lipgloss.NewStyle().
		Border(a.theme.Border).
		BorderForeground(borderColor).
		Width(innerWidth).
		Render(content)

	// Inject centered breadcrumb title into the top border
	breadcrumbTitle := a.breadcrumb.View()
	contentBadge := ""
	if bv, ok := v.(view.HasBadge); ok {
		contentBadge = bv.Badge()
	}
	contentPanel = injectBorderTitle(contentPanel, breadcrumbTitle, contentBadge, borderColor, a.width)
	if si, ok := v.(view.HasScrollInfo); ok {
		thumbColor, _ := a.theme.Table.Selected.GetForeground().(lipgloss.Color)
		contentPanel = injectScrollbar(contentPanel, si.ScrollInfo(), thumbColor)
	}

	// Assemble layout
	var sections []string
	sections = append(sections, headerView)
	if commandView != "" {
		sections = append(sections, commandView)
	}
	sections = append(sections, contentPanel)

	// Render preview panel if provided.
	if hasPreview {
		previewContent := pp.PreviewView()
		previewLines := lipgloss.Height(previewContent)
		if previewLines < previewHeight {
			for i := 0; i < previewHeight-previewLines; i++ {
				previewContent += "\n"
			}
		}
		previewPanel := lipgloss.NewStyle().
			Border(a.theme.Border).
			BorderForeground(borderColor).
			Width(innerWidth).
			Render(previewContent)

		// Build preview breadcrumb title.
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
		previewBadge := ""
		if pbv, ok := v.(view.HasPreviewBadge); ok {
			previewBadge = pbv.PreviewBadge()
		}
		previewPanel = injectBorderTitle(previewPanel, previewBC.View(), previewBadge, borderColor, a.width)
		if psi, ok := v.(view.HasPreviewScrollInfo); ok {
			thumbColor, _ := a.theme.Table.Selected.GetForeground().(lipgloss.Color)
			previewPanel = injectScrollbar(previewPanel, psi.PreviewScrollInfo(), thumbColor)
		}

		sections = append(sections, previewPanel)
	}

	sections = append(sections, a.navTags.View())

	rendered := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Overlay view-level popups (trigger/confirm dialogs, param forms) at full
	// terminal dimensions so they are never clipped by the content panel border.
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
			s.ApplySearch("")
		}
		return a, nil

	case "backspace":
		if len(a.searchInput) > 0 {
			a.searchInput = a.searchInput[:len(a.searchInput)-1]
			a.statusBar.SetInput(a.searchInput)
			if s, ok := a.activeView().(view.Searchable); ok {
				s.ApplySearch(a.searchInput)
			}
		}
		return a, nil

	default:
		if len(msg.String()) == 1 {
			a.searchInput += msg.String()
			a.statusBar.SetInput(a.searchInput)
			if s, ok := a.activeView().(view.Searchable); ok {
				s.ApplySearch(a.searchInput)
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
		bv := a.buildsViewFor(nc)
		a.replaceView(bv)
		a.updateBreadcrumb()
		return a, bv.Init()

	case kindStages:
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			sv := view.NewStageView(a.theme, a.client, a.store, nc, jenkins.Build{Number: nc.Build.Number})
			a.replaceView(sv)
			a.updateBreadcrumb()
			return a, sv.Init()
		}
		scope := nc.AtScope()
		mv := view.NewMyBuildsView(a.theme, a.client, a.store, scope, a.slowInterval)
		a.replaceView(mv)
		a.updateBreadcrumb()
		return a, mv.Init()

	case kindLogs:
		if nc.Level == view.CtxBuild && nc.Build.Number > 0 {
			cv := view.NewConsoleView(a.theme, a.client, nc)
			a.replaceView(cv)
			a.updateBreadcrumb()
			return a, cv.Init()
		}
		scope := nc.AtScope()
		mv := view.NewMyConsoleView(a.theme, a.client, a.store, scope, a.slowInterval)
		a.replaceView(mv)
		a.updateBreadcrumb()
		return a, mv.Init()

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
		a.replaceView(jl)
		a.updateBreadcrumb()
		return a, jl.Init()

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
		scope := nc.AtScope()
		mv := view.NewMyMatrixView(a.theme, a.client, a.store, scope, a.slowInterval)
		a.replaceView(mv)
		a.updateBreadcrumb()
		return a, mv.Init()
	}
	return a, nil
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
func injectScrollbar(rendered string, scroll view.ScrollInfo, thumbColor lipgloss.Color) string {
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

	thumbStyle := lipgloss.NewStyle().Foreground(thumbColor)
	thumb := thumbStyle.Render("│")

	for i := 1; i <= contentLineCount; i++ {
		lineIdx := i - 1
		if lineIdx >= thumbTop && lineIdx < thumbBottom {
			lines[i] = rightBorderRe.ReplaceAllLiteralString(lines[i], thumb)
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
func probeCurrentConn(client jenkins.JenkinsClient) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.WhoAmI(c)
		return connProbeResultMsg{ok: err == nil}
	}
}

// fetchUserInfo calls WhoAmI and returns userInfoMsg with the full name.
// Falls back to fallback if the request fails or returns an empty name.
func fetchUserInfo(client jenkins.JenkinsClient, fallback string) tea.Cmd {
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

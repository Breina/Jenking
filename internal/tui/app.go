package tui

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

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/monitor"
	"github.com/Breina/Jenking/internal/notify"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/view"
)

// debugStats is shared via pointer so value-receiver methods can mutate it.
type debugStats struct {
	renderMs    int64
	updateMs    int64
	updateCount int64
}

type openColorblindMenuMsg struct{}
type openThemeMenuMsg struct{}
type openContextMenuMsg struct{}
type switchContextMsg struct{ name string }
type contextConnStatusMsg struct {
	name string
	ok   bool
}
type addContextProbeResultMsg struct {
	ok  bool
	msg string
}
type openPrefsMsg struct{}
type openBuildsForContextMsg struct{}
type openStagesForContextMsg struct{}
type openJobsForContextMsg struct{}
type openLogForContextMsg struct{}
type openMatrixForContextMsg struct{}
type openRunningBuildsMsg struct{}
type openHelpMsg struct{}
type connCheckMsg struct{}
type connProbeResultMsg struct{ ok bool }
type userInfoMsg struct{ fullName string }

// App is the root bubbletea model.
type App struct {
	theme                 theme.Theme
	baseTheme             theme.Theme
	themeID               theme.ThemeID
	previewThemeID        theme.ThemeID
	previewDegraded       bool // degraded state of the theme before the menu was opened
	showThemeMenu         bool
	themeMenu             view.ThemeMenu
	showContextMenu       bool
	contextMenu           view.ContextMenu
	showAddContextDialog  bool
	addContextDialog      view.AddContextDialog
	addCtxFn              func(config.ContextConfig) error
	delCtxFn              func(string) error
	setCtxFn              func(string) error
	saveThemeFn           func(string) error
	showPrefsDialog       bool
	prefsDialog           view.PrefsDialog
	savePrefsFn           func(notifications bool, gitUsernames []string, refreshInterval, slowInterval time.Duration, maxLogLines int, logLevel string) error
	refreshInterval       time.Duration
	maxLogLines           int
	logLevel              string
	showHelp              bool
	showRoyalPaywall      bool
	royalPaywall          view.RoyalPaywall
	sponsorKey            string
	colorblindnessType    theme.ColorblindnessType
	previewColorblindType theme.ColorblindnessType
	showColorblindMenu    bool
	colorblindMenu        view.ColorblindMenu
	saveFn                func(theme.ColorblindnessType) error
	keys                  KeyMap
	client                jenkins.JenkinsClient
	store                 *cache.Store
	monitor               *monitor.RunningBuildsMonitor
	username              string
	friendlyName          string
	gitUsernames          []string
	contexts              []config.ContextConfig
	currentContextName    string
	diskStoreFn           func(serverURL string) *cache.DiskStore
	slowInterval          time.Duration
	registry              *command.Registry
	header                component.Header
	breadcrumb            component.Breadcrumb
	statusBar             component.StatusBar
	navTags               component.NavTags
	currentView           view.View
	initialView           view.View
	width                 int
	height                int
	cmdInput              string
	searchInput           string
	notifications         bool
	termFocused           bool // true while the terminal window has focus
	connected             bool // tracks live connection status shown in header
	dbg                   *debugStats // non-nil when log_level=debug
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
			return func() tea.Msg { return openColorblindMenuMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "builds",
		Aliases: []string{"b", "build"},
		Help:    "Show builds for the current context",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openBuildsForContextMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "stages",
		Aliases: []string{"s", "stage"},
		Help:    "Show stages of the last build for the current context",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openStagesForContextMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "jobs",
		Aliases: []string{"j", "job"},
		Help:    "Navigate to the job list for the current context",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openJobsForContextMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "log",
		Aliases: []string{"l", "logs"},
		Help:    "Show console log of the last build for the current context",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openLogForContextMsg{} }
		},
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
		Name:   "matrix",
		Help:   "The Matrix has you...",
		Hidden: true,
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openMatrixForContextMsg{} }
		},
	})

	registry.Register(command.Command{
		Name:    "theme",
		Aliases: []string{"th"},
		Help:    "Select colour theme",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openThemeMenuMsg{} }
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
		monitor:            monitor.NewRunningBuildsMonitor(client, store),
		username:           username,
		friendlyName:       friendlyName,
		gitUsernames:       gitUsernames,
		contexts:           contexts,
		currentContextName: currentContextName,
		diskStoreFn:        diskStoreFn,
		addCtxFn:        addCtxFn,
		delCtxFn:        delCtxFn,
		setCtxFn:        setCtxFn,
		savePrefsFn:     savePrefsFn,
		refreshInterval: refreshInterval,
		slowInterval:    slowInterval,
		maxLogLines:     maxLogLines,
		logLevel:        logLevel,
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

func (a App) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tea.SetWindowTitle("Jenking"))
	cmds = append(cmds, tea.EnableReportFocus)
	if a.currentView != nil {
		cmds = append(cmds, a.currentView.Init())
	}
	cmds = append(cmds, a.monitor.Init())
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
			switch result.Status {
			case view.AddContextCancelled:
				a.showAddContextDialog = false
				return a, nil
			case view.AddContextConfirmed:
				a.showAddContextDialog = false
				cfg := result.Config
				if a.addCtxFn != nil {
					if err := a.addCtxFn(cfg); err != nil {
						a.statusBar.SetError(fmt.Sprintf("save context: %v", err))
						return a, nil
					}
				}
				a.contexts = append(a.contexts, cfg)
				return a, probeContext(cfg)
			}
			if result.TestConn {
				return a, probeAddContext(updated.CurrentConfig())
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

	// Context menu intercepts all key events while open.
	if a.showContextMenu {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			updated, result := a.contextMenu.Update(keyMsg)
			a.contextMenu = updated
			switch result.Event {
			case view.ContextMenuClose:
				a.showContextMenu = false
			case view.ContextMenuSwitch:
				a.showContextMenu = false
				return a.Update(switchContextMsg{name: result.Name})
			case view.ContextMenuDelete:
				// Delete the named context
				name := result.Name
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
				a.contextMenu.SetContexts(newContexts, a.currentContextName)
				// If the active context was deleted, switch to first available
				if name == a.currentContextName && len(newContexts) > 0 {
					a.showContextMenu = false
					return a.Update(switchContextMsg{name: newContexts[0].Name})
				}
				return a, nil
			case view.ContextMenuOpenAdd:
				a.showContextMenu = false
				a.showAddContextDialog = true
				a.addContextDialog = view.NewAddContextDialog(a.theme)
				return a, nil
			}
			return a, nil
		}
	}

	// Colorblind menu intercepts all key events while open.
	if a.showColorblindMenu {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			updated, preview, chosen, closed := a.colorblindMenu.Update(keyMsg)
			a.colorblindMenu = updated
			if closed {
				a.showColorblindMenu = false
				if chosen != nil {
					// Enter: confirm and persist.
					a.applyColorblindnessType(*chosen, true)
				} else {
					// Esc: restore the type that was active before the menu opened.
					a.applyColorblindnessType(a.previewColorblindType, false)
				}
			} else {
				// Still navigating: apply live preview (no save).
				a.applyColorblindnessType(preview, false)
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
						// Open browser and revert to the pre-menu theme.
						_ = exec.Command("xdg-open", view.GitHubSponsorsURL).Start()
						a.applyTheme(a.previewThemeID, false, a.previewDegraded)
					case view.PaywallResultDegrade:
						a.applyTheme(theme.ThemeRoyal, true, true)
					case view.PaywallResultCancel:
						a.applyTheme(a.previewThemeID, false, a.previewDegraded)
					}
				}
			}
			return a, nil
		}
	}

	// Theme menu intercepts all key events while open.
	if a.showThemeMenu {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			updated, preview, chosen, closed := a.themeMenu.Update(keyMsg)
			a.themeMenu = updated
			if closed {
				a.showThemeMenu = false
				if chosen != nil {
					// If Royal is chosen without a sponsor key, show the paywall.
					if *chosen == theme.ThemeRoyal && !theme.IsSponsor(a.username, a.sponsorKey) {
						a.showRoyalPaywall = true
						a.royalPaywall = view.NewRoyalPaywall(a.theme)
					} else {
						a.applyTheme(*chosen, true, false)
					}
				} else {
					// Esc: restore the theme that was active before the menu opened.
					a.applyTheme(a.previewThemeID, false, a.previewDegraded)
				}
			} else {
				// Still navigating: apply live preview (no degrade).
				a.applyTheme(preview, false, false)
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
		case key.Matches(msg, a.keys.RunningBuilds):
			if bv, ok := a.activeView().(*view.BuildsView); ok && bv.NC().Level == view.CtxRoot {
				// Already at builds(*) — toggle the running filter.
				bv.ToggleRunning()
				return a, nil
			}
			// Navigate to builds(*) with the running filter pre-enabled.
			a.activeView().Close()
			av := view.NewAllBuildsView(a.theme, a.client, a.store, a.username, a.gitUsernames, a.slowInterval)
			av.ToggleRunning()
			a.currentView = av
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

	// :builds command — navigate to builds scoped to current context.
	if _, ok := msg.(openBuildsForContextMsg); ok {
		bv := a.buildsViewForCurrentContext()
		a.activeView().Close()
		a.currentView = bv
		a.updateBreadcrumb()
		return a, bv.Init()
	}

	// :stages command — navigate to stages of last build scoped to current context.
	if _, ok := msg.(openStagesForContextMsg); ok {
		nc := a.currentContextNC().AtScope()
		nc.Username = a.username
		nc.FriendlyName = a.friendlyName
		nc.GitUsernames = a.gitUsernames
		mv := view.NewMyBuildsView(a.theme, a.client, a.store, nc, a.slowInterval)
		a.activeView().Close()
		a.currentView = mv
		a.updateBreadcrumb()
		return a, mv.Init()
	}

	// :jobs command — navigate to the job list for the current context.
	if _, ok := msg.(openJobsForContextMsg); ok {
		jl := a.jobListForCurrentContext()
		if jl != nil {
			a.activeView().Close()
			a.currentView = jl
			a.updateBreadcrumb()
			return a, jl.Init()
		}
		return a, nil
	}

	// :log command — navigate to console log of last build scoped to current context.
	if _, ok := msg.(openLogForContextMsg); ok {
		nc := a.currentContextNC().AtScope()
		nc.Username = a.username
		nc.FriendlyName = a.friendlyName
		nc.GitUsernames = a.gitUsernames
		mv := view.NewMyConsoleView(a.theme, a.client, a.store, nc, a.slowInterval)
		a.activeView().Close()
		a.currentView = mv
		a.updateBreadcrumb()
		return a, mv.Init()
	}

	// :matrix command — Matrix-mode log view.
	// Only active when: Matrix theme is set, a full log view is open, and the build is running.
	if _, ok := msg.(openMatrixForContextMsg); ok {
		if a.themeID != theme.ThemeMatrix {
			return a, nil
		}
		rlv, ok := a.currentView.(view.RunningLogView)
		if !ok || !rlv.IsBuildRunning() {
			return a, nil
		}
		nc := a.currentContextNC().AtScope()
		nc.Username = a.username
		nc.FriendlyName = a.friendlyName
		nc.GitUsernames = a.gitUsernames
		mv := view.NewMyMatrixView(a.theme, a.client, a.store, nc, a.slowInterval)
		a.activeView().Close()
		a.currentView = mv
		a.updateBreadcrumb()
		return a, mv.Init()
	}

	// :running command — navigate to builds(*) with the running filter pre-enabled.
	if _, ok := msg.(openRunningBuildsMsg); ok {
		if bv, ok := a.activeView().(*view.BuildsView); ok && bv.NC().Level == view.CtxRoot {
			bv.ToggleRunning()
			return a, nil
		}
		a.activeView().Close()
		av := view.NewAllBuildsView(a.theme, a.client, a.store, a.username, a.gitUsernames, a.slowInterval)
		av.ToggleRunning()
		a.currentView = av
		a.updateBreadcrumb()
		return a, av.Init()
	}

	// Connection probe results for the context switcher popup.
	if csm, ok := msg.(contextConnStatusMsg); ok {
		a.contextMenu.SetConnStatus(csm.name, csm.ok)
		return a, nil
	}

	// Connection probe result for the add-context dialog.
	if apr, ok := msg.(addContextProbeResultMsg); ok {
		if a.showAddContextDialog {
			a.addContextDialog.SetConnStatus(apr.ok, apr.msg)
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
		a.activeView().Close()
		a.client = newClient
		a.store = newStore
		a.monitor = monitor.NewRunningBuildsMonitor(newClient, newStore)
		a.username = target.Username
		a.friendlyName = ""
		a.currentContextName = target.Name
		if a.setCtxFn != nil {
			_ = a.setCtxFn(target.Name)
		}
		a.header.SetURL(target.URL)
		a.header.SetUser("")
		dashboard := view.NewJobList(a.theme, newClient, newStore, "", "Dashboard", false, target.Username)
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

	// Open context menu — probe all contexts concurrently.
	if _, ok := msg.(openContextMenuMsg); ok {
		a.contextMenu = view.NewContextMenu(a.theme, a.contexts, a.currentContextName)
		a.showContextMenu = true
		var cmds []tea.Cmd
		for _, ctx := range a.contexts {
			cmds = append(cmds, probeContext(ctx))
		}
		return a, tea.Batch(cmds...)
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
		a.showPrefsDialog = true
		return a, nil
	}

	// Open colorblind menu
	if _, ok := msg.(openColorblindMenuMsg); ok {
		a.previewColorblindType = a.colorblindnessType
		a.colorblindMenu = view.NewColorblindMenu(a.theme, a.colorblindnessType)
		a.showColorblindMenu = true
		return a, nil
	}

	// Open theme menu
	if _, ok := msg.(openThemeMenuMsg); ok {
		a.previewThemeID = a.themeID
		a.previewDegraded = a.baseTheme.Peasant
		sponsored := theme.IsSponsor(a.username, a.sponsorKey)
		a.themeMenu = view.NewThemeMenu(a.theme, a.themeID, sponsored)
		a.showThemeMenu = true
		return a, nil
	}

	// OpenScopedStagesMsg — open scoped last-build stage view (from JobList s shortcut).
	if osm, ok := msg.(view.OpenScopedStagesMsg); ok {
		nc := osm.NC
		nc.Username = a.username
		nc.FriendlyName = a.friendlyName
		nc.GitUsernames = a.gitUsernames
		mv := view.NewMyBuildsView(a.theme, a.client, a.store, nc, a.slowInterval)
		a.activeView().Close()
		a.currentView = mv
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
		prev.Close()
		a.currentView = push.View
		a.updateBreadcrumb()
		return a, push.View.Init()
	}
	if otb, ok := msg.(view.OpenTriggeredBuildMsg); ok {
		a.activeView().Close()
		sv := view.NewPendingStageView(a.theme, a.client, a.store, otb.NC, otb.LastKnownBuild)
		a.currentView = sv
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
			// Resume any streaming view that stopped while disconnected.
			if v := a.activeView(); v != nil {
				model, cmd := v.Update(view.ConnectionRestoredMsg{})
				a.currentView = model.(view.View)
				return a, cmd
			}
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
		a.activeView().Close()
		buildNC := fsm.NC.AtBuild(fsm.Build.Number)
		if fsm.FailedStage != nil && len(fsm.FailedStage.NodeIDs) > 0 {
			sv := view.NewStageView(a.theme, a.client, a.store, buildNC, fsm.Build)
			sv.SetStages(fsm.Stages, fsm.FailedIdx)
			sl := view.NewStageLogView(a.theme, a.client, a.store, buildNC.AtStage(fsm.FailedStage.Name), fsm.FailedStage.NodeIDs, fsm.Build.Status == jenkins.BuildStatusRunning)
			a.currentView = sl
			a.updateBreadcrumb()
			return a, sl.Init()
		}
		// No failed stage found — open full console log
		cv := view.NewConsoleView(a.theme, a.client, buildNC)
		a.currentView = cv
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

	if a.showContextMenu {
		rendered = a.contextMenu.Render(rendered, a.width, a.height)
	}
	if a.showPrefsDialog {
		rendered = a.prefsDialog.Render(rendered, a.width, a.height)
	}
	if a.showAddContextDialog {
		rendered = a.addContextDialog.Render(rendered, a.width, a.height)
	}
	if a.showColorblindMenu {
		rendered = a.colorblindMenu.Render(rendered, a.width, a.height)
	}
	if a.showThemeMenu {
		rendered = a.themeMenu.Render(rendered, a.width, a.height)
	}
	if a.showRoyalPaywall {
		rendered = a.royalPaywall.Render(rendered, a.width, a.height)
	}
	if a.showHelp {
		rendered = view.RenderHelpDialog(a.theme, a.registry.ListVisible(), rendered, a.width, a.height)
	}

	return rendered
}

func (a *App) handleCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		input := a.cmdInput
		a.statusBar.SetMode(component.ModeNormal)
		a.cmdInput = ""

		cmd, err := a.registry.Execute(input)
		if err != nil {
			a.statusBar.SetError(err.Error())
			return a, nil
		}
		return a, cmd

	case "esc":
		a.statusBar.SetMode(component.ModeNormal)
		a.cmdInput = ""
		return a, nil

	case "backspace":
		if len(a.cmdInput) > 0 {
			a.cmdInput = a.cmdInput[:len(a.cmdInput)-1]
			a.statusBar.SetInput(a.cmdInput)
		}
		return a, nil

	default:
		if len(msg.String()) == 1 {
			a.cmdInput += msg.String()
			a.statusBar.SetInput(a.cmdInput)
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

// buildsViewForCurrentContext returns a BuildsView scoped to the NC of the
// currently active view. Used by the :builds command.
func (a *App) buildsViewForCurrentContext() *view.BuildsView {
	nc := a.currentContextNC().AtScope()
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
		return view.NewJobList(a.theme, a.client, a.store, pp, nc.ProjectName, true, a.username)
	case view.CtxProject:
		// Navigate up to the parent folder's job list.
		if nc.FolderPath != "" {
			title := nc.FolderPath
			if idx := strings.LastIndex(nc.FolderPath, "/"); idx >= 0 {
				title = nc.FolderPath[idx+1:]
			}
			return view.NewJobList(a.theme, a.client, a.store, nc.FolderPath, title, false, a.username)
		}
		return a.initialView
	case view.CtxFolder:
		title := nc.FolderPath
		if idx := strings.LastIndex(nc.FolderPath, "/"); idx >= 0 {
			title = nc.FolderPath[idx+1:]
		}
		return view.NewJobList(a.theme, a.client, a.store, nc.FolderPath, title, false, a.username)
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

// probeContext fires a background connection test for a named context.
// The result arrives as contextConnStatusMsg.
func probeContext(ctx config.ContextConfig) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client := jenkins.NewClient(ctx.URL, ctx.Username, ctx.Token, ctx.Insecure)
		_, err := client.WhoAmI(c)
		return contextConnStatusMsg{name: ctx.Name, ok: err == nil}
	}
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

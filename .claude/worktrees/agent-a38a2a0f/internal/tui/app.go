package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/brecht/jenkins-tui/internal/cache"
	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/command"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
	"github.com/brecht/jenkins-tui/internal/tui/view"
)

type runningCountMsg struct{ count int }
type runningCountTickMsg struct{}
type toggleColorblindMsg struct{}

// App is the root bubbletea model.
type App struct {
	theme          theme.Theme
	baseTheme      theme.Theme
	colorblindMode bool
	saveFn         func(colorblindMode bool) error
	keys           KeyMap
	client         jenkins.JenkinsClient
	store          *cache.Store
	registry       *command.Registry
	header         component.Header
	breadcrumb     component.Breadcrumb
	statusBar      component.StatusBar
	viewStack      []view.View
	initialView    view.View
	width          int
	height         int
	cmdInput       string
	searchInput    string
}

// NewApp creates the root application model.
func NewApp(t theme.Theme, baseTheme theme.Theme, colorblindMode bool, keys KeyMap, client jenkins.JenkinsClient, store *cache.Store, header component.Header, breadcrumb component.Breadcrumb, statusBar component.StatusBar, initialView view.View, saveFn func(bool) error) App {
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
		Help:    "Toggle deuteranopia-safe colour palette",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return toggleColorblindMsg{} }
		},
	})

	return App{
		theme:          t,
		baseTheme:      baseTheme,
		colorblindMode: colorblindMode,
		saveFn:         saveFn,
		keys:           keys,
		client:         client,
		store:          store,
		registry:       registry,
		header:         header,
		breadcrumb:     breadcrumb,
		statusBar:      statusBar,
		viewStack:      []view.View{initialView},
		initialView:    initialView,
	}
}

func (a App) Init() tea.Cmd {
	var cmds []tea.Cmd
	if len(a.viewStack) > 0 {
		cmds = append(cmds, a.activeView().Init())
	}
	cmds = append(cmds, a.fetchRunningCount)
	return tea.Batch(cmds...)
}

func (a App) fetchRunningCount() tea.Msg {
	if a.store != nil {
		if age := a.store.RunningBuilds.Age(""); age >= 0 && age < 5*time.Second {
			if e := a.store.RunningBuilds.Get(""); e != nil {
				return runningCountMsg{count: len(e.Value)}
			}
		}
	}
	builds, _ := a.client.ListRunningBuilds(context.Background())
	if a.store != nil {
		a.store.RunningBuilds.Put("", builds)
	}
	return runningCountMsg{count: len(builds)}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
				if v := a.activeView(); v != nil {
					model, cmd := v.Update(msg)
					a.viewStack[len(a.viewStack)-1] = model.(view.View)
					return a, cmd
				}
				return a, nil
			}
			// Esc clears active search before navigating back
			if s, ok := a.activeView().(view.Searchable); ok && s.SearchQuery() != "" {
				s.ApplySearch("")
				a.viewStack[len(a.viewStack)-1] = a.activeView()
				return a, nil
			}
			if len(a.viewStack) > 1 {
				a.viewStack[len(a.viewStack)-1].Close()
				a.viewStack = a.viewStack[:len(a.viewStack)-1]
				a.updateBreadcrumb()
				// Re-init the revealed view so its background work resumes.
				return a, a.activeView().Init()
			} else if _, ok := a.activeView().(*view.RunningBuildsView); ok {
				// RunningBuildsView is a root-level view — Esc returns to Dashboard
				a.viewStack[0].Close()
				a.viewStack = []view.View{a.initialView}
				a.updateBreadcrumb()
			}
			return a, nil
		case key.Matches(msg, a.keys.RunningBuilds):
			if _, already := a.activeView().(*view.RunningBuildsView); !already {
				// Close all stacked views and open Running Builds as a standalone root
				for _, v := range a.viewStack {
					v.Close()
				}
				rv := view.NewRunningBuildsView(a.theme, a.client, a.store)
				a.viewStack = []view.View{rv}
				a.updateBreadcrumb()
				return a, rv.Init()
			}
			return a, nil
		}

		// Delegate to active view
		if v := a.activeView(); v != nil {
			model, cmd := v.Update(msg)
			a.viewStack[len(a.viewStack)-1] = model.(view.View)
			return a, cmd
		}
	}

	// Background running-builds count
	switch msg := msg.(type) {
	case runningCountMsg:
		a.header.SetRunningBuilds(msg.count, "b")
		return a, tea.Tick(15*time.Second, func(time.Time) tea.Msg {
			return runningCountTickMsg{}
		})
	case runningCountTickMsg:
		return a, a.fetchRunningCount
	}

	// Colorblind mode toggle
	if _, ok := msg.(toggleColorblindMsg); ok {
		a.colorblindMode = !a.colorblindMode
		if a.colorblindMode {
			a.theme = theme.WithDeuteranopiaFilter(a.baseTheme)
		} else {
			a.theme = a.baseTheme
		}
		a.header.SetTheme(a.theme)
		a.breadcrumb.SetTheme(a.theme)
		a.statusBar.SetTheme(a.theme)
		themeMsg := view.ThemeChangedMsg{Theme: a.theme}
		for i, v := range a.viewStack {
			updated, _ := v.Update(themeMsg)
			a.viewStack[i] = updated.(view.View)
		}
		if a.saveFn != nil {
			_ = a.saveFn(a.colorblindMode)
		}
		return a, tea.ClearScreen
	}

	// Handle navigation messages
	if push, ok := msg.(view.PushViewMsg); ok {
		a.viewStack = append(a.viewStack, push.View)
		a.updateBreadcrumb()
		return a, push.View.Init()
	}
	if push, ok := msg.(view.PushTwoViewsMsg); ok {
		a.viewStack = append(a.viewStack, push.First, push.Second)
		a.updateBreadcrumb()
		return a, push.Second.Init()
	}
	if otb, ok := msg.(view.OpenTriggeredBuildMsg); ok {
		// Push a BuildList (for breadcrumb) and a pending StageView.
		bl := view.NewBuildList(a.theme, a.client, a.store, otb.JobPath, otb.JobName, otb.BranchName)
		sv := view.NewPendingStageView(a.theme, a.client, a.store, otb.JobPath, otb.LastKnownBuild, otb.JobName, otb.BranchName)
		a.viewStack = append(a.viewStack, bl, sv)
		a.updateBreadcrumb()
		return a, sv.Init()
	}
	if errMsg, ok := msg.(view.ErrorMsg); ok {
		a.statusBar.SetError(errMsg.Err.Error())
		return a, nil
	}
	if fsm, ok := msg.(view.FailedStageMsg); ok {
		if fsm.Err != nil {
			a.statusBar.SetError(fsm.Err.Error())
			return a, nil
		}
		if fsm.FailedStage != nil && len(fsm.FailedStage.NodeIDs) > 0 {
			// Push StageView (pre-populated, cursor on failed stage) + StageLogView.
			sv := view.NewStageView(a.theme, a.client, a.store, fsm.JobPath, fsm.Build, fsm.JobName, fsm.BranchName)
			sv.SetStages(fsm.Stages, fsm.FailedIdx)
			sl := view.NewStageLogView(a.theme, a.client, a.store, fsm.JobPath, fsm.Build.Number, fsm.FailedStage.Name, fsm.FailedStage.NodeIDs, fsm.Build.Status == jenkins.BuildStatusRunning, fsm.JobName, fsm.BranchName)
			a.viewStack = append(a.viewStack, sv, sl)
			a.updateBreadcrumb()
			return a, sl.Init()
		}
		// No failed stage found — open full console log
		cv := view.NewConsoleView(a.theme, a.client, fsm.JobPath, fsm.Build.Number, fsm.JobName, fsm.BranchName)
		a.viewStack = append(a.viewStack, cv)
		a.updateBreadcrumb()
		return a, cv.Init()
	}

	// Delegate non-key messages to active view
	if v := a.activeView(); v != nil {
		model, cmd := v.Update(msg)
		a.viewStack[len(a.viewStack)-1] = model.(view.View)
		return a, cmd
	}

	return a, nil
}

func (a App) View() string {
	borderColor := lipgloss.Color("62")

	// Set view shortcuts before rendering the header so they appear this frame.
	if v := a.activeView(); v != nil {
		a.header.SetViewShortcuts(v.Shortcuts())
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

	contentBorderOverhead := 2 // top + bottom border
	previewBorderOverhead := 0
	if hasPreview {
		previewBorderOverhead = 2 // top + bottom border for preview panel
	}

	totalAvailable := a.height - usedHeight - contentBorderOverhead - previewBorderOverhead
	if totalAvailable < 2 {
		totalAvailable = 2
	}

	var contentHeight, previewHeight int
	if hasPreview {
		previewHeight = totalAvailable / 2
		contentHeight = totalAvailable - previewHeight
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
	contentPanel = injectBorderTitleCenter(contentPanel, breadcrumbTitle, borderColor, a.width)

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
			ViewType: seg.ViewType,
			Context:  seg.Context,
		})
		previewBC.SetTail(true)
		previewBC.SetSearchAnnotation(searchQuery)
		previewPanel = injectBorderTitleCenter(previewPanel, previewBC.View(), borderColor, a.width)

		sections = append(sections, previewPanel)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
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
	if len(a.viewStack) == 0 {
		return nil
	}
	return a.viewStack[len(a.viewStack)-1]
}

func (a *App) updateLayout() {
	a.header.SetWidth(a.width)
	a.statusBar.SetWidth(a.width)
}

func (a *App) updateBreadcrumb() {
	v := a.activeView()
	if v == nil {
		return
	}
	if bp, ok := v.(view.BreadcrumbProvider); ok {
		seg := bp.Breadcrumb()
		a.breadcrumb.SetSegment(&component.BreadcrumbSegment{
			ViewType: seg.ViewType,
			Context:  seg.Context,
		})
	} else {
		a.breadcrumb.SetSegment(nil)
		segments := make([]string, len(a.viewStack))
		for i, v := range a.viewStack {
			segments[i] = v.Title()
		}
		a.breadcrumb.SetSegments(segments)
	}
}

// injectBorderTitleCenter rebuilds the top border line with a centered title.
// Uses the known rounded border characters and total width to avoid ANSI parsing issues.
func injectBorderTitleCenter(rendered, title string, borderColor lipgloss.Color, totalWidth int) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}

	// Rounded border chars: ╭ (top-left), ╮ (top-right), ─ (horizontal)
	const (
		cornerL = "╭"
		cornerR = "╮"
		horiz   = "─"
	)

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// The border occupies totalWidth display columns: corner + (totalWidth-2) horizontal + corner
	innerWidth := totalWidth - 2 // excluding corners

	titleStr := " " + title + " "
	titleDisplayWidth := lipgloss.Width(titleStr)

	leftPad := (innerWidth - titleDisplayWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	rightPad := innerWidth - leftPad - titleDisplayWidth
	if rightPad < 0 {
		rightPad = 0
	}

	topLine := borderStyle.Render(cornerL+strings.Repeat(horiz, leftPad)) +
		borderStyle.Render(" ") + title + borderStyle.Render(" ") +
		borderStyle.Render(strings.Repeat(horiz, rightPad)+cornerR)

	lines[0] = topLine
	return strings.Join(lines, "\n")
}

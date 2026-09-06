package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brecht/jenkins-tui/internal/cache"
	"github.com/brecht/jenkins-tui/internal/config"
	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/logging"
	"github.com/brecht/jenkins-tui/internal/tui"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
	"github.com/brecht/jenkins-tui/internal/tui/view"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	logCleanup, err := logging.Setup(logging.ParseLevel(cfg.Preferences.LogLevel))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up logging: %v\n", err)
		os.Exit(1)
	}
	defer logCleanup()

	client := jenkins.NewClient(cfg.Server.URL, cfg.Server.Username, cfg.Server.Token, cfg.Server.Insecure)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := client.WhoAmI(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to Jenkins: %v\n", err)
		os.Exit(1)
	}

	baseTheme := theme.Default()
	activeTheme := baseTheme
	if cfg.Preferences.ColorblindMode {
		activeTheme = theme.WithDeuteranopiaFilter(baseTheme)
	}

	store := cache.NewStore()
	keys := tui.DefaultKeyMap()
	header := component.NewHeader(activeTheme, cfg.Server.URL, user.FullName, user.JenkinsVersion)
	breadcrumb := component.NewBreadcrumb(activeTheme)
	statusBar := component.NewStatusBar(activeTheme)
	dashboard := view.NewJobList(activeTheme, client, store, "", "Dashboard", false)

	saveFn := func(colorblindMode bool) error {
		return cfg.SetColorblindMode(colorblindMode)
	}

	app := tui.NewApp(activeTheme, baseTheme, cfg.Preferences.ColorblindMode, keys, client, store, header, breadcrumb, statusBar, dashboard, saveFn)

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

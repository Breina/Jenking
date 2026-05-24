package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/app"
	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/logging"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
	"github.com/Breina/Jenking/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logCleanup, err := logging.Setup(logging.ParseLevel(cfg.Preferences.LogLevel))
	if err != nil {
		return fmt.Errorf("setting up logging: %w", err)
	}
	defer logCleanup()

	active := cfg.ActiveContext()
	client := jenkins.NewClient(active.URL, active.Username, active.Token, active.Insecure)
	disk := newDiskStore(active.URL)
	store := cache.NewStore(disk)

	// Headless --raw mode: bypass TUI entirely. Skips WhoAmI; auth errors
	// surface from the underlying API call instead.
	if cleanArgs, raw := stripRawFlag(os.Args[1:]); raw {
		runHeadless(client, store, cleanArgs)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := client.WhoAmI(ctx)
	if err != nil {
		return fmt.Errorf("connecting to Jenkins at %s (user: %s): %w", active.URL, active.Username, err)
	}

	themeID := theme.ThemeID(cfg.Preferences.Theme)
	if themeID == "" {
		themeID = theme.ThemeDefault
	}
	sponsorKey := cfg.Preferences.SponsorKey
	baseTheme := theme.ByID(themeID)
	if themeID == theme.ThemeRoyal && !theme.IsSponsor(user.ID, sponsorKey) {
		baseTheme.Peasant = true
	}
	cbType := theme.ColorblindnessType(cfg.Preferences.ColorblindnessType)
	if cbType == "" {
		cbType = theme.ColorblindnessNone
	}
	activeTheme := theme.ApplyColorblindFilter(baseTheme, cbType)

	widget.SetVimPolicy(widget.VimPolicy{
		Enabled:         cfg.Preferences.VimIntegration.Enabled,
		PrefetchSymbols: cfg.Preferences.VimIntegration.PrefetchSymbols,
		ValidateOnSave:  cfg.Preferences.VimIntegration.ValidateOnSave,
	})

	keys := app.DefaultKeyMap()
	debug := logging.ParseLevel(cfg.Preferences.LogLevel) == logging.LevelDebug
	header := component.NewHeader(activeTheme, active.URL, user.FullName, user.JenkinsVersion, version.App, debug)
	breadcrumb := component.NewBreadcrumb(activeTheme)
	statusBar := component.NewStatusBar(activeTheme)
	dashboard := view.NewJobList(activeTheme, client, store, "", "Dashboard", false, user.ID, cfg.Preferences.GitUsernames)

	// Optional deep-link from CLI: `jenking <verb> [args...]`. When the first
	// positional argument is a known navigation verb, attempt to construct
	// the target view and use it as the initial view. On failure, error to
	// stderr and exit non-zero — the user gets a fast actionable failure
	// rather than landing on the dashboard with an obscure status-bar message.
	var initialView view.View = dashboard
	if len(os.Args) > 1 && isDeepLinkVerb(os.Args[1]) {
		v, err := buildDeepLinkView(os.Args[1], os.Args[2:], deepLinkArgs{
			theme:        activeTheme,
			client:       client,
			store:        store,
			username:     user.ID,
			friendlyName: user.FullName,
			gitUsernames: cfg.Preferences.GitUsernames,
			slowInterval: cfg.Preferences.SlowRefreshInterval,
		})
		if err != nil {
			return fmt.Errorf("jenking: %w", err)
		}
		initialView = v
	}

	saveFn := func(t theme.ColorblindnessType) error {
		return cfg.SetColorblindnessType(string(t))
	}
	saveThemeFn := func(t string) error {
		return cfg.SetTheme(t)
	}
	savePrefsFn := func(notifications bool, gitUsernames []string, refreshInterval, slowInterval time.Duration, maxLogLines int, logLevel string) error {
		return cfg.SetPreferences(notifications, gitUsernames, refreshInterval, slowInterval, maxLogLines, logLevel)
	}

	model := app.NewApp(activeTheme, baseTheme, themeID, cbType, keys, client, store, user.ID, user.FullName, cfg.Preferences.GitUsernames, cfg.Preferences.RefreshInterval, cfg.Preferences.SlowRefreshInterval, header, breadcrumb, statusBar, initialView, saveFn, saveThemeFn, debug, sponsorKey, cfg.Preferences.Notifications, cfg.Preferences.MaxLogLines, cfg.Preferences.LogLevel, cfg.Contexts, active.Name, newDiskStore, cfg.AddContext, cfg.DeleteContext, cfg.SetCurrentContext, savePrefsFn)

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	if a, ok := finalModel.(app.App); ok && a.UpdatedTo != "" {
		fmt.Printf("Jenking updated to %s — please restart to use the new version.\n", a.UpdatedTo)
	}
	return nil
}

// newDiskStore creates a DiskStore under the XDG cache dir for the given server URL.
// Returns nil on failure so the app starts without persistence rather than crashing.
func newDiskStore(serverURL string) *cache.DiskStore {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		base = filepath.Join(home, ".cache")
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(serverURL))
	dir := filepath.Join(base, "jenking", fmt.Sprintf("%08x", h.Sum32()))
	disk, err := cache.NewDiskStore(dir)
	if err != nil {
		return nil
	}
	return disk
}

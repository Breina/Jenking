package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/logging"
	"github.com/Breina/Jenking/internal/tui"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/view"
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

	disk := newDiskStore(cfg.Server.URL)
	store := cache.NewStore(disk)
	keys := tui.DefaultKeyMap()
	debug := logging.ParseLevel(cfg.Preferences.LogLevel) == logging.LevelDebug
	header := component.NewHeader(activeTheme, cfg.Server.URL, user.FullName, user.JenkinsVersion, debug)
	breadcrumb := component.NewBreadcrumb(activeTheme)
	statusBar := component.NewStatusBar(activeTheme)
	dashboard := view.NewJobList(activeTheme, client, store, "", "Dashboard", false, user.ID)

	saveFn := func(t theme.ColorblindnessType) error {
		return cfg.SetColorblindnessType(string(t))
	}
	saveThemeFn := func(t string) error {
		return cfg.SetTheme(t)
	}

	app := tui.NewApp(activeTheme, baseTheme, themeID, cbType, keys, client, store, user.ID, user.FullName, cfg.Preferences.GitUsernames, cfg.Preferences.SlowRefreshInterval, header, breadcrumb, statusBar, dashboard, saveFn, saveThemeFn, debug, sponsorKey)

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
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

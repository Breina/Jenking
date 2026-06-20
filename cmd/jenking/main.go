package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"

	"github.com/Breina/Jenking/internal/app"
	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "jenking:", err)
		os.Exit(1)
	}
}

// buildAppConfig assembles the AppConfig from wired-up composition root components.
func buildAppConfig(
	mgr *config.Manager, contextName string,
	activeTheme, baseTheme theme.Theme, themeID theme.ThemeID, cbType theme.ColorblindnessType,
	keys app.KeyMap, client jmodel.JenkinsClient, store *cache.Store,
	user *jmodel.User, debug bool, sponsorKey string,
	header component.Header, breadcrumb component.Breadcrumb, statusBar component.StatusBar,
	initialView view.View,
) app.AppConfig {
	return app.AppConfig{
		Theme:                activeTheme,
		BaseTheme:            baseTheme,
		ThemeID:              themeID,
		ColorblindnessType:   cbType,
		Keys:                 keys,
		Client:               client,
		Store:                store,
		DiskStoreFn:          newDiskStore,
		Username:             user.ID,
		FriendlyName:         user.FullName,
		GitUsernames:         mgr.Preferences.GitUsernames,
		RefreshInterval:      mgr.Preferences.RefreshInterval,
		SlowRefreshInterval:  mgr.Preferences.SlowRefreshInterval,
		Header:               header,
		Breadcrumb:           breadcrumb,
		StatusBar:            statusBar,
		InitialView:          initialView,
		Debug:                debug,
		SponsorKey:           sponsorKey,
		Notifications:        mgr.Preferences.Notifications,
		MaxLogLines:          mgr.Preferences.MaxLogLines,
		LogLevel:             mgr.Preferences.LogLevel,
		Contexts:             mgr.Contexts,
		CurrentContextName:   contextName,
		AddContextFn:         mgr.AddContext,
		DeleteContextFn:      mgr.DeleteContext,
		SetContextFn:         mgr.SetCurrentContext,
		SaveColorblindnessFn: func(t theme.ColorblindnessType) error { return mgr.SetColorblindnessType(string(t)) },
		SaveThemeFn:          func(t string) error { return mgr.SetTheme(t) },
		SavePrefsFn: func(notifications bool, gitUsernames []string, refreshInterval, slowInterval time.Duration, maxLogLines int, logLevel string, textArtifactExtensions []string) error {
			return mgr.SetPreferences(notifications, gitUsernames, refreshInterval, slowInterval, maxLogLines, logLevel, textArtifactExtensions)
		},
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

package app

import (
	"time"

	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// AppConfig holds all constructor parameters for NewApp. Using a struct instead
// of a long positional argument list makes call sites readable and allows new
// fields to be added without touching every caller.
type AppConfig struct {
	// Theme
	Theme              theme.Theme
	BaseTheme          theme.Theme
	ThemeID            theme.ThemeID
	ColorblindnessType theme.ColorblindnessType

	// Input
	Keys KeyMap

	// Infrastructure
	Client      jmodel.JenkinsClient
	Store       *cache.Store
	DiskStoreFn func(serverURL string) *cache.DiskStore

	// User identity
	Username     string
	FriendlyName string
	GitUsernames []string

	// Timing
	RefreshInterval     time.Duration
	SlowRefreshInterval time.Duration

	// UI components (pre-constructed by the composition root)
	Header     component.Header
	Breadcrumb component.Breadcrumb
	StatusBar  component.StatusBar

	// Navigation
	InitialView view.View

	// Feature flags
	Debug         bool
	SponsorKey    string
	Notifications bool
	MaxLogLines   int
	LogLevel      string

	// Context management
	Contexts           []config.ContextConfig
	CurrentContextName string
	AddContextFn       func(config.ContextConfig) error
	DeleteContextFn    func(string) error
	SetContextFn       func(string) error

	// Persistence callbacks
	SaveColorblindnessFn func(theme.ColorblindnessType) error
	SaveThemeFn          func(string) error
	SavePrefsFn          func(notifications bool, gitUsernames []string, refreshInterval, slowInterval time.Duration, maxLogLines int, logLevel string, textArtifactExtensions []string) error
}

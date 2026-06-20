package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// ContextConfig holds a named Jenkins environment.
type ContextConfig struct {
	Name     string `mapstructure:"name"`
	URL      string `mapstructure:"url"`
	Username string `mapstructure:"username"`
	Token    string `mapstructure:"token"`
	Insecure bool   `mapstructure:"insecure"`
}

// Config is the top-level configuration.
type Config struct {
	Server         ServerConfig      `mapstructure:"server"`
	Contexts       []ContextConfig   `mapstructure:"contexts"`
	CurrentContext string            `mapstructure:"current_context"`
	Preferences    PreferencesConfig `mapstructure:"preferences"`
}

// ServerConfig holds Jenkins server connection details.
type ServerConfig struct {
	URL      string `mapstructure:"url"`
	Username string `mapstructure:"username"`
	Token    string `mapstructure:"token"`
	Insecure bool   `mapstructure:"insecure"`
}

// PreferencesConfig holds user preferences.
type PreferencesConfig struct {
	RefreshInterval     time.Duration        `mapstructure:"refresh_interval"`
	SlowRefreshInterval time.Duration        `mapstructure:"slow_refresh_interval"`
	Theme               string               `mapstructure:"theme"`
	MaxLogLines         int                  `mapstructure:"max_log_lines"`
	ColorblindnessType  string               `mapstructure:"colorblindness_type"`
	LogLevel            string               `mapstructure:"log_level"`
	GitUsernames        []string             `mapstructure:"git_usernames"`
	SponsorKey          string               `mapstructure:"sponsor_key"`
	Notifications       bool                 `mapstructure:"notifications"`
	VimIntegration      VimIntegrationConfig `mapstructure:"vim_integration"`
	// TextArtifactExtensions overrides the default allowlist of file extensions
	// (without leading dot) that open in the in-TUI artifact viewer instead of
	// the browser. Empty keeps the package default.
	TextArtifactExtensions []string `mapstructure:"text_artifact_extensions"`
}

// VimIntegrationConfig gates the per-build vim runtime and Jenkinsfile
// validation features. All flags default to true — a missing block keeps
// everything on so users get the richer editor experience out of the box.
type VimIntegrationConfig struct {
	Enabled         bool `mapstructure:"enabled"`
	PrefetchSymbols bool `mapstructure:"prefetch_symbols"`
	ValidateOnSave  bool `mapstructure:"validate_on_save"`
}

// Manager wraps Config and the underlying viper instance so preferences can
// be persisted back to disk without restarting.
type Manager struct {
	Config
	v *viper.Viper
}

// ActiveContext returns the currently selected ContextConfig.
// Falls back to the first context, or the legacy server block if no contexts defined.
func (m *Manager) ActiveContext() ContextConfig {
	if len(m.Contexts) == 0 {
		return ContextConfig{
			Name:     "default",
			URL:      m.Server.URL,
			Username: m.Server.Username,
			Token:    m.Server.Token,
			Insecure: m.Server.Insecure,
		}
	}
	for _, c := range m.Contexts {
		if c.Name == m.CurrentContext {
			return c
		}
	}
	return m.Contexts[0]
}

// SetCurrentContext persists the current_context to the config file.
func (m *Manager) SetCurrentContext(name string) error {
	m.CurrentContext = name
	m.v.Set("current_context", name)
	return m.v.WriteConfig()
}

// SetColorblindnessType persists the colorblindness_type preference to the config file.
func (m *Manager) SetColorblindnessType(t string) error {
	m.Preferences.ColorblindnessType = t
	m.v.Set("preferences.colorblindness_type", t)
	return m.v.WriteConfig()
}

// SetPreferences persists a subset of user preferences to the config file.
// Only the fields managed by the preferences dialog are updated; theme and
// colorblindness_type are left untouched.
func (m *Manager) SetPreferences(notifications bool, gitUsernames []string, refreshInterval, slowRefreshInterval time.Duration, maxLogLines int, logLevel string, textArtifactExtensions []string) error {
	m.Preferences.Notifications = notifications
	m.Preferences.GitUsernames = gitUsernames
	m.Preferences.RefreshInterval = refreshInterval
	m.Preferences.SlowRefreshInterval = slowRefreshInterval
	m.Preferences.MaxLogLines = maxLogLines
	m.Preferences.LogLevel = logLevel
	m.Preferences.TextArtifactExtensions = textArtifactExtensions
	m.v.Set("preferences.notifications", notifications)
	m.v.Set("preferences.git_usernames", gitUsernames)
	m.v.Set("preferences.refresh_interval", refreshInterval.String())
	m.v.Set("preferences.slow_refresh_interval", slowRefreshInterval.String())
	m.v.Set("preferences.max_log_lines", maxLogLines)
	m.v.Set("preferences.log_level", logLevel)
	m.v.Set("preferences.text_artifact_extensions", textArtifactExtensions)
	return m.v.WriteConfig()
}

// SetTextArtifactExtensions persists just the artifact-extension allowlist. Used
// at startup to seed the config file with defaults when the key is absent.
func (m *Manager) SetTextArtifactExtensions(exts []string) error {
	m.Preferences.TextArtifactExtensions = exts
	m.v.Set("preferences.text_artifact_extensions", exts)
	return m.v.WriteConfig()
}

// SetTheme persists the theme preference to the config file.
func (m *Manager) SetTheme(t string) error {
	m.Preferences.Theme = t
	m.v.Set("preferences.theme", t)
	return m.v.WriteConfig()
}

// AddContext appends a new named context and persists the config.
func (m *Manager) AddContext(ctx ContextConfig) error {
	m.Contexts = append(m.Contexts, ctx)
	m.v.Set("contexts", contextsToMaps(m.Contexts))
	return m.v.WriteConfig()
}

// DeleteContext removes a named context and persists the config.
// If the deleted context was the active one, switches to the first remaining context.
func (m *Manager) DeleteContext(name string) error {
	newContexts := make([]ContextConfig, 0, len(m.Contexts))
	for _, c := range m.Contexts {
		if c.Name != name {
			newContexts = append(newContexts, c)
		}
	}
	m.Contexts = newContexts
	m.v.Set("contexts", contextsToMaps(newContexts))
	if m.CurrentContext == name {
		if len(newContexts) > 0 {
			m.CurrentContext = newContexts[0].Name
		} else {
			m.CurrentContext = ""
		}
		m.v.Set("current_context", m.CurrentContext)
	}
	return m.v.WriteConfig()
}

// contextsToMaps converts a ContextConfig slice to the map format viper uses when writing YAML.
func contextsToMaps(contexts []ContextConfig) []map[string]interface{} {
	out := make([]map[string]interface{}, len(contexts))
	for i, c := range contexts {
		out[i] = map[string]interface{}{
			"name":     c.Name,
			"url":      c.URL,
			"username": c.Username,
			"token":    c.Token,
			"insecure": c.Insecure,
		}
	}
	return out
}

// Load reads configuration from ~/.config/jenking/config.yaml.
func Load() (*Manager, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("preferences.refresh_interval", "5s")
	v.SetDefault("preferences.slow_refresh_interval", "2m")
	v.SetDefault("preferences.theme", "default")
	v.SetDefault("preferences.max_log_lines", 10000)
	v.SetDefault("preferences.colorblindness_type", "none")
	v.SetDefault("preferences.log_level", "off")
	v.SetDefault("preferences.notifications", true)
	v.SetDefault("preferences.vim_integration.enabled", true)
	v.SetDefault("preferences.vim_integration.prefetch_symbols", true)
	v.SetDefault("preferences.vim_integration.validate_on_save", true)

	// Config file location
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("$HOME/.config/jenking")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Expand env vars in legacy server block token
	cfg.Server.Token = os.ExpandEnv(cfg.Server.Token)

	// Normalize legacy server URL
	cfg.Server.URL = strings.TrimRight(cfg.Server.URL, "/")

	// Expand env vars and normalize URLs in all contexts
	for i := range cfg.Contexts {
		cfg.Contexts[i].Token = os.ExpandEnv(cfg.Contexts[i].Token)
		cfg.Contexts[i].URL = strings.TrimRight(cfg.Contexts[i].URL, "/")
	}

	m := &Manager{Config: cfg, v: v}

	// Validate: at least one context must be usable
	active := m.ActiveContext()
	if active.URL == "" {
		return nil, fmt.Errorf("server.url is required")
	}
	if active.Username == "" {
		return nil, fmt.Errorf("server.username is required")
	}
	if active.Token == "" {
		return nil, fmt.Errorf("server.token is required")
	}

	return m, nil
}

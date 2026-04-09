package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the top-level configuration.
type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	Preferences PreferencesConfig `mapstructure:"preferences"`
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
	RefreshInterval     time.Duration `mapstructure:"refresh_interval"`
	SlowRefreshInterval time.Duration `mapstructure:"slow_refresh_interval"`
	Theme               string        `mapstructure:"theme"`
	MaxLogLines         int           `mapstructure:"max_log_lines"`
	ColorblindnessType  string        `mapstructure:"colorblindness_type"`
	LogLevel            string        `mapstructure:"log_level"`
	GitUsernames        []string      `mapstructure:"git_usernames"`
	SponsorKey          string        `mapstructure:"sponsor_key"`
	Notifications       bool          `mapstructure:"notifications"`
}

// Manager wraps Config and the underlying viper instance so preferences can
// be persisted back to disk without restarting.
type Manager struct {
	Config
	v *viper.Viper
}

// SetColorblindnessType persists the colorblindness_type preference to the config file.
func (m *Manager) SetColorblindnessType(t string) error {
	m.Preferences.ColorblindnessType = t
	m.v.Set("preferences.colorblindness_type", t)
	return m.v.WriteConfig()
}

// SetTheme persists the theme preference to the config file.
func (m *Manager) SetTheme(t string) error {
	m.Preferences.Theme = t
	m.v.Set("preferences.theme", t)
	return m.v.WriteConfig()
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

	// Expand env vars in token
	cfg.Server.Token = os.ExpandEnv(cfg.Server.Token)

	// Normalize URL
	cfg.Server.URL = strings.TrimRight(cfg.Server.URL, "/")

	// Validate required fields
	if cfg.Server.URL == "" {
		return nil, fmt.Errorf("server.url is required")
	}
	if cfg.Server.Username == "" {
		return nil, fmt.Errorf("server.username is required")
	}
	if cfg.Server.Token == "" {
		return nil, fmt.Errorf("server.token is required")
	}

	return &Manager{Config: cfg, v: v}, nil
}

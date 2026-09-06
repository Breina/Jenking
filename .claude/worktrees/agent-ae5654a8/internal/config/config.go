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
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	Theme           string        `mapstructure:"theme"`
	MaxLogLines     int           `mapstructure:"max_log_lines"`
	ColorblindMode  bool          `mapstructure:"colorblind_mode"`
	LogLevel        string        `mapstructure:"log_level"`
}

// Manager wraps Config and the underlying viper instance so preferences can
// be persisted back to disk without restarting.
type Manager struct {
	Config
	v *viper.Viper
}

// SetColorblindMode persists the colorblind_mode preference to the config file.
func (m *Manager) SetColorblindMode(enabled bool) error {
	m.Preferences.ColorblindMode = enabled
	m.v.Set("preferences.colorblind_mode", enabled)
	return m.v.WriteConfig()
}

// Load reads configuration from ~/.config/jenkins-tui/config.yaml.
func Load() (*Manager, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("preferences.refresh_interval", "5s")
	v.SetDefault("preferences.theme", "default")
	v.SetDefault("preferences.max_log_lines", 10000)
	v.SetDefault("preferences.colorblind_mode", false)
	v.SetDefault("preferences.log_level", "off")

	// Config file location
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("$HOME/.config/jenkins-tui")

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

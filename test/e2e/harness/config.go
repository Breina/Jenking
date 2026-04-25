//go:build integration

package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

// contextEntry holds the fields for a single Jenkins context entry.
type contextEntry struct {
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Token    string `yaml:"token"`
	Insecure bool   `yaml:"insecure"`
}

// rawConfig is used to parse the user's real config.yaml without importing
// the internal/config package (harness must not import internal/).
type rawConfig struct {
	Contexts       []contextEntry `yaml:"contexts"`
	CurrentContext string         `yaml:"current_context"`
}

// findContext returns a pointer to the named context in cfg, or nil.
func findContext(cfg rawConfig, name string) *contextEntry {
	for i := range cfg.Contexts {
		if cfg.Contexts[i].Name == name {
			return &cfg.Contexts[i]
		}
	}
	return nil
}

// BakeConfigRaw reads the real ~/.config/jenking/config.yaml, extracts the
// named context (and any extra contexts), and writes an isolated config into
// tmpHome/tmpCache. Returns the environment slice.
//
// If contextName is empty, the user's current_context is used.
// extraContextNames are additional contexts to include in the isolated config
// (useful for context-switch tests that need 2+ contexts).
// Returns an error if any context is not found or credentials are incomplete.
func BakeConfigRaw(contextName, tmpHome, tmpCache string, extraContextNames ...string) (env []string, err error) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine HOME: %w", err)
	}

	realConfig := filepath.Join(realHome, ".config", "jenking", "config.yaml")
	data, err := os.ReadFile(realConfig)
	if err != nil {
		return nil, fmt.Errorf("no jenking config at %s: %w", realConfig, err)
	}

	var cfg rawConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", realConfig, err)
	}

	if contextName == "" {
		contextName = cfg.CurrentContext
	}

	primary := findContext(cfg, contextName)
	if primary == nil {
		return nil, fmt.Errorf("Jenkins context %q not found in %s", contextName, realConfig)
	}
	if primary.URL == "" || primary.Username == "" || primary.Token == "" {
		return nil, fmt.Errorf("Jenkins context %q is incomplete (missing url/username/token)", contextName)
	}

	// Collect extra contexts
	allContexts := []contextEntry{*primary}
	for _, extraName := range extraContextNames {
		extra := findContext(cfg, extraName)
		if extra == nil {
			return nil, fmt.Errorf("extra Jenkins context %q not found in %s", extraName, realConfig)
		}
		if extra.URL == "" || extra.Username == "" || extra.Token == "" {
			return nil, fmt.Errorf("extra Jenkins context %q is incomplete", extraName)
		}
		allContexts = append(allContexts, *extra)
	}

	cfgDir := filepath.Join(tmpHome, ".config", "jenking")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", cfgDir, err)
	}

	// Build config as a structured value and marshal it to avoid YAML injection
	// from tokens/URLs that contain special characters (colons, hashes, etc.).
	type prefsOut struct {
		RefreshInterval     string   `yaml:"refresh_interval"`
		SlowRefreshInterval string   `yaml:"slow_refresh_interval"`
		Theme               string   `yaml:"theme"`
		MaxLogLines         int      `yaml:"max_log_lines"`
		ColorblindnessType  string   `yaml:"colorblindness_type"`
		LogLevel            string   `yaml:"log_level"`
		Notifications       bool     `yaml:"notifications"`
		GitUsernames        []string `yaml:"git_usernames"`
	}
	type cfgOut struct {
		Contexts       []contextEntry `yaml:"contexts"`
		CurrentContext string         `yaml:"current_context"`
		Preferences    prefsOut       `yaml:"preferences"`
	}
	out := cfgOut{
		Contexts:       allContexts,
		CurrentContext: primary.Name,
		Preferences: prefsOut{
			RefreshInterval:     "2s",
			SlowRefreshInterval: "30s",
			Theme:               "default",
			MaxLogLines:         10000,
			ColorblindnessType:  "none",
			LogLevel:            "debug",
			Notifications:       false,
			GitUsernames:        []string{},
		},
	}
	yamlBytes, err := yaml.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshalling isolated config: %w", err)
	}

	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgPath, yamlBytes, 0600); err != nil {
		return nil, fmt.Errorf("writing isolated config: %w", err)
	}

	env = []string{
		"HOME=" + tmpHome,
		"XDG_CACHE_HOME=" + tmpCache,
		"TERM=xterm-256color",
		"PATH=" + os.Getenv("PATH"),
		"USER=" + os.Getenv("USER"),
		"LANG=" + os.Getenv("LANG"),
	}
	return env, nil
}

// BakeConfig is the testing.T-aware wrapper around BakeConfigRaw.
// It skips the test instead of returning errors.
// extraContextNames are included in the isolated config but are not made current.
func BakeConfig(t *testing.T, contextName string, extraContextNames ...string) (env []string, tmpHome, tmpCache string) {
	t.Helper()
	tmpHome = t.TempDir()
	tmpCache = t.TempDir()
	var err error
	env, err = BakeConfigRaw(contextName, tmpHome, tmpCache, extraContextNames...)
	if err != nil {
		// Treat missing/incomplete config as a skip (not a hard failure)
		t.Skipf("harness: %v", err)
	}
	return env, tmpHome, tmpCache
}

// DebugLogPath returns the expected debug.log path for a given tmpHome.
func DebugLogPath(tmpHome string) string {
	return filepath.Join(tmpHome, ".config", "jenking", "debug.log")
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config", "jenkins-tui")
	os.MkdirAll(configDir, 0755)

	configContent := `
server:
  url: https://jenkins.example.com/
  username: testuser
  token: mytoken
preferences:
  refresh_interval: 10s
  max_log_lines: 5000
`
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0644)

	// Override HOME so viper finds our test config
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// URL should be normalized (trailing slash stripped)
	if cfg.Server.URL != "https://jenkins.example.com" {
		t.Errorf("URL = %q, want trailing slash stripped", cfg.Server.URL)
	}
	if cfg.Server.Username != "testuser" {
		t.Errorf("Username = %q, want %q", cfg.Server.Username, "testuser")
	}
	if cfg.Preferences.MaxLogLines != 5000 {
		t.Errorf("MaxLogLines = %d, want 5000", cfg.Preferences.MaxLogLines)
	}
}

func TestLoadEnvVarExpansion(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config", "jenkins-tui")
	os.MkdirAll(configDir, 0755)

	configContent := `
server:
  url: https://jenkins.example.com
  username: testuser
  token: ${TEST_JENKINS_TOKEN}
`
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	os.Setenv("TEST_JENKINS_TOKEN", "expanded-token")
	defer os.Unsetenv("TEST_JENKINS_TOKEN")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Token != "expanded-token" {
		t.Errorf("Token = %q, want %q", cfg.Server.Token, "expanded-token")
	}
}

func TestLoadMissingURL(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config", "jenkins-tui")
	os.MkdirAll(configDir, 0755)

	configContent := `
server:
  username: testuser
  token: mytoken
`
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing URL, got nil")
	}
}

func TestLoadMissingConfigFile(t *testing.T) {
	dir := t.TempDir()

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing config file, got nil")
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config", "jenkins-tui")
	os.MkdirAll(configDir, 0755)

	configContent := `
server:
  url: https://jenkins.example.com
  username: testuser
  token: mytoken
`
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Preferences.Theme != "default" {
		t.Errorf("Theme = %q, want %q", cfg.Preferences.Theme, "default")
	}
	if cfg.Preferences.MaxLogLines != 10000 {
		t.Errorf("MaxLogLines = %d, want 10000", cfg.Preferences.MaxLogLines)
	}
}

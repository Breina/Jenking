package view

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// PrefsValues holds all preferences managed by PrefsDialog.
type PrefsValues struct {
	Notifications          bool
	GitUsernames           []string
	RefreshInterval        time.Duration
	SlowRefreshInterval    time.Duration
	MaxLogLines            int
	LogLevel               string   // "off" or "debug"
	TextArtifactExtensions []string // extensions that open in the in-TUI viewer
}

// PrefsStatus is the outcome of a PrefsDialog.Update call.
type PrefsStatus int

const (
	PrefsActive PrefsStatus = iota
	PrefsConfirmed
	PrefsCancelled
)

// PrefsResult is returned by Update.
type PrefsResult struct {
	Status PrefsStatus
	Prefs  PrefsValues
}

const (
	prefsKeyNotifications = "notifications"
	prefsKeyLogLevel      = "log_level"
	prefsKeyGitUsernames  = "git_usernames"
	prefsKeyRefresh       = "refresh_interval"
	prefsKeySlowRefresh   = "slow_refresh_interval"
	prefsKeyMaxLogLines   = "max_log_lines"
	prefsKeyArtifactExts  = "text_artifact_extensions"
)

// PrefsDialog is a modal form for editing user preferences.
type PrefsDialog struct {
	form component.PopupForm
}

// NewPrefsDialog creates a dialog pre-populated with the given values.
func NewPrefsDialog(t theme.Theme, v PrefsValues) PrefsDialog {
	logLevel := v.LogLevel
	if logLevel != "debug" {
		logLevel = "off"
	}

	notifDefault := "false"
	if v.Notifications {
		notifDefault = "true"
	}

	fields := []component.Field{
		{
			Key: prefsKeyNotifications, Label: "notifications", Kind: component.FieldBool,
			Default:     notifDefault,
			Description: "Show desktop notifications when builds you triggered finish.",
		},
		{
			Key: prefsKeyLogLevel, Label: "log level", Kind: component.FieldChoice,
			Choices:     []string{"off", "debug"},
			Default:     logLevel,
			Description: "Logging verbosity. Requires restart to take effect.",
		},
		{
			Key: prefsKeyGitUsernames, Label: "git usernames", Kind: component.FieldText,
			Default:     strings.Join(v.GitUsernames, ", "),
			Description: "Comma-separated list. Builds you triggered are highlighted in lists.",
		},
		{
			Key: prefsKeyRefresh, Label: "refresh interval", Kind: component.FieldText,
			Required:    true,
			Default:     v.RefreshInterval.String(),
			Description: "How often job and build lists poll Jenkins. e.g. 5s, 10s. Minimum 1s.",
			Validator:   parseDurationMin1s,
		},
		{
			Key: prefsKeySlowRefresh, Label: "slow refresh interval", Kind: component.FieldText,
			Required:    true,
			Default:     v.SlowRefreshInterval.String(),
			Description: "Polling interval used by the global builds view. e.g. 30s, 2m. Minimum 1s.",
			Validator:   parseDurationMin1s,
		},
		{
			Key: prefsKeyMaxLogLines, Label: "max log lines", Kind: component.FieldText,
			Required:    true,
			Default:     strconv.Itoa(v.MaxLogLines),
			Description: "Maximum number of log lines retained in the log viewer.",
			Validator:   parsePositiveInt,
		},
		{
			Key: prefsKeyArtifactExts, Label: "text artifact extensions", Kind: component.FieldText,
			Default:     strings.Join(v.TextArtifactExtensions, ", "),
			Description: "Comma-separated file extensions that open in the in-TUI viewer instead of the browser. Empty restores defaults.",
		},
	}

	pf := component.NewPopupForm(t, "Preferences", fields)
	// Start cursor on the first text field so typing is immediate.
	pf.FocusKey(prefsKeyGitUsernames)
	return PrefsDialog{form: pf}
}

func parseDurationMin1s(s string) error {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	if d < time.Second {
		return fmt.Errorf("must be >= 1s")
	}
	return nil
}

func parsePositiveInt(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("must be a positive integer")
	}
	if n < 1 {
		return fmt.Errorf("must be a positive integer")
	}
	return nil
}

// SetTheme refreshes the theme used for rendering.
func (d *PrefsDialog) SetTheme(t theme.Theme) { d.form.SetTheme(t) }

// SetSize updates the popup width/height from terminal dimensions.
func (d *PrefsDialog) SetSize(termW, termH int) { d.form.SetSize(termW, termH) }

// Update processes a key message.
func (d PrefsDialog) Update(msg tea.KeyMsg) (PrefsDialog, PrefsResult) {
	res := d.form.Update(msg)
	switch res.Status {
	case component.PopupCancelled:
		return d, PrefsResult{Status: PrefsCancelled}
	case component.PopupSubmitted:
		return d, PrefsResult{Status: PrefsConfirmed, Prefs: d.collectValues()}
	}
	return d, PrefsResult{Status: PrefsActive}
}

func (d PrefsDialog) collectValues() PrefsValues {
	v := d.form.Values()
	refresh, _ := time.ParseDuration(strings.TrimSpace(v[prefsKeyRefresh]))
	slow, _ := time.ParseDuration(strings.TrimSpace(v[prefsKeySlowRefresh]))
	maxLog, _ := strconv.Atoi(strings.TrimSpace(v[prefsKeyMaxLogLines]))

	var gitUsernames []string
	for _, s := range strings.Split(v[prefsKeyGitUsernames], ",") {
		if u := strings.TrimSpace(s); u != "" {
			gitUsernames = append(gitUsernames, u)
		}
	}

	var artifactExts []string
	for _, s := range strings.Split(v[prefsKeyArtifactExts], ",") {
		if e := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), ".")); e != "" {
			artifactExts = append(artifactExts, e)
		}
	}

	return PrefsValues{
		Notifications:          v[prefsKeyNotifications] == "true",
		GitUsernames:           gitUsernames,
		RefreshInterval:        refresh,
		SlowRefreshInterval:    slow,
		MaxLogLines:            maxLog,
		LogLevel:               v[prefsKeyLogLevel],
		TextArtifactExtensions: artifactExts,
	}
}

// View renders the dialog box.
func (d PrefsDialog) View() string { return d.form.View() }

// Render overlays the dialog centered on bg.
func (d PrefsDialog) Render(bg string, width, height int) string {
	return overlayCenter(bg, d.form.View(), width, height)
}

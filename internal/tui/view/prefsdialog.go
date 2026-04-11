package view

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// PrefsValues holds all preferences managed by PrefsDialog.
type PrefsValues struct {
	Notifications       bool
	GitUsernames        []string
	RefreshInterval     time.Duration
	SlowRefreshInterval time.Duration
	MaxLogLines         int
	LogLevel            string // "off" or "debug"
}

// PrefsStatus is the outcome of a PrefsDialog.Update call.
type PrefsStatus int

const (
	PrefsActive    PrefsStatus = iota
	PrefsConfirmed             // user submitted
	PrefsCancelled             // user pressed Esc
)

// PrefsResult is returned by Update.
type PrefsResult struct {
	Status PrefsStatus
	Prefs  PrefsValues
}

type prefsField int

const (
	prefsFieldNotifications prefsField = iota
	prefsFieldLogLevel
	prefsFieldGitUsernames
	prefsFieldRefreshInterval
	prefsFieldSlowInterval
	prefsFieldMaxLogLines
	prefsFieldCount
)

// PrefsDialog is a modal form for editing user preferences.
type PrefsDialog struct {
	notifications  bool
	logLevel       string // "off" or "debug"
	gitInput       textinput.Model
	refreshInput   textinput.Model
	slowInput      textinput.Model
	maxLogInput    textinput.Model
	cursor         prefsField
	parseErr       string
	theme          theme.Theme
}

// NewPrefsDialog creates a dialog pre-populated with the given values.
func NewPrefsDialog(t theme.Theme, v PrefsValues) PrefsDialog {
	accentColor, _ := t.Popup.Title.GetForeground().(lipgloss.Color)
	mk := func(val string) textinput.Model {
		ti := textinput.New()
		ti.CharLimit = 128
		ti.Width = 32
		ti.TextStyle = t.Popup.Normal
		ti.PlaceholderStyle = t.Popup.Hint
		ti.Cursor.Style = lipgloss.NewStyle().Foreground(accentColor)
		ti.SetValue(val)
		return ti
	}

	logLevel := v.LogLevel
	if logLevel != "debug" {
		logLevel = "off"
	}

	d := PrefsDialog{
		notifications: v.Notifications,
		logLevel:      logLevel,
		gitInput:      mk(strings.Join(v.GitUsernames, ", ")),
		refreshInput:  mk(v.RefreshInterval.String()),
		slowInput:     mk(v.SlowRefreshInterval.String()),
		maxLogInput:   mk(strconv.Itoa(v.MaxLogLines)),
		theme:         t,
	}
	// Start on git usernames (first text field) and focus it.
	d.cursor = prefsFieldGitUsernames
	d.gitInput.Focus()
	return d
}

// currentValues assembles PrefsValues from dialog state, returns error if invalid.
func (d PrefsDialog) currentValues() (PrefsValues, error) {
	refresh, err := time.ParseDuration(strings.TrimSpace(d.refreshInput.Value()))
	if err != nil {
		return PrefsValues{}, fmt.Errorf("refresh interval: %w", err)
	}
	if refresh < time.Second {
		return PrefsValues{}, fmt.Errorf("refresh interval must be >= 1s")
	}

	slow, err := time.ParseDuration(strings.TrimSpace(d.slowInput.Value()))
	if err != nil {
		return PrefsValues{}, fmt.Errorf("slow refresh interval: %w", err)
	}
	if slow < time.Second {
		return PrefsValues{}, fmt.Errorf("slow refresh interval must be >= 1s")
	}

	maxLog, err := strconv.Atoi(strings.TrimSpace(d.maxLogInput.Value()))
	if err != nil || maxLog < 1 {
		return PrefsValues{}, fmt.Errorf("max log lines must be a positive integer")
	}

	var gitUsernames []string
	for _, s := range strings.Split(d.gitInput.Value(), ",") {
		if u := strings.TrimSpace(s); u != "" {
			gitUsernames = append(gitUsernames, u)
		}
	}

	return PrefsValues{
		Notifications:       d.notifications,
		GitUsernames:        gitUsernames,
		RefreshInterval:     refresh,
		SlowRefreshInterval: slow,
		MaxLogLines:         maxLog,
		LogLevel:            d.logLevel,
	}, nil
}

// Update processes a key message.
func (d PrefsDialog) Update(msg tea.KeyMsg) (PrefsDialog, PrefsResult) {
	switch msg.String() {
	case "esc":
		return d, PrefsResult{Status: PrefsCancelled}

	case "ctrl+s", "ctrl+w":
		v, err := d.currentValues()
		if err != nil {
			d.parseErr = err.Error()
			return d, PrefsResult{Status: PrefsActive}
		}
		return d, PrefsResult{Status: PrefsConfirmed, Prefs: v}

	case "tab", "down":
		d.moveCursor(1)
		return d, PrefsResult{Status: PrefsActive}

	case "shift+tab", "up":
		d.moveCursor(-1)
		return d, PrefsResult{Status: PrefsActive}

	case "enter":
		switch d.cursor {
		case prefsFieldNotifications:
			d.notifications = !d.notifications
		case prefsFieldLogLevel:
			d.toggleLogLevel()
		default:
			if d.cursor == prefsFieldCount-1 {
				v, err := d.currentValues()
				if err != nil {
					d.parseErr = err.Error()
					return d, PrefsResult{Status: PrefsActive}
				}
				return d, PrefsResult{Status: PrefsConfirmed, Prefs: v}
			}
			d.moveCursor(1)
		}
		return d, PrefsResult{Status: PrefsActive}

	case " ":
		switch d.cursor {
		case prefsFieldNotifications:
			d.notifications = !d.notifications
		case prefsFieldLogLevel:
			d.toggleLogLevel()
		}
		return d, PrefsResult{Status: PrefsActive}
	}

	// Delegate to focused text input.
	d.parseErr = ""
	switch d.cursor {
	case prefsFieldGitUsernames:
		var cmd tea.Cmd
		d.gitInput, cmd = d.gitInput.Update(msg)
		_ = cmd
	case prefsFieldRefreshInterval:
		var cmd tea.Cmd
		d.refreshInput, cmd = d.refreshInput.Update(msg)
		_ = cmd
	case prefsFieldSlowInterval:
		var cmd tea.Cmd
		d.slowInput, cmd = d.slowInput.Update(msg)
		_ = cmd
	case prefsFieldMaxLogLines:
		var cmd tea.Cmd
		d.maxLogInput, cmd = d.maxLogInput.Update(msg)
		_ = cmd
	}
	return d, PrefsResult{Status: PrefsActive}
}

func (d *PrefsDialog) toggleLogLevel() {
	if d.logLevel == "debug" {
		d.logLevel = "off"
	} else {
		d.logLevel = "debug"
	}
}

func (d *PrefsDialog) moveCursor(delta int) {
	// Blur current text input.
	switch d.cursor {
	case prefsFieldGitUsernames:
		d.gitInput.Blur()
	case prefsFieldRefreshInterval:
		d.refreshInput.Blur()
	case prefsFieldSlowInterval:
		d.slowInput.Blur()
	case prefsFieldMaxLogLines:
		d.maxLogInput.Blur()
	}

	next := int(d.cursor) + delta
	if next < 0 {
		next = 0
	}
	if next >= int(prefsFieldCount) {
		next = int(prefsFieldCount) - 1
	}
	d.cursor = prefsField(next)

	// Focus new text input.
	switch d.cursor {
	case prefsFieldGitUsernames:
		d.gitInput.Focus()
	case prefsFieldRefreshInterval:
		d.refreshInput.Focus()
	case prefsFieldSlowInterval:
		d.slowInput.Focus()
	case prefsFieldMaxLogLines:
		d.maxLogInput.Focus()
	}
}

// View renders the dialog content without overlaying.
func (d PrefsDialog) View() string {
	t := d.theme
	accentColor := t.Popup.Title.GetForeground()
	titleStyle := t.Popup.Title
	labelStyle := t.Popup.Label
	hintStyle := t.Popup.Hint
	selectedStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)

	renderLabel := func(label string, active bool) string {
		if active {
			return selectedStyle.Render("▸ " + label)
		}
		return labelStyle.Render("  " + label)
	}

	renderToggle := func(label, value string, active bool, hint string) string {
		check := fmt.Sprintf("[ ] %s", label)
		if value == "on" || value == "true" {
			check = fmt.Sprintf("[✔] %s", label)
		}
		lbl := renderLabel(check, active)
		if active {
			lbl += hintStyle.Render("  " + hint)
		}
		return lbl
	}

	renderInput := func(label string, ti textinput.Model, active bool, hint string) []string {
		val := ti.View()
		if !active {
			if v := ti.Value(); v != "" {
				val = v
			} else {
				val = hintStyle.Render("(empty)")
			}
		}
		lines := []string{renderLabel(label, active), "    " + val}
		if active && hint != "" {
			lines = append(lines, "    "+hintStyle.Render(hint))
		}
		return lines
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Preferences"), "")

	// Notifications
	notifVal := "false"
	if d.notifications {
		notifVal = "on"
	}
	lines = append(lines, renderToggle("notifications", notifVal, d.cursor == prefsFieldNotifications, "(space/enter to toggle)"))
	lines = append(lines, "")

	// Log level
	logLabel := fmt.Sprintf("[ ] log level: debug")
	if d.logLevel == "debug" {
		logLabel = "[✔] log level: debug"
	}
	lbl := renderLabel(logLabel, d.cursor == prefsFieldLogLevel)
	if d.cursor == prefsFieldLogLevel {
		lbl += hintStyle.Render("  (space/enter to toggle, requires restart)")
	}
	lines = append(lines, lbl)
	lines = append(lines, "")

	// Git usernames
	lines = append(lines, renderInput("git usernames", d.gitInput, d.cursor == prefsFieldGitUsernames, "comma-separated, used to highlight your builds")...)
	lines = append(lines, "")

	// Refresh interval
	lines = append(lines, renderInput("refresh interval", d.refreshInput, d.cursor == prefsFieldRefreshInterval, "e.g. 5s, 10s — polling interval for job/build lists")...)
	lines = append(lines, "")

	// Slow refresh interval
	lines = append(lines, renderInput("slow refresh interval", d.slowInput, d.cursor == prefsFieldSlowInterval, "e.g. 2m, 30s — polling interval for builds views")...)
	lines = append(lines, "")

	// Max log lines
	lines = append(lines, renderInput("max log lines", d.maxLogInput, d.cursor == prefsFieldMaxLogLines, "maximum lines kept in log viewer")...)
	lines = append(lines, "")

	if d.parseErr != "" {
		lines = append(lines, t.Header.Disconnected.Render("✗ "+d.parseErr))
		lines = append(lines, "")
	}

	lines = append(lines, hintStyle.Render("Tab/↑↓ navigate  Ctrl+S save  Esc cancel"))

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 3).
		Render(content)
}

// Render overlays the dialog centered on bg.
func (d PrefsDialog) Render(bg string, width, height int) string {
	return overlayCenter(bg, d.View(), width, height)
}

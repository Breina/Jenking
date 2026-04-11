package view

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// AddContextStatus is the outcome of an AddContextDialog.Update call.
type AddContextStatus int

const (
	AddContextActive    AddContextStatus = iota
	AddContextConfirmed                  // user submitted
	AddContextCancelled                  // user pressed Esc
)

// AddContextResult is returned by Update.
type AddContextResult struct {
	Status   AddContextStatus
	TestConn bool                 // app should probe connection with CurrentConfig()
	Config   config.ContextConfig // valid when Status == AddContextConfirmed
}

const (
	addCtxFieldName     = 0
	addCtxFieldURL      = 1
	addCtxFieldUsername = 2
	addCtxFieldToken    = 3
	addCtxFieldInsecure = 4
	addCtxFieldCount    = 5
)

// AddContextDialog is a modal form for adding a new Jenkins context.
type AddContextDialog struct {
	nameInput     textinput.Model
	urlInput      textinput.Model
	usernameInput textinput.Model
	tokenInput    textinput.Model
	insecure      bool
	cursor        int
	connOK        *bool
	connMsg       string
	theme         theme.Theme
}

// NewAddContextDialog creates an empty dialog ready for input.
func NewAddContextDialog(t theme.Theme) AddContextDialog {
	accentColor, _ := t.Popup.Title.GetForeground().(lipgloss.Color)
	mk := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 512
		ti.Width = 40
		ti.TextStyle = t.Popup.Normal
		ti.PlaceholderStyle = t.Popup.Hint
		ti.Cursor.Style = lipgloss.NewStyle().Foreground(accentColor)
		return ti
	}
	token := mk("API token")
	token.EchoMode = textinput.EchoPassword
	token.EchoCharacter = '•'

	d := AddContextDialog{
		nameInput:     mk("e.g. production"),
		urlInput:      mk("https://jenkins.example.com"),
		usernameInput: mk("your-username"),
		tokenInput:    token,
		theme:         t,
	}
	d.nameInput.Focus()
	return d
}

// SetConnStatus records the result of a connection probe.
func (d *AddContextDialog) SetConnStatus(ok bool, msg string) {
	d.connOK = &ok
	d.connMsg = msg
}

// CurrentConfig returns the ContextConfig built from current field values.
func (d AddContextDialog) CurrentConfig() config.ContextConfig {
	return config.ContextConfig{
		Name:     strings.TrimSpace(d.nameInput.Value()),
		URL:      strings.TrimSpace(d.urlInput.Value()),
		Username: strings.TrimSpace(d.usernameInput.Value()),
		Token:    d.tokenInput.Value(),
		Insecure: d.insecure,
	}
}

// Update processes a key message.
func (d AddContextDialog) Update(msg tea.KeyMsg) (AddContextDialog, AddContextResult) {
	switch msg.String() {
	case "esc":
		return d, AddContextResult{Status: AddContextCancelled}

	case "ctrl+s":
		return d, AddContextResult{Status: AddContextConfirmed, Config: d.CurrentConfig()}

	case "ctrl+t":
		return d, AddContextResult{Status: AddContextActive, TestConn: true}

	case "tab", "down":
		d.moveCursor(1)
		result := AddContextResult{Status: AddContextActive}
		if d.cursor == addCtxFieldInsecure {
			result.TestConn = true // auto-test when landing on the last field
		}
		return d, result

	case "shift+tab", "up":
		d.moveCursor(-1)
		return d, AddContextResult{Status: AddContextActive}

	case "enter":
		if d.cursor == addCtxFieldInsecure {
			return d, AddContextResult{Status: AddContextConfirmed, Config: d.CurrentConfig()}
		}
		d.moveCursor(1)
		result := AddContextResult{Status: AddContextActive}
		if d.cursor == addCtxFieldInsecure {
			result.TestConn = true
		}
		return d, result
	}

	// Field-specific handling
	switch d.cursor {
	case addCtxFieldInsecure:
		if msg.String() == " " {
			d.insecure = !d.insecure
		}
	default:
		d.updateCurrentInput(msg)
		// Clear stale probe result when inputs change
		d.connOK = nil
		d.connMsg = ""
	}

	return d, AddContextResult{Status: AddContextActive}
}

func (d *AddContextDialog) moveCursor(delta int) {
	// Blur current text field
	switch d.cursor {
	case addCtxFieldName:
		d.nameInput.Blur()
	case addCtxFieldURL:
		d.urlInput.Blur()
	case addCtxFieldUsername:
		d.usernameInput.Blur()
	case addCtxFieldToken:
		d.tokenInput.Blur()
	}

	d.cursor += delta
	if d.cursor < 0 {
		d.cursor = 0
	}
	if d.cursor >= addCtxFieldCount {
		d.cursor = addCtxFieldCount - 1
	}

	// Focus new text field
	switch d.cursor {
	case addCtxFieldName:
		d.nameInput.Focus()
	case addCtxFieldURL:
		d.urlInput.Focus()
	case addCtxFieldUsername:
		d.usernameInput.Focus()
	case addCtxFieldToken:
		d.tokenInput.Focus()
	}
}

func (d *AddContextDialog) updateCurrentInput(msg tea.KeyMsg) {
	var cmd tea.Cmd
	switch d.cursor {
	case addCtxFieldName:
		d.nameInput, cmd = d.nameInput.Update(msg)
	case addCtxFieldURL:
		d.urlInput, cmd = d.urlInput.Update(msg)
	case addCtxFieldUsername:
		d.usernameInput, cmd = d.usernameInput.Update(msg)
	case addCtxFieldToken:
		d.tokenInput, cmd = d.tokenInput.Update(msg)
	}
	_ = cmd
}

// View renders the dialog box. Use Render to overlay it on a background.
func (d AddContextDialog) View() string {
	t := d.theme
	accentColor := t.Popup.Title.GetForeground()
	titleStyle := t.Popup.Title
	labelStyle := t.Popup.Label
	hintStyle := t.Popup.Hint
	selectedStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)

	renderField := func(label, value string, active bool) string {
		prefix := "  "
		ls := labelStyle
		if active {
			prefix = "▸ "
			ls = selectedStyle
		}
		return ls.Render(prefix+label) + "\n    " + value
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Add Context"), "")

	// Name
	{
		active := d.cursor == addCtxFieldName
		val := d.nameInput.View()
		if !active {
			if v := d.nameInput.Value(); v != "" {
				val = v
			} else {
				val = hintStyle.Render("(empty)")
			}
		}
		lines = append(lines, renderField("Name", val, active))
	}
	// URL
	{
		active := d.cursor == addCtxFieldURL
		val := d.urlInput.View()
		if !active {
			if v := d.urlInput.Value(); v != "" {
				val = v
			} else {
				val = hintStyle.Render("(empty)")
			}
		}
		lines = append(lines, renderField("URL", val, active))
	}
	// Username
	{
		active := d.cursor == addCtxFieldUsername
		val := d.usernameInput.View()
		if !active {
			if v := d.usernameInput.Value(); v != "" {
				val = v
			} else {
				val = hintStyle.Render("(empty)")
			}
		}
		lines = append(lines, renderField("Username", val, active))
	}
	// Token
	{
		active := d.cursor == addCtxFieldToken
		val := d.tokenInput.View()
		if !active {
			if v := d.tokenInput.Value(); v != "" {
				val = strings.Repeat("•", len(v))
			} else {
				val = hintStyle.Render("(empty)")
			}
		}
		lines = append(lines, renderField("Token", val, active))
	}
	// Insecure toggle
	{
		active := d.cursor == addCtxFieldInsecure
		check := "[ ] skip TLS verify"
		if d.insecure {
			check = "[✔] skip TLS verify"
		}
		var line string
		if active {
			line = selectedStyle.Render("▸ ") + check + hintStyle.Render("  (space to toggle)")
		} else {
			line = "   " + check
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")

	// Connection status
	{
		var connLine string
		switch {
		case d.connOK == nil:
			connLine = hintStyle.Render("○ not tested  (Ctrl+T to test connection)")
		case *d.connOK:
			connLine = t.Header.Connected.Render("● connected")
		default:
			errMsg := d.connMsg
			if len(errMsg) > 60 {
				errMsg = errMsg[:57] + "..."
			}
			connLine = t.Header.Disconnected.Render("● failed: " + errMsg)
		}
		lines = append(lines, connLine)
	}

	lines = append(lines, "", hintStyle.Render("Tab/↑↓ navigate  Ctrl+S confirm  Esc cancel"))

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 3).
		Render(content)
}

// Render overlays the dialog centered on bg.
func (d AddContextDialog) Render(bg string, width, height int) string {
	return overlayCenter(bg, d.View(), width, height)
}

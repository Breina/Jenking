package view

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// AddContextStatus is the outcome of an AddContextDialog.Update or
// ConsumePending call.
type AddContextStatus int

const (
	AddContextActive AddContextStatus = iota
	AddContextConfirmed
	AddContextCancelled
)

// AddContextResult is returned by Update and ConsumePending.
type AddContextResult struct {
	Status   AddContextStatus
	TestConn bool                 // app should probe connection with CurrentConfig()
	Config   config.ContextConfig // valid when Status == AddContextConfirmed
}

const (
	addCtxKeyName     = "name"
	addCtxKeyURL      = "url"
	addCtxKeyUsername = "username"
	addCtxKeyToken    = "token"
	addCtxKeyInsecure = "insecure"
)

type addContextProbeState int

const (
	probeIdle addContextProbeState = iota
	probeTesting
	probeTestingThenSubmit
)

// AddContextDialog is a modal form for adding a new Jenkins context.
type AddContextDialog struct {
	form         component.PopupForm
	probeState   addContextProbeState
	pendingReady bool // submit pending, waiting for app to read via ConsumePending
}

// NewAddContextDialog creates an empty dialog ready for input.
func NewAddContextDialog(t theme.Theme) AddContextDialog {
	fields := []component.Field{
		{
			Key: addCtxKeyName, Label: "Name", Kind: component.FieldText, Required: true,
			Description: "A short identifier for this Jenkins instance, e.g. production.",
		},
		{
			Key: addCtxKeyURL, Label: "URL", Kind: component.FieldText, Required: true,
			Description: "Full Jenkins base URL including scheme, e.g. https://jenkins.example.com.",
			Validator:   validateURL,
		},
		{
			Key: addCtxKeyUsername, Label: "Username", Kind: component.FieldText,
			Description: "Your Jenkins username.",
		},
		{
			Key: addCtxKeyToken, Label: "Token", Kind: component.FieldPassword,
			Description: "Jenkins API token. Stored locally; rotate if compromised.",
		},
		{
			Key: addCtxKeyInsecure, Label: "skip TLS verify", Kind: component.FieldBool,
			Default:     "false",
			Description: "Disable TLS certificate verification. Use only for self-signed test instances.",
		},
	}
	pf := component.NewPopupForm(t, "Add Context", fields)
	pf.RegisterCustomKey("ctrl+t", "test", "test connection")
	// Reserve room for the connection status line so the popup height does not
	// shift between "○ testing…", "● connected" and "● failed: …".
	pf.ReserveStatusLines(2)
	return AddContextDialog{form: pf}
}

func validateURL(s string) error {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return fmt.Errorf("must start with http:// or https://")
	}
	return nil
}

// SetTheme refreshes the theme used for rendering.
func (d *AddContextDialog) SetTheme(t theme.Theme) { d.form.SetTheme(t) }

// SetSize updates the popup width/height from terminal dimensions.
func (d *AddContextDialog) SetSize(termW, termH int) { d.form.SetSize(termW, termH) }

// SetConnStatus records the result of a connection probe and decides whether
// to confirm an in-flight Enter submit.
func (d *AddContextDialog) SetConnStatus(ok bool, msg string) {
	switch {
	case ok:
		d.form.SetStatus(component.StatusOK, "● connected")
	default:
		d.form.SetStatus(component.StatusError, "● failed: "+msg)
	}
	if d.probeState == probeTestingThenSubmit && ok {
		d.pendingReady = true
	}
	d.probeState = probeIdle
}

// CurrentConfig returns the ContextConfig built from current field values.
func (d AddContextDialog) CurrentConfig() config.ContextConfig {
	v := d.form.Values()
	return config.ContextConfig{
		Name:     strings.TrimSpace(v[addCtxKeyName]),
		URL:      strings.TrimSpace(v[addCtxKeyURL]),
		Username: strings.TrimSpace(v[addCtxKeyUsername]),
		Token:    v[addCtxKeyToken],
		Insecure: v[addCtxKeyInsecure] == "true",
	}
}

// ConsumePending returns a Confirmed result if a queued submit is ready (after
// a successful test), otherwise (zero, false). Callers (the app) should call
// this after every SetConnStatus and after every Update.
func (d *AddContextDialog) ConsumePending() (AddContextResult, bool) {
	if !d.pendingReady {
		return AddContextResult{}, false
	}
	d.pendingReady = false
	return AddContextResult{Status: AddContextConfirmed, Config: d.CurrentConfig()}, true
}

// Update processes a key message.
func (d AddContextDialog) Update(msg tea.KeyMsg) (AddContextDialog, AddContextResult) {
	res := d.form.Update(msg)
	switch res.Status {
	case component.PopupCancelled:
		return d, AddContextResult{Status: AddContextCancelled}
	case component.PopupCustom:
		if res.Custom == "test" {
			d.probeState = probeTesting
			d.form.SetStatus(component.StatusInfo, "○ testing…")
			return d, AddContextResult{Status: AddContextActive, TestConn: true}
		}
	case component.PopupSubmitted:
		d.probeState = probeTestingThenSubmit
		d.form.SetStatus(component.StatusInfo, "○ testing…")
		return d, AddContextResult{Status: AddContextActive, TestConn: true}
	}
	return d, AddContextResult{Status: AddContextActive}
}

// View renders the dialog box.
func (d AddContextDialog) View() string { return d.form.View() }

// Render overlays the dialog centered on bg.
func (d AddContextDialog) Render(bg string, width, height int) string {
	return overlayCenter(bg, d.form.View(), width, height)
}

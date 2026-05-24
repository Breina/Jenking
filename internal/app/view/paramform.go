package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// ParamFormStatus represents the outcome of a form interaction.
type ParamFormStatus int

const (
	ParamFormActive ParamFormStatus = iota
	ParamFormDone
	ParamFormCancelled
)

// ParamFormResult is returned by Update after each key press.
type ParamFormResult struct {
	Status ParamFormStatus
	Values map[string]string
}

// ParamForm is a self-contained form component for entering Jenkins build
// parameters. It is a thin adapter on top of component.PopupForm.
type ParamForm struct {
	form component.PopupForm
}

// NewParamForm creates a form from parameter definitions.
func NewParamForm(t theme.Theme, params []jmodel.ParameterDefinition) ParamForm {
	defs := make([]component.Field, len(params))
	for i, p := range params {
		defs[i] = paramToField(p)
	}
	return ParamForm{form: component.NewPopupForm(t, "Trigger Build", defs)}
}

func paramToField(p jmodel.ParameterDefinition) component.Field {
	f := component.Field{
		Key:         p.Name,
		Label:       p.Name,
		Description: p.Description,
		Default:     p.Default,
	}
	switch p.Type {
	case jmodel.ParamTypeBool:
		f.Kind = component.FieldBool
	case jmodel.ParamTypeChoice:
		f.Kind = component.FieldChoice
		f.Choices = p.Choices
	case jmodel.ParamTypePassword:
		f.Kind = component.FieldPassword
	default:
		f.Kind = component.FieldText
	}
	return f
}

// SetTheme updates the theme used for rendering.
func (pf *ParamForm) SetTheme(t theme.Theme) { pf.form.SetTheme(t) }

// SetSize updates both the popup content width and visible row count from the
// host's terminal dimensions.
func (pf *ParamForm) SetSize(termW, termH int) { pf.form.SetSize(termW, termH) }

// Update processes a key message and returns the form result.
func (pf *ParamForm) Update(msg tea.KeyMsg) ParamFormResult {
	res := pf.form.Update(msg)
	switch res.Status {
	case component.PopupSubmitted:
		return ParamFormResult{Status: ParamFormDone, Values: pf.form.Values()}
	case component.PopupCancelled:
		return ParamFormResult{Status: ParamFormCancelled}
	}
	return ParamFormResult{Status: ParamFormActive}
}

// View renders the form.
func (pf ParamForm) View() string { return pf.form.View() }

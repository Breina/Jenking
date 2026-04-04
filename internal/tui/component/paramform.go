package component

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// ParamFormStatus represents the outcome of a form interaction.
type ParamFormStatus int

const (
	ParamFormActive    ParamFormStatus = iota
	ParamFormDone                      // user submitted
	ParamFormCancelled                 // user pressed esc
)

// ParamFormResult is returned by Update after each key press.
type ParamFormResult struct {
	Status ParamFormStatus
	Values map[string]string // populated when Status == ParamFormDone
}

// paramField holds the state for one parameter field.
type paramField struct {
	param     jenkins.ParameterDefinition
	textInput textinput.Model // used for string type
	boolValue bool            // used for bool type
	choiceIdx int             // used for choice type
	stringVal string          // current value for string type
}

// ParamForm is a self-contained form component for entering build parameters.
type ParamForm struct {
	theme     theme.Theme
	fields    []paramField
	cursor    int
	offset    int
	maxHeight int
	width     int
}

// NewParamForm creates a form from parameter definitions.
func NewParamForm(t theme.Theme, params []jenkins.ParameterDefinition) ParamForm {
	fields := make([]paramField, len(params))
	for i, p := range params {
		f := paramField{param: p}
		switch p.Type {
		case jenkins.ParamTypeBool:
			f.boolValue = p.Default == "true"
		case jenkins.ParamTypeChoice:
			f.choiceIdx = 0
			for ci, c := range p.Choices {
				if c == p.Default {
					f.choiceIdx = ci
					break
				}
			}
		default: // string
			ti := textinput.New()
			ti.Placeholder = p.Description
			ti.SetValue(p.Default)
			ti.CharLimit = 256
			ti.Width = 40
			f.textInput = ti
			f.stringVal = p.Default
		}
		fields[i] = f
	}
	if len(fields) > 0 && fields[0].param.Type == jenkins.ParamTypeString {
		fields[0].textInput.Focus()
	}
	return ParamForm{
		theme:     t,
		fields:    fields,
		maxHeight: 20,
		width:     50,
	}
}

// SetTheme updates the theme used for rendering.
func (pf *ParamForm) SetTheme(t theme.Theme) { pf.theme = t }

// SetMaxHeight sets the maximum visible height for scrolling.
func (pf *ParamForm) SetMaxHeight(h int) {
	pf.maxHeight = h
}

// Update processes a key message and returns the form result.
func (pf *ParamForm) Update(msg tea.KeyMsg) ParamFormResult {
	switch msg.String() {
	case "esc":
		return ParamFormResult{Status: ParamFormCancelled}

	case "ctrl+s":
		return ParamFormResult{Status: ParamFormDone, Values: pf.collectValues()}

	case "tab", "down":
		pf.moveCursor(1)
		return ParamFormResult{Status: ParamFormActive}

	case "shift+tab", "up":
		pf.moveCursor(-1)
		return ParamFormResult{Status: ParamFormActive}

	case "enter":
		if pf.cursor == len(pf.fields)-1 {
			return ParamFormResult{Status: ParamFormDone, Values: pf.collectValues()}
		}
		pf.moveCursor(1)
		return ParamFormResult{Status: ParamFormActive}
	}

	// Field-specific handling
	if pf.cursor >= 0 && pf.cursor < len(pf.fields) {
		f := &pf.fields[pf.cursor]
		switch f.param.Type {
		case jenkins.ParamTypeBool:
			if msg.String() == " " {
				f.boolValue = !f.boolValue
			}
		case jenkins.ParamTypeChoice:
			switch msg.String() {
			case "left", "h":
				if f.choiceIdx > 0 {
					f.choiceIdx--
				}
			case "right", "l":
				if f.choiceIdx < len(f.param.Choices)-1 {
					f.choiceIdx++
				}
			}
		default: // string
			var cmd tea.Cmd
			f.textInput, cmd = f.textInput.Update(msg)
			_ = cmd
			f.stringVal = f.textInput.Value()
		}
	}

	return ParamFormResult{Status: ParamFormActive}
}

func (pf *ParamForm) moveCursor(delta int) {
	// Blur current field if string type
	if pf.cursor >= 0 && pf.cursor < len(pf.fields) {
		if pf.fields[pf.cursor].param.Type == jenkins.ParamTypeString {
			pf.fields[pf.cursor].textInput.Blur()
		}
	}

	pf.cursor += delta
	if pf.cursor < 0 {
		pf.cursor = 0
	}
	if pf.cursor >= len(pf.fields) {
		pf.cursor = len(pf.fields) - 1
	}

	// Focus new field if string type
	if pf.fields[pf.cursor].param.Type == jenkins.ParamTypeString {
		pf.fields[pf.cursor].textInput.Focus()
	}

	// Adjust scroll offset
	if pf.cursor < pf.offset {
		pf.offset = pf.cursor
	}
	visibleRows := pf.maxHeight / 3 // each field takes ~3 lines
	if visibleRows < 1 {
		visibleRows = 1
	}
	if pf.cursor >= pf.offset+visibleRows {
		pf.offset = pf.cursor - visibleRows + 1
	}
}

func (pf *ParamForm) collectValues() map[string]string {
	vals := make(map[string]string, len(pf.fields))
	for _, f := range pf.fields {
		switch f.param.Type {
		case jenkins.ParamTypeBool:
			if f.boolValue {
				vals[f.param.Name] = "true"
			} else {
				vals[f.param.Name] = "false"
			}
		case jenkins.ParamTypeChoice:
			if f.choiceIdx >= 0 && f.choiceIdx < len(f.param.Choices) {
				vals[f.param.Name] = f.param.Choices[f.choiceIdx]
			}
		default:
			vals[f.param.Name] = f.stringVal
		}
	}
	return vals
}

// View renders the form.
func (pf ParamForm) View() string {
	titleStyle := pf.theme.Popup.Title
	accentColor := titleStyle.GetForeground()
	labelStyle := pf.theme.Popup.Label
	descStyle := pf.theme.Popup.Desc
	selectedStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)

	var lines []string
	lines = append(lines, titleStyle.Render("Trigger Build"))
	lines = append(lines, "")

	visibleRows := pf.maxHeight / 3
	if visibleRows < 1 {
		visibleRows = len(pf.fields)
	}
	end := pf.offset + visibleRows
	if end > len(pf.fields) {
		end = len(pf.fields)
	}

	for i := pf.offset; i < end; i++ {
		f := pf.fields[i]
		prefix := "  "
		style := labelStyle
		if i == pf.cursor {
			prefix = "▸ "
			style = selectedStyle
		}

		// Label
		lines = append(lines, style.Render(prefix+f.param.Name))

		// Value
		switch f.param.Type {
		case jenkins.ParamTypeBool:
			check := "[ ]"
			if f.boolValue {
				check = "[✔]"
			}
			if i == pf.cursor {
				lines = append(lines, "    "+selectedStyle.Render(check)+" (space to toggle)")
			} else {
				lines = append(lines, "    "+check)
			}
		case jenkins.ParamTypeChoice:
			val := ""
			if f.choiceIdx >= 0 && f.choiceIdx < len(f.param.Choices) {
				val = f.param.Choices[f.choiceIdx]
			}
			if i == pf.cursor {
				lines = append(lines, "    "+selectedStyle.Render("◂ "+val+" ▸")+" (←/→)")
			} else {
				lines = append(lines, "    "+val)
			}
		default: // string
			if i == pf.cursor {
				lines = append(lines, "    "+f.textInput.View())
			} else {
				val := f.stringVal
				if val == "" {
					val = "(empty)"
				}
				lines = append(lines, "    "+val)
			}
		}

		// Description
		if f.param.Description != "" {
			lines = append(lines, "    "+descStyle.Render(f.param.Description))
		}
	}

	lines = append(lines, "")
	hintStyle := pf.theme.Popup.Hint
	lines = append(lines, hintStyle.Render("  ctrl+s/enter: submit  esc: cancel"))

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 3).
		Render(content)

	return box
}

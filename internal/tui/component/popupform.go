package component

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// FieldKind identifies the input shape of a PopupForm field.
type FieldKind int

const (
	FieldText FieldKind = iota
	FieldPassword
	FieldBool
	FieldChoice
)

// Field describes one input in a PopupForm.
type Field struct {
	Key         string
	Label       string
	Kind        FieldKind
	Description string
	Required    bool
	Choices     []string
	Default     string
	Validator   func(string) error
}

// PopupStatus is the outcome of a PopupForm.Update call.
type PopupStatus int

const (
	PopupActive PopupStatus = iota
	PopupSubmitted
	PopupCancelled
	PopupCustom
)

// PopupResult is returned by PopupForm.Update.
type PopupResult struct {
	Status PopupStatus
	Custom string // populated when Status == PopupCustom
}

// StatusKind selects the styling of the optional status line.
type StatusKind int

const (
	StatusNone StatusKind = iota
	StatusInfo
	StatusOK
	StatusError
)

type customKey struct {
	id, hint string
}

type popupField struct {
	def       Field
	textInput textinput.Model
	boolValue bool
	choiceIdx int
	stringVal string
}

// PopupForm is a reusable modal form with shared key bindings, validation and
// rendering. Hosts wrap it to expose domain-specific APIs.
type PopupForm struct {
	theme               theme.Theme
	title               string
	fields              []popupField
	cursor              int
	fieldErrors         []string // parallel to fields
	statusKind          StatusKind
	statusText          string
	statusReservedLines int
	customKeys          map[string]customKey
	customOrder         []string
	contentW            int
}

// NewPopupForm constructs a popup form with the given fields and a default
// content width of 60 columns. Hosts should call SetSize once they know the
// terminal dimensions.
func NewPopupForm(t theme.Theme, title string, defs []Field) PopupForm {
	pf := PopupForm{
		theme:      t,
		title:      title,
		customKeys: map[string]customKey{},
		contentW:   60,
	}
	pf.fields = make([]popupField, len(defs))
	pf.fieldErrors = make([]string, len(defs))
	for i, def := range defs {
		pf.fields[i] = newPopupField(t, def, pf.contentW)
	}
	pf.focusCurrent()
	return pf
}

func newPopupField(t theme.Theme, def Field, contentW int) popupField {
	f := popupField{def: def}
	switch def.Kind {
	case FieldBool:
		f.boolValue = def.Default == "true"
	case FieldChoice:
		for i, c := range def.Choices {
			if c == def.Default {
				f.choiceIdx = i
				break
			}
		}
	default: // FieldText, FieldPassword
		ti := textinput.New()
		ti.CharLimit = 512
		ti.Width = textInputWidth(contentW)
		ti.TextStyle = t.Popup.Normal
		ti.PlaceholderStyle = t.Popup.Hint
		accentColor, _ := t.Popup.Title.GetForeground().(lipgloss.Color)
		ti.Cursor.Style = lipgloss.NewStyle().Foreground(accentColor)
		ti.SetValue(def.Default)
		if def.Kind == FieldPassword {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		f.textInput = ti
		f.stringVal = def.Default
	}
	return f
}

func textInputWidth(contentW int) int {
	w := contentW - 4
	if w < 10 {
		w = 10
	}
	return w
}

// SetTheme refreshes the theme used for rendering.
func (pf *PopupForm) SetTheme(t theme.Theme) {
	pf.theme = t
	accentColor, _ := t.Popup.Title.GetForeground().(lipgloss.Color)
	for i := range pf.fields {
		f := &pf.fields[i]
		if f.def.Kind == FieldText || f.def.Kind == FieldPassword {
			f.textInput.TextStyle = t.Popup.Normal
			f.textInput.PlaceholderStyle = t.Popup.Hint
			f.textInput.Cursor.Style = lipgloss.NewStyle().Foreground(accentColor)
		}
	}
}

// SetSize updates content width (clamped 50..80) from the terminal dimensions.
// Width snaps once and remains stable until called again, so typing into a
// field cannot grow the popup. termH is accepted for future use but currently
// ignored — all fields are rendered without scrolling.
func (pf *PopupForm) SetSize(termW, termH int) {
	cw := termW - 8
	if cw < 50 {
		cw = 50
	}
	if cw > 80 {
		cw = 80
	}
	pf.contentW = cw
	for i := range pf.fields {
		f := &pf.fields[i]
		if f.def.Kind == FieldText || f.def.Kind == FieldPassword {
			f.textInput.Width = textInputWidth(cw)
		}
	}
}

// SetStatus sets the optional status line. Pass StatusNone to clear.
func (pf *PopupForm) SetStatus(kind StatusKind, text string) {
	pf.statusKind = kind
	pf.statusText = text
}

// ReserveStatusLines reserves a fixed number of rows for the status section so
// the popup's overall height does not change when status text appears or
// disappears. n=0 disables reservation (status renders only when set).
func (pf *PopupForm) ReserveStatusLines(n int) {
	if n < 0 {
		n = 0
	}
	pf.statusReservedLines = n
}

// RegisterCustomKey associates a key binding with an id surfaced via
// PopupResult.Custom and a footer hint.
func (pf *PopupForm) RegisterCustomKey(key, id, hint string) {
	if _, ok := pf.customKeys[key]; !ok {
		pf.customOrder = append(pf.customOrder, key)
	}
	pf.customKeys[key] = customKey{id: id, hint: hint}
}

// ContentWidth reports the current inner width (before border + padding).
func (pf PopupForm) ContentWidth() int { return pf.contentW }

// Cursor returns the active field index.
func (pf PopupForm) Cursor() int { return pf.cursor }

// FocusKey moves the cursor to the field with the given Key, if any.
func (pf *PopupForm) FocusKey(key string) {
	for i, f := range pf.fields {
		if f.def.Key == key {
			pf.moveTo(i)
			return
		}
	}
}

// Values returns the current value for each field as a string map keyed by
// Field.Key.
func (pf PopupForm) Values() map[string]string {
	out := make(map[string]string, len(pf.fields))
	for _, f := range pf.fields {
		switch f.def.Kind {
		case FieldBool:
			if f.boolValue {
				out[f.def.Key] = "true"
			} else {
				out[f.def.Key] = "false"
			}
		case FieldChoice:
			if f.choiceIdx >= 0 && f.choiceIdx < len(f.def.Choices) {
				out[f.def.Key] = f.def.Choices[f.choiceIdx]
			}
		default:
			out[f.def.Key] = f.stringVal
		}
	}
	return out
}

// Update processes a key event and returns the resulting popup state.
func (pf *PopupForm) Update(msg tea.KeyMsg) PopupResult {
	keyStr := msg.String()

	if ck, ok := pf.customKeys[keyStr]; ok {
		return PopupResult{Status: PopupCustom, Custom: ck.id}
	}

	switch keyStr {
	case "esc":
		return PopupResult{Status: PopupCancelled}

	case "enter":
		if !pf.validate() {
			return PopupResult{Status: PopupActive}
		}
		pf.clearFieldErrors()
		return PopupResult{Status: PopupSubmitted}

	case "tab", "down":
		pf.moveCursor(1)
		return PopupResult{Status: PopupActive}

	case "shift+tab", "up":
		pf.moveCursor(-1)
		return PopupResult{Status: PopupActive}
	}

	if pf.cursor >= 0 && pf.cursor < len(pf.fields) {
		f := &pf.fields[pf.cursor]
		switch f.def.Kind {
		case FieldBool:
			if keyStr == " " {
				f.boolValue = !f.boolValue
			}
		case FieldChoice:
			switch keyStr {
			case "left", "h":
				if f.choiceIdx > 0 {
					f.choiceIdx--
				}
			case "right", "l":
				if f.choiceIdx < len(f.def.Choices)-1 {
					f.choiceIdx++
				}
			}
		default:
			var cmd tea.Cmd
			f.textInput, cmd = f.textInput.Update(msg)
			_ = cmd
			f.stringVal = f.textInput.Value()
			if pf.cursor < len(pf.fieldErrors) {
				pf.fieldErrors[pf.cursor] = ""
			}
		}
	}

	return PopupResult{Status: PopupActive}
}

func (pf *PopupForm) validate() bool {
	if len(pf.fieldErrors) != len(pf.fields) {
		pf.fieldErrors = make([]string, len(pf.fields))
	}
	firstInvalid := -1
	valid := true
	for i, f := range pf.fields {
		pf.fieldErrors[i] = ""
		if f.def.Kind != FieldText && f.def.Kind != FieldPassword {
			continue
		}
		val := f.stringVal
		trimmed := strings.TrimSpace(val)
		if f.def.Required && trimmed == "" {
			pf.fieldErrors[i] = "required"
			if firstInvalid < 0 {
				firstInvalid = i
			}
			valid = false
			continue
		}
		if f.def.Validator != nil && trimmed != "" {
			if err := f.def.Validator(val); err != nil {
				pf.fieldErrors[i] = err.Error()
				if firstInvalid < 0 {
					firstInvalid = i
				}
				valid = false
			}
		}
	}
	if firstInvalid >= 0 {
		pf.moveTo(firstInvalid)
	}
	return valid
}

func (pf *PopupForm) clearFieldErrors() {
	for i := range pf.fieldErrors {
		pf.fieldErrors[i] = ""
	}
}

func (pf *PopupForm) moveCursor(delta int) {
	pf.moveTo(pf.cursor + delta)
}

func (pf *PopupForm) moveTo(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(pf.fields) {
		idx = len(pf.fields) - 1
	}
	if pf.cursor >= 0 && pf.cursor < len(pf.fields) {
		f := &pf.fields[pf.cursor]
		if f.def.Kind == FieldText || f.def.Kind == FieldPassword {
			f.textInput.Blur()
		}
	}
	pf.cursor = idx
	pf.focusCurrent()
}

func (pf *PopupForm) focusCurrent() {
	if pf.cursor < 0 || pf.cursor >= len(pf.fields) {
		return
	}
	f := &pf.fields[pf.cursor]
	if f.def.Kind == FieldText || f.def.Kind == FieldPassword {
		f.textInput.Focus()
	}
}

// popupStyles bundles the styles used for one View() invocation.
type popupStyles struct {
	title    lipgloss.Style
	label    lipgloss.Style
	hint     lipgloss.Style
	desc     lipgloss.Style
	err      lipgloss.Style
	selected lipgloss.Style
	accent   lipgloss.TerminalColor
}

func (pf PopupForm) buildStyles() popupStyles {
	t := pf.theme
	accent := t.Popup.Title.GetForeground()
	return popupStyles{
		title:    t.Popup.Title,
		label:    t.Popup.Label,
		hint:     t.Popup.Hint,
		desc:     t.Popup.Desc,
		err:      t.Header.Disconnected,
		selected: lipgloss.NewStyle().Foreground(accent).Bold(true),
		accent:   accent,
	}
}

// View renders the popup as a styled box. Callers overlay it on a background
// using their preferred mechanism (e.g. view.overlayCenter).
func (pf PopupForm) View() string {
	st := pf.buildStyles()

	lines := []string{st.title.Render(pf.title), ""}
	for i, f := range pf.fields {
		lines = append(lines, pf.renderField(i, f, st)...)
	}
	lines = append(lines, pf.renderStatusSection()...)
	lines = append(lines, pf.renderFooterLines()...)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(st.accent).
		Padding(1, 3).
		Width(pf.contentW + 6).
		Render(strings.Join(lines, "\n"))
}

// renderField returns the lines for one field block: label + value + description + error spacer.
func (pf PopupForm) renderField(i int, f popupField, st popupStyles) []string {
	active := i == pf.cursor
	var lines []string
	lines = append(lines, pf.renderFieldLabel(f, active, st))
	lines = append(lines, pf.renderFieldValue(f, active, st))

	if f.def.Description != "" {
		for _, dl := range wrapText(f.def.Description, pf.contentW-4) {
			lines = append(lines, "    "+st.desc.Render(dl))
		}
	}
	lines = append(lines, pf.renderFieldError(i, f, st)...)
	return lines
}

func (pf PopupForm) renderFieldLabel(f popupField, active bool, st popupStyles) string {
	prefix := "  "
	ls := st.label
	if active {
		prefix = "▸ "
		ls = st.selected
	}
	labelText := f.def.Label
	if f.def.Required {
		labelText += " *"
	}
	return ls.Render(prefix + labelText)
}

func (pf PopupForm) renderFieldValue(f popupField, active bool, st popupStyles) string {
	switch f.def.Kind {
	case FieldBool:
		return pf.renderBoolValue(f, active, st)
	case FieldChoice:
		return pf.renderChoiceValue(f, active, st)
	case FieldPassword:
		return pf.renderPasswordValue(f, active, st)
	default:
		return pf.renderTextValue(f, active, st)
	}
}

func (pf PopupForm) renderBoolValue(f popupField, active bool, st popupStyles) string {
	check := "[ ]"
	if f.boolValue {
		check = "[✔]"
	}
	if active {
		return "    " + st.selected.Render(check) + st.hint.Render("  (space to toggle)")
	}
	return "    " + check
}

func (pf PopupForm) renderChoiceValue(f popupField, active bool, st popupStyles) string {
	val := ""
	if f.choiceIdx >= 0 && f.choiceIdx < len(f.def.Choices) {
		val = f.def.Choices[f.choiceIdx]
	}
	if active {
		return "    " + st.selected.Render("◂ "+val+" ▸") + st.hint.Render("  (←/→)")
	}
	return "    " + val
}

func (pf PopupForm) renderPasswordValue(f popupField, active bool, st popupStyles) string {
	if active {
		return "    " + f.textInput.View()
	}
	if v := f.stringVal; v != "" {
		return "    " + strings.Repeat("•", len(v))
	}
	return "    " + st.hint.Render("(empty)")
}

func (pf PopupForm) renderTextValue(f popupField, active bool, st popupStyles) string {
	if active {
		return "    " + f.textInput.View()
	}
	if v := f.stringVal; v != "" {
		return "    " + v
	}
	return "    " + st.hint.Render("(empty)")
}

// renderFieldError returns the per-field error block: either the wrapped error
// lines or a single blank spacer so the field height stays stable.
func (pf PopupForm) renderFieldError(i int, f popupField, st popupStyles) []string {
	canError := (f.def.Kind == FieldText || f.def.Kind == FieldPassword) &&
		(f.def.Required || f.def.Validator != nil)
	if !canError {
		return []string{""}
	}
	errText := ""
	if i < len(pf.fieldErrors) {
		errText = pf.fieldErrors[i]
	}
	if errText == "" {
		return []string{""}
	}
	wrapped := wrapText("✗ "+errText, pf.contentW-4)
	out := make([]string, 0, len(wrapped))
	for _, el := range wrapped {
		out = append(out, "    "+st.err.Render(el))
	}
	return out
}

// renderStatusSection returns the lines for the optional status section,
// honouring statusReservedLines for stable popup height.
func (pf PopupForm) renderStatusSection() []string {
	if pf.statusReservedLines > 0 {
		return pf.renderReservedStatus()
	}
	if pf.statusText == "" {
		return nil
	}
	style := pf.statusStyle()
	var out []string
	for _, sl := range wrapText(pf.statusText, pf.contentW) {
		out = append(out, style.Render(sl))
	}
	out = append(out, "")
	return out
}

func (pf PopupForm) renderReservedStatus() []string {
	var statusLines []string
	if pf.statusText != "" {
		style := pf.statusStyle()
		for _, sl := range wrapText(pf.statusText, pf.contentW) {
			statusLines = append(statusLines, style.Render(sl))
		}
	}
	for len(statusLines) < pf.statusReservedLines {
		statusLines = append(statusLines, "")
	}
	if len(statusLines) > pf.statusReservedLines {
		statusLines = statusLines[:pf.statusReservedLines]
	}
	return append(statusLines, "")
}

func (pf PopupForm) renderFooterLines() []string {
	t := pf.theme
	keyStyle := t.StatusBar.Key
	actStyle := t.StatusBar.Help

	type fc struct{ key, hint string }
	builtin := []fc{
		{"↑↓", "navigate"},
		{"enter", "accept"},
		{"esc", "cancel"},
	}

	items := make([]string, 0, len(builtin)+len(pf.customOrder))
	for _, b := range builtin {
		items = append(items, keyStyle.Render("<"+b.key+">")+actStyle.Render(" "+b.hint))
	}
	for _, k := range pf.customOrder {
		ck := pf.customKeys[k]
		items = append(items, keyStyle.Render("<"+k+">")+actStyle.Render(" "+ck.hint))
	}

	const sep = "   "
	sepW := lipgloss.Width(sep)

	var out []string
	var cur string
	curW := 0
	for _, it := range items {
		w := lipgloss.Width(it)
		if cur == "" {
			cur = it
			curW = w
			continue
		}
		if curW+sepW+w > pf.contentW {
			out = append(out, cur)
			cur = it
			curW = w
			continue
		}
		cur += sep + it
		curW += sepW + w
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func (pf PopupForm) statusStyle() lipgloss.Style {
	switch pf.statusKind {
	case StatusOK:
		return pf.theme.Header.Connected
	case StatusError:
		return pf.theme.Header.Disconnected
	default:
		return pf.theme.Popup.Hint
	}
}

// wrapText wraps s into lines no wider than width, breaking on whitespace and
// hard-breaking words longer than width.
func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	if s == "" {
		return []string{""}
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := ""
		for _, w := range words {
			for len(w) > width {
				if cur != "" {
					out = append(out, cur)
					cur = ""
				}
				out = append(out, w[:width])
				w = w[width:]
			}
			if cur == "" {
				cur = w
				continue
			}
			if len(cur)+1+len(w) <= width {
				cur += " " + w
			} else {
				out = append(out, cur)
				cur = w
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

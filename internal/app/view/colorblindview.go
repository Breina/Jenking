package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// ColorblindView is a native table view for selecting a colorblindness type.
// It previews the selection live on cursor move, persists on Enter, and
// restores the original on Esc.
type ColorblindView struct {
	theme     theme.Theme
	table     component.Table
	options   []theme.ColorblindnessType
	original  theme.ColorblindnessType
	confirmed bool
	width     int
	height    int
}

// NewColorblindView creates a ColorblindView positioned at current.
func NewColorblindView(t theme.Theme, current theme.ColorblindnessType) *ColorblindView {
	columns := []component.Column{{Title: "MODE", Width: 40}}
	v := &ColorblindView{
		theme:    t,
		table:    component.NewTable(t, columns),
		options:  theme.AllColorblindnessTypes,
		original: current,
	}
	v.populateTable()
	cursor := 0
	for i, o := range v.options {
		if o == current {
			cursor = i
			break
		}
	}
	v.table.SetCursor(cursor)
	return v
}

func (v *ColorblindView) populateTable() {
	rows := make([]component.Row, len(v.options))
	for i, o := range v.options {
		rows[i] = component.Row{colorblindLabel(o)}
	}
	v.table.SetRows(rows)
}

func (v *ColorblindView) Init() tea.Cmd { return nil }

func (v *ColorblindView) selected() theme.ColorblindnessType {
	return v.options[v.table.Cursor()]
}

func (v *ColorblindView) previewCmd() tea.Cmd {
	sel := v.selected()
	return func() tea.Msg { return ColorblindPreviewMsg{Type: sel} }
}

func (v *ColorblindView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		v.theme = msg.Theme
		v.table.SetTheme(msg.Theme)
		return v, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			v.table.MoveUp()
			return v, v.previewCmd()
		case "down", "j":
			v.table.MoveDown()
			return v, v.previewCmd()
		case "pgup":
			v.table.PageUp()
			return v, v.previewCmd()
		case "pgdown":
			v.table.PageDown()
			return v, v.previewCmd()
		case "home":
			v.table.Home()
			return v, v.previewCmd()
		case "end":
			v.table.End()
			return v, v.previewCmd()
		case "enter":
			v.confirmed = true
			sel := v.selected()
			return v, tea.Batch(
				func() tea.Msg { return ColorblindConfirmMsg{Type: sel} },
				func() tea.Msg { return PopViewMsg{} },
			)
		}
	}
	return v, nil
}

func (v *ColorblindView) View() string { return v.table.View() }

func (v *ColorblindView) Title() string { return "colorblind" }

func (v *ColorblindView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{ViewType: "colorblind"}
}

func (v *ColorblindView) ItemCount() int { return v.table.TotalRows() }

func (v *ColorblindView) Commands() []command.Command { return nil }

func (v *ColorblindView) Shortcuts() []component.Shortcut {
	return []component.Shortcut{
		component.Nav("enter", "select"),
		component.Nav("esc", "cancel"),
	}
}

func (v *ColorblindView) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.table.SetColumnWidth(0, max(20, width-2))
	v.table.SetSize(width, height)
}

func (v *ColorblindView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: v.table.ScrollOffset(), TotalLines: v.table.TotalRows(), ViewHeight: v.table.ContentHeight()}
}

// CloseCmd returns a restore command when the view is popped without confirming.
func (v *ColorblindView) CloseCmd() tea.Cmd {
	if v.confirmed {
		return nil
	}
	orig := v.original
	return func() tea.Msg { return ColorblindPreviewMsg{Type: orig} }
}

func (v *ColorblindView) Close() error { return nil }

// colorblindLabel returns a human-readable label for a ColorblindnessType.
func colorblindLabel(t theme.ColorblindnessType) string {
	switch t {
	case theme.ColorblindnessNone:
		return "None"
	case theme.ColorblindnessDeuteranopia:
		return "Deuteranopia  (red-green, M-cone)"
	case theme.ColorblindnessProtanopia:
		return "Protanopia    (red-green, L-cone)"
	case theme.ColorblindnessTritanopia:
		return "Tritanopia    (blue-yellow)"
	case theme.ColorblindnessAchromatopsia:
		return "Achromatopsia (no colour)"
	default:
		return string(t)
	}
}

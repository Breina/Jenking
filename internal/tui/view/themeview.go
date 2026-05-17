package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// ThemeView is a native table view for selecting a colour theme.
// Previews on cursor move, persists on Enter, restores on Esc.
type ThemeView struct {
	theme            theme.Theme
	table            component.Table
	options          []theme.ThemeInfo
	originalID       theme.ThemeID
	originalDegraded bool
	sponsored        bool
	confirmed        bool
	width            int
	height           int
}

// NewThemeView creates a ThemeView positioned at current.
func NewThemeView(t theme.Theme, current theme.ThemeID, currentDegraded bool, sponsored bool) *ThemeView {
	columns := []component.Column{
		{Title: "THEME", Width: 20},
		{Title: "DESCRIPTION", Width: 40},
	}
	v := &ThemeView{
		theme:            t,
		table:            component.NewTable(t, columns),
		options:          theme.AllThemes,
		originalID:       current,
		originalDegraded: currentDegraded,
		sponsored:        sponsored,
	}
	v.populateTable()
	cursor := 0
	for i, o := range v.options {
		if o.ID == current {
			cursor = i
			break
		}
	}
	v.table.SetCursor(cursor)
	return v
}

func (v *ThemeView) populateTable() {
	rows := make([]component.Row, len(v.options))
	for i, o := range v.options {
		name := o.Name
		if o.ID == theme.ThemeRoyal && !v.sponsored {
			name += " 🔒"
		}
		rows[i] = component.Row{name, o.Desc}
	}
	v.table.SetRows(rows)
}

func (v *ThemeView) Init() tea.Cmd { return nil }

func (v *ThemeView) selected() theme.ThemeInfo {
	return v.options[v.table.Cursor()]
}

func (v *ThemeView) previewCmd() tea.Cmd {
	sel := v.selected()
	return func() tea.Msg { return ThemePreviewMsg{ID: sel.ID} }
}

func (v *ThemeView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			sel := v.selected()
			v.confirmed = true
			if sel.ID == theme.ThemeRoyal && !v.sponsored {
				origID, origDeg := v.originalID, v.originalDegraded
				return v, tea.Batch(
					func() tea.Msg {
						return ThemeLockedRoyalMsg{OriginalID: origID, OriginalDegraded: origDeg}
					},
					func() tea.Msg { return PopViewMsg{} },
				)
			}
			return v, tea.Batch(
				func() tea.Msg { return ThemeConfirmMsg{ID: sel.ID} },
				func() tea.Msg { return PopViewMsg{} },
			)
		}
	}
	return v, nil
}

func (v *ThemeView) View() string { return v.table.View() }

func (v *ThemeView) Title() string { return "theme" }

func (v *ThemeView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{ViewType: "theme"}
}

func (v *ThemeView) ItemCount() int { return v.table.TotalRows() }

func (v *ThemeView) Commands() []command.Command { return nil }

func (v *ThemeView) Shortcuts() []component.Shortcut {
	return []component.Shortcut{
		component.Nav("enter", "select"),
		component.Nav("esc", "cancel"),
	}
}

func (v *ThemeView) SetSize(width, height int) {
	v.width = width
	v.height = height
	nameW := 20
	descW := max(10, width-nameW-4)
	v.table.SetColumnWidth(0, nameW)
	v.table.SetColumnWidth(1, descW)
	v.table.SetSize(width, height)
}

func (v *ThemeView) ScrollInfo() ScrollInfo {
	return ScrollInfo{Offset: v.table.ScrollOffset(), TotalLines: v.table.TotalRows(), ViewHeight: v.table.ContentHeight()}
}

// CloseCmd returns a restore command when the view is popped without confirming.
func (v *ThemeView) CloseCmd() tea.Cmd {
	if v.confirmed {
		return nil
	}
	id, deg := v.originalID, v.originalDegraded
	return func() tea.Msg { return ThemePreviewMsg{ID: id, Degraded: deg} }
}

func (v *ThemeView) Close() error { return nil }

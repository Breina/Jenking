package view

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// contextConnState is the probed connection state for a context.
type contextConnState int

const (
	ctxConnUnknown contextConnState = iota
	ctxConnOK
	ctxConnFailed
)

// ContextView is a native table view for managing Jenkins contexts
// (switch, add, delete). Failed contexts are rendered as disabled.
type ContextView struct {
	theme      theme.Theme
	table      component.Table
	contexts   []config.ContextConfig
	current    string
	connStatus map[string]contextConnState

	dialog widget.ConfirmDialog

	width, height int
}

// NewContextView creates a ContextView listing the given contexts.
func NewContextView(t theme.Theme, contexts []config.ContextConfig, current string) *ContextView {
	columns := []component.Column{
		{Title: "", Width: 2},
		{Title: "NAME", Width: 20},
		{Title: "URL", Width: 40},
	}
	v := &ContextView{
		theme:      t,
		table:      component.NewTable(t, columns),
		contexts:   contexts,
		current:    current,
		connStatus: make(map[string]contextConnState),
	}
	v.populateTable()
	return v
}

func (v *ContextView) populateTable() {
	okStyle := v.theme.Header.Connected
	failStyle := v.theme.Header.Disconnected

	rows := make([]component.Row, len(v.contexts))
	disabled := map[int]bool{}
	for i, c := range v.contexts {
		var indicator string
		switch v.connStatus[c.Name] {
		case ctxConnOK:
			indicator = okStyle.Render("●")
		case ctxConnFailed:
			indicator = failStyle.Render("●")
			disabled[i] = true
		default:
			indicator = "○"
		}
		rows[i] = component.Row{indicator, c.Name, c.URL}
	}
	v.table.SetRows(rows)
	v.table.SetDisabled(disabled)

	for i, c := range v.contexts {
		if c.Name == v.current {
			v.table.SetCursor(i)
			break
		}
	}
}

// ProbeContextCmd runs a connection probe against the given context and
// emits ContextProbeMsg with the result.
func ProbeContextCmd(ctx config.ContextConfig) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client := jenkins.NewClient(ctx.URL, ctx.Username, ctx.Token, ctx.Insecure)
		_, err := client.WhoAmI(c)
		return ContextProbeMsg{Name: ctx.Name, OK: err == nil}
	}
}

func (v *ContextView) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(v.contexts))
	for _, c := range v.contexts {
		cmds = append(cmds, ProbeContextCmd(c))
	}
	return tea.Batch(cmds...)
}

func (v *ContextView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		return v, v.handleThemeChanged(msg)
	case ContextProbeMsg:
		return v, v.handleContextProbe(msg)
	case ContextListUpdatedMsg:
		return v, v.handleContextListUpdated(msg)
	case tea.KeyMsg:
		return v, v.handleKeyMsg(msg)
	}
	return v, nil
}

func (v *ContextView) handleThemeChanged(msg ThemeChangedMsg) tea.Cmd {
	v.theme = msg.Theme
	v.table.SetTheme(msg.Theme)
	v.populateTable()
	return nil
}

func (v *ContextView) handleContextProbe(msg ContextProbeMsg) tea.Cmd {
	if msg.OK {
		v.connStatus[msg.Name] = ctxConnOK
	} else {
		v.connStatus[msg.Name] = ctxConnFailed
	}
	v.populateTable()
	return nil
}

func (v *ContextView) handleContextListUpdated(msg ContextListUpdatedMsg) tea.Cmd {
	v.contexts = msg.Contexts
	v.current = msg.Current
	// Drop probe state for removed contexts.
	for name := range v.connStatus {
		found := false
		for _, c := range v.contexts {
			if c.Name == name {
				found = true
				break
			}
		}
		if !found {
			delete(v.connStatus, name)
		}
	}
	v.populateTable()
	// Re-probe any newly-added contexts (those with no status yet).
	var cmds []tea.Cmd
	for _, c := range v.contexts {
		if _, ok := v.connStatus[c.Name]; !ok {
			cmds = append(cmds, ProbeContextCmd(c))
		}
	}
	if len(cmds) > 0 {
		return tea.Batch(cmds...)
	}
	return nil
}

func (v *ContextView) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	if v.dialog.IsOpen() {
		if v.dialog.Update(msg) && len(v.contexts) > 0 {
			name := v.contexts[v.table.Cursor()].Name
			return func() tea.Msg { return ContextDeleteRequestMsg{Name: name} }
		}
		return nil
	}
	switch msg.String() {
	case "up", "k":
		v.table.MoveUp()
	case "down", "j":
		v.table.MoveDown()
	case "pgup":
		v.table.PageUp()
	case "pgdown":
		v.table.PageDown()
	case "home":
		v.table.Home()
	case "end":
		v.table.End()
	case "enter":
		if len(v.contexts) == 0 {
			return nil
		}
		sel := v.contexts[v.table.Cursor()]
		return tea.Batch(
			func() tea.Msg { return ContextSwitchRequestMsg{Name: sel.Name} },
			func() tea.Msg { return PopViewMsg{} },
		)
	case "delete", "ctrl+d":
		if len(v.contexts) > 0 {
			v.dialog.Open()
		}
	case "ctrl+n", "insert":
		return func() tea.Msg { return OpenAddContextDialogMsg{} }
	}
	return nil
}

func (v *ContextView) View() string {
	return v.table.View()
}

func (v *ContextView) PopupView() string {
	if !v.dialog.IsOpen() || len(v.contexts) == 0 {
		return ""
	}
	name := v.contexts[v.table.Cursor()].Name
	return v.dialog.View(v.theme,
		"Delete Context",
		fmt.Sprintf("Delete context %q?", name),
	)
}

func (v *ContextView) Title() string { return "context" }

func (v *ContextView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{ViewType: "context"}
}

func (v *ContextView) ItemCount() int { return v.table.TotalRows() }

func (v *ContextView) Commands() []command.Command { return nil }

func (v *ContextView) Shortcuts() []component.Shortcut {
	return []component.Shortcut{
		component.Nav("enter", "switch"),
		component.Nav("esc", "cancel"),
		component.Action("del", "delete"),
		component.Action("ctrl+n", "add"),
	}
}

func (v *ContextView) HasPopup() bool { return v.dialog.IsOpen() }

func (v *ContextView) SetSize(width, height int) {
	v.width = width
	v.height = height
	// 2 (indicator) + 20 (name) + 3*2 (padding for 3 cols) = 28; remainder for URL.
	nameW := 20
	indW := 2
	urlW := max(10, width-indW-nameW-3*2)
	v.table.SetColumnWidth(0, indW)
	v.table.SetColumnWidth(1, nameW)
	v.table.SetColumnWidth(2, urlW)
	v.table.SetSize(width, height)
}

func (v *ContextView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: v.table.ScrollOffset(), TotalLines: v.table.TotalRows(), ViewHeight: v.table.ContentHeight()}
}

func (v *ContextView) Close() error { return nil }

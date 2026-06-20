package view

import (
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// ArtifactView lists the build artifacts and allows opening them in the browser.
type ArtifactView struct {
	BaseView
	table     component.Table
	artifacts []jmodel.Artifact
	build     jmodel.Build
}

// NewArtifactView creates an ArtifactView for the given build's artifacts.
func NewArtifactView(t theme.Theme, artifacts []jmodel.Artifact, nc NavigationContext, build jmodel.Build, client jmodel.JenkinsClient, store *cache.Store) *ArtifactView {
	columns := []component.Column{
		{Title: "ARTIFACT", Width: 60},
	}
	v := &ArtifactView{
		BaseView:  NewBaseView(t, client, store, nc, CtxBuild),
		table:     component.NewTable(t, columns),
		artifacts: artifacts,
		build:     build,
	}
	v.populateTable()
	return v
}

func (v *ArtifactView) Init() tea.Cmd {
	return nil
}

func (v *ArtifactView) populateTable() {
	rows := make([]component.Row, len(v.artifacts))
	for i, a := range v.artifacts {
		rows[i] = component.Row{a.DisplayPath}
	}
	v.table.SetRows(rows)
}

func (v *ArtifactView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		v.theme = msg.Theme
		v.table.SetTheme(msg.Theme)
		return v, nil

	case tea.KeyMsg:
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
			if cmd := v.openSelected(); cmd != nil {
				return v, cmd
			}
		case "o":
			if idx := v.table.Cursor(); idx >= 0 && idx < len(v.artifacts) {
				return v, openURLCmd(v.artifacts[idx].URL)
			}
		default:
			if cmd, ok := v.handleTabKey(msg.String()); ok {
				return v, cmd
			}
		}
	}
	return v, nil
}

// handleTabKey routes the sibling detail-view tab shortcuts (l/s/d/T). Returns
// ok=false for any other key so the caller can keep handling it.
func (v *ArtifactView) handleTabKey(key string) (tea.Cmd, bool) {
	switch key {
	case "s":
		return func() tea.Msg {
			return SwapViewMsg{View: NewStageView(v.theme, v.client, v.store, v.nc, v.build)}
		}, true
	case "l":
		nc := v.nc
		build := v.build
		return func() tea.Msg {
			cv := NewConsoleView(v.theme, v.client, nc)
			cv.build = build
			cv.store = v.store
			return SwapViewMsg{View: cv}
		}, true
	case "d":
		return func() tea.Msg {
			return SwapViewMsg{View: NewDescribeView(v.theme, v.client, v.store, v.nc, v.build)}
		}, true
	case "T":
		if v.store != nil {
			storeKey := fmt.Sprintf("%s:%d", v.nc.JobPath(), v.build.Number)
			if entry := v.store.TestReports.Get(storeKey); entry != nil && entry.Value != nil && len(entry.Value.Suites) > 0 {
				child := NewTestReportView(v.theme, *entry.Value, v.nc, v.build, v.client, v.store)
				return func() tea.Msg { return SwapViewMsg{View: child} }, true
			}
		}
		return nil, true
	}
	return nil, false
}

// openSelected opens the highlighted artifact: text files in the in-TUI viewer,
// everything else in the system browser. Returns nil when no row is selected.
func (v *ArtifactView) openSelected() tea.Cmd {
	idx := v.table.Cursor()
	if idx < 0 || idx >= len(v.artifacts) {
		return nil
	}
	art := v.artifacts[idx]
	if IsTextArtifact(art.DisplayPath) {
		child := NewArtifactFileView(v.theme, v.client, v.store, v.nc, art, v.build, v.artifacts)
		return func() tea.Msg { return PushViewMsg{View: child} }
	}
	return func() tea.Msg {
		_ = exec.Command("xdg-open", art.URL).Start()
		return nil
	}
}

func (v *ArtifactView) View() string {
	return v.table.View()
}

func (v *ArtifactView) Title() string {
	return decodeName(v.nc.ProjectName)
}

func (v *ArtifactView) Breadcrumb() BreadcrumbSegment {
	return v.MakeBreadcrumb("artifacts")
}

func (v *ArtifactView) ItemCount() int {
	return len(v.artifacts)
}

func (v *ArtifactView) Commands() []command.Command {
	return nil
}

func (v *ArtifactView) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{component.Nav("esc", "builds")}
	if idx := v.table.Cursor(); idx >= 0 && idx < len(v.artifacts) {
		action := "open"
		if IsTextArtifact(v.artifacts[idx].DisplayPath) {
			action = "view"
		}
		sc = append(sc, component.Nav("enter", action))
		sc = append(sc, component.Nav("o", "browser"))
	}
	sc = append(sc, detailViewTabs("")...)
	if v.store != nil {
		key := fmt.Sprintf("%s:%d", v.nc.JobPath(), v.build.Number)
		if entry := v.store.TestReports.Get(key); entry != nil && entry.Value != nil && len(entry.Value.Suites) > 0 {
			badge := renderTestBadge(v.theme, entry.Value)
			sc = append(sc, component.ViewSCRanked("T", "tests: "+badge, false, rankViewTests))
		}
	}
	sc = append(sc, component.ViewSCRanked("A", artifactShortcutAction(v.artifacts), true, rankViewArtifacts))
	return sc
}

func (v *ArtifactView) SetSize(width, height int) {
	v.BaseView.SetSize(width, height)
	v.table.SetColumnWidth(0, width-2)
	v.table.SetSize(width, height)
}

func (v *ArtifactView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: v.table.ScrollOffset(), TotalLines: v.table.TotalRows(), ViewHeight: v.table.ContentHeight()}
}

func (v *ArtifactView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	nc := v.nc.AtBranch(v.nc.BranchName)
	if v.nc.Level == CtxProject {
		return NewBuildsView(t, c, s, nc, NewProjectBuildsProvider(c, s, nc))
	}
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

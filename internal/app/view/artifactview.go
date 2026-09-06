package view

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// ArtifactView browses the build artifacts as a directory tree and opens them
// in the in-TUI viewer or the browser. Archived report trees run to hundreds of
// files, so a flat list is unusable; the tree mirrors what Jenkins' web UI
// shows for the same build.
type ArtifactView struct {
	BaseView
	tree        widget.Tree
	artifacts   []jmodel.Artifact
	byURL       map[string]jmodel.Artifact
	build       jmodel.Build
	searchQuery string
}

// NewArtifactView creates an ArtifactView for the given build's artifacts.
func NewArtifactView(t theme.Theme, artifacts []jmodel.Artifact, nc NavigationContext, build jmodel.Build, client jmodel.JenkinsClient, store *cache.Store) *ArtifactView {
	v := &ArtifactView{
		BaseView:  NewBaseView(t, client, store, nc, CtxBuild),
		tree:      widget.NewTree(t),
		artifacts: artifacts,
		byURL:     make(map[string]jmodel.Artifact, len(artifacts)),
		build:     build,
	}
	v.tree.HideLeafValues()
	v.tree.SetEmptyText("(no artifacts)")
	v.SeedBuildIdentity(build)
	v.populateTree()
	return v
}

func (v *ArtifactView) Init() tea.Cmd {
	return nil
}

func (v *ArtifactView) populateTree() {
	for _, a := range v.artifacts {
		v.byURL[a.URL] = a
	}
	v.tree.SetRoot(BuildArtifactTree(v.artifacts))
}

func (v *ArtifactView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		v.theme = msg.Theme
		v.tree.SetTheme(msg.Theme)
		return v, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			v.tree.MoveUp()
		case "down", "j":
			v.tree.MoveDown()
		case "pgup":
			v.tree.PageUp()
		case "pgdown":
			v.tree.PageDown()
		case "home":
			v.tree.Home()
		case "end":
			v.tree.End()
		case "left", "h":
			v.tree.Collapse()
		case "right", " ":
			v.tree.Expand()
		case "enter":
			if cmd := v.openSelected(); cmd != nil {
				return v, cmd
			}
		case "o":
			if _, url, _, ok := v.tree.Selected(); ok && url != "" {
				return v, openURLCmd(url)
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
			cv.SetBuild(build)
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

// openSelected acts on the highlighted row: a directory holding an index.html
// opens that report in the browser, any other directory toggles open, text
// files go to the in-TUI viewer and everything else to the browser. Returns nil
// when the row has no action.
func (v *ArtifactView) openSelected() tea.Cmd {
	_, url, container, ok := v.tree.Selected()
	if !ok {
		return nil
	}
	if container {
		if url == "" {
			v.tree.Expand()
			return nil
		}
		return openURLCmd(url)
	}
	art, found := v.byURL[url]
	if !found {
		return nil
	}
	if IsTextArtifact(art.DisplayPath) {
		child := NewArtifactFileView(v.theme, v.client, v.store, v.nc, art, v.build, v.artifacts)
		return func() tea.Msg { return PushViewMsg{View: child} }
	}
	return openURLCmd(art.URL)
}

func (v *ArtifactView) View() string {
	return v.tree.View()
}

// ApplySearch / SearchQuery implement Searchable so `/` filters the tree,
// auto-expanding the ancestors of every match.
func (v *ArtifactView) ApplySearch(pattern string) tea.Cmd {
	v.searchQuery = pattern
	v.tree.ApplySearch(widget.CompileSearchRegex(pattern))
	return nil
}

func (v *ArtifactView) SearchQuery() string { return v.searchQuery }

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
	if key, url, container, ok := v.tree.Selected(); ok {
		sc = append(sc, component.Nav("enter", artifactEnterAction(key, url, container)))
		if url != "" {
			sc = append(sc, component.Nav("o", "browser"))
		}
	}
	sc = append(sc, component.Filter("/", "search", v.searchQuery != ""))
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

// artifactEnterAction labels the enter shortcut for the highlighted row.
func artifactEnterAction(key, url string, container bool) string {
	switch {
	case container && url != "":
		return "open report"
	case container:
		return "expand"
	case IsTextArtifact(key):
		return "view"
	default:
		return "open"
	}
}

func (v *ArtifactView) SetSize(width, height int) {
	v.BaseView.SetSize(width, height)
	v.tree.SetSize(width, height)
}

func (v *ArtifactView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: v.tree.ScrollOffset(), TotalLines: v.tree.TotalRows(), ViewHeight: v.tree.ContentHeight()}
}

func (v *ArtifactView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	nc := v.nc.AtBranch(v.nc.BranchName)
	if v.nc.Level == CtxProject {
		return NewBuildsView(t, c, s, nc, NewProjectBuildsProvider(c, s, nc))
	}
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

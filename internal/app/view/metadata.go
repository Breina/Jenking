package view

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// metadataMsg carries a fetched metadata tree (at a given depth) for a job or
// build. seq guards against a stale shallow fetch overwriting a deeper one.
type metadataMsg struct {
	root  jmodel.MetaNode
	depth int
	err   error
}

// MetadataView is a generic, plugin-agnostic inspector: a collapsible tree of
// a job's or build's raw Jenkins JSON. It starts at depth=1 and fetches deeper
// on demand as the user expands nodes (Jenkins can't lazily fetch an arbitrary
// subtree, so a deeper expand re-fetches the whole object one level deeper —
// the tree widget preserves expansion + cursor across the swap).
type MetadataView struct {
	BaseView

	tree        widget.Tree
	buildNumber int // 0 ⇒ job-level metadata
	loadedDepth int
	loading     bool
	searchQuery string
	copied      bool
	objectURL   string   // SCM project/branch web URL, when present (see objectMetadataClass)
	navChain    []string // nav-tag trail (parent chain + "metadata"), set at construction
}

// maxMetaDepth caps the auto/explicit depth growth so a deeply-nested object
// can't pull an unbounded payload.
const maxMetaDepth = 10

// objectMetadataClass is the core SCM-API action that carries the agnostic
// project/branch web URL in its objectUrl field (not a per-platform plugin).
const objectMetadataClass = "jenkins.scm.api.metadata.ObjectMetadataAction"

// NewMetadataView constructs a metadata inspector. The nc decides the source:
// a concrete build number ⇒ build metadata, otherwise the job's metadata.
// parentChain is the nav-tag trail of the view it was opened from; "metadata"
// is appended so the inspector shows e.g. <jobs> <builds> <stages> <metadata>.
func NewMetadataView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, parentChain []string) *MetadataView {
	chain := append(append([]string{}, parentChain...), "metadata")
	return &MetadataView{
		BaseView:    NewBaseView(t, client, store, nc, nc.Level),
		tree:        widget.NewTree(t),
		buildNumber: nc.Build.Number,
		navChain:    chain,
	}
}

func (mv *MetadataView) Init() tea.Cmd {
	return mv.fetchCmd(1)
}

func (mv *MetadataView) fetchCmd(depth int) tea.Cmd {
	mv.loading = true
	ctx := mv.ctx
	client := mv.client
	jobPath := mv.nc.JobPath()
	build := mv.buildNumber
	return func() tea.Msg {
		var (
			root jmodel.MetaNode
			err  error
		)
		if build > 0 {
			root, err = client.GetBuildMetadata(ctx, jobPath, build, depth)
		} else {
			root, err = client.GetJobMetadata(ctx, jobPath, depth)
		}
		if ctx.Err() != nil {
			return nil
		}
		return metadataMsg{root: root, depth: depth, err: err}
	}
}

func (mv *MetadataView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case metadataMsg:
		mv.loading = false
		if msg.err != nil {
			return mv, func() tea.Msg { return ErrorMsg{Err: msg.err} }
		}
		// Ignore a stale fetch that's shallower than what we already have.
		if msg.depth < mv.loadedDepth {
			return mv, nil
		}
		mv.loadedDepth = msg.depth
		mv.objectURL = findObjectURL(msg.root)
		mv.tree.SetRoot(toTreeNode(msg.root))
		return mv, nil
	case ThemeChangedMsg:
		mv.theme = msg.Theme
		mv.tree.SetTheme(msg.Theme)
		return mv, nil
	case widget.CopyFlashMsg:
		mv.copied = true
		return mv, widget.CopyFlashTimer(msg.IsSel)
	case widget.CopyFlashDoneMsg:
		mv.copied = false
		return mv, nil
	case tea.KeyMsg:
		return mv, mv.handleKey(msg)
	}
	return mv, nil
}

func (mv *MetadataView) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		mv.tree.MoveUp()
	case "down", "j":
		mv.tree.MoveDown()
	case "pgup":
		mv.tree.PageUp()
	case "pgdown":
		mv.tree.PageDown()
	case "home":
		mv.tree.Home()
	case "end":
		mv.tree.End()
	case "right", "l", "enter", " ":
		return mv.expand()
	case "left", "h":
		mv.tree.Collapse()
	case "]":
		return mv.fetchDeeper()
	case "[":
		return mv.fetchShallower()
	case "o":
		if mv.objectURL != "" {
			return openURLCmd(mv.objectURL)
		}
	case "c":
		if val, ok := mv.tree.SelectedValue(); ok {
			return widget.CopyTextCmd(val, false)
		}
	}
	return nil
}

// findObjectURL scans the metadata tree for the first
// ObjectMetadataAction.objectUrl — the agnostic SCM project/branch web URL.
// Top-level actions are visited before nested jobs (children are sorted by
// key), so a multibranch project yields its repo URL and a branch its tree URL.
func findObjectURL(n jmodel.MetaNode) string {
	if !n.Container {
		return ""
	}
	var class, url string
	for _, c := range n.Children {
		switch {
		case c.Key == "_class" && !c.Container:
			class = c.Value
		case c.Key == "objectUrl" && !c.Container:
			url = c.Value
		}
	}
	if class == objectMetadataClass && url != "" {
		return url
	}
	for _, c := range n.Children {
		if c.Container {
			if u := findObjectURL(c); u != "" {
				return u
			}
		}
	}
	return ""
}

// expand opens the cursor node. Expanding a truncated/dead-end container (no
// container children loaded) auto-fetches one level deeper, since Jenkins
// doesn't mark where it cut the tree off.
func (mv *MetadataView) expand() tea.Cmd {
	expanded, hasContainerChild := mv.tree.Expand()
	if !expanded {
		return nil
	}
	if !hasContainerChild {
		return mv.fetchDeeper()
	}
	return nil
}

func (mv *MetadataView) fetchDeeper() tea.Cmd {
	if mv.loading || mv.loadedDepth >= maxMetaDepth {
		return nil
	}
	return mv.fetchCmd(mv.loadedDepth + 1)
}

func (mv *MetadataView) fetchShallower() tea.Cmd {
	if mv.loading || mv.loadedDepth <= 1 {
		return nil
	}
	target := mv.loadedDepth - 1
	// A shallower payload would be dropped by the stale-guard, so reset depth
	// tracking before forcing the reload.
	mv.loadedDepth = 0
	return mv.fetchCmd(target)
}

// toTreeNode maps the domain metadata tree to the widget's decoupled node type.
func toTreeNode(n jmodel.MetaNode) widget.TreeNode {
	out := widget.TreeNode{Key: n.Key, Value: n.Value, Container: n.Container}
	if len(n.Children) > 0 {
		out.Children = make([]widget.TreeNode, len(n.Children))
		for i, c := range n.Children {
			out.Children[i] = toTreeNode(c)
		}
	}
	return out
}

func (mv *MetadataView) View() string {
	return mv.tree.View()
}

func (mv *MetadataView) SetSize(width, height int) {
	mv.BaseView.SetSize(width, height)
	mv.tree.SetSize(width, height)
}

func (mv *MetadataView) Title() string { return "metadata" }

func (mv *MetadataView) ItemCount() int { return mv.tree.TotalRows() }

func (mv *MetadataView) Commands() []command.Command { return nil }

func (mv *MetadataView) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{component.Nav("esc", "back")}
	if mv.objectURL != "" {
		sc = append(sc, component.Nav("o", "open repo"))
	}
	sc = append(sc,
		component.Filter("/", "search", mv.searchQuery != ""),
		component.Action("]", "deeper"),
	)
	if mv.loadedDepth > 1 {
		sc = append(sc, component.Action("[", "shallower"))
	}
	if _, ok := mv.tree.SelectedValue(); ok {
		// Mirror the log/describe copy: flash the shortcut's Active state for a
		// second rather than showing a separate badge.
		sc = append(sc, component.Shortcut{
			Key: "c", Action: "copy", Active: mv.copied,
			Group: component.GroupAction, Rank: rankActionCopy,
		})
	}
	return sc
}

func (mv *MetadataView) Breadcrumb() BreadcrumbSegment {
	seg := mv.MakeBreadcrumb("metadata")
	seg.NavChain = mv.navChain
	return seg
}

// Badge shows the loaded depth.
func (mv *MetadataView) Badge() string {
	if mv.loadedDepth > 0 {
		return "depth " + strconv.Itoa(mv.loadedDepth)
	}
	return ""
}

func (mv *MetadataView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: mv.tree.ScrollOffset(), TotalLines: mv.tree.TotalRows(), ViewHeight: mv.tree.ContentHeight()}
}

// ApplySearch / SearchQuery implement Searchable so the app's `/` search bar
// drives the tree filter.
func (mv *MetadataView) ApplySearch(pattern string) tea.Cmd {
	mv.searchQuery = pattern
	mv.tree.ApplySearch(widget.CompileSearchRegex(pattern))
	return nil
}

func (mv *MetadataView) SearchQuery() string { return mv.searchQuery }

// InspectTarget disables `:inspect` while already in the inspector (it would
// just re-open the same tree), overriding the BaseView default.
func (mv *MetadataView) InspectTarget() (NavigationContext, bool) {
	return NavigationContext{}, false
}

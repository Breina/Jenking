package widget

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// TreeNode is one node of a generic key/value tree. Containers (objects/arrays)
// carry Children; scalar leaves carry Value. It is deliberately decoupled from
// any domain type so the Tree stays a reusable widget.
type TreeNode struct {
	Key       string
	Value     string
	Container bool
	Children  []TreeNode
}

// treeRow is a flattened, currently-visible line in the tree.
type treeRow struct {
	path            string // stable identity across rebuilds (NUL-joined keys)
	depth           int    // render indentation level (root's children = 0)
	key             string
	value           string
	container       bool
	hasKids         bool
	hasContainerKid bool // a loaded child is itself a container (more to explore)
	expanded        bool
}

// Tree is a scrollable, collapsible key/value tree with search filtering. It
// renders TreeNode data; expansion state and cursor survive SetRoot so callers
// can swap in a deeper-fetched root without losing the user's place.
type Tree struct {
	theme    theme.Theme
	root     TreeNode
	expanded map[string]bool
	rows     []treeRow
	cursor   int
	offset   int
	width    int
	height   int
	search   *regexp.Regexp

	cursorPath string // remembered to restore the cursor across rebuilds
}

// NewTree creates an empty tree.
func NewTree(t theme.Theme) Tree {
	return Tree{theme: t, expanded: map[string]bool{}, width: 80, height: 20}
}

func (t *Tree) SetTheme(th theme.Theme) { t.theme = th }

func (t *Tree) SetSize(w, h int) {
	t.width = w
	t.height = h
	t.clampOffset()
}

// SetRoot replaces the tree data, preserving expansion state and (by path) the
// cursor position. This is what makes a depth refetch seamless.
func (t *Tree) SetRoot(root TreeNode) {
	t.root = root
	t.rebuild()
}

func (t *Tree) ApplySearch(re *regexp.Regexp) {
	t.search = re
	t.rebuild()
}

// ---- navigation ----

func (t *Tree) MoveUp() {
	if t.cursor > 0 {
		t.cursor--
		t.syncCursorPath()
		t.clampOffset()
	}
}

func (t *Tree) MoveDown() {
	if t.cursor < len(t.rows)-1 {
		t.cursor++
		t.syncCursorPath()
		t.clampOffset()
	}
}

func (t *Tree) PageUp() {
	t.cursor -= t.viewHeight()
	if t.cursor < 0 {
		t.cursor = 0
	}
	t.syncCursorPath()
	t.clampOffset()
}

func (t *Tree) PageDown() {
	t.cursor += t.viewHeight()
	if t.cursor > len(t.rows)-1 {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	t.syncCursorPath()
	t.clampOffset()
}

func (t *Tree) Home() {
	t.cursor = 0
	t.syncCursorPath()
	t.clampOffset()
}

func (t *Tree) End() {
	t.cursor = len(t.rows) - 1
	if t.cursor < 0 {
		t.cursor = 0
	}
	t.syncCursorPath()
	t.clampOffset()
}

// Expand opens the cursor container (no-op if already open or a leaf). Returns
// whether it actually expanded and whether the node has any container child
// loaded. A truncated/dead-end container (expanded=true, hasContainerChild=
// false) is the caller's cue to fetch one level deeper.
func (t *Tree) Expand() (expanded, hasContainerChild bool) {
	r, ok := t.cursorRow()
	if !ok || !r.container || t.expanded[r.path] {
		return false, false
	}
	t.expanded[r.path] = true
	hasC := r.hasContainerKid
	t.rebuild()
	return true, hasC
}

// Collapse closes the cursor container (or jumps to the parent when the cursor
// is already on a collapsed/leaf node, mirroring common tree UIs).
func (t *Tree) Collapse() {
	r, ok := t.cursorRow()
	if !ok {
		return
	}
	if r.container && t.expanded[r.path] {
		delete(t.expanded, r.path)
		t.rebuild()
		return
	}
	// Jump to parent: the nearest preceding row at a shallower depth.
	for i := t.cursor - 1; i >= 0; i-- {
		if t.rows[i].depth < r.depth {
			t.cursor = i
			t.syncCursorPath()
			t.clampOffset()
			return
		}
	}
}

// ---- queries ----

// SelectedValue returns the cursor leaf's value, ok=false for containers/empty.
func (t *Tree) SelectedValue() (string, bool) {
	r, ok := t.cursorRow()
	if !ok || r.container {
		return "", false
	}
	return r.value, true
}

func (t *Tree) ScrollOffset() int  { return t.offset }
func (t *Tree) TotalRows() int     { return len(t.rows) }
func (t *Tree) ContentHeight() int { return t.viewHeight() }

// ---- internals ----

func (t *Tree) cursorRow() (treeRow, bool) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return treeRow{}, false
	}
	return t.rows[t.cursor], true
}

func (t *Tree) viewHeight() int {
	if t.height < 1 {
		return 1
	}
	return t.height
}

func (t *Tree) clampOffset() {
	vh := t.viewHeight()
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+vh {
		t.offset = t.cursor - vh + 1
	}
	maxOffset := len(t.rows) - vh
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

func (t *Tree) syncCursorPath() {
	if r, ok := t.cursorRow(); ok {
		t.cursorPath = r.path
	}
}

// rebuild flattens the visible rows from the current root, expansion set and
// search filter, then restores the cursor to its remembered path.
func (t *Tree) rebuild() {
	t.rows = t.rows[:0]
	t.collect(t.root, "", 0)
	// Restore cursor by path; fall back to clamping.
	t.cursor = 0
	for i, r := range t.rows {
		if r.path == t.cursorPath {
			t.cursor = i
			break
		}
	}
	if t.cursor > len(t.rows)-1 {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	t.syncCursorPath()
	t.clampOffset()
}

// collect appends visible rows for node's children. With search active it
// includes a node only when it (or a descendant) matches, auto-expanding the
// ancestors of matches.
func (t *Tree) collect(node TreeNode, parentPath string, depth int) {
	for _, c := range node.Children {
		path := parentPath + "\x00" + c.Key
		if t.search == nil {
			expanded := c.Container && t.expanded[path]
			t.rows = append(t.rows, treeRow{
				path: path, depth: depth, key: c.Key, value: c.Value,
				container: c.Container, hasKids: len(c.Children) > 0,
				hasContainerKid: anyContainerChild(c.Children), expanded: expanded,
			})
			if expanded && len(c.Children) > 0 {
				t.collect(c, path, depth+1)
			}
			continue
		}
		// Filtered: recurse first so we know whether any descendant matched.
		mark := len(t.rows)
		t.collect(c, path, depth+1)
		kidShown := len(t.rows) > mark
		if t.matches(c) || kidShown {
			row := treeRow{
				path: path, depth: depth, key: c.Key, value: c.Value,
				container: c.Container, hasKids: len(c.Children) > 0,
				hasContainerKid: anyContainerChild(c.Children), expanded: kidShown,
			}
			// Insert the parent row before its already-appended descendants.
			t.rows = append(t.rows, treeRow{})
			copy(t.rows[mark+1:], t.rows[mark:])
			t.rows[mark] = row
		}
	}
}

func anyContainerChild(children []TreeNode) bool {
	for _, c := range children {
		if c.Container {
			return true
		}
	}
	return false
}

func (t *Tree) matches(n TreeNode) bool {
	if t.search == nil {
		return false
	}
	return t.search.MatchString(n.Key) || (!n.Container && t.search.MatchString(n.Value))
}

func (t *Tree) View() string {
	if len(t.rows) == 0 {
		return t.theme.Log.Dim.Render("(no metadata)")
	}
	vh := t.viewHeight()
	end := t.offset + vh
	if end > len(t.rows) {
		end = len(t.rows)
	}
	var b strings.Builder
	for i := t.offset; i < end; i++ {
		b.WriteString(t.renderRow(t.rows[i], i == t.cursor))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (t *Tree) renderRow(r treeRow, selected bool) string {
	indent := strings.Repeat("  ", r.depth)
	glyph := "  "
	if r.container {
		if r.expanded {
			glyph = "▾ "
		} else {
			glyph = "▸ "
		}
	}

	label := r.key
	if r.container {
		if r.hasKids {
			label = r.key + " " + t.theme.Breadcrumb.Paren.Render("["+strconv.Itoa(t.childCount(r))+"]")
		}
	} else {
		label = r.key + t.theme.Breadcrumb.Paren.Render(" = ") + r.value
	}

	line := indent + glyph + label
	line = ansi.Truncate(line, t.width, "…")

	if selected {
		// Strip existing styling so the selected background is uniform.
		return t.theme.Table.Selected.Width(t.width).Inline(true).Render(ansi.Strip(line))
	}
	if t.search != nil {
		line = t.highlight(line)
	}
	return line
}

// childCount returns the number of direct children of the node at row r by
// re-walking the root (rows don't store children to stay light).
func (t *Tree) childCount(r treeRow) int {
	keys := strings.Split(strings.TrimPrefix(r.path, "\x00"), "\x00")
	node := t.root
	for _, k := range keys {
		found := false
		for _, c := range node.Children {
			if c.Key == k {
				node = c
				found = true
				break
			}
		}
		if !found {
			return 0
		}
	}
	return len(node.Children)
}

// highlight wraps search matches in the Match style. Operates on the plain
// (already key=value) text; safe because non-selected rows carry no styling
// except the dim "=" separator which won't match typical queries.
func (t *Tree) highlight(line string) string {
	plain := ansi.Strip(line)
	locs := t.search.FindAllStringIndex(plain, -1)
	if len(locs) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		b.WriteString(plain[last:loc[0]])
		b.WriteString(t.theme.Search.Match.Render(plain[loc[0]:loc[1]]))
		last = loc[1]
	}
	b.WriteString(plain[last:])
	return b.String()
}

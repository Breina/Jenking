package view

import (
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// indexFile is the entry point of a generated HTML report tree (coverage,
// lint, docs). A directory holding one is openable in the browser directly,
// which is nearly always what the user wants from such a folder.
const indexFile = "index.html"

// artifactDir is a mutable directory node used while folding the flat artifact
// list into a tree. Children keep API order (already alphabetical) via order.
type artifactDir struct {
	dirs  map[string]*artifactDir
	files map[string]jmodel.Artifact
	order []string // child names in insertion order (dirs and files interleaved)
}

func newArtifactDir() *artifactDir {
	return &artifactDir{dirs: map[string]*artifactDir{}, files: map[string]jmodel.Artifact{}}
}

// BuildArtifactTree folds artifacts into a widget tree keyed on their
// slash-separated DisplayPath. Leaf values carry the artifact URL (hidden from
// display), and a directory's value is its index.html URL when it has one.
// Single-child directory chains are collapsed into one "a/b/c" row so a report
// buried three levels deep costs one keypress, not three.
func BuildArtifactTree(artifacts []jmodel.Artifact) widget.TreeNode {
	root := newArtifactDir()
	for _, a := range artifacts {
		segs := strings.Split(a.DisplayPath, "/")
		dir := root
		for _, seg := range segs[:len(segs)-1] {
			dir = dir.child(seg)
		}
		dir.addFile(segs[len(segs)-1], a)
	}
	return widget.TreeNode{Container: true, Children: root.toNodes()}
}

// child returns (creating on demand) the subdirectory named name.
func (d *artifactDir) child(name string) *artifactDir {
	if sub, ok := d.dirs[name]; ok {
		return sub
	}
	sub := newArtifactDir()
	d.dirs[name] = sub
	d.order = append(d.order, name)
	return sub
}

func (d *artifactDir) addFile(name string, a jmodel.Artifact) {
	if _, ok := d.files[name]; !ok {
		d.order = append(d.order, name)
	}
	d.files[name] = a
}

// toNodes converts the directory's children to widget nodes, collapsing any
// directory that holds exactly one subdirectory and nothing else.
func (d *artifactDir) toNodes() []widget.TreeNode {
	out := make([]widget.TreeNode, 0, len(d.order))
	for _, name := range d.order {
		if art, ok := d.files[name]; ok {
			out = append(out, widget.TreeNode{Key: name, Value: art.URL})
			continue
		}
		sub := d.dirs[name]
		for len(sub.files) == 0 && len(sub.dirs) == 1 {
			only := sub.order[0]
			name += "/" + only
			sub = sub.dirs[only]
		}
		out = append(out, widget.TreeNode{
			Key:       name,
			Value:     sub.indexURL(),
			Container: true,
			Children:  sub.toNodes(),
		})
	}
	return out
}

// indexURL returns the URL of this directory's index.html, or "" if it has none.
func (d *artifactDir) indexURL() string {
	if a, ok := d.files[indexFile]; ok {
		return a.URL
	}
	return ""
}

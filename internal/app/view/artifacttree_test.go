package view

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/widget"
)

func findChild(nodes []widget.TreeNode, key string) (widget.TreeNode, bool) {
	for _, n := range nodes {
		if n.Key == key {
			return n, true
		}
	}
	return widget.TreeNode{}, false
}

func TestBuildArtifactTreeCollapsesSingleChildChains(t *testing.T) {
	arts := []jmodel.Artifact{
		{DisplayPath: "webapp/coverage/lcov-report/base.css", URL: "u1"},
		{DisplayPath: "webapp/coverage/lcov-report/index.html", URL: "u2"},
		{DisplayPath: "webapp/coverage/lcov-report/src/app.ts.html", URL: "u3"},
		{DisplayPath: "trivy-report.html", URL: "u4"},
	}
	root := BuildArtifactTree(arts)

	if len(root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}
	dir, ok := findChild(root.Children, "webapp/coverage/lcov-report")
	if !ok {
		t.Fatalf("single-child chain not collapsed, got %+v", root.Children)
	}
	if !dir.Container {
		t.Error("collapsed chain should be a container")
	}
	if dir.Value != "u2" {
		t.Errorf("dir value = %q, want the index.html URL", dir.Value)
	}
	if _, ok := findChild(dir.Children, "src"); !ok {
		t.Errorf("expected nested src dir, got %+v", dir.Children)
	}

	leaf, ok := findChild(root.Children, "trivy-report.html")
	if !ok {
		t.Fatal("top-level file missing")
	}
	if leaf.Container || leaf.Value != "u4" {
		t.Errorf("leaf = %+v, want file carrying its URL", leaf)
	}
}

func TestBuildArtifactTreeDirWithoutIndexHasNoValue(t *testing.T) {
	root := BuildArtifactTree([]jmodel.Artifact{
		{DisplayPath: "logs/a.txt", URL: "u1"},
		{DisplayPath: "logs/b.txt", URL: "u2"},
	})
	dir, ok := findChild(root.Children, "logs")
	if !ok {
		t.Fatalf("logs dir missing, got %+v", root.Children)
	}
	if dir.Value != "" {
		t.Errorf("dir value = %q, want empty (no index.html)", dir.Value)
	}
	if len(dir.Children) != 2 {
		t.Errorf("children = %d, want 2", len(dir.Children))
	}
}

func TestArtifactEnterAction(t *testing.T) {
	cases := []struct {
		key, url string
		dir      bool
		want     string
	}{
		{"coverage", "u", true, "open report"},
		{"logs", "", true, "expand"},
		{"out.txt", "u", false, "view"},
		{"app.jar", "u", false, "open"},
	}
	for _, c := range cases {
		if got := artifactEnterAction(c.key, c.url, c.dir); got != c.want {
			t.Errorf("artifactEnterAction(%q,%v) = %q, want %q", c.key, c.dir, got, c.want)
		}
	}
}

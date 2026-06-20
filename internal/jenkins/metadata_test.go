package jenkins

import (
	"encoding/json"
	"testing"
)

func TestBuildMetaTreeAndFlatten(t *testing.T) {
	const raw = `{
		"_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
		"name": "my-project",
		"buildable": true,
		"nextBuildNumber": 42,
		"actions": [
			{},
			{"_class": "hudson.plugins.git.util.BuildData", "remoteUrls": ["git@gitlab.example.com:team/repo.git"]}
		],
		"property": [
			{"_class": "x", "value": null}
		]
	}`

	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	tree := buildMetaTree("", root)
	if !tree.Container {
		t.Fatalf("root should be a container")
	}

	// Flatten gives dotted/bracketed paths for scalar leaves.
	got := make(map[string]string)
	for _, e := range tree.Flatten() {
		got[e.Path] = e.Value
	}

	want := map[string]string{
		"_class":                   "org.jenkinsci.plugins.workflow.job.WorkflowJob",
		"name":                     "my-project",
		"buildable":                "true",
		"nextBuildNumber":          "42",
		"actions[1]._class":        "hudson.plugins.git.util.BuildData",
		"actions[1].remoteUrls[0]": "git@gitlab.example.com:team/repo.git",
		"property[0]._class":       "x",
	}
	for path, val := range want {
		if got[path] != val {
			t.Errorf("path %q = %q, want %q", path, got[path], val)
		}
	}

	// null leaf is an empty-string scalar at its path.
	if v, ok := got["property[0].value"]; !ok || v != "" {
		t.Errorf("property[0].value = (%q, %v), want (\"\", true)", v, ok)
	}

	// "actions" is a container; "name" is a leaf.
	for _, c := range tree.Children {
		switch c.Key {
		case "actions":
			if !c.Container {
				t.Errorf("actions should be a container")
			}
		case "name":
			if c.Container {
				t.Errorf("name should be a leaf")
			}
		}
	}
}

package pipelinesyntax

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleUserGDSL = `
// Custom symbols for the Cumuli shared library.
contributor(context(ctype: 'maven')) {
    method(name: 'setVersion', type: 'Object',
           params: [version:'java.lang.String'],
           doc: 'Set Maven project version (e.g. 1.2.3-SNAPSHOT)')
    method(name: 'goal', type: 'Object',
           namedParams: [parameter(name: 'name', type: 'java.lang.String'),
                         parameter(name: 'profile', type: 'java.lang.String')],
           doc: 'Run a Maven goal in the project workspace')
    property(name: 'projectName', type: 'java.lang.String',
             doc: 'Currently configured Maven project name')
}

contributor(context(ctype: 'helm')) {
    method(name: 'install', type: 'Object', params: [release:'java.lang.String'], doc: 'Install a release')
}

// A stray method outside any contributor — must be ignored.
method(name: 'doNotInclude', type: 'Object', params: [:], doc: 'top-level')
`

func TestParseUserGDSL(t *testing.T) {
	got := ParseUserGDSL(sampleUserGDSL)
	if len(got) != 2 {
		t.Fatalf("expected 2 receivers, got %d: %+v", len(got), got)
	}

	maven, ok := got["maven"]
	if !ok || len(maven) != 3 {
		t.Fatalf("maven members wrong: %+v", maven)
	}
	byName := map[string]Member{}
	for _, m := range maven {
		byName[m.Name] = m
	}
	if byName["setVersion"].Signature != "setVersion(version: String)" {
		t.Errorf("setVersion sig wrong: %q", byName["setVersion"].Signature)
	}
	if byName["goal"].Signature != "goal(name: String, profile: String)" {
		t.Errorf("goal sig wrong: %q", byName["goal"].Signature)
	}
	if _, hasProperty := byName["projectName"]; !hasProperty {
		t.Errorf("projectName property missing")
	}

	helm, ok := got["helm"]
	if !ok || len(helm) != 1 || helm[0].Name != "install" {
		t.Errorf("helm members wrong: %+v", helm)
	}

	for recv := range got {
		for _, m := range got[recv] {
			if m.Name == "doNotInclude" {
				t.Errorf("top-level method leaked into receiver %s", recv)
			}
		}
	}
}

// cumulusStyleGDSL mirrors the format the user's Cumulus library actually
// ships: parenless named-arg method/property calls, triple-quoted multi-line
// HTML doc strings, and fully-qualified ctype paired with a separate
// jenkinsContext property declaration that maps type → user-facing name.
const cumulusStyleGDSL = `
def jenkinsContext = context(filetypes: ['groovy', 'Jenkinsfile'])

contributor(jenkinsContext) {
    property name: 'maven', type: 'be.cumulus.jenkins.dsl.MavenDsl'
}
contributor(context(ctype: 'be.cumulus.jenkins.dsl.MavenDsl')) {
    method name: 'setVersion',
           type: 'java.lang.Object',
           params: [version: 'String'],
           doc: '''<html><body><pre>Stelt de Maven projectversie in.

## Voorbeeld
` + "```" + `groovy
maven.setVersion('1.2.3')
` + "```" + `</pre></body></html>'''

    method name: 'goal',
           type: 'java.lang.Object',
           params: [argMap: 'Map'],
           doc: '''<html><body><pre>Roept maven aan.

* ` + "`goal`" + `: het doel, met spaties gescheiden
* ` + "`extraArgs`" + ` (optioneel): extra argumenten
</pre></body></html>'''

    method name: 'podSpec',
           type: 'String',
           params: [version: 'int'],
           doc: '''<html><body><pre>Specs voor de pod.</pre></body></html>'''
}

contributor(jenkinsContext) {
    property name: 'git', type: 'be.cumulus.jenkins.dsl.GitDsl'
}
contributor(context(ctype: 'be.cumulus.jenkins.dsl.GitDsl')) {
    method name: 'tag',
           type: 'java.lang.Object',
           params: [tagName: 'String', commitSha: 'String'],
           doc: '''<html><body><pre>Plaatst een git tag.</pre></body></html>'''

    method name: 'push',
           type: 'java.lang.Object',
           params: [],
           doc: '''<html><body><pre>Pushed lokale commits.</pre></body></html>'''
}
`

func TestParseUserGDSL_CumulusFormat(t *testing.T) {
	got := ParseUserGDSL(cumulusStyleGDSL)

	maven, ok := got["maven"]
	if !ok {
		t.Fatalf("maven receiver missing; got keys: %v", keysOf(got))
	}
	if len(maven) != 3 {
		t.Fatalf("expected 3 maven members, got %d: %+v", len(maven), maven)
	}
	byName := map[string]Member{}
	for _, m := range maven {
		byName[m.Name] = m
	}
	if _, ok := byName["setVersion"]; !ok {
		t.Errorf("setVersion missing")
	}
	if _, ok := byName["goal"]; !ok {
		t.Errorf("goal missing")
	}
	if _, ok := byName["podSpec"]; !ok {
		t.Errorf("podSpec missing")
	}
	// Doc must come through with the triple-quoted body, HTML-stripped.
	if doc := byName["setVersion"].Doc; doc == "" {
		t.Errorf("setVersion doc empty")
	} else if !contains(doc, "Stelt de Maven projectversie") {
		t.Errorf("setVersion doc lost content: %q", doc)
	}
	// Brace inside doc must NOT prematurely terminate the contributor block —
	// if it did, podSpec (last in the block) would be missing.

	git, ok := got["git"]
	if !ok || len(git) != 2 {
		t.Fatalf("git receiver wrong: %+v", git)
	}
}

func keysOf(m map[string][]Member) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoadUserGDSLDir_MissingIsEmpty(t *testing.T) {
	got, err := LoadUserGDSLDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing dir should yield empty map, got %+v", got)
	}
}

func TestLoadUserGDSLDir_MergesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	must := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must("maven.gdsl", `contributor(context(ctype: 'maven')) {
		method(name: 'goal', type: 'Object', params: [name:'java.lang.String'], doc: 'Run a Maven goal')
	}`)
	must("git.gdsl", `contributor(context(ctype: 'git')) {
		method(name: 'tag', type: 'Object', params: [name:'java.lang.String'], doc: 'Create a tag')
	}`)
	must("ignore-me.txt", `not a gdsl file`)

	got, err := LoadUserGDSLDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 receivers (maven, git), got %d: %+v", len(got), got)
	}
	if len(got["maven"]) != 1 || got["maven"][0].Name != "goal" {
		t.Errorf("maven from file wrong: %+v", got["maven"])
	}
	if len(got["git"]) != 1 || got["git"][0].Name != "tag" {
		t.Errorf("git from file wrong: %+v", got["git"])
	}
}

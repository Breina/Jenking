package pipelinesyntax

import (
	"strings"
	"testing"
)

const sampleGDSL = `
//The global script scope
def ctx = context(scope: scriptScope())

contributor(ctx) {
    method(name: 'sh', type: 'Object', params: [script:'java.lang.String'], doc: 'Shell Script: Runs a shell script.')
    method(name: 'sh', type: 'Object',
           namedParams: [parameter(name: 'script', type: 'java.lang.String'),
                         parameter(name: 'returnStdout', type: 'boolean'),
                         parameter(name: 'label', type: 'java.lang.String')],
           doc: 'Shell Script (named-params form)')
    method(name: 'cumuliDeploy', type: 'Object',
           namedParams: [parameter(name: 'environment', type: 'java.lang.String'),
                         parameter(name: 'version', type: 'java.lang.String')],
           doc: 'Deploys the artifact to a Cumuli env.\nSupports versions like v1.2.3.')
    property(name: 'currentBuild', type: 'org.jenkinsci.plugins.workflow.support.steps.build.RunWrapper')
    property(name: 'env', type: 'EnvActionImpl')
}
`

func TestParseGDSL_Methods(t *testing.T) {
	steps, _ := ParseGDSL(sampleGDSL)

	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %+v", len(steps), steps)
	}

	// Positional params on the first sh.
	if steps[0].Name != "sh" || len(steps[0].Params) != 1 || steps[0].Params[0].Name != "script" {
		t.Errorf("first sh params wrong: %+v", steps[0])
	}
	if !strings.Contains(steps[0].Doc, "Shell Script") {
		t.Errorf("first sh doc wrong: %q", steps[0].Doc)
	}

	// Named params on the second sh — prefer them over positional.
	if steps[1].Name != "sh" || len(steps[1].Params) != 3 {
		t.Errorf("second sh params wrong: %+v", steps[1])
	}
	if !steps[1].Params[0].Named {
		t.Errorf("expected Named=true on namedParams entries")
	}

	// Library var with multi-line doc.
	if steps[2].Name != "cumuliDeploy" {
		t.Errorf("cumuliDeploy step missing")
	}
	if !strings.Contains(steps[2].Doc, "v1.2.3") {
		t.Errorf("multi-line doc lost: %q", steps[2].Doc)
	}
}

func TestParseGDSL_Properties(t *testing.T) {
	_, globals := ParseGDSL(sampleGDSL)
	want := map[string]bool{"currentBuild": false, "env": false}
	for _, g := range globals {
		if _, ok := want[g.Name]; ok {
			want[g.Name] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("property %q not parsed", n)
		}
	}
}

func TestStepSignature(t *testing.T) {
	s := Step{
		Name: "sh",
		Params: []Param{
			{Name: "script", Type: "java.lang.String", Named: true},
			{Name: "returnStdout", Type: "boolean", Named: true},
		},
	}
	got := s.Signature()
	if got != "sh(script: String, returnStdout: boolean)" {
		t.Errorf("unexpected signature: %q", got)
	}
	if (Step{Name: "checkout"}).Signature() != "checkout()" {
		t.Errorf("zero-param signature wrong")
	}
}

func TestUnescapeGroovyString(t *testing.T) {
	if got := unescapeGroovyString(`a\nb\\c\'d`); got != "a\nb\\c'd" {
		t.Errorf("unescape wrong: %q", got)
	}
}

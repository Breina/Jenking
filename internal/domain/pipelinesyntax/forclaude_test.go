package pipelinesyntax

import (
	"testing"
)

// jenkinsRealGDSLSnippet mirrors a snippet of an actual Jenkins controller's
// gdsl output: top-level steps, properties, and a node-scoped contributor
// block whose method declarations sit at column 0 (Jenkins's gdsl is poorly
// formatted — children of the contributor are NOT indented).
const jenkinsRealGDSLSnippet = `
//The global script scope
def ctx = context(scope: scriptScope())
contributor(ctx) {
method(name: 'parallel', type: 'Object', params: ['closures':'java.util.Map'], doc: 'Execute in parallel')
method(name: 'echo', type: 'Object', params: [message:'java.lang.String'], doc: 'Print Message')
property(name: 'env', type: 'EnvActionImpl')
property(name: 'params', type: 'ParamsVariable')
}
//Steps that require a node context
def nodeCtx = context(scope: closureScope())
contributor(nodeCtx) {
    def call = enclosingCall('node')
    if (call) {
method(name: 'bat', type: 'Object', params: [script:'java.lang.String'], doc: 'Windows Batch Script')
method(name: 'git', type: 'Object', params: [url:'java.lang.String'], doc: 'Git')
method(name: 'junit', type: 'Object', params: [testResults:'java.lang.String'], doc: 'Archive JUnit-formatted test results')
method(name: 'sh', type: 'Object', params: [script:'java.lang.String'], doc: 'Shell Script')
method(name: 'stash', type: 'Object', params: [name:'java.lang.String'], doc: 'Stash')
method(name: 'archive', type: 'Object', params: [includes:'java.lang.String'], doc: 'Advanced/Deprecated Archive artifacts')
    }
}
`

// TestParseGDSL_NodeScopedMethods proves our parser finds method declarations
// even when they sit inside a contributor(nodeCtx) + if(call) block — that's
// where Jenkins puts node-context steps like sh, bat, junit, archive.
func TestParseGDSL_NodeScopedMethods(t *testing.T) {
	steps, globals := ParseGDSL(jenkinsRealGDSLSnippet)

	stepNames := map[string]bool{}
	for _, s := range steps {
		stepNames[s.Name] = true
	}
	t.Logf("parsed %d steps (%v), %d globals", len(steps), stepNames, len(globals))

	for _, want := range []string{"sh", "bat", "junit", "archive", "stash", "git", "parallel", "echo"} {
		if !stepNames[want] {
			t.Errorf("expected step %q in parsed result; got %v", want, stepNames)
		}
	}
}

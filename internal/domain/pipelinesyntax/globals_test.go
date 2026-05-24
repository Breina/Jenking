package pipelinesyntax

import (
	"strings"
	"testing"
)

const sampleGlobalsHTML = `
<!DOCTYPE html>
<html>
<body>
<dl>
  <dt id="env">
    <a name="env"></a>
    <code>env</code>
  </dt>
  <dd>
    <p>Exposes environment variables for use in steps, e.g. <code>env.PATH</code>.</p>
  </dd>

  <dt id="currentBuild">
    <a name="currentBuild"></a>
    <code>currentBuild</code>
  </dt>
  <dd>
    Refers to the currently running build.
  </dd>

  <dt id="cumuliDeploy">
    <a name="cumuliDeploy"></a>
    <code>cumuliDeploy</code>
  </dt>
  <dd>
    <p>Deploys the artifact built in this pipeline.</p>
    <p><b>Parameters:</b></p>
    <ul>
      <li><code>environment</code> &mdash; target env</li>
      <li><code>version</code> &mdash; semver tag</li>
    </ul>
  </dd>
</dl>
</body>
</html>
`

func TestParseGlobals(t *testing.T) {
	gs := ParseGlobals(sampleGlobalsHTML)
	if len(gs) != 3 {
		t.Fatalf("expected 3 globals, got %d: %+v", len(gs), gs)
	}

	byName := map[string]GlobalVar{}
	for _, g := range gs {
		byName[g.Name] = g
	}

	if g, ok := byName["env"]; !ok || !strings.Contains(g.Doc, "Exposes environment variables") {
		t.Errorf("env doc wrong: %+v", g)
	}
	if g := byName["cumuliDeploy"]; !strings.Contains(g.Doc, "Deploys the artifact") ||
		!strings.Contains(g.Doc, "semver tag") {
		t.Errorf("cumuliDeploy doc wrong: %q", g.Doc)
	}

	// HTML entities should be unescaped.
	if g := byName["cumuliDeploy"]; strings.Contains(g.Doc, "&mdash;") {
		t.Errorf("HTML entity not unescaped: %q", g.Doc)
	}
}

func TestStripHTML(t *testing.T) {
	in := `<p>hello <b>world</b>&amp;friend</p>`
	if got := stripHTML(in); got != "hello world&friend" {
		t.Errorf("stripHTML wrong: %q", got)
	}
}

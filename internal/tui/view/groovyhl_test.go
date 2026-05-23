package view

import (
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/jenkins/pipelinesyntax"
	"github.com/Breina/Jenking/internal/tui/theme"
)

func TestNewSyntaxOverlay_Nil(t *testing.T) {
	if newSyntaxOverlay(nil) != nil {
		t.Errorf("nil symbols should yield nil overlay")
	}
	empty := &pipelinesyntax.Symbols{}
	if newSyntaxOverlay(empty) != nil {
		t.Errorf("empty symbols should yield nil overlay")
	}
}

func TestSyntaxOverlay_MatchesSymbols(t *testing.T) {
	sym := &pipelinesyntax.Symbols{
		DSLKeywords: []string{"pipeline", "stage"},
		Steps:       []pipelinesyntax.Step{{Name: "sh"}, {Name: "cumuliDeploy"}},
		Globals:     []pipelinesyntax.GlobalVar{{Name: "env"}},
	}
	o := newSyntaxOverlay(sym)
	if o == nil || o.dslRe == nil || o.symbolRe == nil {
		t.Fatalf("expected non-nil overlay regexes")
	}
	if !o.dslRe.MatchString("stage('build')") {
		t.Errorf("dsl regex should match 'stage'")
	}
	if !o.symbolRe.MatchString("cumuliDeploy(environment: 'prod')") {
		t.Errorf("symbol regex should match library var")
	}
	if o.symbolRe.MatchString("noSuchSymbol()") {
		t.Errorf("symbol regex should not match unknown identifier")
	}
}

func TestGroovySpans_OverlayHighlightsLibraryVar(t *testing.T) {
	th := theme.Default()
	overlay := newSyntaxOverlay(&pipelinesyntax.Symbols{
		Steps: []pipelinesyntax.Step{{Name: "cumuliDeploy"}},
	})
	spans := groovySpans("cumuliDeploy(env: 'prod')", th, overlay, nil)

	var hit bool
	for _, s := range spans {
		if s.start == 0 && s.end == len("cumuliDeploy") {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected a span covering the library symbol, got %+v", spans)
	}
}

func TestGroovySpans_DefaultDSLWhenNoOverlay(t *testing.T) {
	th := theme.Default()
	spans := groovySpans("pipeline { agent any }", th, nil, nil)
	if len(spans) == 0 {
		t.Fatal("expected DSL spans even without overlay")
	}
	// Spans are sorted; the first one should cover 'pipeline'.
	first := spans[0]
	if first.start != 0 || !strings.HasPrefix("pipeline", "pipeline") {
		t.Errorf("first span should start at 0, got %+v", first)
	}
}

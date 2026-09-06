package mcp

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

// status404 is a transport error carrying a 404 so jmodel.IsNotFound trips.
type status404 struct{}

func (status404) Error() string       { return "404 not found" }
func (status404) HTTPStatusCode() int { return 404 }

// pipeFakeClient serves canned pipeline symbols and lint results, counting
// FetchPipelineSyntax calls so capability caching can be asserted.
type pipeFakeClient struct {
	jmodel.JenkinsClient
	mu      sync.Mutex
	fetches int
	sym     *pipelinesyntax.Symbols
	symErr  error
	lint    jmodel.ValidationResult
	lintErr error
}

// ListJobs answers the existence probe in usecase.CanonicalJobPath.
func (*pipeFakeClient) ListJobs(context.Context, string) ([]jmodel.Job, error) {
	return nil, nil
}

func (f *pipeFakeClient) FetchPipelineSyntax(context.Context, string, int) (*pipelinesyntax.Symbols, error) {
	f.mu.Lock()
	f.fetches++
	f.mu.Unlock()
	return f.sym, f.symErr
}
func (f *pipeFakeClient) GetJobParameters(context.Context, string) ([]jmodel.ParameterDefinition, error) {
	return nil, nil
}
func (f *pipeFakeClient) ValidateJenkinsfile(context.Context, string) (jmodel.ValidationResult, error) {
	return f.lint, f.lintErr
}

func sampleSymbols() *pipelinesyntax.Symbols {
	return &pipelinesyntax.Symbols{
		Steps: []pipelinesyntax.Step{
			{Name: "sh", ReturnType: "String", Doc: "Run a shell", Params: []pipelinesyntax.Param{{Name: "script", Type: "java.lang.String"}}},
			{Name: "git", Doc: "Clone a repo"},
		},
		Globals: []pipelinesyntax.GlobalVar{
			{Name: "env", Doc: "Environment", Members: []pipelinesyntax.Member{{Name: "BUILD_ID"}}},
		},
	}
}

func TestGetPipelineSymbols_ListDetailFilter(t *testing.T) {
	fc := &pipeFakeClient{sym: sampleSymbols()}
	cs, done := connectTest(t, usecase.Deps{Client: fc})
	defer done()
	ctx := context.Background()

	// List mode: names + signatures, no docs, plus keywords.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_symbols",
		Arguments: map[string]any{"job_path": "team/app", "build_number": 7},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	steps := sc["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(steps))
	}
	first := steps[0].(map[string]any)
	if first["name"] != "sh" || first["signature"] == "" {
		t.Errorf("list step missing name/signature: %v", first)
	}
	if _, hasDoc := first["doc"]; hasDoc {
		t.Errorf("list mode should omit doc: %v", first)
	}
	if len(sc["keywords"].([]any)) == 0 {
		t.Error("expected DSL keywords in list mode")
	}

	// Detail mode: name selects one symbol with full doc/params.
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_symbols",
		Arguments: map[string]any{"job_path": "team/app", "build_number": 7, "name": "sh"},
	})
	sc = res.StructuredContent.(map[string]any)
	steps = sc["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("detail want 1 step, got %d", len(steps))
	}
	if steps[0].(map[string]any)["doc"] != "Run a shell" {
		t.Errorf("detail step missing doc: %v", steps[0])
	}

	// kind=step + query filters out globals and non-matching steps.
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_symbols",
		Arguments: map[string]any{"job_path": "team/app", "build_number": 7, "kind": "step", "query": "gi"},
	})
	sc = res.StructuredContent.(map[string]any)
	if len(sc["steps"].([]any)) != 1 {
		t.Errorf("query gi want 1 step, got %v", sc["steps"])
	}
	if _, ok := sc["globals"]; ok {
		t.Errorf("kind=step should omit globals, got %v", sc["globals"])
	}
	if _, ok := sc["keywords"]; ok {
		t.Errorf("kind=step should omit keywords, got %v", sc["keywords"])
	}
}

func TestLintPipeline(t *testing.T) {
	fc := &pipeFakeClient{lint: jmodel.ValidationResult{
		OK:     false,
		Issues: []jmodel.ValidationIssue{{Line: 3, Col: 5, Message: "unexpected token"}},
	}}
	cs, done := connectTest(t, usecase.Deps{Client: fc})
	defer done()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "lint_pipeline",
		Arguments: map[string]any{"script": "pipeline { "},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("error result: %+v", res.Content)
	}
	sc := res.StructuredContent.(map[string]any)
	if sc["ok"] != false {
		t.Errorf("ok = %v, want false", sc["ok"])
	}
	issues := sc["issues"].([]any)
	if len(issues) != 1 || issues[0].(map[string]any)["message"] != "unexpected token" {
		t.Errorf("issues = %v", issues)
	}
}

func TestGetPipelineSymbols_CapabilityProbe(t *testing.T) {
	// A freestyle job: pipeline-syntax 404s. First call probes, records the
	// capability missing, and returns an actionable error. The second call must
	// short-circuit without another fetch.
	fc := &pipeFakeClient{sym: &pipelinesyntax.Symbols{}, symErr: status404{}}
	cs, done := connectTest(t, usecase.Deps{Client: fc})
	defer done()
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_symbols",
		Arguments: map[string]any{"job_path": "team/freestyle", "build_number": 7},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for missing pipeline-syntax")
	}
	if txt := errorText(res); !strings.Contains(txt, "Pipeline: Groovy") {
		t.Errorf("error text not actionable: %q", txt)
	}

	res2, _ := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_pipeline_symbols",
		Arguments: map[string]any{"job_path": "team/freestyle", "build_number": 7},
	})
	if !res2.IsError {
		t.Error("cached missing capability should still error")
	}
	fc.mu.Lock()
	n := fc.fetches
	fc.mu.Unlock()
	if n != 1 {
		t.Errorf("expected exactly 1 fetch (second call cached), got %d", n)
	}
}

// errorText concatenates the text content of an error result.
func errorText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

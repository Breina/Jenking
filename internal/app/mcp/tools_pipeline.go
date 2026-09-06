package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/dto"
	"github.com/Breina/Jenking/internal/app/usecase"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

// Plugin-gated capabilities. The hint is what the model sees when the endpoint
// 404s — actionable, naming the plugin that provides it.
const (
	capSymbols     = "pipeline-symbols"
	capSymbolsHint = "pipeline-syntax is unavailable for this job — it requires a Pipeline job (Pipeline: Groovy plugin). Freestyle jobs have no pipeline symbols."
	capLint        = "pipeline-lint"
	capLintHint    = "the declarative pipeline validator is not available — it requires the Pipeline: Model Definition plugin (pipeline-model-converter) on the controller."
)

// registerPipelineTools registers the Jenkinsfile-authoring tools: fetching a
// build's script, symbol discovery, and declarative linting. Symbols and lint
// are plugin-gated and probe lazily.
func (s *Server) registerPipelineTools() {
	d := s.deps

	addBuildScopedTool(s, &mcp.Tool{
		Name:        "describe_pipeline",
		Description: "Return a build's Jenkinsfile (the replay script Jenkins recorded). Start the authoring loop here, then get_pipeline_symbols, edit, lint_pipeline, and replay. Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, d usecase.Deps, jobPath string, n int) (scriptOut, error) {
		script, err := d.Describe(ctx, jobPath, n)
		if err != nil {
			return scriptOut{}, err
		}
		return scriptOut{BuildNumber: n, Script: script}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_pipeline_symbols",
		Description: "List the pipeline steps, globals, and DSL keywords available to a build's Jenkinsfile, resolved against the exact shared-library versions that ran. " +
			"Returns names only by default (token-disciplined); set name to fetch one symbol's full signature, params, and docs. " +
			"Filter with query (substring) and kind (step|global|keyword). Use this before editing a Jenkinsfile to avoid hallucinating steps.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in symbolsIn) (*mcp.CallToolResult, symbolsOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		if err := s.gateBefore(capSymbols); err != nil {
			return nil, symbolsOut{}, err
		}
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, symbolsOut{}, err
		}
		sym, err := d.GetPipelineSymbols(ctx, in.JobPath, n)
		if err != nil {
			// A 404 means the pipeline-syntax endpoint is absent (freestyle
			// job / missing plugin): cache it and return the actionable hint.
			if capErr := s.gateAfter(capSymbols, capSymbolsHint, err); capErr != nil {
				return nil, symbolsOut{}, capErr
			}
			// A non-404 fetch error with no usable data is a hard failure; if
			// partial server data survived, degrade gracefully and return it.
			if sym == nil || (len(sym.Steps) == 0 && len(sym.Globals) == 0) {
				return nil, symbolsOut{}, err
			}
		}
		out := buildSymbolsOut(sym, in)
		out.BuildNumber = n
		return nil, out, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "lint_pipeline",
		Description: "Validate a Jenkinsfile against the controller's declarative pipeline validator. " +
			"Returns ok plus any syntax/semantic issues with line and column. Run this after editing and before triggering a replay.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in lintIn) (*mcp.CallToolResult, lintOut, error) {
		if err := s.gateBefore(capLint); err != nil {
			return nil, lintOut{}, err
		}
		res, err := d.Lint(ctx, in.Script)
		if err != nil {
			if capErr := s.gateAfter(capLint, capLintHint, err); capErr != nil {
				return nil, lintOut{}, capErr
			}
			return nil, lintOut{}, err
		}
		return nil, lintOut{OK: res.OK, Issues: mapSlice(res.Issues, dto.ToLintIssue)}, nil
	})
}

// buildSymbolsOut projects sym into the wire output, applying the kind/query/
// name filters. name selects detail mode (full signature/params/doc); otherwise
// only names (and step signatures) are emitted to stay token-disciplined.
func buildSymbolsOut(sym *pipelinesyntax.Symbols, in symbolsIn) symbolsOut {
	var out symbolsOut
	if sym == nil {
		return out
	}
	kind := strings.ToLower(in.Kind)
	detail := in.Name != ""
	match := symbolMatcher(in)

	if kind == "" || kind == "step" {
		out.Steps = filterSteps(sym.Steps, match, detail)
	}
	if kind == "" || kind == "global" {
		out.Globals = filterGlobals(sym.Globals, match, detail)
	}
	if kind == "" || kind == "keyword" {
		keywords := sym.DSLKeywords
		if len(keywords) == 0 {
			keywords = pipelinesyntax.DefaultDSLKeywords
		}
		for _, k := range keywords {
			if match(k) {
				out.Keywords = append(out.Keywords, k)
			}
		}
	}
	return out
}

// symbolMatcher builds the name predicate: exact when name is set (detail mode),
// otherwise a case-insensitive substring on query (empty query matches all).
func symbolMatcher(in symbolsIn) func(string) bool {
	if in.Name != "" {
		return func(name string) bool { return strings.EqualFold(name, in.Name) }
	}
	q := strings.ToLower(in.Query)
	return func(name string) bool {
		return q == "" || strings.Contains(strings.ToLower(name), q)
	}
}

func filterSteps(steps []pipelinesyntax.Step, match func(string) bool, detail bool) []dto.PipelineStep {
	var out []dto.PipelineStep
	for _, st := range steps {
		if !match(st.Name) {
			continue
		}
		if detail {
			out = append(out, dto.ToPipelineStepDetail(st))
		} else {
			out = append(out, dto.ToPipelineStepName(st))
		}
	}
	return out
}

func filterGlobals(globals []pipelinesyntax.GlobalVar, match func(string) bool, detail bool) []dto.PipelineGlobal {
	var out []dto.PipelineGlobal
	for _, g := range globals {
		if !match(g.Name) {
			continue
		}
		if detail {
			out = append(out, dto.ToPipelineGlobalDetail(g))
		} else {
			out = append(out, dto.ToPipelineGlobalName(g))
		}
	}
	return out
}

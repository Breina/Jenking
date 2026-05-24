// Package pipelinesyntax parses Jenkins' per-build pipeline-syntax endpoints
// (`<build>/pipeline-syntax/gdsl` and `<build>/pipeline-syntax/globals`) into
// a structured Symbols set the TUI can drive completion, syntax highlighting,
// and signature popups from.
//
// The set is build-scoped on purpose: Jenkins evaluates @Library at build
// time, so the symbols exposed here already reflect the exact shared-library
// version that ran for this build. No git access or @Library parsing on our
// side.
package pipelinesyntax

import "time"

// Param is one parameter of a step's signature.
type Param struct {
	Name  string
	Type  string // fully-qualified Java/Groovy type (e.g. "java.lang.String")
	Named bool   // true for namedParams entries (foo: bar), false for positional
}

// Step is a callable pipeline step (`sh`, `git`, or a library-defined `var`).
type Step struct {
	Name       string
	ReturnType string
	Params     []Param
	Doc        string // plain text, HTML stripped
}

// Signature renders a human-readable signature like "sh(script: String, returnStdout: boolean)".
func (s Step) Signature() string {
	if len(s.Params) == 0 {
		return s.Name + "()"
	}
	out := s.Name + "("
	for i, p := range s.Params {
		if i > 0 {
			out += ", "
		}
		if p.Name != "" {
			out += p.Name + ": "
		}
		out += shortType(p.Type)
	}
	out += ")"
	return out
}

// GlobalVar is a globally accessible variable (`env`, `currentBuild`, a
// library-provided global).
type GlobalVar struct {
	Name    string
	Doc     string   // plain text, HTML stripped
	Members []Member // methods/fields scraped from the doc (e.g. maven.goal)
}

// Member is one callable or property on a GlobalVar. Sourced from user-
// provided GDSL files (or, for `params`, from the job's parameter
// definitions). Signature/doc may be empty when the source is terse.
type Member struct {
	Name      string
	Signature string  // e.g. "setVersion(version: String)"
	Doc       string  // free-form description
	Params    []Param // declared parameter names — drives in-call completion
}

// Symbols is the merged symbol set for a single build.
type Symbols struct {
	Steps       []Step
	Globals     []GlobalVar
	DSLKeywords []string // declarative Pipeline DSL keywords (`pipeline`, `stages`, …)
	FetchedAt   time.Time
}

// DefaultDSLKeywords are the declarative Pipeline DSL block names. They are
// part of the Pipeline grammar itself rather than steps, so they don't show
// up in gdsl/globals — we hard-code them. Safe and stable across versions.
var DefaultDSLKeywords = []string{
	"pipeline", "agent", "stages", "stage", "steps", "post",
	"when", "environment", "options", "parameters", "triggers",
	"tools", "input", "parallel", "matrix", "script", "library",
	"always", "success", "failure", "unstable", "aborted", "cleanup",
	"changed", "fixed", "regression", "unsuccessful", "any", "none",
	"label", "docker", "dockerfile", "kubernetes", "node",
}

// shortType strips package prefixes ("java.lang.String" → "String").
func shortType(t string) string {
	if t == "" {
		return ""
	}
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] == '.' {
			return t[i+1:]
		}
	}
	return t
}

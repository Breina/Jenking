package pipelinesyntax

import (
	"regexp"
	"strings"
)

// GDSL format example (one entry per logical line, may be wrapped):
//
//   method(name: 'sh', type: 'Object',
//          params: [script:'java.lang.String'],
//          doc: 'Shell Script: ...')
//   method(name: 'sh', type: 'Object',
//          namedParams: [parameter(name: 'script', type: 'java.lang.String'),
//                        parameter(name: 'returnStdout', type: 'boolean')],
//          doc: '...')
//   property(name: 'currentBuild', type: 'org.jenkinsci...RunWrapper')
//
// We tolerate the variations: positional `params` map, `namedParams` parameter
// list, missing doc, and string escapes inside doc.

var (
	// Matches both `method(name: ...)` (Jenkins's own gdsl) and
	// `method name: ...` (Groovy named-arg syntax used by hand-authored files).
	gdslEntryRe = regexp.MustCompile(`(?m)^\s*(method|property)\b`)

	// Field regexes accept either single- or triple-single-quoted strings —
	// hand-authored GDSL often uses '''...''' for multi-line HTML docs.
	gdslFieldNameRe  = regexp.MustCompile(`(?s)\bname\s*:\s*(?:'''(.*?)'''|'((?:\\.|[^'\\])*)')`)
	gdslFieldTypeRe  = regexp.MustCompile(`(?s)\btype\s*:\s*(?:'''(.*?)'''|'((?:\\.|[^'\\])*)')`)
	gdslFieldDocRe   = regexp.MustCompile(`(?s)\bdoc\s*:\s*(?:'''(.*?)'''|'((?:\\.|[^'\\])*)')`)
	gdslParamPairRe  = regexp.MustCompile(`(\w+)\s*:\s*'((?:\\.|[^'\\])*)'`)
	gdslNamedParamRe = regexp.MustCompile(`(?s)parameter\s*\(\s*name\s*:\s*'((?:\\.|[^'\\])*)'\s*,\s*type\s*:\s*'((?:\\.|[^'\\])*)'\s*\)`)
)

// ParseGDSL extracts steps and properties from a raw gdsl document.
// Properties are surfaced as Globals (no signature).
func ParseGDSL(src string) (steps []Step, globals []GlobalVar) {
	for _, entry := range splitGDSLEntries(src) {
		kind, body := entry.kind, entry.body
		name := firstSubmatch(gdslFieldNameRe, body)
		if name == "" {
			continue
		}
		typ := firstSubmatch(gdslFieldTypeRe, body)
		doc := unescapeGroovyString(firstSubmatch(gdslFieldDocRe, body))

		switch kind {
		case "property":
			globals = append(globals, GlobalVar{Name: name, Doc: stripHTML(doc)})
		case "method":
			steps = append(steps, Step{
				Name:       name,
				ReturnType: typ,
				Params:     parseGDSLParams(body),
				Doc:        stripHTML(doc),
			})
		}
	}
	return steps, globals
}

type gdslEntry struct {
	kind string // "method" or "property"
	body string // text between the outer ( ... )
}

// splitGDSLEntries scans src for `method`/`property` declarations and returns
// the body of each. Supports both call forms:
//
//   - Parenthesised:   method(name: 'X', type: 'Y', doc: '...')
//   - Parenless (Groovy named-arg): method name: 'X', type: 'Y', doc: ”'...”'
//
// Respects string state (single + triple-single quotes) when scanning, so
// brackets/braces/parens inside doc text don't confuse the boundary search.
func splitGDSLEntries(src string) []gdslEntry {
	var out []gdslEntry
	matches := gdslEntryRe.FindAllStringSubmatchIndex(src, -1)
	for i, m := range matches {
		kind := src[m[2]:m[3]]
		afterKW := m[3]
		// Skip whitespace after the keyword.
		for afterKW < len(src) && (src[afterKW] == ' ' || src[afterKW] == '\t') {
			afterKW++
		}
		if afterKW >= len(src) {
			continue
		}

		if src[afterKW] == '(' {
			// Paren form: body is between matching ( ).
			closeIdx := matchParen(src, afterKW)
			if closeIdx < 0 {
				continue
			}
			out = append(out, gdslEntry{kind: kind, body: src[afterKW+1 : closeIdx]})
			continue
		}

		// Parenless form: body extends to the next entry boundary (next
		// method/property/contributor keyword OR the `}` closing the
		// containing contributor block), respecting string state.
		hardEnd := len(src)
		if i+1 < len(matches) {
			hardEnd = matches[i+1][0]
		}
		end := scanToBlockBoundary(src, afterKW, hardEnd)
		out = append(out, gdslEntry{kind: kind, body: src[afterKW:end]})
	}
	return out
}

// scanToBlockBoundary walks src[start:end] respecting string state and
// nested brackets, and returns the position of the first `}` it encounters
// at brace-depth 0. That `}` is the closing brace of the enclosing contributor
// block — every parenless entry ends there at the latest, even if the next
// `method`/`property` keyword sits further down. Returns end if no such
// boundary appears.
func scanToBlockBoundary(src string, start, end int) int {
	ctx := scanCtx{state: stateDefault}
	i := start
	for i < end {
		switch ctx.state {
		case stateTriple:
			i = stepTriple(src, i, &ctx)
		case stateSingle:
			i = stepSingle(src, i, &ctx)
		default: // stateDefault
			next, boundary := stepDefault(src, i, &ctx)
			if boundary >= 0 {
				return boundary
			}
			i = next
		}
	}
	return end
}

// matchParen returns the index of the `)` that closes the `(` at openIdx,
// respecting single-quoted Groovy string literals. Returns -1 on no match.
func matchParen(s string, openIdx int) int {
	depth := 0
	inStr := false
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '\'' {
				inStr = false
			}
			continue
		}
		switch c {
		case '\'':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseGDSLParams extracts a step's parameter list from its body. Prefers
// namedParams (richer info) when present, falls back to the positional
// `params: [name:'type', ...]` map.
func parseGDSLParams(body string) []Param {
	// Try namedParams first: parameter(name: '...', type: '...') entries.
	var named []Param
	for _, m := range gdslNamedParamRe.FindAllStringSubmatch(body, -1) {
		named = append(named, Param{Name: m[1], Type: m[2], Named: true})
	}
	if len(named) > 0 {
		return named
	}

	// Fallback: positional `params: [name:'type', ...]`.
	if idx := strings.Index(body, "params"); idx >= 0 {
		// Find the `[` that follows `params:` and walk to matching `]`.
		open := strings.IndexByte(body[idx:], '[')
		if open < 0 {
			return nil
		}
		open += idx
		close := matchBracket(body, open)
		if close < 0 {
			return nil
		}
		inner := body[open+1 : close]
		var ps []Param
		for _, m := range gdslParamPairRe.FindAllStringSubmatch(inner, -1) {
			ps = append(ps, Param{Name: m[1], Type: m[2]})
		}
		return ps
	}
	return nil
}

// matchBracket is matchParen for `[` / `]`.
func matchBracket(s string, openIdx int) int {
	depth := 0
	inStr := false
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '\'' {
				inStr = false
			}
			continue
		}
		switch c {
		case '\'':
			inStr = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// firstSubmatch returns the first non-empty capture group from re's match
// against s. Field regexes now have two alternation groups (triple-quoted
// then single-quoted) and exactly one will be populated per match.
func firstSubmatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}

// unescapeGroovyString undoes the backslash escapes Jenkins applies inside
// single-quoted GDSL strings. Conservative: only handles the escapes Jenkins
// actually emits.
func unescapeGroovyString(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\', '\'', '"':
				b.WriteByte(s[i+1])
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

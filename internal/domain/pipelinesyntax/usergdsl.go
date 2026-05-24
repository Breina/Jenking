package pipelinesyntax

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ParseUserGDSL parses a single user-authored GDSL file and returns the
// receiver-to-members map it declares.
//
// Recognised structure (standard IntelliJ GDSL syntax):
//
//	// Variant 1 — ctype is the user-facing receiver name:
//	contributor(context(ctype: 'maven')) {
//	    method(name: 'setVersion', type: 'Object',
//	           params: [version: 'java.lang.String'],
//	           doc: 'Set Maven project version')
//	}
//
//	// Variant 2 — ctype is a fully-qualified class name; the user-facing
//	// receiver name is paired up in a separate top-level contributor block:
//	contributor(jenkinsContext) {
//	    property name: 'maven', type: 'be.cumulus.jenkins.dsl.MavenDsl'
//	}
//	contributor(context(ctype: 'be.cumulus.jenkins.dsl.MavenDsl')) {
//	    method name: 'setVersion', type: 'Object',
//	           params: [version: 'java.lang.String'],
//	           doc: '''Set Maven project version'''
//	}
//
// Both invocation styles (paren + parenless) and both doc-string forms
// (single + triple-quoted) are accepted.
//
// Any `method`/`property` declarations outside a contributor block are
// ignored — we deliberately don't let user files inject top-level steps.
func ParseUserGDSL(src string) map[string][]Member {
	blocks := splitContributorBlocks(src)

	// Pass 1: build a type→user-name map from property declarations in
	// non-ctype contributor blocks (e.g. `contributor(jenkinsContext)`).
	// These pair an exported user-facing global with its implementation
	// class, which downstream ctype-scoped blocks reference.
	typeToName := map[string]string{}
	for _, b := range blocks {
		if b.ctype != "" {
			continue
		}
		for _, e := range splitGDSLEntries(b.body) {
			if e.kind != "property" {
				continue
			}
			name := firstSubmatch(gdslFieldNameRe, e.body)
			typ := firstSubmatch(gdslFieldTypeRe, e.body)
			if name != "" && typ != "" {
				typeToName[typ] = name
			}
		}
	}

	// Pass 2: collect member declarations from ctype-scoped blocks.
	out := map[string][]Member{}
	for _, b := range blocks {
		if b.ctype == "" {
			continue
		}
		receiver := typeToName[b.ctype]
		if receiver == "" {
			// No mapping found — fall back to using ctype verbatim.
			// Works for the simple-name form (`ctype: 'maven'`).
			receiver = b.ctype
		}
		members := parseContributorBody(b.body)
		if len(members) > 0 {
			out[receiver] = append(out[receiver], members...)
		}
	}
	return out
}

// LoadUserGDSLDir walks dir (non-recursive), parses every *.gdsl file inside,
// and merges them into a single receiver-to-members map. Missing dir → nil
// result + nil error, so a fresh install just produces no extras.
func LoadUserGDSLDir(dir string) (map[string][]Member, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errIsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read user gdsl dir %s: %w", dir, err)
	}
	merged := map[string][]Member{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gdsl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return merged, fmt.Errorf("read %s: %w", path, err)
		}
		for recv, ms := range ParseUserGDSL(string(data)) {
			merged[recv] = append(merged[recv], ms...)
		}
	}
	return merged, nil
}

// errIsNotExist mirrors os.IsNotExist while also covering fs.ErrNotExist
// wrapped by other layers — keeps the public API clean of stdlib internals.
func errIsNotExist(err error) bool {
	return os.IsNotExist(err) || err == fs.ErrNotExist
}

// contributorBlock holds one `contributor(<scope>) { … }` block. ctype is
// non-empty when the scope was `context(ctype: 'X')`; for variable-scoped
// blocks like `contributor(jenkinsContext) { … }` ctype is "" and the body
// is scanned for property declarations that map user-facing names to types.
type contributorBlock struct {
	ctype string
	body  string
}

var (
	contributorHeadRe = regexp.MustCompile(`\bcontributor\s*\(`)
	scopeCtypeRe      = regexp.MustCompile(`(?s)\bctype\s*:\s*'((?:\\.|[^'\\])*)'`)
)

// splitContributorBlocks finds every `contributor(...) { ... }` in src,
// regardless of the scope expression. Uses brace + paren matching that
// respects single-quoted and triple-quoted Groovy strings, so `}` in doc
// text doesn't end the block prematurely.
func splitContributorBlocks(src string) []contributorBlock {
	var out []contributorBlock
	for _, m := range contributorHeadRe.FindAllStringIndex(src, -1) {
		// m[0]:m[1] = "contributor(" (including the open paren).
		openParen := m[1] - 1
		closeParen := matchParen(src, openParen)
		if closeParen < 0 {
			continue
		}
		scope := src[openParen+1 : closeParen]
		// Skip whitespace + `{`.
		braceIdx := closeParen + 1
		for braceIdx < len(src) && (src[braceIdx] == ' ' || src[braceIdx] == '\t' || src[braceIdx] == '\n' || src[braceIdx] == '\r') {
			braceIdx++
		}
		if braceIdx >= len(src) || src[braceIdx] != '{' {
			continue
		}
		closeBrace := matchBraceQuoteAware(src, braceIdx)
		if closeBrace < 0 {
			continue
		}
		ctype := ""
		if sm := scopeCtypeRe.FindStringSubmatch(scope); sm != nil {
			ctype = sm[1]
		}
		out = append(out, contributorBlock{
			ctype: ctype,
			body:  src[braceIdx+1 : closeBrace],
		})
	}
	return out
}

// matchBraceQuoteAware is matchBrace with awareness of triple-quoted Groovy
// strings (”'…”') as well as ordinary single quotes.
func matchBraceQuoteAware(s string, openIdx int) int {
	depth := 0
	inSingle, inTriple := false, false
	for i := openIdx; i < len(s); i++ {
		if inTriple {
			if i+2 < len(s) && s[i] == '\'' && s[i+1] == '\'' && s[i+2] == '\'' {
				inTriple = false
				i += 2
			}
			continue
		}
		if inSingle {
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		if i+2 < len(s) && s[i] == '\'' && s[i+1] == '\'' && s[i+2] == '\'' {
			inTriple = true
			i += 2
			continue
		}
		switch s[i] {
		case '\'':
			inSingle = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseContributorBody walks one contributor body and converts each
// method(...)/property(...) entry into a Member. Reuses splitGDSLEntries and
// the same field extractors as the top-level GDSL parser, so users get the
// full power of GDSL — named params, types, doc strings.
func parseContributorBody(body string) []Member {
	var out []Member
	for _, e := range splitGDSLEntries(body) {
		name := firstSubmatch(gdslFieldNameRe, e.body)
		if name == "" {
			continue
		}
		doc := unescapeGroovyString(firstSubmatch(gdslFieldDocRe, e.body))
		switch e.kind {
		case "method":
			step := Step{
				Name:       name,
				ReturnType: firstSubmatch(gdslFieldTypeRe, e.body),
				Params:     parseGDSLParams(e.body),
				Doc:        stripHTML(doc),
			}
			out = append(out, Member{
				Name:      name,
				Signature: step.Signature(),
				Doc:       step.Doc,
				Params:    step.Params,
			})
		case "property":
			out = append(out, Member{Name: name, Doc: stripHTML(doc)})
		}
	}
	return out
}

package pipelinesyntax

import (
	"html"
	"regexp"
	"strings"
)

// Globals page format (workflow-cps-plugin). The interesting structure is a
// sequence of <dt>/<dd> pairs inside one or more <dl> blocks:
//
//   <dl>
//     <dt id="env">
//       <a name="env"></a>
//       <code>env</code>
//     </dt>
//     <dd>
//       <p>Exposes environment variables ...</p>
//     </dd>
//     ...
//   </dl>
//
// Library-provided globals appear under their own section using the same
// <dt>/<dd> pattern, so we can parse the whole page uniformly without caring
// about which section a name came from.

var (
	dtDdRe = regexp.MustCompile(`(?is)<dt\b([^>]*)>(.*?)</dt>\s*<dd\b[^>]*>(.*?)</dd>`)
	dtIDRe = regexp.MustCompile(`(?i)\bid\s*=\s*["']([^"']+)["']`)
	// Fallback: <h1 id="foo"> ... <div>doc</div> — older Jenkins layouts.
	hSectionRe = regexp.MustCompile(`(?is)<h[12]\b[^>]*\bid\s*=\s*["']([^"']+)["'][^>]*>.*?</h[12]>\s*(.*?)(?:<h[12]\b|$)`)
	tagRe      = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRunRe    = regexp.MustCompile(`[ \t]*\n[ \t\n]*`)
)

// ParseGlobals extracts global-variable docs from the pipeline-syntax/globals
// HTML page. Tolerates both the dt/dd and h1/section layouts.
func ParseGlobals(htmlSrc string) []GlobalVar {
	seen := make(map[string]bool)
	var out []GlobalVar

	for _, m := range dtDdRe.FindAllStringSubmatch(htmlSrc, -1) {
		attrs, dtInner, ddInner := m[1], m[2], m[3]
		name := globalName(attrs, dtInner)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, GlobalVar{Name: name, Doc: stripHTML(ddInner)})
	}

	if len(out) == 0 {
		for _, m := range hSectionRe.FindAllStringSubmatch(htmlSrc, -1) {
			name := strings.TrimSpace(m[1])
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, GlobalVar{Name: name, Doc: stripHTML(m[2])})
		}
	}

	return out
}

// globalName resolves the variable's symbol name. Prefer the dt's id attr;
// fall back to the inner text (stripped of tags, take the first word) — that
// covers older layouts that only used <code>name</code>.
func globalName(attrs, inner string) string {
	if m := dtIDRe.FindStringSubmatch(attrs); m != nil {
		return strings.TrimSpace(m[1])
	}
	text := strings.TrimSpace(stripHTML(inner))
	if text == "" {
		return ""
	}
	if i := strings.IndexAny(text, " \t\n("); i > 0 {
		return text[:i]
	}
	return text
}

// stripHTML strips HTML tags, unescapes entities, and collapses whitespace.
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = wsRunRe.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

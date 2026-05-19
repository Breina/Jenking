package view

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// compileSearchRegex compiles a search pattern into a regexp.
// Falls back to literal string matching when the pattern is not a valid regex
// (e.g. bare `\` while the user is still typing an escape sequence).
func compileSearchRegex(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(pattern))
	}
	return re
}

// highlightMatches highlights all regex matches in text using matchStyle,
// rendering non-matching portions with normalStyle.
func highlightMatches(text string, re *regexp.Regexp, matchStyle, normalStyle lipgloss.Style) string {
	if re == nil {
		return normalStyle.Render(text)
	}
	locs := re.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return normalStyle.Render(text)
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		if loc[0] > prev {
			b.WriteString(normalStyle.Render(text[prev:loc[0]]))
		}
		b.WriteString(matchStyle.Render(text[loc[0]:loc[1]]))
		prev = loc[1]
	}
	if prev < len(text) {
		b.WriteString(normalStyle.Render(text[prev:]))
	}
	return b.String()
}

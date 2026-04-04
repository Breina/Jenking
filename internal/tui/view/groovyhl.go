package view

import (
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

var (
	// Comments take highest priority.
	groovyLineCommentRe  = regexp.MustCompile(`//.*$`)
	groovyBlockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

	// String literals (triple-quoted first to avoid overlapping single-quoted).
	groovyTripleStringRe = regexp.MustCompile(`(?s)""".*?"""|'''.*?'''`)
	groovyStringRe       = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)

	// Pipeline DSL + Groovy keywords.
	groovyKeywordRe = regexp.MustCompile(
		`\b(pipeline|agent|any|none|label|docker|stages|stage|steps|script|post|` +
			`always|success|failure|unstable|aborted|changed|cleanup|when|` +
			`environment|options|parameters|triggers|tools|input|` +
			`string|booleanParam|choice|password|text|file|credentials|` +
			`def|void|return|import|class|interface|extends|implements|` +
			`if|else|for|while|do|try|catch|finally|throw|new|` +
			`null|true|false|this|super|static|final|private|public|protected|` +
			`parallel|matrix|axes|axis|excludes|cell)\b`,
	)

	// Numbers.
	groovyNumberRe = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
)

type hlSpan struct {
	start, end int
	style      lipgloss.Style
}

func overlapsAny(spans []hlSpan, start, end int) bool {
	for _, s := range spans {
		if start < s.end && end > s.start {
			return true
		}
	}
	return false
}

// groovySpans collects all non-overlapping highlight spans for a single line.
// Priority: comments > strings > keywords > numbers.
func groovySpans(line string, t theme.Theme) []hlSpan {
	var spans []hlSpan

	addSpans := func(re *regexp.Regexp, style lipgloss.Style) {
		for _, m := range re.FindAllStringIndex(line, -1) {
			if !overlapsAny(spans, m[0], m[1]) {
				spans = append(spans, hlSpan{m[0], m[1], style})
			}
		}
	}

	// For multi-line block comments a single line may be a continuation; skip.
	addSpans(groovyBlockCommentRe, t.Log.Dim)
	addSpans(groovyLineCommentRe, t.Log.Dim)
	addSpans(groovyTripleStringRe, t.BuildStatus.Success)
	addSpans(groovyStringRe, t.BuildStatus.Success)
	addSpans(groovyKeywordRe, t.Log.Warning)
	addSpans(groovyNumberRe, t.BuildStatus.Running)

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	return spans
}

// renderGroovyLine returns a single Groovy source line with ANSI syntax coloring.
func renderGroovyLine(line string, t theme.Theme) string {
	spans := groovySpans(line, t)
	if len(spans) == 0 {
		return t.Log.Normal.Render(line)
	}
	var b strings.Builder
	prev := 0
	for _, s := range spans {
		if s.start > prev {
			b.WriteString(t.Log.Normal.Render(line[prev:s.start]))
		}
		b.WriteString(s.style.Render(line[s.start:s.end]))
		prev = s.end
	}
	if prev < len(line) {
		b.WriteString(t.Log.Normal.Render(line[prev:]))
	}
	return b.String()
}

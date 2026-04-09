package view

import (
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// renderGroovyLogLine renders one display line of a Groovy script with
// horizontal scroll, truncation, and optional search highlighting applied.
// It is used as LogViewer.renderFn for the script pane in DescribeView.
func renderGroovyLogLine(dl displayLine, wrap bool, hOffset, width int, searchRe *regexp.Regexp, t theme.Theme, isCurrent bool) string {
	line := dl.text
	truncated := false
	if !wrap && width > 0 {
		line = skipColumns(line, hOffset)
		if lipgloss.Width(line) > width {
			line, _ = truncateToColumns(line, width-1)
			truncated = true
		}
	}
	suffix := ""
	if truncated {
		suffix = t.Log.Trunc.Render("»")
	}
	if searchRe != nil {
		matchStyle := t.Search.Match
		if isCurrent {
			matchStyle = t.Search.CurrentMatch
		}
		return renderGroovyWithSearch(line, searchRe, matchStyle, t) + suffix
	}
	return renderGroovyLine(line, t) + suffix
}

// renderGroovyWithSearch renders a Groovy line keeping syntax colors for
// non-matched text while overlaying matchStyle on search match spans.
func renderGroovyWithSearch(line string, searchRe *regexp.Regexp, matchStyle lipgloss.Style, t theme.Theme) string {
	// Build base segments: Groovy spans + Normal-styled gaps between them.
	type seg struct {
		start, end int
		style      lipgloss.Style
	}
	spans := groovySpans(line, t)
	base := make([]seg, 0, len(spans)*2+1)
	pos := 0
	for _, sp := range spans {
		if sp.start > pos {
			base = append(base, seg{pos, sp.start, t.Log.Normal})
		}
		base = append(base, seg{sp.start, sp.end, sp.style})
		pos = sp.end
	}
	if pos < len(line) {
		base = append(base, seg{pos, len(line), t.Log.Normal})
	}

	// Overlay search match spans, splitting base segments as needed.
	matches := searchRe.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		// No matches — render with plain Groovy styles.
		var b strings.Builder
		for _, s := range base {
			b.WriteString(s.style.Render(line[s.start:s.end]))
		}
		return b.String()
	}

	var b strings.Builder
	for _, s := range base {
		p := s.start
		for _, m := range matches {
			ms, me := m[0], m[1]
			if ms >= s.end || me <= s.start {
				continue
			}
			cs := max(ms, s.start)
			ce := min(me, s.end)
			if cs > p {
				b.WriteString(s.style.Render(line[p:cs]))
			}
			b.WriteString(matchStyle.Render(line[cs:ce]))
			p = ce
		}
		if p < s.end {
			b.WriteString(s.style.Render(line[p:s.end]))
		}
	}
	return b.String()
}

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

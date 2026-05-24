package view

import (
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// renderGroovyLogLine renders one display line of a Groovy script with
// horizontal scroll, truncation, and optional search highlighting applied.
// It is used as LogViewer.renderFn for the script pane in DescribeView.
//
// overlay (may be nil) augments highlighting with build-specific knowledge of
// Jenkins step and global-variable names sourced from pipelinesyntax.
// commentSpansInLine returns byte ranges within line that fall inside
// /* ... */ block comments, given whether the line starts already inside
// an open block comment carried over from a previous line. Caller is
// responsible for tracking startInComment across lines.
func commentSpansInLine(line string, startInComment bool) []hlSpan {
	var out []hlSpan
	pos := 0
	in := startInComment
	for pos < len(line) {
		if in {
			idx := strings.Index(line[pos:], "*/")
			if idx < 0 {
				out = append(out, hlSpan{pos, len(line), lipgloss.Style{}})
				return out
			}
			end := pos + idx + 2
			out = append(out, hlSpan{pos, end, lipgloss.Style{}})
			pos = end
			in = false
		} else {
			idx := strings.Index(line[pos:], "/*")
			if idx < 0 {
				return out
			}
			pos += idx
			in = true
		}
	}
	return out
}

// ComputeBlockCommentStartFlags returns, for each raw line, whether that
// line begins inside an open /* ... */ block comment carried from prior
// lines. Single-quoted and double-quoted Groovy strings are respected so
// that `'/*'` literals don't open a fake comment.
func ComputeBlockCommentStartFlags(rawLines []string) []bool {
	flags := make([]bool, len(rawLines))
	inComment := false
	for i, line := range rawLines {
		flags[i] = inComment
		inComment = scanCommentState(line, inComment)
	}
	return flags
}

// commentScanState enumerates the explicit lexer states used by
// scanCommentState. The original implementation tracked the same information
// through three booleans (inComment/inSingle/inDouble) toggled inline, making
// transitions implicit. Named states make the FSM obvious — same pattern as
// pipelinesyntax.scanToBlockBoundary in internal/domain/pipelinesyntax.
type commentScanState int

const (
	commentScanDefault commentScanState = iota // outside strings/comments
	commentScanBlock                           // inside /* ... */
	commentScanSingle                          // inside '...'
	commentScanDouble                          // inside "..."
	commentScanDone                            // line-comment encountered; bail
)

// scanCommentState reports whether a line ends inside an open /* ... */ block
// comment, given whether it began inside one. Strings (single & double quoted)
// shield their contents so embedded `/*` doesn't open a fake comment.
func scanCommentState(line string, inComment bool) bool {
	state := commentScanDefault
	if inComment {
		state = commentScanBlock
	}
	for i := 0; i < len(line); {
		switch state {
		case commentScanBlock:
			i, state = stepBlockComment(line, i, state)
		case commentScanSingle:
			i, state = stepQuoted(line, i, '\'', commentScanSingle, state)
		case commentScanDouble:
			i, state = stepQuoted(line, i, '"', commentScanDouble, state)
		default:
			i, state = stepCommentDefault(line, i, state)
		}
		if state == commentScanDone {
			break
		}
	}
	return state == commentScanBlock
}

// stepBlockComment advances inside /* ... */; exits on the closing */.
func stepBlockComment(line string, i int, state commentScanState) (int, commentScanState) {
	if line[i] == '*' && i+1 < len(line) && line[i+1] == '/' {
		return i + 2, commentScanDefault
	}
	return i + 1, state
}

// stepQuoted advances inside a quoted string. Honours backslash escapes and
// closes when the matching quote is found.
func stepQuoted(line string, i int, quote byte, self, state commentScanState) (int, commentScanState) {
	c := line[i]
	if c == '\\' && i+1 < len(line) {
		return i + 2, state
	}
	if c == quote {
		return i + 1, commentScanDefault
	}
	_ = self
	return i + 1, state
}

// stepCommentDefault advances outside strings/comments, switching state on
// quote opens and on /*. A // line-comment short-circuits the scan.
func stepCommentDefault(line string, i int, state commentScanState) (int, commentScanState) {
	switch line[i] {
	case '\'':
		return i + 1, commentScanSingle
	case '"':
		return i + 1, commentScanDouble
	case '/':
		if i+1 < len(line) && line[i+1] == '*' {
			return i + 2, commentScanBlock
		}
		if i+1 < len(line) && line[i+1] == '/' {
			return i + 1, commentScanDone
		}
	}
	_ = state
	return i + 1, state
}

func renderGroovyLogLine(dl widget.DisplayLine, wrap bool, hOffset, width int, searchRe *regexp.Regexp, t theme.Theme, isCurrent bool, overlay *syntaxOverlay, startInComment bool) string {
	line := dl.Text
	truncated := false
	if !wrap && width > 0 {
		line = widget.SkipColumns(line, hOffset)
		if lipgloss.Width(line) > width {
			line, _ = widget.TruncateToColumns(line, width-1)
			truncated = true
		}
	}
	suffix := ""
	if truncated {
		suffix = t.Log.Trunc.Render("»")
	}

	// Error/warning lines bypass Groovy syntax colouring and render as a flat
	// tinted line, matching how the log view paints classified entries. The
	// uniform colour reads as "this is the issue" at a glance; layering Groovy
	// hues on top of an error tint just produces noise. The active n/N
	// selection uses CurrentHighlight so jumps are unambiguous.
	if dl.Kind != widget.LineKindNormal && searchRe == nil {
		style := t.Log.Normal
		switch dl.Kind {
		case widget.LineKindError:
			style = t.Log.Error
		case widget.LineKindWarning:
			style = t.Log.Warning
		}
		if isCurrent {
			style = t.Log.CurrentHighlight
		}
		return style.Render(line) + suffix
	}

	forced := commentSpansInLine(line, startInComment)
	for i := range forced {
		forced[i].style = t.Log.Dim
	}
	if searchRe != nil {
		matchStyle := t.Search.Match
		if isCurrent {
			matchStyle = t.Search.CurrentMatch
		}
		return renderGroovyWithSearch(line, searchRe, matchStyle, t, overlay, forced) + suffix
	}
	return renderGroovyLine(line, t, overlay, forced) + suffix
}

// renderGroovyWithSearch renders a Groovy line keeping syntax colors for
// non-matched text while overlaying matchStyle on search match spans.
func renderGroovyWithSearch(line string, searchRe *regexp.Regexp, matchStyle lipgloss.Style, t theme.Theme, overlay *syntaxOverlay, forced []hlSpan) string {
	type seg struct {
		start, end int
		style      lipgloss.Style
	}
	spans := groovySpans(line, t, overlay, forced)
	base := make([]seg, 0, len(spans)*2+1)
	pos := 0
	for _, sp := range spans {
		if sp.start > pos {
			base = append(base, seg{pos, sp.start, t.Log.Normal})
		}
		base = append(base, seg(sp))
		pos = sp.end
	}
	if pos < len(line) {
		base = append(base, seg{pos, len(line), t.Log.Normal})
	}

	matches := searchRe.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
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

	// Generic Groovy keywords. Pipeline DSL block names are highlighted by the
	// overlay's DSL regex (so they pop in their own colour even when no symbol
	// data is loaded) — see defaultDSLRe below.
	groovyKeywordRe = regexp.MustCompile(
		`\b(def|void|return|import|class|interface|extends|implements|` +
			`if|else|for|while|do|try|catch|finally|throw|new|` +
			`null|true|false|this|super|static|final|private|public|protected|` +
			`string|booleanParam|choice|password|text|file|credentials)\b`,
	)

	// Default DSL keyword regex — used when no Symbols are available so the
	// declarative blocks still pop visually. Sourced from
	// pipelinesyntax.DefaultDSLKeywords; lazily compiled once.
	defaultDSLRe = compileWordSet(pipelinesyntax.DefaultDSLKeywords)

	groovyNumberRe = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)

	// Groovy named-argument syntax: `name: value`, `defaultValue: '...'`,
	// `choices: [...]`. Captures the identifier (group 1) so we only colour
	// the key, not the trailing colon — gives parameter blocks the same
	// "structure" feel as DSL block names.
	groovyNamedArgRe = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)(?:\s*):`)
)

// syntaxOverlay is a per-build precompiled symbol-name matcher. Built once
// when Symbols arrive, then reused for every line render.
type syntaxOverlay struct {
	dslRe    *regexp.Regexp // declarative DSL keywords (pipeline, stages, …)
	symbolRe *regexp.Regexp // top-level Step + Global names
	memberRe *regexp.Regexp // receiver.member patterns: `\b(maven|git)\.(setVersion|tag|…)\b`
}

// newSyntaxOverlay precompiles regexes for the symbol set. Returns nil if
// the symbol set is empty (caller treats nil as "use defaults").
func newSyntaxOverlay(sym *pipelinesyntax.Symbols) *syntaxOverlay {
	if sym == nil {
		return nil
	}
	o := &syntaxOverlay{}
	if len(sym.DSLKeywords) > 0 {
		o.dslRe = compileWordSet(sym.DSLKeywords)
	}
	names := make([]string, 0, len(sym.Steps)+len(sym.Globals))
	for _, s := range sym.Steps {
		names = append(names, s.Name)
	}
	for _, g := range sym.Globals {
		names = append(names, g.Name)
	}
	if len(names) > 0 {
		o.symbolRe = compileWordSet(uniqueStrings(names))
	}
	o.memberRe = buildMemberRegex(sym)
	if o.dslRe == nil && o.symbolRe == nil && o.memberRe == nil {
		return nil
	}
	return o
}

// buildMemberRegex emits a single alternation matching all known
// receiver.member combinations from user-GDSL. Receiver-scoped so we don't
// accidentally light up `something.setVersion` where `something` is unrelated.
func buildMemberRegex(sym *pipelinesyntax.Symbols) *regexp.Regexp {
	if sym == nil {
		return nil
	}
	var receivers []string
	memberSet := map[string]struct{}{}
	for _, g := range sym.Globals {
		if len(g.Members) == 0 {
			continue
		}
		receivers = append(receivers, regexp.QuoteMeta(g.Name))
		for _, m := range g.Members {
			memberSet[regexp.QuoteMeta(m.Name)] = struct{}{}
		}
	}
	if len(receivers) == 0 || len(memberSet) == 0 {
		return nil
	}
	members := make([]string, 0, len(memberSet))
	for m := range memberSet {
		members = append(members, m)
	}
	// Match `<receiver>.<member>` with the member captured for span sizing.
	return regexp.MustCompile(
		`\b(?:` + strings.Join(receivers, "|") + `)\.(?:` + strings.Join(members, "|") + `)\b`,
	)
}

// compileWordSet builds a `\b(a|b|c)\b` alternation. Returns nil for an
// empty input slice so the caller can short-circuit.
func compileWordSet(words []string) *regexp.Regexp {
	if len(words) == 0 {
		return nil
	}
	escaped := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		escaped = append(escaped, regexp.QuoteMeta(w))
	}
	if len(escaped) == 0 {
		return nil
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(escaped, "|") + `)\b`)
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

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
// Priority: comments > strings > DSL keywords > known symbols > Groovy keywords > numbers.
func groovySpans(line string, t theme.Theme, overlay *syntaxOverlay, forced []hlSpan) []hlSpan {
	spans := clampForcedSpans(forced, len(line))

	addSpans := func(re *regexp.Regexp, style lipgloss.Style) {
		if re == nil {
			return
		}
		for _, m := range re.FindAllStringIndex(line, -1) {
			if !overlapsAny(spans, m[0], m[1]) {
				spans = append(spans, hlSpan{m[0], m[1], style})
			}
		}
	}

	addSpans(groovyBlockCommentRe, t.Log.Dim)
	addSpans(groovyLineCommentRe, t.Log.Dim)
	addSpans(groovyTripleStringRe, t.BuildStatus.Success)
	addSpans(groovyStringRe, t.BuildStatus.Success)

	// Named-argument keys come BEFORE step/global highlighting so that
	// `description: '…'` (where `description` would otherwise be matched as
	// a step) renders consistently with `name:`, `defaultValue:`, `choices:`.
	// Use the same hue as steps (running colour) but without bold, so named
	// args feel related to but distinct from step calls.
	spans = addNamedArgSpans(spans, line, t.BuildStatus.Running)

	dslStyle := t.Log.Warning
	if overlay != nil && overlay.dslRe != nil {
		addSpans(overlay.dslRe, dslStyle)
	} else {
		addSpans(defaultDSLRe, dslStyle)
	}

	if overlay != nil && overlay.symbolRe != nil {
		// Bold for callable Jenkins symbols (steps + globals). Bold on top of
		// the build-status Running colour stands out without needing a new
		// theme field.
		addSpans(overlay.symbolRe, t.BuildStatus.Running.Bold(true))
	}
	if overlay != nil && overlay.memberRe != nil {
		// Same colour as top-level symbols — receiver.member is conceptually
		// "a known Jenkins thing", just qualified. Highlights the dotted
		// pair (e.g. `maven.setVersion`) as one unit so users can see at a
		// glance whether the library call is recognised.
		addSpans(overlay.memberRe, t.BuildStatus.Running.Bold(true))
	}

	addSpans(groovyKeywordRe, t.Log.Warning)
	addSpans(groovyNumberRe, t.BuildStatus.Running)

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	return spans
}

// clampForcedSpans normalises caller-provided forced spans (typically
// multi-line block comments resolved from cross-line state) into in-bounds
// non-empty ranges. These get top priority — added before any regex pass
// so subsequent passes skip them via overlapsAny.
func clampForcedSpans(forced []hlSpan, lineLen int) []hlSpan {
	var out []hlSpan
	for _, f := range forced {
		if f.start < 0 {
			f.start = 0
		}
		if f.end > lineLen {
			f.end = lineLen
		}
		if f.start < f.end {
			out = append(out, f)
		}
	}
	return out
}

// addNamedArgSpans appends styled spans for Groovy named-argument keys
// (`name:`, `defaultValue:`, …) to the existing span list.
func addNamedArgSpans(spans []hlSpan, line string, style lipgloss.Style) []hlSpan {
	for _, m := range groovyNamedArgRe.FindAllStringSubmatchIndex(line, -1) {
		if len(m) < 4 {
			continue
		}
		s, e := m[2], m[3]
		if !overlapsAny(spans, s, e) {
			spans = append(spans, hlSpan{s, e, style})
		}
	}
	return spans
}

// renderGroovyLine returns a single Groovy source line with ANSI syntax coloring.
func renderGroovyLine(line string, t theme.Theme, overlay *syntaxOverlay, forced []hlSpan) string {
	spans := groovySpans(line, t, overlay, forced)
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

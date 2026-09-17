package usecase

import (
	"regexp"
	"strings"

	"github.com/Breina/Jenking/internal/jenkins"
)

// LogMatch is one matching log line with its surrounding context.
type LogMatch struct {
	LineNumber int // 1-based
	Line       string
	Before     []string
	After      []string
}

// SearchText returns up to maxMatches lines of text matching re, each with up
// to contextLines lines either side, plus the count of every matching line.
func SearchText(text string, re *regexp.Regexp, maxMatches, contextLines int) (matches []LogMatch, total int) {
	lines := splitLines(text)
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		total++
		if len(matches) >= maxMatches {
			continue
		}
		matches = append(matches, LogMatch{
			LineNumber: i + 1,
			Line:       line,
			Before:     lines[max(0, i-contextLines):i],
			After:      lines[i+1 : min(len(lines), i+1+contextLines)],
		})
	}
	return matches, total
}

// LineWindow returns up to maxLines lines of text starting at the 1-based
// startLine; a negative startLine counts back from the end (-100 = the last 100
// lines). It also reports the number of the first line returned (0 when the
// window is empty) and the text's total line count.
func LineWindow(text string, startLine, maxLines int) (window string, first, total int) {
	lines := splitLines(text)
	total = len(lines)
	idx := startLine - 1
	if startLine < 0 {
		idx = total + startLine
	}
	idx = max(idx, 0)
	if idx >= total || maxLines <= 0 {
		return "", 0, total
	}
	end := min(total, idx+maxLines)
	return strings.Join(lines[idx:end], "\n"), idx + 1, total
}

// splitLines splits a log into lines, dropping CR line-ending residue and the
// empty element after a trailing newline. Each line is stripped of the ANSI
// hidden blocks Jenkins embeds (base64 metadata that would otherwise dominate
// the output and produce false regex matches) and of colour escapes. Stripping
// is within lines, so line numbers still match the log file.
func splitLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = jenkins.CleanLogLine(strings.TrimSuffix(l, "\r"))
	}
	return lines
}

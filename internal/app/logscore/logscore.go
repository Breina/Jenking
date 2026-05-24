// Package logscore assigns per-character importance scores to a log line and a
// single aggregate score for the whole line. The scoring is pure: no I/O, no
// UI dependencies, no global mutable state. It encodes Jenking's conventions
// for what makes a Jenkins build log line "interesting" (severity keywords,
// standalone numeric values, stage banners) so the preview minimap and any
// future heat-map rendering share a single source of truth.
package logscore

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	timestampRe     = regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}|\d{2}:\d{2}:\d{2}[.,]\d{3}`)
	bracketNoiseRe  = regexp.MustCompile(`(?i)\[(INFO|DEBUG|TRACE|FINE|Pipeline|main|pool-\d+-thread-\d+)\]`)
	pipelineStageRe = regexp.MustCompile(`\[Pipeline\] \{ \(([^)]+)\)`)
)

type kwEntry struct {
	word  string
	score float64
	exact bool // require word boundaries for short words
}

var importanceKeywords = []kwEntry{
	// Critical
	{"error", 0.95, false}, {"fatal", 0.95, false}, {"exception", 0.95, false},
	{"panic", 0.95, true}, {"failed", 0.95, false}, {"failure", 0.95, false},
	// Important
	{"warning", 0.8, false}, {"warn", 0.8, true}, {"deprecated", 0.75, false},
	{"caused by", 0.85, false}, {"timeout", 0.8, false}, {"refused", 0.8, false},
	{"denied", 0.8, false}, {"unable", 0.75, false}, {"cannot", 0.75, false},
	{"not found", 0.75, false}, {"rejected", 0.8, false},
	// Medium
	{"success", 0.65, false}, {"passed", 0.65, false}, {"completed", 0.6, false},
	{"skipped", 0.55, false},
	{"null", 0.7, true}, {"nil", 0.7, true}, {"undefined", 0.7, false},
	{"true", 0.5, true}, {"false", 0.5, true},
}

// lineLevel classifies the overall importance tier of a line based on the
// first keyword found in roughly the first 30 characters (to catch [ERROR],
// [WARNING], etc.). Returns "error", "warning", or "".
// INFO/DEBUG/TRACE in the prefix suppresses the whole-line boost entirely.
func lineLevel(lower string) string {
	prefix := lower
	if len(prefix) > 30 {
		prefix = prefix[:30]
	}
	if bracketNoiseRe.MatchString(prefix) {
		return ""
	}
	for _, kw := range importanceKeywords {
		if kw.score >= 0.9 {
			if strings.Contains(prefix, kw.word) {
				return "error"
			}
		}
	}
	for _, kw := range importanceKeywords {
		if kw.score >= 0.75 && kw.score < 0.9 {
			if strings.Contains(prefix, kw.word) {
				return "warning"
			}
		}
	}
	return ""
}

// ScoreImportance returns per-rune importance scores in [0,1] and an aggregate
// line score in [0,1]. The aggregate weights peak more than mean so a single
// bright keyword pulls the line up.
func ScoreImportance(line string) ([]float64, float64) {
	runes := []rune(line)
	n := len(runes)
	if n == 0 {
		return nil, 0.3
	}

	// [Pipeline] lines are Jenkins bookkeeping — barely visible.
	// Exception: stage-header lines like "[Pipeline] { (Sonar scan)" highlight the stage name.
	if strings.HasPrefix(line, "[Pipeline]") {
		scores := make([]float64, n)
		for i := range scores {
			scores[i] = 0.02
		}
		if m := pipelineStageRe.FindStringSubmatchIndex(line); m != nil {
			stageStart := utf8.RuneCountInString(line[:m[2]])
			stageEnd := utf8.RuneCountInString(line[:m[3]])
			for i := stageStart; i < stageEnd && i < n; i++ {
				scores[i] = 0.95
			}
			for d := 1; d <= 3; d++ {
				glow := 0.95 * (1.0 - float64(d)*0.15)
				if stageStart-d >= 0 && scores[stageStart-d] < glow {
					scores[stageStart-d] = glow
				}
				if stageEnd-1+d < n && scores[stageEnd-1+d] < glow {
					scores[stageEnd-1+d] = glow
				}
			}
		}
		return scores, computeLineScore(scores)
	}

	scores := make([]float64, n)

	for i, r := range runes {
		scores[i] = baseCharScore(r)
	}

	lower := strings.ToLower(line)
	for _, kw := range importanceKeywords {
		applyKeywordBoost(scores, lower, kw)
	}

	boostNumbers(scores, runes)

	dampenRe(scores, line, timestampRe, 0.08)
	dampenRe(scores, line, bracketNoiseRe, 0.1)

	smoothScores(scores, 3)

	switch lineLevel(lower) {
	case "error":
		for i := range scores {
			if scores[i] < 0.80 {
				scores[i] = 0.80
			}
		}
	case "warning":
		for i := range scores {
			if scores[i] < 0.60 {
				scores[i] = 0.60
			}
			if scores[i] > 0.80 {
				scores[i] = 0.80
			}
		}
	}

	return scores, computeLineScore(scores)
}

func baseCharScore(r rune) float64 {
	switch {
	case r == ' ' || r == '\t':
		return 0.05
	case r == '[' || r == ']' || r == '{' || r == '}' || r == '(' || r == ')':
		return 0.08
	case r == '-' || r == '=' || r == '.' || r == ',' || r == ';' ||
		r == '|' || r == '/' || r == '\\' || r == '+' || r == '*' ||
		r == '_' || r == '~':
		return 0.12
	case r == ':':
		return 0.15
	case r >= '0' && r <= '9':
		return 0.45
	case r >= 'A' && r <= 'Z':
		return 0.55
	case r >= 'a' && r <= 'z':
		return 0.30
	case r == '!' || r == '?':
		return 0.40
	default:
		return 0.20
	}
}

func isWordCharByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func applyKeywordBoost(scores []float64, lower string, kw kwEntry) {
	kwLen := len(kw.word)
	pos := 0
	for {
		idx := strings.Index(lower[pos:], kw.word)
		if idx < 0 {
			break
		}
		bs := pos + idx
		be := bs + kwLen

		if kw.exact {
			if bs > 0 && isWordCharByte(lower[bs-1]) {
				pos = bs + 1
				continue
			}
			if be < len(lower) && isWordCharByte(lower[be]) {
				pos = bs + 1
				continue
			}
		}

		rs := utf8.RuneCountInString(lower[:bs])
		re := rs + utf8.RuneCountInString(lower[bs:be])

		for i := rs; i < re && i < len(scores); i++ {
			if scores[i] < kw.score {
				scores[i] = kw.score
			}
		}
		for d := 1; d <= 3; d++ {
			glow := kw.score * (1.0 - float64(d)*0.15)
			if rs-d >= 0 && scores[rs-d] < glow {
				scores[rs-d] = glow
			}
			if re-1+d >= 0 && re-1+d < len(scores) && scores[re-1+d] < glow {
				scores[re-1+d] = glow
			}
		}
		pos = be
	}
}

// boostNumbers finds standalone digit sequences (word-bounded) and boosts
// them based on context. Numbers after ':' or '=' get the highest boost;
// "0" is treated as less interesting than non-zero values.
func boostNumbers(scores []float64, runes []rune) {
	n := len(runes)
	i := 0
	for i < n {
		if runes[i] < '0' || runes[i] > '9' {
			i++
			continue
		}
		start := i
		for i < n && runes[i] >= '0' && runes[i] <= '9' {
			i++
		}
		end := i
		if start > 0 && isAlphaRune(runes[start-1]) {
			continue
		}
		if end < n && isAlphaRune(runes[end]) {
			continue
		}
		afterContext := false
		for k := start - 1; k >= 0; k-- {
			if runes[k] == ' ' {
				continue
			}
			if runes[k] == ':' || runes[k] == '=' {
				afterContext = true
			}
			break
		}
		isZero := string(runes[start:end]) == "0"
		boost := 0.70
		if afterContext {
			boost = 0.90
		}
		if isZero {
			boost -= 0.25
		}
		for j := start; j < end; j++ {
			if scores[j] < boost {
				scores[j] = boost
			}
		}
	}
}

func isAlphaRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func dampenRe(scores []float64, text string, re *regexp.Regexp, score float64) {
	for _, loc := range re.FindAllStringIndex(text, -1) {
		rs := utf8.RuneCountInString(text[:loc[0]])
		re := rs + utf8.RuneCountInString(text[loc[0]:loc[1]])
		for i := rs; i < re && i < len(scores); i++ {
			scores[i] = score
		}
	}
}

func smoothScores(scores []float64, radius int) {
	n := len(scores)
	if n < 3 {
		return
	}
	tmp := make([]float64, n)
	for i := range scores {
		wsum, wcount := 0.0, 0.0
		lo, hi := max(0, i-radius), min(n-1, i+radius)
		for j := lo; j <= hi; j++ {
			d := i - j
			if d < 0 {
				d = -d
			}
			w := 1.0 - float64(d)/float64(radius+1)
			wsum += scores[j] * w
			wcount += w
		}
		tmp[i] = wsum / wcount
	}
	// Preserve peaks: only raise, never lower.
	for i := range scores {
		if tmp[i] > scores[i] {
			scores[i] = tmp[i]
		}
	}
}

func computeLineScore(scores []float64) float64 {
	if len(scores) == 0 {
		return 0.3
	}
	sum, peak := 0.0, 0.0
	for _, s := range scores {
		sum += s
		if s > peak {
			peak = s
		}
	}
	avg := sum / float64(len(scores))
	return avg*0.4 + peak*0.6
}

package view

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// matrixTickMsg drives the animation loop at ~16 FPS.
type matrixTickMsg struct{}

const (
	shadeInvisible = 0
	shadeVeryDim   = 1
	shadeDim       = 2
	shadeMid       = 3
	shadeBright    = 4
	shadeHead      = 5
)

// ---------- Wide character replacement ----------

// Single-width mathematical symbols used to replace double-width emoji/CJK.
var wideCharMap = [...]rune{
	'∀', '∃', '∅', '∆', '∇', '∈', '∋', '∏', '∑', '∗',
	'√', '∝', '∞', '∠', '∧', '∨', '∩', '∪', '∫', '∴',
	'≈', '≠', '≡', '≤', '≥', '⊂', '⊃', '⊆', '⊇', '⊕',
	'⊗', '⊥', '⋆', '⋮', '⌀', '⌊', '⌋', '⌈', '⌉', '◊',
}

// Half-width katakana + digits + select symbols — single-cell-width glyphs
// used as the cycling leading character at the head of each column.
var headGlyphs = [...]rune{
	'ｦ', 'ｧ', 'ｨ', 'ｩ', 'ｪ', 'ｫ', 'ｬ', 'ｭ', 'ｮ', 'ｯ',
	'ｰ', 'ｱ', 'ｲ', 'ｳ', 'ｴ', 'ｵ', 'ｶ', 'ｷ', 'ｸ', 'ｹ',
	'ｺ', 'ｻ', 'ｼ', 'ｽ', 'ｾ', 'ｿ', 'ﾀ', 'ﾁ', 'ﾂ', 'ﾃ',
	'ﾄ', 'ﾅ', 'ﾆ', 'ﾇ', 'ﾈ', 'ﾉ', 'ﾊ', 'ﾋ', 'ﾌ', 'ﾍ',
	'ﾎ', 'ﾏ', 'ﾐ', 'ﾑ', 'ﾒ', 'ﾓ', 'ﾔ', 'ﾕ', 'ﾖ', 'ﾗ',
	'ﾘ', 'ﾙ', 'ﾚ', 'ﾛ', 'ﾜ', 'ﾝ',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
	'∀', '∃', '∅', '∇', '∈', '∑', '√', '∞', '≠', '⊕',
}

func replaceWideRunes(runes []rune) []rune {
	out := make([]rune, len(runes))
	for i, r := range runes {
		if columnWidth(r) > 1 {
			out[i] = wideCharMap[int(r)%len(wideCharMap)]
		} else {
			out[i] = r
		}
	}
	return out
}

// ---------- Importance scoring ----------

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
// [WARNING], etc.).  Returns "error", "warning", or "".
// INFO/DEBUG/TRACE in the prefix suppresses the whole-line boost entirely.
func lineLevel(lower string) string {
	// Look in the first 30 chars (or whole line if shorter).
	prefix := lower
	if len(prefix) > 30 {
		prefix = prefix[:30]
	}
	// INFO/DEBUG/TRACE/FINE suppress the whole-line boost, just as warning
	// caps peaks so a later error word doesn't spike above warning tier.
	if bracketNoiseRe.MatchString(prefix) {
		return ""
	}
	for _, kw := range importanceKeywords {
		if kw.score >= 0.9 { // error-tier
			if strings.Contains(prefix, kw.word) {
				return "error"
			}
		}
	}
	for _, kw := range importanceKeywords {
		if kw.score >= 0.75 && kw.score < 0.9 { // warning-tier
			if strings.Contains(prefix, kw.word) {
				return "warning"
			}
		}
	}
	return ""
}

func scoreImportance(line string) ([]float64, float64) {
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

	// Phase 1: base character scores.
	for i, r := range runes {
		scores[i] = baseCharScore(r)
	}

	// Phase 2: keyword boosts.
	lower := strings.ToLower(line)
	for _, kw := range importanceKeywords {
		applyKeywordBoost(scores, lower, kw)
	}

	// Phase 3: smart number boost — standalone digits get boosted,
	// higher boost after : or =, "0" values dimmer than non-zero.
	boostNumbers(scores, runes)

	// Phase 4: dampen noise (timestamps, bracket prefixes).
	dampenRe(scores, line, timestampRe, 0.08)
	dampenRe(scores, line, bracketNoiseRe, 0.1)

	// Phase 5: smooth — let bright keywords glow onto neighbours.
	smoothScores(scores, 3)

	// Phase 6: line-level boost — if the line starts with a warning/error
	// keyword, lift the entire line to that tier.  For warning-start lines,
	// cap peaks so that a later "error" word doesn't spike above warning.
	level := lineLevel(lower)
	switch level {
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
		// Glow ±3 around match.
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
// them based on context.  Numbers after ':' or '=' get the highest boost;
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
		// Word-boundary check.
		if start > 0 && isAlphaRune(runes[start-1]) {
			continue
		}
		if end < n && isAlphaRune(runes[end]) {
			continue
		}
		// Determine context: is there a ':' or '=' before (skipping spaces)?
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
		boost := 0.70 // standalone number
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

// ---------- Data structures ----------

type matrixCell struct {
	ch    rune
	shade int
}

type matrixColumn struct {
	x             int
	y             float64
	runes         []rune    // visible characters (wide chars replaced, leading whitespace stripped)
	charScores    []float64 // per-character importance (parallel to runes)
	lineScore     float64   // overall line importance 0..1
	kind          lineKind
	speed         float64 // base rows per tick
	trailLen      int
	headChar      rune // cycling random glyph at the leading edge
	headCycleRate int  // ticks between head glyph changes (logarithmic: 1=fast, ~30=slow)
	headCycleTick int  // countdown until next change
}

type pendingLine struct {
	text string
	kind lineKind
}

type matrixStyles struct {
	shades [6]lipgloss.Style
}

func newMatrixStyles() matrixStyles {
	return matrixStyles{
		shades: [6]lipgloss.Style{
			lipgloss.NewStyle(), // 0: invisible
			lipgloss.NewStyle().Foreground(lipgloss.Color("22")),             // 1: very dark green
			lipgloss.NewStyle().Foreground(lipgloss.Color("28")),             // 2: dark green
			lipgloss.NewStyle().Foreground(lipgloss.Color("34")).Bold(true),  // 3: medium green, bold
			lipgloss.NewStyle().Foreground(lipgloss.Color("40")).Bold(true),  // 4: bright green, bold
			lipgloss.NewStyle().Foreground(lipgloss.Color("157")).Bold(true), // 5: white-green head, bold
		},
	}
}

// ---------- MatrixView ----------

// MatrixView renders build logs as Matrix-style digital rain.
type MatrixView struct {
	client     jenkins.JenkinsClient
	nc         NavigationContext
	ctx        context.Context
	cancel     context.CancelFunc
	width      int
	height     int
	columns    []matrixColumn
	pending    []pendingLine
	grid       [][]matrixCell
	tick       int
	rng        *rand.Rand
	done       bool
	fetchStart int
	styles     matrixStyles
}

func NewMatrixView(client jenkins.JenkinsClient, nc NavigationContext) *MatrixView {
	ctx, cancel := context.WithCancel(context.Background())
	return &MatrixView{
		client: client,
		nc:     nc,
		ctx:    ctx,
		cancel: cancel,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		styles: newMatrixStyles(),
	}
}

func (mv *MatrixView) IsFullScreen() bool { return true }

func (mv *MatrixView) Init() tea.Cmd {
	return tea.Batch(
		consoleFetch(mv.ctx, mv.client, mv.nc.JobPath(), mv.nc.Build.Number, mv.fetchStart, 0),
		tea.Tick(62*time.Millisecond, func(time.Time) tea.Msg { return matrixTickMsg{} }),
	)
}

func (mv *MatrixView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		return mv, nil

	case consoleChunkMsg:
		for _, raw := range msg.lines {
			kind := classifyLine(raw)
			mv.pending = append(mv.pending, pendingLine{text: raw, kind: kind})
		}
		// Cap pending queue to avoid unbounded growth.
		if len(mv.pending) > 200 {
			mv.pending = mv.pending[len(mv.pending)-200:]
		}
		if msg.moreData {
			return mv, consoleFetch(mv.ctx, mv.client, mv.nc.JobPath(), mv.nc.Build.Number, msg.nextStart, time.Second)
		}
		mv.done = true
		return mv, nil

	case consoleAbortMsg:
		return mv, nil

	case matrixTickMsg:
		mv.tick++
		mv.advanceColumns()
		mv.cullColumns()
		mv.spawnColumns()
		mv.compositeGrid()
		return mv, tea.Tick(62*time.Millisecond, func(time.Time) tea.Msg { return matrixTickMsg{} })

	case tea.KeyMsg:
		return mv, nil
	}

	return mv, nil
}

func (mv *MatrixView) View() string {
	if mv.width <= 0 || mv.height <= 0 {
		return ""
	}

	var b strings.Builder
	b.Grow(mv.width * mv.height * 4)

	for y := 0; y < mv.height; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		row := mv.grid[y]
		currentShade := -1
		var run []rune
		for x := 0; x < mv.width; x++ {
			cell := row[x]
			if cell.shade != currentShade {
				if len(run) > 0 {
					b.WriteString(mv.renderRun(currentShade, run))
					run = run[:0]
				}
				currentShade = cell.shade
			}
			if cell.shade == shadeInvisible {
				run = append(run, ' ')
			} else {
				run = append(run, cell.ch)
			}
		}
		if len(run) > 0 {
			b.WriteString(mv.renderRun(currentShade, run))
		}
	}
	return b.String()
}

func (mv *MatrixView) renderRun(shade int, runes []rune) string {
	if shade == shadeInvisible {
		return string(runes)
	}
	return mv.styles.shades[shade].Render(string(runes))
}

// ---------- Animation ----------

func (mv *MatrixView) advanceColumns() {
	// Each column's speed was fixed at spawn time (includes the queue-depth
	// multiplier that was active when it was created).  Trails never change
	// speed after spawning.
	for i := range mv.columns {
		mv.columns[i].y += mv.columns[i].speed
		mv.columns[i].headCycleTick--
		if mv.columns[i].headCycleTick <= 0 {
			mv.columns[i].headChar = headGlyphs[mv.rng.Intn(len(headGlyphs))]
			mv.columns[i].headCycleTick = mv.columns[i].headCycleRate
		}
	}
}

func (mv *MatrixView) cullColumns() {
	alive := mv.columns[:0]
	for _, col := range mv.columns {
		if int(col.y)-col.trailLen <= mv.height {
			alive = append(alive, col)
		}
	}
	mv.columns = alive
}

func (mv *MatrixView) spawnColumns() {
	if len(mv.pending) == 0 || mv.width <= 0 {
		return
	}
	if len(mv.columns) >= 60 {
		return
	}

	p := len(mv.pending)

	// Adaptive speed multiplier: bake into each new trail's speed at spawn so
	// existing trails are never affected.
	speedMul := 1.0
	switch {
	case p > 150:
		speedMul = 4.0
	case p > 80:
		speedMul = 2.5
	case p > 40:
		speedMul = 1.8
	case p > 15:
		speedMul = 1.3
	}

	count := 1
	if p > 80 {
		count = 4
	} else if p > 40 {
		count = 3
	} else if p > 15 {
		count = 2
	}

	for i := 0; i < count && len(mv.pending) > 0 && len(mv.columns) < 60; i++ {
		line := mv.pending[0]
		mv.pending = mv.pending[1:]

		runes := replaceWideRunes([]rune(line.text))
		charScores, lineScore := scoreImportance(line.text)

		// Strip leading whitespace (closes the gap at the head).
		start := 0
		for start < len(runes) && (runes[start] == ' ' || runes[start] == '\t') {
			start++
		}
		runes = runes[start:]
		if start < len(charScores) {
			charScores = charScores[start:]
		} else {
			charScores = nil
		}

		if len(runes) == 0 {
			continue // skip blank lines
		}

		x := mv.pickColumnX()
		speed := (0.12 - lineScore*0.06) * speedMul          // base speed scaled by queue depth at spawn
		trailLen := 20 + int(lineScore*60) + mv.rng.Intn(10) // 20 – 90

		// Logarithmic head glyph cycle rate: exp(uniform(0, ln(30))) → 1..30 ticks.
		// Most columns cycle fast (1-3 ticks), a few drift very slowly (~30 ticks ≈ 2s).
		cycleRate := int(math.Exp(mv.rng.Float64()*math.Log(30))) + 1

		mv.columns = append(mv.columns, matrixColumn{
			x:             x,
			y:             0,
			runes:         runes,
			charScores:    charScores,
			lineScore:     lineScore,
			kind:          line.kind,
			speed:         speed,
			trailLen:      trailLen,
			headChar:      headGlyphs[mv.rng.Intn(len(headGlyphs))],
			headCycleRate: cycleRate,
			headCycleTick: cycleRate,
		})
	}
}

func (mv *MatrixView) pickColumnX() int {
	for attempt := 0; attempt < 5; attempt++ {
		x := mv.rng.Intn(mv.width)
		ok := true
		for _, col := range mv.columns {
			if col.x == x && int(col.y) < col.trailLen+3 {
				ok = false
				break
			}
		}
		if ok {
			return x
		}
	}
	return mv.rng.Intn(mv.width)
}

func (mv *MatrixView) compositeGrid() {
	if len(mv.grid) != mv.height || (mv.height > 0 && len(mv.grid[0]) != mv.width) {
		mv.allocateGrid()
	}

	// Clear.
	for y := range mv.grid {
		for x := range mv.grid[y] {
			mv.grid[y][x] = matrixCell{}
		}
	}

	// Write columns.
	for _, col := range mv.columns {
		headY := int(col.y)
		if col.x < 0 || col.x >= mv.width {
			continue
		}

		// dist=0 is the head glyph (cycling katakana), text starts at dist=1.
		for dist := 0; dist <= col.trailLen; dist++ {
			row := headY - dist
			if row < 0 || row >= mv.height {
				continue
			}

			var ch rune
			var shade int

			if dist == 0 {
				// Leading head character — always max brightness.
				ch = col.headChar
				shade = shadeHead
			} else {
				// Trail: text characters, offset by 1 for the head.
				ti := dist - 1
				if ti < len(col.runes) {
					ch = col.runes[ti]
				} else {
					ch = ' '
				}
				var cs float64
				if ti < len(col.charScores) {
					cs = col.charScores[ti]
				} else {
					cs = 0.05
				}
				shade = shadeForChar(dist, col.trailLen, cs)
			}

			if shade == shadeInvisible {
				continue
			}
			if ch == ' ' {
				continue
			}

			existing := mv.grid[row][col.x]
			if shade > existing.shade {
				mv.grid[row][col.x] = matrixCell{ch: ch, shade: shade}
			}
		}
	}
}

func (mv *MatrixView) allocateGrid() {
	mv.grid = make([][]matrixCell, mv.height)
	for y := range mv.grid {
		mv.grid[y] = make([]matrixCell, mv.width)
	}
}

// shadeForChar computes the shade for a trail character.
// PRIMARY driver: charScore (importance determines brightness).
// SECONDARY: distance from head (gentle overall fade toward tail).
func shadeForChar(dist, trailLen int, charScore float64) int {
	if trailLen <= 0 {
		return shadeInvisible
	}
	distFrac := float64(dist) / float64(trailLen)
	if distFrac >= 1.0 {
		return shadeInvisible
	}

	// charScore (0..1) → base shade (1..5).
	baseShade := 1.0 + charScore*4.0

	// Gentle distance fade: 1.0 at head → 0.65 at tail.
	distMul := 1.0 - distFrac*0.35

	shade := int(baseShade*distMul + 0.5)
	if shade > shadeHead {
		shade = shadeHead
	}
	if shade < shadeVeryDim {
		shade = shadeVeryDim
	}
	return shade
}

// ---------- View interface ----------

func (mv *MatrixView) Title() string {
	return fmt.Sprintf("Matrix #%d", mv.nc.Build.Number)
}

func (mv *MatrixView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbFor("matrix", mv.nc)
}

func (mv *MatrixView) NC() NavigationContext { return mv.nc }

func (mv *MatrixView) ItemCount() int { return len(mv.columns) }

func (mv *MatrixView) Commands() []command.Command { return nil }

func (mv *MatrixView) Shortcuts() []component.Shortcut {
	return []component.Shortcut{component.Nav("esc", "back")}
}

func (mv *MatrixView) SetSize(w, h int) {
	if w == mv.width && h == mv.height {
		return
	}
	mv.width = w
	mv.height = h
	mv.allocateGrid()
}

func (mv *MatrixView) Close() error {
	if mv.cancel != nil {
		mv.cancel()
	}
	return nil
}

func (mv *MatrixView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
	return NewStageView(t, c, s, mv.nc, jenkins.Build{Number: mv.nc.Build.Number})
}

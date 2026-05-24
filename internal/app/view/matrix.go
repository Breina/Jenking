package view

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/app/logscore"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
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
		if lipgloss.Width(string(r)) > 1 {
			out[i] = wideCharMap[int(r)%len(wideCharMap)]
		} else {
			out[i] = r
		}
	}
	return out
}

// Per-character importance scoring lives in internal/app/logscore. This view
// calls logscore.ScoreImportance directly; do not reintroduce a local copy.

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
	kind          widget.LineKind
	speed         float64 // base rows per tick
	trailLen      int
	headChar      rune // cycling random glyph at the leading edge
	headCycleRate int  // ticks between head glyph changes (logarithmic: 1=fast, ~30=slow)
	headCycleTick int  // countdown until next change
}

type pendingLine struct {
	text string
	kind widget.LineKind
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
	client     jmodel.JenkinsClient
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

func NewMatrixView(client jmodel.JenkinsClient, nc NavigationContext) *MatrixView {
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
			kind := widget.ClassifyContentLine(raw)
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
		charScores, lineScore := logscore.ScoreImportance(line.text)

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
	mv.clearGrid()
	for i := range mv.columns {
		mv.writeColumn(&mv.columns[i])
	}
}

// clearGrid resets every cell to the empty matrixCell.
func (mv *MatrixView) clearGrid() {
	for y := range mv.grid {
		for x := range mv.grid[y] {
			mv.grid[y][x] = matrixCell{}
		}
	}
}

// writeColumn paints one falling column (head + trail) onto the grid.
func (mv *MatrixView) writeColumn(col *matrixColumn) {
	if col.x < 0 || col.x >= mv.width {
		return
	}
	headY := int(col.y)
	for dist := 0; dist <= col.trailLen; dist++ {
		row := headY - dist
		if row < 0 || row >= mv.height {
			continue
		}
		ch, shade := columnCellAt(col, dist)
		if shade == shadeInvisible || ch == ' ' {
			continue
		}
		if existing := mv.grid[row][col.x]; shade > existing.shade {
			mv.grid[row][col.x] = matrixCell{ch: ch, shade: shade}
		}
	}
}

// columnCellAt returns the rune + shade for a given distance from the head.
// dist=0 is the head glyph (cycling katakana), text runes start at dist=1.
func columnCellAt(col *matrixColumn, dist int) (rune, int) {
	if dist == 0 {
		return col.headChar, shadeHead
	}
	ti := dist - 1
	ch := ' '
	if ti < len(col.runes) {
		ch = col.runes[ti]
	}
	cs := 0.05
	if ti < len(col.charScores) {
		cs = col.charScores[ti]
	}
	return ch, shadeForChar(dist, col.trailLen, cs)
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

func (mv *MatrixView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	return NewStageView(t, c, s, mv.nc, jmodel.Build{Number: mv.nc.Build.Number})
}

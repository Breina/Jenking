package view

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// This file wraps the ntcharts line chart (running/queued, with throughput
// stacked bars overlaid on the same canvas) and provides a horizontal bar
// renderer with stacked segments and value labels beside each bar — neither of
// which ntcharts' barchart can express.

// lineSeries is one named line for the activity chart.
type lineSeries struct {
	name  string
	pts   []timeserieslinechart.TimePoint
	style lipgloss.Style
}

// completionEvent is a finished build placed in time for the throughput bars.
type completionEvent struct {
	t   time.Time
	seg int // 0 success, 1 failed, 2 unstable
}

// Series names shared by the activity chart builder and its data provider.
const (
	runningSeriesName = "running"
	queuedSeriesName  = "queued"
)

// buildActivityChart renders running/queued as braille lines and throughput as
// stacked bars overlaid on the same time axis. The braille pass clears and
// redraws the canvas, so the bars are drawn last (on top) to remain visible.
func buildActivityChart(w, h int, th theme.Theme, now time.Time, window time.Duration,
	lines []lineSeries, completions []completionEvent, segStyles []lipgloss.Style) string {
	if w < 6 || h < 4 {
		return ""
	}
	axisStyle := th.PanelBorder
	labelStyle := lipgloss.NewStyle().Foreground(th.PanelBorder.GetForeground())
	lc := timeserieslinechart.New(w, h,
		timeserieslinechart.WithAxesStyles(axisStyle, labelStyle),
		timeserieslinechart.WithXLabelFormatter(func(_ int, v float64) string {
			return time.Unix(int64(v), 0).Format("15:04")
		}),
		timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string {
			return strconv.Itoa(int(v + 0.5))
		}),
		timeserieslinechart.WithXYSteps(4, 2),
	)
	start := now.Add(-window)
	lc.SetViewTimeRange(start, now)

	bucketize := func(n int) [][3]float64 {
		bd := window / time.Duration(n)
		if bd <= 0 {
			bd = 1
		}
		bs := make([][3]float64, n)
		for _, c := range completions {
			if c.seg < 0 || c.seg > 2 || c.t.Before(start) || c.t.After(now) {
				continue
			}
			idx := int(c.t.Sub(start) / bd)
			if idx < 0 {
				idx = 0
			}
			if idx >= n {
				idx = n - 1
			}
			bs[idx][c.seg]++
		}
		return bs
	}

	for _, s := range lines {
		lc.SetDataSetStyle(s.name, s.style)
		for _, p := range s.pts {
			lc.PushDataSet(s.name, p)
		}
	}

	// Pushing auto-adjusts the range to the data extent, and the view range is
	// clamped to the data range. Reset the *data* range to the full window so
	// lines and bars share one axis even when the window isn't full of data yet.
	// Settle the Y range on the line maximum first so GraphWidth (hence the
	// column count) is fixed, bucket the completions to that exact width, then
	// lift the range to fit the tallest stacked bar. The *same* buckets are used
	// to draw, so every column is scaled against the geometry it is drawn in.
	setRange := func(maxY float64) {
		lc.SetTimeRange(start, now)
		lc.SetYRange(0, maxY)
		lc.SetViewTimeRange(start, now)
		lc.SetViewYRange(0, maxY)
	}
	maxY := maxLineY(lines)
	setRange(maxY)
	buckets := bucketize(graphCols(&lc))
	if bt := maxBucketTotal(buckets); bt > maxY {
		maxY = bt
		setRange(maxY)
		buckets = bucketize(graphCols(&lc))
	}

	lc.DrawBrailleAll()                      // clears canvas, draws axes + lines
	drawStackedBars(&lc, buckets, segStyles) // bars on top so they show
	return lc.View()
}

// graphCols is the drawable column count of the chart (at least 1).
func graphCols(lc *timeserieslinechart.Model) int {
	gw := lc.GraphWidth()
	if gw < 1 {
		gw = 1
	}
	return gw
}

// maxLineY is the tallest running/queued point (at least 1, so the axis is
// never degenerate).
func maxLineY(lines []lineSeries) float64 {
	maxY := 1.0
	for _, s := range lines {
		for _, p := range s.pts {
			if p.Value > maxY {
				maxY = p.Value
			}
		}
	}
	return maxY
}

// maxBucketTotal is the tallest stacked throughput column.
func maxBucketTotal(buckets [][3]float64) float64 {
	maxY := 0.0
	for _, b := range buckets {
		if total := b[0] + b[1] + b[2]; total > maxY {
			maxY = total
		}
	}
	return maxY
}

// drawStackedBars paints throughput buckets as stacked colored columns onto the
// chart canvas, one column per bucket, sitting on the X axis.
func drawStackedBars(lc *timeserieslinechart.Model, buckets [][3]float64, styles []lipgloss.Style) {
	origin := lc.Origin()
	for j, segs := range buckets {
		colX := origin.X + 1 + j
		prevRows := 0
		cum := 0.0
		for k := 0; k < 3; k++ {
			if segs[k] <= 0 {
				continue
			}
			cum += segs[k]
			rows := int(math.Round(lc.ScaleFloat64Point(canvas.Float64Point{X: 0, Y: cum}).Y))
			if rows > origin.Y { // never draw above the top of the graph area
				rows = origin.Y
			}
			for r := prevRows; r < rows; r++ {
				lc.Canvas.SetCell(canvas.Point{X: colX, Y: origin.Y - r}, canvas.NewCellWithStyle(runes.FullBlock, styles[k]))
			}
			prevRows = rows
		}
	}
}

// hseg is one colored segment of a stacked horizontal bar.
type hseg struct {
	value float64
	style lipgloss.Style
}

// hbar is one row of a horizontal bar chart: a label, one or more stacked
// segments, and a value label printed beside the bar.
type hbar struct {
	label string
	segs  []hseg
	text  string
}

func (b hbar) total() float64 {
	t := 0.0
	for _, s := range b.segs {
		t += s.value
	}
	return t
}

// renderHBars draws label · [stacked bar] · value rows to exactly w×h. Bars
// auto-scale to the largest total present (a dynamic axis), each segment is
// colored, and the value is printed to the right rather than in the label.
func renderHBars(w, h int, th theme.Theme, bars []hbar) string {
	lines := make([]string, h)
	for i := range lines {
		lines[i] = strings.Repeat(" ", w)
	}
	if w < 8 || h < 1 || len(bars) == 0 {
		return strings.Join(lines, "\n")
	}
	maxVal := 0.0
	labelW, valueW := 0, 0
	for _, b := range bars {
		if t := b.total(); t > maxVal {
			maxVal = t
		}
		labelW = maxInt(labelW, lipgloss.Width(b.label))
		valueW = maxInt(valueW, lipgloss.Width(b.text))
	}
	if maxVal <= 0 {
		maxVal = 1
	}
	if labelW > w/3 {
		labelW = w / 3
	}
	barW := w - labelW - valueW - 2
	if barW < 1 {
		barW = 1
	}
	track := lipgloss.NewStyle().Foreground(th.PanelBorder.GetForeground())

	n := minInt(len(bars), h)
	for i := 0; i < n; i++ {
		b := bars[i]
		lab := vpad(vtrunc(b.label, labelW), labelW)
		var sb strings.Builder
		filled := 0
		for _, sg := range b.segs {
			if sg.value <= 0 {
				continue
			}
			f := int(math.Round(sg.value / maxVal * float64(barW)))
			if filled+f > barW {
				f = barW - filled
			}
			if f <= 0 {
				continue
			}
			sb.WriteString(sg.style.Render(strings.Repeat("█", f)))
			filled += f
		}
		bar := sb.String() + track.Render(strings.Repeat("─", barW-filled))
		val := padLeft(b.text, valueW)
		lines[i] = vpad(vtrunc(lab+" "+bar+" "+val, w), w)
	}
	return strings.Join(lines, "\n")
}

func padLeft(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

package view

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"

	"github.com/Breina/Jenking/internal/app/engine"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

const (
	dashRepaintInterval = 2 * time.Second
	dashNodeInterval    = 10 * time.Second
	dashListRows        = 20 // upper bound; panes clamp to their height
)

type dashRepaintTickMsg struct{}
type dashNodeTickMsg struct{}
type dashNodesMsg struct {
	nodes []jmodel.Node
	err   error
}

// DashboardView is a single-screen overview of Jenkins activity. It reads
// entirely from the in-memory registry and the engine's live sampler/queue
// tracker (kept fresh by the engine's poll loop) on a repaint tick, and polls
// /computer for node health on its own slower tick. A shared, zoomable timescale
// drives every time-based tile. Sampled history is owned and persisted by the
// engine. It renders borderless and the only interaction is +/- zoom.
type DashboardView struct {
	theme   theme.Theme
	client  jmodel.JenkinsClient
	store   *cache.Store
	grid    *component.PaneGrid
	sampler *engine.Sampler
	queue   *engine.QueueTracker
	ts      timescale
	nodes   []jmodel.Node

	width, height int
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewDashboardView constructs the dashboard over the engine's live sampler and
// queue tracker, so it renders the same history the engine keeps rather than
// sampling independently.
func NewDashboardView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store, eng *engine.Engine) *DashboardView {
	ctx, cancel := context.WithCancel(context.Background())
	return &DashboardView{
		theme:   t,
		client:  c,
		store:   s,
		grid:    component.NewPaneGrid(t),
		sampler: eng.Sampler(),
		queue:   eng.Queue(),
		ts:      newTimescale(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Init starts the node poll and the repaint tick; the engine already samples, so
// the dashboard only needs to re-render periodically to reflect new data.
func (d *DashboardView) Init() tea.Cmd {
	return tea.Batch(
		d.fetchNodes(),
		dashTick(d.ctx, dashRepaintInterval, dashRepaintTickMsg{}),
	)
}

// Update handles zoom keys and the two poll loops. Repaints happen naturally as
// bubbletea re-renders after every message.
func (d *DashboardView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "+", "=":
			d.ts.zoomIn()
			return d, nil
		case "-", "_":
			d.ts.zoomOut()
			return d, nil
		}
	case dashRepaintTickMsg:
		// The engine owns sampling; this tick just drives a periodic repaint so
		// new samples become visible between other messages.
		return d, dashTick(d.ctx, dashRepaintInterval, dashRepaintTickMsg{})
	case dashNodeTickMsg:
		return d, d.fetchNodes()
	case dashNodesMsg:
		if msg.err == nil {
			d.nodes = msg.nodes
		}
		return d, dashTick(d.ctx, dashNodeInterval, dashNodeTickMsg{})
	}
	return d, nil
}

// View lays out and renders the grid at the current size.
func (d *DashboardView) View() string {
	if d.width < 2 || d.height < 2 {
		return ""
	}
	d.grid.SetRows(d.layout())
	return d.grid.View(d.width, d.height)
}

func (d *DashboardView) Title() string { return "dashboard" }

// Breadcrumb renders the k9s-style segment for the header.
func (d *DashboardView) Breadcrumb() BreadcrumbSegment {
	return BreadcrumbSegment{
		ViewType: "dashboard",
		Context:  []component.BreadcrumbPart{{Text: "*"}},
	}
}

func (d *DashboardView) ItemCount() int { return 0 }

func (d *DashboardView) Commands() []command.Command { return nil }

// Shortcuts advertises the zoom control (and the active window) in the header.
func (d *DashboardView) Shortcuts() []component.Shortcut {
	return []component.Shortcut{
		{Key: "+/-", Action: "zoom (" + d.ts.label() + ")", Group: component.GroupView},
	}
}

// IsBorderlessContent makes the app skip the outer content-panel border so the
// pane grid sits at the root of the screen (header and command bar stay).
func (d *DashboardView) IsBorderlessContent() bool { return true }

// SetSize stores the panel dimensions.
func (d *DashboardView) SetSize(w, h int) { d.width, d.height = w, h }

// Close cancels the dashboard's poll loops. The engine owns the sample buffer
// and its persistence, so there is nothing to flush here.
func (d *DashboardView) Close() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}

// --- data collection ---

func (d *DashboardView) fetchNodes() tea.Cmd {
	return func() tea.Msg {
		nodes, err := d.client.ListNodes(d.ctx)
		return dashNodesMsg{nodes: nodes, err: err}
	}
}

// dashTick fires msg after d, unless ctx is cancelled first.
func dashTick(ctx context.Context, d time.Duration, msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d):
		}
		if ctx.Err() != nil {
			return nil
		}
		return msg
	}
}

// --- layout ---

func (d *DashboardView) layout() [][]component.Pane {
	now := time.Now()
	window := d.ts.window()
	snap := d.snapshot()

	activity := activityPane{
		title:       d.activityTitle(),
		theme:       d.theme,
		now:         now,
		window:      window,
		lines:       d.activityLines(now, window),
		completions: d.completionEvents(snap, now, window),
		segStyles:   []lipgloss.Style{d.theme.BuildStatus.Success, d.theme.BuildStatus.Failed, d.theme.BuildStatus.Unstable},
	}
	queueWait := hbarPane{
		title: d.queueWaitTitle(),
		theme: d.theme,
		bars:  d.queueWaitBars(now, window),
		empty: "no queue waits in window",
	}
	concurrency := hbarPane{
		title: d.titleDim("Concurrency / project"),
		theme: d.theme,
		bars:  d.concurrencyBars(),
		empty: "nothing running",
	}
	overrun, absolute := d.durationLists(snap, now, window)
	nodes := nodeMatrixPane{
		title: d.titleDim("Node health"),
		theme: d.theme,
		nodes: d.nodes,
		empty: "no node data",
	}

	return [][]component.Pane{
		{activity},
		{queueWait, concurrency},
		{overrun, absolute},
		{nodes},
	}
}

func (d *DashboardView) snapshot() []buildregistry.Record {
	if d.store == nil || d.store.Registry == nil {
		return nil
	}
	return d.store.Registry.Snapshot()
}

// --- titles (legend colors match the series) ---

func (d *DashboardView) titleDim(s string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(d.theme.PanelBorder.GetForeground()).Render(s)
}

func (d *DashboardView) activityTitle() string {
	run := d.theme.BuildStatus.Running.Render("Running")
	q := d.theme.BuildStatus.PausedInput.Render("Queued")
	ok := d.theme.BuildStatus.Success.Render("✔")
	fail := d.theme.BuildStatus.Failed.Render("✖")
	unst := d.theme.BuildStatus.Unstable.Render("⚠")
	return run + d.titleDim(" / ") + q +
		d.titleDim("   throughput ") + ok + " " + fail + " " + unst +
		d.titleDim("  ·  "+d.ts.label())
}

// --- activity chart data ---

func (d *DashboardView) activityLines(now time.Time, window time.Duration) []lineSeries {
	pts := d.sampler.Points(now, window)
	run := make([]timeserieslinechart.TimePoint, 0, len(pts))
	q := make([]timeserieslinechart.TimePoint, 0, len(pts))
	for _, p := range pts {
		run = append(run, timeserieslinechart.TimePoint{Time: p.T, Value: float64(p.Running)})
		q = append(q, timeserieslinechart.TimePoint{Time: p.T, Value: float64(p.Queued)})
	}
	return []lineSeries{
		{name: runningSeriesName, pts: run, style: d.theme.BuildStatus.Running},
		{name: queuedSeriesName, pts: q, style: d.theme.BuildStatus.PausedInput},
	}
}

// completionEvents returns finished builds (within the window) tagged with their
// outcome segment, for the throughput bars.
func (d *DashboardView) completionEvents(snap []buildregistry.Record, now time.Time, window time.Duration) []completionEvent {
	start := now.Add(-window)
	out := make([]completionEvent, 0, len(snap))
	for _, rec := range snap {
		if !rec.Terminal {
			continue
		}
		seg := statusSegment(rec.Build.Status)
		if seg < 0 {
			continue
		}
		comp := completionTime(rec)
		if comp.Before(start) || comp.After(now) {
			continue
		}
		out = append(out, completionEvent{t: comp, seg: seg})
	}
	return out
}

// statusSegment maps a build status to a throughput segment index, or -1.
func statusSegment(s jmodel.BuildStatus) int {
	switch s {
	case jmodel.BuildStatusSuccess:
		return 0
	case jmodel.BuildStatusFailed, jmodel.BuildStatusAborted:
		return 1
	case jmodel.BuildStatusUnstable:
		return 2
	default:
		return -1
	}
}

func completionTime(rec buildregistry.Record) time.Time {
	return rec.Build.Timestamp.Add(rec.Build.Duration)
}

// --- queue tile: wait distribution stacked by reason, over the window ---

// reasonStyles returns the per-reason colors in reason-index order.
func (d *DashboardView) reasonStyles() [engine.ReasonCount]lipgloss.Style {
	return [engine.ReasonCount]lipgloss.Style{
		d.theme.BuildStatus.Unstable,    // stuck
		d.theme.BuildStatus.PausedInput, // blocked
		d.theme.BuildStatus.Running,     // pending
		d.theme.BuildStatus.Aborted,     // buildable
	}
}

func (d *DashboardView) queueWaitTitle() string {
	st := d.reasonStyles()
	legend := st[engine.ReasonStuck].Render("stuck") + " " + st[engine.ReasonBlocked].Render("blocked") + " " +
		st[engine.ReasonPending].Render("pending") + " " + st[engine.ReasonBuildable].Render("buildable")
	return d.titleDim("Queue wait ") + legend + d.titleDim("  ·  "+d.ts.label())
}

// queueWaitBars renders the queue-wait distribution over the window: one stacked
// bar per occupied wait bin, segments colored by reason. Each bar counts the
// items that finished queuing in the window, placed in their final wait bin.
// Only bins with data are shown, so the visible buckets adapt to the data.
func (d *DashboardView) queueWaitBars(now time.Time, window time.Duration) []hbar {
	// Finalized items (summed over the window) plus items still queueing (live,
	// at their current wait) so in-progress waits are visible and shift between
	// bins as items age.
	sum := d.sampler.SumWaitReason(now, window)
	pend := d.queue.Pending(now)
	var counts [engine.WaitBinCount][engine.ReasonCount]int
	for b := 0; b < engine.WaitBinCount; b++ {
		for r := 0; r < engine.ReasonCount; r++ {
			counts[b][r] = sum[b][r] + pend[b][r]
		}
	}
	styles := d.reasonStyles()

	// Show a contiguous range from the lowest to the highest occupied bin, so
	// intermediate empty buckets stay visible (they carry meaning: a gap in the
	// wait distribution) rather than collapsing the axis.
	lo, hi := -1, -1
	for b := 0; b < engine.WaitBinCount; b++ {
		total := 0
		for r := 0; r < engine.ReasonCount; r++ {
			total += counts[b][r]
		}
		if total > 0 {
			if lo < 0 {
				lo = b
			}
			hi = b
		}
	}
	if lo < 0 {
		return nil
	}

	bars := make([]hbar, 0, hi-lo+1)
	for b := lo; b <= hi; b++ {
		total := 0
		segs := make([]hseg, 0, engine.ReasonCount)
		for r := 0; r < engine.ReasonCount; r++ {
			if counts[b][r] > 0 {
				segs = append(segs, hseg{value: float64(counts[b][r]), style: styles[r]})
				total += counts[b][r]
			}
		}
		bars = append(bars, hbar{label: engine.WaitBins[b].Label, segs: segs, text: strconv.Itoa(total)})
	}
	return bars
}

func (d *DashboardView) concurrencyBars() []hbar {
	if d.store == nil || d.store.Registry == nil {
		return nil
	}
	counts := map[string]int{}
	for _, b := range d.store.Registry.RunningBuilds() {
		counts[projectKey(b.JobPath)]++
	}
	type kv struct {
		name string
		n    int
	}
	rows := make([]kv, 0, len(counts))
	for k, v := range counts {
		rows = append(rows, kv{k, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].name < rows[j].name
	})
	bars := make([]hbar, 0, len(rows))
	for _, r := range rows {
		bars = append(bars, hbar{
			label: decodeName(r.name),
			segs:  []hseg{{value: float64(r.n), style: d.theme.BuildStatus.Running}},
			text:  strconv.Itoa(r.n),
		})
	}
	return bars
}

// --- duration lists ---

// durationLists returns the overrun-ranked and absolute-duration-ranked panes
// for completed builds whose completion falls within the active window.
func (d *DashboardView) durationLists(snap []buildregistry.Record, now time.Time, window time.Duration) (listPane, listPane) {
	start := now.Add(-window)
	finished := make([]buildregistry.Record, 0, len(snap))
	for _, rec := range snap {
		if !rec.Terminal {
			continue
		}
		comp := completionTime(rec)
		if comp.Before(start) || comp.After(now) {
			continue
		}
		finished = append(finished, rec)
	}

	overrunRows := d.overrunRows(finished)
	absRows := d.absoluteRows(finished)
	return listPane{title: d.titleDim("Overrun vs estimate"), rows: overrunRows, empty: "no completed builds in window"},
		listPane{title: d.titleDim("Slowest builds"), rows: absRows, empty: "no completed builds in window"}
}

func (d *DashboardView) overrunRows(finished []buildregistry.Record) []dashRow {
	recs := make([]buildregistry.Record, 0, len(finished))
	for _, rec := range finished {
		if rec.Build.EstimatedDuration > 0 {
			recs = append(recs, rec)
		}
	}
	// Deterministic tiebreak on identity: the registry snapshot iterates a map,
	// so without it equal-ratio rows (e.g. every +0%) reorder on every repaint.
	sort.Slice(recs, func(i, j int) bool {
		if a, b := overrunRatio(recs[i]), overrunRatio(recs[j]); a != b {
			return a > b
		}
		return recordLess(recs[i], recs[j])
	})
	rows := make([]dashRow, 0, dashListRows)
	for _, rec := range recs {
		if len(rows) >= dashListRows {
			break
		}
		pct := int(overrunRatio(rec) * 100)
		style := d.theme.BuildStatus.Success
		if pct > 0 {
			style = d.theme.BuildStatus.Failed
		}
		rows = append(rows, dashRow{label: buildLabel(rec), value: signedPct(pct), style: style})
	}
	return rows
}

func (d *DashboardView) absoluteRows(finished []buildregistry.Record) []dashRow {
	recs := append([]buildregistry.Record(nil), finished...)
	sort.Slice(recs, func(i, j int) bool {
		if a, b := recs[i].Build.Duration, recs[j].Build.Duration; a != b {
			return a > b
		}
		return recordLess(recs[i], recs[j])
	})
	rows := make([]dashRow, 0, dashListRows)
	for _, rec := range recs {
		if len(rows) >= dashListRows {
			break
		}
		rows = append(rows, dashRow{label: buildLabel(rec), value: formatDashDur(rec.Build.Duration), style: d.theme.BuildStatus.Running})
	}
	return rows
}

// recordLess is a stable identity ordering (job path, then build number) used
// as a tiebreak so ranked lists don't jitter when primary keys are equal.
func recordLess(a, b buildregistry.Record) bool {
	if a.JobPath != b.JobPath {
		return a.JobPath < b.JobPath
	}
	return a.Build.Number < b.Build.Number
}

func overrunRatio(rec buildregistry.Record) float64 {
	est := rec.Build.EstimatedDuration
	if est <= 0 {
		return 0
	}
	return float64(rec.Build.Duration-est) / float64(est)
}

// buildLabel is the full (URL-decoded) job path plus build number; listPane
// truncates it to the pane width.
func buildLabel(rec buildregistry.Record) string {
	return decodeName(rec.JobPath) + " #" + strconv.Itoa(rec.Build.Number)
}

func signedPct(pct int) string {
	if pct >= 0 {
		return "+" + strconv.Itoa(pct) + "%"
	}
	return strconv.Itoa(pct) + "%"
}

// projectKey drops the last path segment (branch) to group multibranch builds
// under their project; standalone jobs group under themselves.
func projectKey(jobPath string) string {
	parts := strings.Split(jobPath, "/")
	if len(parts) <= 1 {
		return jobPath
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

// formatDashDur renders a duration compactly: "45s", "3m20s", "1h04m".
func formatDashDur(dur time.Duration) string {
	dur = dur.Round(time.Second)
	if dur < 0 {
		dur = 0
	}
	h := int(dur.Hours())
	m := int(dur.Minutes()) % 60
	s := int(dur.Seconds()) % 60
	switch {
	case h > 0:
		return strconv.Itoa(h) + "h" + pad2(m) + "m"
	case m > 0:
		return strconv.Itoa(m) + "m" + pad2(s) + "s"
	default:
		return strconv.Itoa(s) + "s"
	}
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

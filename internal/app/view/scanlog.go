package view

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// ScanLogView streams a container's scan log — branch indexing on a multibranch
// project, or a folder's computation. It is the console for a run that has no
// build number: Jenkins keeps exactly one scan log per container, always the
// latest, so there is nothing to select and no history to page through.
//
// It shares the progressive-streaming loop with ConsoleView (logSource) but not
// its behaviors: test reports, artifacts and describe are build concepts a scan
// has no answer for, and offering them would be a dead end.
type ScanLogView struct {
	BaseView
	lv          widget.LogViewer
	done        bool
	pending     bool
	queued      *jmodel.QueueItem
	progressBar component.ProgressBar
	fetchStart  int
	jobPath     string
	stopDialog  widget.ConfirmDialog
}

// scanStartTickMsg re-checks whether a queued scan has started.
type scanStartTickMsg struct{}

// NewScanLogView creates a scan log stream for a container path. When the
// container's scan is still sitting in the queue the view opens in a pending
// state: Jenkins would serve the *previous* scan's log until this one starts,
// so it waits for the queue item to leave rather than showing stale output —
// the same "waiting for the run" shape the stage view has after a trigger.
func NewScanLogView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, jobPath string) *ScanLogView {
	sv := &ScanLogView{
		BaseView:    NewBaseView(t, client, store, nc, nc.Level),
		lv:          widget.NewLogViewer(t, widget.WithInternalLineCheck(widget.IsInternalLine)),
		progressBar: component.NewProgressBar(t),
		jobPath:     jobPath,
	}
	sv.pending = sv.refreshQueued()
	return sv
}

// refreshQueued re-reads the queue snapshot for this container's scan, keeping
// the waiting bar's reason ("blocked", "queued", …) current, and reports
// whether the scan is still waiting.
func (sv *ScanLogView) refreshQueued() bool {
	sv.queued = nil
	if sv.store == nil || sv.store.Queue == nil {
		return false
	}
	it, ok := sv.store.Queue.ScanFor(sv.jobPath)
	if ok {
		sv.queued = &it
	}
	return ok
}

func (sv *ScanLogView) source() logSource {
	return func(start int) (*jmodel.ProgressiveLog, error) {
		return sv.client.GetScanLogProgressive(sv.ctx, sv.jobPath, start)
	}
}

func (sv *ScanLogView) Init() tea.Cmd {
	if sv.pending {
		return tea.Batch(scanStartTick(), widget.SelectionCheckCmd())
	}
	return tea.Batch(
		progressiveFetch(sv.ctx, sv.source(), sv.fetchStart, 0),
		widget.SelectionCheckCmd(),
	)
}

// scanStartTick polls the shared queue snapshot the engine already maintains,
// so waiting costs no requests of its own.
func scanStartTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return scanStartTickMsg{} })
}

func (sv *ScanLogView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case scanStartTickMsg:
		if sv.refreshQueued() {
			return sv, scanStartTick()
		}
		sv.pending = false
		sv.applySize()
		return sv, progressiveFetch(sv.ctx, sv.source(), 0, 0)
	case consoleChunkMsg:
		sv.fetchStart = msg.nextStart
		pinned := sv.lv.IsPinnedToBottom()
		sv.lv.AppendRawLines(msg.lines)
		if pinned {
			sv.lv.ScrollToBottom()
		}
		if msg.moreData {
			// A scan that is still writing is a scan that is still running —
			// the only liveness signal Jenkins offers for one, and the reason
			// the job list does not try to show a "scanning" badge.
			return sv, progressiveFetch(sv.ctx, sv.source(), msg.nextStart, time.Second)
		}
		sv.done = true
		return sv, nil
	case consoleAbortMsg:
		return sv, nil
	case ThemeChangedMsg:
		sv.theme = msg.Theme
		sv.lv.SetTheme(msg.Theme)
		sv.progressBar.SetTheme(msg.Theme)
		return sv, nil
	case CancelBuildResultMsg:
		if msg.Err != nil {
			return sv, func() tea.Msg { return ErrorMsg(msg) }
		}
		return sv, nil
	case tea.KeyMsg:
		if sv.stopDialog.IsOpen() {
			if sv.stopDialog.Update(msg) {
				return sv, sv.stopCmd()
			}
			return sv, nil
		}
		// x stops the scan. Offered only while the stream is live, because that
		// is the only evidence Jenkins gives that a scan is running at all — it
		// publishes no status object. A finished scan simply has no x.
		if msg.String() == "x" && !sv.done && !sv.pending {
			sv.stopDialog.Open()
			return sv, nil
		}
		if handleLogScrollKey(&sv.lv, msg, true) {
			return sv, nil
		}
	}
	return sv, nil
}

func (sv *ScanLogView) stopCmd() tea.Cmd {
	client, jobPath := sv.client, sv.jobPath
	return func() tea.Msg {
		return CancelBuildResultMsg{Err: client.StopScan(context.Background(), jobPath)}
	}
}

func (sv *ScanLogView) HasPopup() bool { return sv.stopDialog.IsOpen() }

func (sv *ScanLogView) PopupView() string {
	return sv.stopDialog.View(sv.theme, "Stop Scan",
		fmt.Sprintf("Stop the running scan of %s?", decodePath(sv.jobPath)))
}

func (sv *ScanLogView) View() string {
	if sv.pending {
		width := sv.width
		if width < 1 {
			width = 1
		}
		label := "Pending"
		if sv.queued != nil {
			label = queueStateLabel(*sv.queued)
		}
		return sv.progressBar.RenderPendingTall(width, label) + "\n" +
			sv.lv.RenderVisible(false, "")
	}
	return sv.lv.RenderVisible(sv.done, "─── end of scan ───")
}

func (sv *ScanLogView) Title() string { return "Scan log" }

func (sv *ScanLogView) Breadcrumb() BreadcrumbSegment { return sv.MakeBreadcrumb("scanlog") }

func (sv *ScanLogView) ItemCount() int { return sv.lv.ItemCount() }

func (sv *ScanLogView) Commands() []command.Command { return nil }

func (sv *ScanLogView) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{component.Nav("esc", "back")}
	// While the scan is still queued there is nothing running to stop — the
	// queued item is cancelled with x from the job list, by queue id.
	if !sv.done && !sv.pending {
		sc = append(sc, component.ActionRanked("x", "stop scan", rankActionCancel))
	}
	return sc
}

func (sv *ScanLogView) SetSize(width, height int) {
	sv.BaseView.SetSize(width, height)
	sv.applySize()
}

// applySize gives the log viewer the room the waiting bar is not using. It is
// re-applied when the scan starts, since the bar disappears at that moment.
func (sv *ScanLogView) applySize() {
	height := sv.height
	if sv.pending {
		// 3-line bar + separator line.
		height -= stageBarHeight + 1
	}
	if height < 1 {
		height = 1
	}
	sv.lv.SetSize(sv.width, height)
}

// ApplySearch/SearchQuery make the scan log searchable like any other log.
func (sv *ScanLogView) ApplySearch(pattern string) tea.Cmd {
	sv.lv.ApplySearch(pattern)
	return nil
}

func (sv *ScanLogView) SearchQuery() string { return sv.lv.SearchQuery() }

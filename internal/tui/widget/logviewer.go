package widget

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// CopyFlashMsg is emitted when a clipboard write completes.
// IsSel distinguishes selection copy (C) from log copy (c).
type CopyFlashMsg struct{ IsSel bool }

// CopyFlashDoneMsg is emitted after the 1-second flash expires.
type CopyFlashDoneMsg struct{ IsSel bool }

// SelectionCheckMsg carries the cleaned primary selection from the background poll.
type SelectionCheckMsg struct {
	Text      string
	LineCount int
}

var (
	urlRe = regexp.MustCompile(`https?://[^\s]+`)
	// Covers CSI sequences (\x1b[...X) and other ESC two-char sequences.
	ansiRe = regexp.MustCompile(`\x1b(?:[@-Z\\-_]|\[[0-9;?]*[@-~])`)
	// Matches a Jenkins XStream object reference (serialised Java object).
	xstreamRe = regexp.MustCompile(`ha:////[A-Za-z0-9+/=]+`)
	// Matches a line that consists ENTIRELY of one or more xstream refs (plus optional whitespace).
	xstreamOnlyRe = regexp.MustCompile(`^\s*(ha:////[A-Za-z0-9+/=]+\s*)+$`)
	errorRe       = regexp.MustCompile(`(?i)\b(error|errors|fatal|exception|panic|failed|failure)\b`)
	warningRe     = regexp.MustCompile(`(?i)\b(warning|warnings|warn|deprecated|deprecation)\b`)
	infoRe        = regexp.MustCompile(`(?i)\b(info|trace|fine|debug)\b`)
)

// LineKind classifies a log line for colour/navigation purposes.
type LineKind uint8

const (
	LineKindNormal  LineKind = iota
	LineKindWarning          // yellow
	LineKindError            // red
)

// DisplayLine pairs the text of a display row with its rendering style.
// All wrapped chunks of one raw log line share the same style so that dim
// lines remain dim across every continuation chunk.
// Src is the full source line; SrcOffset is the byte offset of Text within Src.
// In no-wrap mode Text == Src and SrcOffset == 0.
// In wrap mode each chunk carries Src/SrcOffset so OSC 8 links span chunk boundaries.
type DisplayLine struct {
	Text      string
	Src       string
	SrcOffset int
	Dim       bool
	Kind      LineKind
	RawIdx    int
}

// LineRenderFn renders one display line. Set via WithLineRenderer when callers
// need custom rendering (e.g. Groovy syntax highlighting).
type LineRenderFn func(dl DisplayLine, wrap bool, hOffset, width int, searchRe *regexp.Regexp, t theme.Theme, isCurrent bool) string

// ClassifyFn supplies a LineKind for a raw line. Set via WithClassifier when
// classification comes from external data (e.g. validation issues by line number).
type ClassifyFn func(rawIdx int, raw string) LineKind

// NavItemsFn returns the n/N navigation sequence as rawIdx values in display
// order. Duplicates allowed — multiple items can target the same line.
type NavItemsFn func() []int

// SearchResultMsg carries results of a background search scan.
// Only the LogViewer that initiated the search will apply it (pointer + version check).
type SearchResultMsg struct {
	lv          *LogViewer
	version     int
	snapshotLen int
	indices     []int
}

// LogViewer holds all shared state and logic for rendering scrollable log output.
type LogViewer struct {
	rawLines           []string
	lines              []DisplayLine
	offset             int
	hOffset            int // rune columns; only active when !wrap
	width, height      int
	wrap               bool
	showInternal       bool
	searchQuery        string
	searchRe           *regexp.Regexp
	searchMatchLines   []int
	currentMatchLine   int
	searchVersion      int
	searchCancel       context.CancelFunc
	searching          bool
	errCount           int
	warnCount          int
	highlightedLines   []int
	highlightErrors    bool
	currentNavIdx      int
	lastVisibleKind    LineKind
	selectionText      string
	selectionLineCount int
	selectionInLog     bool
	lastNavWrapped     bool
	theme              theme.Theme
	renderFn           LineRenderFn
	classifyFn         ClassifyFn
	navItemsFn         NavItemsFn
}

// Option configures a LogViewer at construction time.
type Option func(*LogViewer)

// WithClassifier installs a caller-supplied classifier.
func WithClassifier(fn ClassifyFn) Option { return func(lv *LogViewer) { lv.classifyFn = fn } }

// WithNavigationItems installs a caller-supplied n/N navigation sequence.
func WithNavigationItems(fn NavItemsFn) Option { return func(lv *LogViewer) { lv.navItemsFn = fn } }

// WithLineRenderer installs a caller-supplied line renderer.
func WithLineRenderer(fn LineRenderFn) Option { return func(lv *LogViewer) { lv.renderFn = fn } }

// NewLogViewer constructs a LogViewer in its initial empty state.
func NewLogViewer(t theme.Theme, opts ...Option) LogViewer {
	lv := LogViewer{theme: t, highlightErrors: true, currentMatchLine: -1, currentNavIdx: -1}
	for _, o := range opts {
		o(&lv)
	}
	return lv
}

// SetTheme updates the active theme without recomputing display lines.
func (lv *LogViewer) SetTheme(t theme.Theme) { lv.theme = t }

// Wrap reports whether soft-wrap is enabled.
func (lv *LogViewer) Wrap() bool { return lv.wrap }

// HighlightErrors reports whether error/warning highlighting is enabled.
func (lv *LogViewer) HighlightErrors() bool { return lv.highlightErrors }

// ShowInternal reports whether Jenkins-internal lines are visible.
func (lv *LogViewer) ShowInternal() bool { return lv.showInternal }

// HasSearch reports whether a search pattern is currently active.
func (lv *LogViewer) HasSearch() bool { return lv.searchRe != nil }

// SelectionInLog reports whether the cached primary selection matches log content.
func (lv *LogViewer) SelectionInLog() bool { return lv.selectionInLog }

// classify returns the LineKind for a raw line, using classifyFn when set.
func (lv *LogViewer) classify(rawIdx int, raw string) LineKind {
	if lv.classifyFn != nil {
		return lv.classifyFn(rawIdx, raw)
	}
	return classifyLine(raw)
}

// CurrentNavIdx returns the active n/N navigation index, or -1 when none is
// selected.
func (lv *LogViewer) CurrentNavIdx() int { return lv.currentNavIdx }

// NavItemsCount returns the number of n/N navigation targets currently
// resolved (one entry per item, including duplicates).
func (lv *LogViewer) NavItemsCount() int { return len(lv.highlightedLines) }

// contentHeight returns the number of log lines that fit in the view.
func (lv *LogViewer) contentHeight() int { return lv.height }

// RenderVisible returns the visible window as a single newline-joined string,
// padded to height. When done is true and there is room left, endMarker is
// rendered with the dim log style as a trailing "this stream ended" line.
func (lv *LogViewer) RenderVisible(done bool, endMarker string) string {
	if lv.height <= 0 {
		return ""
	}
	ch := lv.contentHeight()
	end := min(lv.offset+ch, len(lv.lines))
	visible := lv.lines[lv.offset:end]
	rows := make([]string, 0, lv.height)
	for i, dl := range visible {
		rows = append(rows, lv.renderLineAt(dl, lv.offset+i))
	}
	if done && len(rows) < lv.height && endMarker != "" {
		rows = append(rows, lv.theme.Log.Dim.Render(endMarker))
	}
	for len(rows) < lv.height {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
}

// ScrollInfo returns the current scroll position for use by the border scrollbar.
func (lv *LogViewer) ScrollInfo() ScrollInfo {
	markers := make([]ScrollMarker, 0, len(lv.searchMatchLines)+len(lv.highlightedLines))
	for _, idx := range lv.searchMatchLines {
		markers = append(markers, ScrollMarker{Line: idx, Kind: ScrollMarkerSearch})
	}
	seenMarker := make(map[int]struct{}, len(lv.highlightedLines))
	for _, idx := range lv.highlightedLines {
		if _, ok := seenMarker[idx]; ok {
			continue
		}
		seenMarker[idx] = struct{}{}
		kind := ScrollMarkerWarning
		if idx < len(lv.lines) && lv.lines[idx].Kind == LineKindError {
			kind = ScrollMarkerError
		}
		markers = append(markers, ScrollMarker{Line: idx, Kind: kind})
	}
	return ScrollInfo{
		Offset:     lv.offset,
		TotalLines: len(lv.lines),
		ViewHeight: lv.contentHeight(),
		Markers:    markers,
	}
}

// SetRawLines replaces the raw line buffer and rebuilds display lines.
func (lv *LogViewer) SetRawLines(lines []string) {
	lv.rawLines = lines
	lv.recomputeLines()
}

// Recompute rebuilds display lines from the current raw buffer. Call after
// external state used by classifyFn/navItemsFn changes.
func (lv *LogViewer) Recompute() { lv.recomputeLines() }

// recomputeState snapshots state that must survive a rebuild.
type recomputeState struct {
	atBottom      bool
	savedMatchIdx int
	savedNavIdx   int
	savedRawIdx   int
}

// recomputeLines rebuilds display lines from rawLines using current settings.
// Preserves bottom-pin: if we were at the bottom, stay at the bottom.
// Preserves the active search match position by match-list index.
func (lv *LogViewer) recomputeLines() {
	lv.cancelActiveSearch()
	state := lv.snapshotRecomputeState()
	firstDisplayIdx := lv.rebuildDisplayLines()
	lv.appendNavItemHighlights(firstDisplayIdx)
	lv.restoreScrollPosition(state)
}

// cancelActiveSearch tears down any in-flight background search scan.
func (lv *LogViewer) cancelActiveSearch() {
	if lv.searchCancel != nil {
		lv.searchCancel()
		lv.searchCancel = nil
		lv.searchVersion++
		lv.searching = false
	}
}

// snapshotRecomputeState captures the cursor/anchor info needed to restore
// the viewport after a rebuild.
func (lv *LogViewer) snapshotRecomputeState() recomputeState {
	state := recomputeState{
		atBottom:      len(lv.lines) == 0 || lv.offset >= max(0, len(lv.lines)-lv.contentHeight()),
		savedMatchIdx: -1,
		savedNavIdx:   lv.currentNavIdx,
		savedRawIdx:   -1,
	}
	if lv.currentMatchLine >= 0 {
		for i, line := range lv.searchMatchLines {
			if line == lv.currentMatchLine {
				state.savedMatchIdx = i
				break
			}
		}
	}
	if !state.atBottom && lv.offset < len(lv.lines) {
		state.savedRawIdx = lv.lines[lv.offset].RawIdx
	}
	return state
}

// rebuildDisplayLines wipes the display state and re-derives lines, match
// indices, highlighted lines, and error/warning counts from the raw buffer.
// Returns a map of raw-line index → first display-line index, used to map
// caller-supplied navigation items to display rows.
func (lv *LogViewer) rebuildDisplayLines() map[int]int {
	lv.lines = nil
	lv.errCount, lv.warnCount = 0, 0
	lv.highlightedLines = lv.highlightedLines[:0]
	lv.searchMatchLines = lv.searchMatchLines[:0]
	lv.currentMatchLine = -1
	lv.currentNavIdx = -1
	firstDisplayIdx := map[int]int{}
	prevKind := LineKindNormal
	useNavItems := lv.navItemsFn != nil
	for rawIdx, raw := range lv.rawLines {
		internal := isInternalLine(raw)
		if !lv.showInternal && internal {
			continue
		}
		dl := lv.toDisplayLines(rawIdx, raw)
		for i := range dl {
			dl[i].RawIdx = rawIdx
		}
		firstDisplayIdx[rawIdx] = len(lv.lines)
		if lv.searchRe != nil && lv.searchRe.MatchString(raw) {
			lv.searchMatchLines = append(lv.searchMatchLines, len(lv.lines))
		}
		prevKind = lv.recordLineKind(rawIdx, raw, internal, prevKind, useNavItems)
		lv.lines = append(lv.lines, dl...)
	}
	lv.lastVisibleKind = prevKind
	return firstDisplayIdx
}

// recordLineKind classifies a non-internal line, updates error/warning counts
// and (when no external nav source is set) the highlightedLines list. Returns
// the prevKind value to thread through the next iteration.
func (lv *LogViewer) recordLineKind(rawIdx int, raw string, internal bool, prevKind LineKind, useNavItems bool) LineKind {
	if internal {
		return LineKindNormal
	}
	kind := lv.classify(rawIdx, raw)
	if lv.highlightErrors && kind != LineKindNormal && (lv.classifyFn != nil || kind != prevKind) {
		switch kind {
		case LineKindError:
			lv.errCount++
		case LineKindWarning:
			lv.warnCount++
		}
		if !useNavItems {
			lv.highlightedLines = append(lv.highlightedLines, len(lv.lines))
		}
	}
	return kind
}

// appendNavItemHighlights resolves caller-supplied nav items (rawIdx → first
// display line) into the highlightedLines slice when navItemsFn is set.
func (lv *LogViewer) appendNavItemHighlights(firstDisplayIdx map[int]int) {
	if lv.navItemsFn == nil {
		return
	}
	for _, rawIdx := range lv.navItemsFn() {
		if disp, ok := firstDisplayIdx[rawIdx]; ok {
			lv.highlightedLines = append(lv.highlightedLines, disp)
		}
	}
}

// restoreScrollPosition repositions the viewport after a rebuild, preferring
// (in order) a saved search match, an active search jumping to its first
// match, a saved nav-highlight cursor, bottom-pin, a raw-line anchor, or the
// previous offset clamped to the new maximum.
func (lv *LogViewer) restoreScrollPosition(state recomputeState) {
	newMax := max(0, len(lv.lines)-lv.contentHeight())
	switch {
	case state.savedMatchIdx >= 0 && len(lv.searchMatchLines) > 0:
		idx := state.savedMatchIdx
		if idx >= len(lv.searchMatchLines) {
			idx = len(lv.searchMatchLines) - 1
		}
		lv.currentMatchLine = lv.searchMatchLines[idx]
		lv.offset = min(lv.currentMatchLine, newMax)
	case lv.searchRe != nil && len(lv.searchMatchLines) > 0:
		lv.nextSearchMatch(true)
	case state.savedNavIdx >= 0 && len(lv.highlightedLines) > 0:
		idx := state.savedNavIdx
		if idx >= len(lv.highlightedLines) {
			idx = len(lv.highlightedLines) - 1
		}
		lv.currentNavIdx = idx
		lv.offset = min(lv.highlightedLines[idx], newMax)
	case state.atBottom:
		lv.offset = newMax
	case state.savedRawIdx >= 0:
		lv.offset = min(lv.findDisplayLineForRaw(state.savedRawIdx), newMax)
	default:
		lv.offset = min(lv.offset, newMax)
	}
}

// findDisplayLineForRaw returns the first display-line index whose RawIdx
// matches the given raw line, or 0 when no such line exists.
func (lv *LogViewer) findDisplayLineForRaw(rawIdx int) int {
	for i, dl := range lv.lines {
		if dl.RawIdx == rawIdx {
			return i
		}
	}
	return 0
}

// toDisplayLines converts one raw log line to one or more display rows.
func (lv *LogViewer) toDisplayLines(rawIdx int, raw string) []DisplayLine {
	dim := isInternalLine(raw)
	kind := LineKindNormal
	if !dim {
		kind = lv.classify(rawIdx, raw)
	}
	if !lv.wrap || lv.width <= 0 {
		return []DisplayLine{{Text: raw, Src: raw, Dim: dim, Kind: kind}}
	}
	if lipgloss.Width(raw) <= lv.width {
		return []DisplayLine{{Text: raw, Src: raw, Dim: dim, Kind: kind}}
	}
	var chunks []DisplayLine
	remaining := raw
	for len(remaining) > 0 {
		chunk, _ := TruncateToColumns(remaining, lv.width)
		srcOffset := len(raw) - len(remaining)
		chunks = append(chunks, DisplayLine{Text: chunk, Src: raw, SrcOffset: srcOffset, Dim: dim, Kind: kind})
		remaining = remaining[len(chunk):]
	}
	return chunks
}

// Badge renders the error/warning counter chip shown in panel border-titles.
func (lv *LogViewer) Badge() string {
	return renderErrWarnBadge(lv.theme, lv.warnCount, lv.errCount)
}

// RenderErrWarnBadge formats the warning/error counter chip. Exposed so other
// renderers can show consistent counts.
func RenderErrWarnBadge(t theme.Theme, warnCount, errCount int) string {
	return renderErrWarnBadge(t, warnCount, errCount)
}

func renderErrWarnBadge(t theme.Theme, warnCount, errCount int) string {
	if warnCount+errCount == 0 {
		return ""
	}
	warnIcon := t.Icons.Warning
	if warnIcon == "" {
		warnIcon = "⚠"
	}
	errIcon := t.Icons.Error
	if errIcon == "" {
		errIcon = "✕"
	}
	var parts []string
	if warnCount > 0 {
		parts = append(parts, t.Log.Warning.Render(fmt.Sprintf("%s %d", warnIcon, warnCount)))
	}
	if errCount > 0 {
		parts = append(parts, t.Log.Error.Render(fmt.Sprintf("%s %d", errIcon, errCount)))
	}
	return strings.Join(parts, "  ")
}

// CopyLogCmd copies the visible log lines to the clipboard.
func (lv *LogViewer) CopyLogCmd() tea.Cmd {
	var lines []string
	for _, raw := range lv.rawLines {
		if !lv.showInternal && isInternalLine(raw) {
			continue
		}
		lines = append(lines, raw)
	}
	return func() tea.Msg {
		writeToClipboard(strings.Join(lines, "\n"))
		return CopyFlashMsg{IsSel: false}
	}
}

// CopySelectionCmd copies the cached primary selection text to the clipboard.
func (lv *LogViewer) CopySelectionCmd() tea.Cmd {
	text := lv.selectionText
	return func() tea.Msg {
		if strings.TrimSpace(text) != "" {
			writeToClipboard(text)
		}
		return CopyFlashMsg{IsSel: true}
	}
}

// SelectionCheckCmd reads the primary selection every 300 ms, cleans it, and
// returns a SelectionCheckMsg. Views should re-issue it on each receipt.
func SelectionCheckCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(300 * time.Millisecond)
		cleaned := cleanBorderChars(readPrimarySelection())
		count := 0
		for _, l := range strings.Split(cleaned, "\n") {
			if strings.TrimSpace(l) != "" {
				count++
			}
		}
		return SelectionCheckMsg{Text: cleaned, LineCount: count}
	}
}

// RecordSelection updates the cached primary selection state from a poll result.
func (lv *LogViewer) RecordSelection(text string, lineCount int) {
	lv.selectionText = text
	lv.selectionLineCount = lineCount
	lv.selectionInLog = lv.checkSelectionInLog(text)
}

func (lv *LogViewer) checkSelectionInLog(cleaned string) bool {
	var needle string
	for _, l := range strings.Split(cleaned, "\n") {
		if s := strings.TrimSpace(l); s != "" {
			needle = s
			break
		}
	}
	if needle == "" {
		return false
	}
	for _, raw := range lv.rawLines {
		if strings.Contains(raw, needle) {
			return true
		}
	}
	return false
}

// LogLabel returns the "copy all [N]" shortcut label.
func (lv *LogViewer) LogLabel() string {
	count := len(lv.rawLines)
	if !lv.showInternal {
		count = 0
		for _, raw := range lv.rawLines {
			if !isInternalLine(raw) {
				count++
			}
		}
	}
	return fmt.Sprintf("copy all [%d]", count)
}

// SelLabel returns the "copy sel [N]" shortcut label.
func (lv *LogViewer) SelLabel() string {
	return fmt.Sprintf("copy sel [%d]", lv.selectionLineCount)
}

func readPrimarySelection() string {
	if exec.Command("wl-paste", "--primary", "--list-types").Run() == nil {
		if b, err := exec.Command("wl-paste", "--primary", "--no-newline").Output(); err == nil {
			return string(b)
		}
	}
	if exec.Command("xclip", "-selection", "primary", "-t", "TARGETS", "-o").Run() == nil {
		if b, err := exec.Command("xclip", "-selection", "primary", "-o").Output(); err == nil {
			return string(b)
		}
	}
	if b, err := exec.Command("xsel", "--primary", "--output").Output(); err == nil {
		return string(b)
	}
	return ""
}

func writeToClipboard(text string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		fmt.Fprintf(tty, "\x1b]52;c;%s\x07", encoded)
		tty.Close()
	}
}

func cleanBorderChars(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimPrefix(line, "│")
		line = strings.TrimRight(line, " │")
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// CopyFlashTimer returns a cmd that fires CopyFlashDoneMsg after 1 second.
func CopyFlashTimer(isSel bool) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return CopyFlashDoneMsg{IsSel: isSel}
	}
}

// NextHighlight advances the n/N navigation cursor. When a search is active,
// navigates to the next/previous search match; otherwise navigates to the next
// error/warning highlight.
func (lv *LogViewer) NextHighlight(forward bool) {
	if lv.searchRe != nil {
		lv.nextSearchMatch(forward)
		return
	}
	if !lv.highlightErrors || len(lv.highlightedLines) == 0 {
		return
	}
	lv.lastNavWrapped = false
	maxOff := max(0, len(lv.lines)-lv.contentHeight())
	next, wrapped := lv.pickNextHighlight(forward)
	lv.lastNavWrapped = wrapped
	lv.currentNavIdx = next
	lv.offset = min(lv.highlightedLines[next], maxOff)
}

// pickNextHighlight returns the next highlighted-line index in the chosen
// direction along with whether wrap-around occurred. Assumes
// len(lv.highlightedLines) > 0.
func (lv *LogViewer) pickNextHighlight(forward bool) (int, bool) {
	if forward {
		return lv.pickHighlightForward()
	}
	return lv.pickHighlightBackward()
}

func (lv *LogViewer) pickHighlightForward() (int, bool) {
	if lv.currentNavIdx >= 0 {
		next := lv.currentNavIdx + 1
		if next >= len(lv.highlightedLines) {
			return 0, true
		}
		return next, false
	}
	for i, idx := range lv.highlightedLines {
		if idx > lv.offset {
			return i, false
		}
	}
	return 0, true
}

func (lv *LogViewer) pickHighlightBackward() (int, bool) {
	if lv.currentNavIdx >= 0 {
		next := lv.currentNavIdx - 1
		if next < 0 {
			return len(lv.highlightedLines) - 1, true
		}
		return next, false
	}
	for i := len(lv.highlightedLines) - 1; i >= 0; i-- {
		if lv.highlightedLines[i] < lv.offset {
			return i, false
		}
	}
	return len(lv.highlightedLines) - 1, true
}

func (lv *LogViewer) nextSearchMatch(forward bool) {
	lv.lastNavWrapped = false
	if len(lv.searchMatchLines) == 0 {
		return
	}
	maxOff := max(0, len(lv.lines)-lv.contentHeight())
	if forward {
		for _, idx := range lv.searchMatchLines {
			if idx > lv.currentMatchLine {
				lv.currentMatchLine = idx
				lv.offset = min(idx, maxOff)
				return
			}
		}
		lv.lastNavWrapped = true
		lv.currentMatchLine = lv.searchMatchLines[0]
		lv.offset = min(lv.searchMatchLines[0], maxOff)
	} else {
		for i := len(lv.searchMatchLines) - 1; i >= 0; i-- {
			if lv.searchMatchLines[i] < lv.currentMatchLine {
				lv.currentMatchLine = lv.searchMatchLines[i]
				lv.offset = min(lv.searchMatchLines[i], maxOff)
				return
			}
		}
		lv.lastNavWrapped = true
		last := lv.searchMatchLines[len(lv.searchMatchLines)-1]
		lv.currentMatchLine = last
		lv.offset = min(last, maxOff)
	}
}

// ErrorNavActive reports whether error/warning navigation mode is active.
func (lv *LogViewer) ErrorNavActive() bool {
	return lv.highlightErrors && len(lv.highlightedLines) > 0
}

// NavigationPositionInfo returns a position indicator for active navigation.
func (lv *LogViewer) NavigationPositionInfo() string {
	if lv.searchRe != nil {
		total := len(lv.searchMatchLines)
		if total == 0 {
			if lv.searching {
				return "⟳"
			}
			return "[0]"
		}
		if lv.currentMatchLine < 0 {
			return fmt.Sprintf("[%d]", total)
		}
		for i, line := range lv.searchMatchLines {
			if line == lv.currentMatchLine {
				if lv.lastNavWrapped {
					return fmt.Sprintf("[%d/%d ↩]", i+1, total)
				}
				return fmt.Sprintf("[%d/%d]", i+1, total)
			}
		}
		return ""
	}
	if !lv.highlightErrors || len(lv.highlightedLines) == 0 || lv.currentNavIdx < 0 {
		return ""
	}
	if lv.lastNavWrapped {
		return fmt.Sprintf("[%d/%d ↩]", lv.currentNavIdx+1, len(lv.highlightedLines))
	}
	return fmt.Sprintf("[%d/%d]", lv.currentNavIdx+1, len(lv.highlightedLines))
}

// HasActiveNavigation reports whether a navigation match is currently selected.
func (lv *LogViewer) HasActiveNavigation() bool {
	return lv.currentMatchLine >= 0 || lv.currentNavIdx >= 0
}

// ClearActiveNavigation drops the active match selection without clearing the
// search query or error highlighting.
func (lv *LogViewer) ClearActiveNavigation() {
	lv.currentMatchLine = -1
	lv.currentNavIdx = -1
	lv.lastNavWrapped = false
}

func (lv *LogViewer) renderLineAt(dl DisplayLine, absIdx int) string {
	isCurrent := lv.searchRe != nil && absIdx == lv.currentMatchLine
	isCurrentHighlight := false
	if lv.searchRe == nil && lv.highlightErrors && lv.currentNavIdx >= 0 && lv.currentNavIdx < len(lv.highlightedLines) {
		activeLine := lv.highlightedLines[lv.currentNavIdx]
		if activeLine >= 0 && activeLine < len(lv.lines) {
			selectedKind := lv.lines[activeLine].Kind
			blockEnd := activeLine + 1
			for blockEnd < len(lv.lines) && lv.lines[blockEnd].Kind == selectedKind {
				blockEnd++
			}
			isCurrentHighlight = absIdx >= activeLine && absIdx < blockEnd
		}
	}
	if !lv.highlightErrors {
		dl.Kind = LineKindNormal
	}
	if lv.renderFn != nil {
		return lv.renderFn(dl, lv.wrap, lv.hOffset, lv.width, lv.searchRe, lv.theme, isCurrent || isCurrentHighlight)
	}
	return renderLogLine(dl, lv.wrap, lv.hOffset, lv.width, lv.searchRe, lv.theme, isCurrent || isCurrentHighlight)
}

// RenderRows returns the visible lines rendered and padded to lv.height rows.
func (lv *LogViewer) RenderRows() []string {
	ch := lv.contentHeight()
	end := min(lv.offset+ch, len(lv.lines))
	visible := lv.lines[lv.offset:end]
	rows := make([]string, 0, lv.height)
	for i, dl := range visible {
		rows = append(rows, lv.renderLineAt(dl, lv.offset+i))
	}
	for len(rows) < lv.height {
		rows = append(rows, "")
	}
	return rows
}

// SetSize updates the viewport dimensions, recomputing display lines when the
// width changes in wrap mode.
func (lv *LogViewer) SetSize(w, h int) {
	needRecompute := lv.wrap && w != lv.width && len(lv.rawLines) > 0
	lv.width = w
	lv.height = h
	if needRecompute {
		lv.recomputeLines()
	} else {
		lv.offset = min(lv.offset, max(0, len(lv.lines)-lv.contentHeight()))
	}
}

// --- Scrolling primitives ---

func (lv *LogViewer) maxOffset() int {
	return max(0, len(lv.lines)-lv.contentHeight())
}

// ScrollByLines moves the viewport by delta lines (negative = up).
func (lv *LogViewer) ScrollByLines(delta int) {
	lv.offset = clamp(lv.offset+delta, 0, lv.maxOffset())
}

// ScrollByPages moves the viewport by delta pages (page = height-1 lines).
func (lv *LogViewer) ScrollByPages(delta int) {
	pageSize := max(1, lv.height-1)
	lv.ScrollByLines(delta * pageSize)
}

// ScrollByCols shifts the horizontal offset by delta columns (no-op when wrap).
func (lv *LogViewer) ScrollByCols(delta int) {
	if lv.wrap {
		return
	}
	lv.hOffset = max(0, lv.hOffset+delta)
}

// ScrollToTop snaps the viewport to the first line.
func (lv *LogViewer) ScrollToTop() { lv.offset = 0 }

// ScrollToBottom snaps the viewport to the last page.
func (lv *LogViewer) ScrollToBottom() { lv.offset = lv.maxOffset() }

// IsPinnedToBottom reports whether the viewport is at the bottom.
func (lv *LogViewer) IsPinnedToBottom() bool { return lv.offset >= lv.maxOffset() }

// ToggleWrap flips soft-wrap; resets horizontal offset when enabling wrap.
func (lv *LogViewer) ToggleWrap() {
	lv.wrap = !lv.wrap
	if lv.wrap {
		lv.hOffset = 0
	}
	lv.recomputeLines()
}

// ToggleHighlightErrors flips error/warning highlighting and rebuilds display lines.
func (lv *LogViewer) ToggleHighlightErrors() {
	lv.highlightErrors = !lv.highlightErrors
	lv.currentNavIdx = -1
	lv.recomputeLines()
}

// ToggleShowInternal flips visibility of Jenkins-internal lines.
func (lv *LogViewer) ToggleShowInternal() {
	lv.showInternal = !lv.showInternal
	lv.recomputeLines()
}

// ApplySearch sets the active search pattern and launches a background scan.
func (lv *LogViewer) ApplySearch(pattern string) tea.Cmd {
	lv.searchQuery = pattern
	lv.searchRe = CompileSearchRegex(pattern)
	lv.currentMatchLine = -1
	lv.searchMatchLines = lv.searchMatchLines[:0]
	lv.lastNavWrapped = false
	if lv.searchCancel != nil {
		lv.searchCancel()
		lv.searchCancel = nil
	}
	if lv.searchRe == nil || len(lv.rawLines) == 0 {
		lv.searching = false
		return nil
	}
	lv.searching = true
	lv.searchVersion++
	ctx, cancel := context.WithCancel(context.Background())
	lv.searchCancel = cancel
	snapshot := lv.rawLines
	snapshotLen := len(snapshot)
	re := lv.searchRe
	version := lv.searchVersion
	return func() tea.Msg {
		var indices []int
		for i, raw := range snapshot[:snapshotLen] {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			if re.MatchString(raw) {
				indices = append(indices, i)
			}
		}
		return SearchResultMsg{lv: lv, version: version, snapshotLen: snapshotLen, indices: indices}
	}
}

// ApplySearchResult applies a completed background search scan.
func (lv *LogViewer) ApplySearchResult(msg SearchResultMsg) {
	if msg.lv != lv || msg.version != lv.searchVersion {
		return
	}
	var tail []int
	for _, idx := range lv.searchMatchLines {
		if idx < len(lv.lines) && lv.lines[idx].RawIdx >= msg.snapshotLen {
			tail = append(tail, idx)
		}
	}
	matchSet := make(map[int]struct{}, len(msg.indices))
	for _, ri := range msg.indices {
		matchSet[ri] = struct{}{}
	}
	seen := make(map[int]struct{}, len(msg.indices))
	lv.searchMatchLines = lv.searchMatchLines[:0]
	for i, dl := range lv.lines {
		if dl.RawIdx >= msg.snapshotLen {
			break
		}
		if _, ok := matchSet[dl.RawIdx]; ok {
			if _, already := seen[dl.RawIdx]; !already {
				lv.searchMatchLines = append(lv.searchMatchLines, i)
				seen[dl.RawIdx] = struct{}{}
			}
		}
	}
	lv.searchMatchLines = append(lv.searchMatchLines, tail...)
	lv.searching = false
	lv.currentMatchLine = -1
	if len(lv.searchMatchLines) > 0 {
		lv.nextSearchMatch(true)
	}
}

// SearchQuery returns the current search pattern string.
func (lv *LogViewer) SearchQuery() string { return lv.searchQuery }

// ItemCount returns the number of display lines.
func (lv *LogViewer) ItemCount() int { return len(lv.lines) }

// AppendRawLines classifies and appends new raw lines incrementally.
func (lv *LogViewer) AppendRawLines(newLines []string) {
	prevKind := lv.lastVisibleKind
	for _, raw := range newLines {
		rawIdx := len(lv.rawLines)
		lv.rawLines = append(lv.rawLines, raw)
		internal := isInternalLine(raw)
		if !lv.showInternal && internal {
			continue
		}
		dl := lv.toDisplayLines(rawIdx, raw)
		for i := range dl {
			dl[i].RawIdx = rawIdx
		}
		if lv.searchRe != nil && lv.searchRe.MatchString(raw) {
			lv.searchMatchLines = append(lv.searchMatchLines, len(lv.lines))
		}
		if !internal {
			kind := lv.classify(rawIdx, raw)
			if lv.highlightErrors && kind != LineKindNormal && (lv.classifyFn != nil || kind != prevKind) {
				switch kind {
				case LineKindError:
					lv.errCount++
				case LineKindWarning:
					lv.warnCount++
				}
				if lv.navItemsFn == nil {
					lv.highlightedLines = append(lv.highlightedLines, len(lv.lines))
				}
			}
			prevKind = kind
		} else {
			prevKind = LineKindNormal
		}
		lv.lines = append(lv.lines, dl...)
	}
	lv.lastVisibleKind = prevKind
}

// --- Package-level utility functions ---

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClassifyContentLine is the default content-regex classifier — flags lines
// containing error/warning keywords. Exported so other render code (e.g. the
// matrix view) can share the same heuristic.
func ClassifyContentLine(raw string) LineKind { return classifyLine(raw) }

func classifyLine(raw string) LineKind {
	if infoRe.MatchString(raw) {
		return LineKindNormal
	}
	if warningRe.MatchString(raw) {
		return LineKindWarning
	}
	if errorRe.MatchString(raw) {
		return LineKindError
	}
	return LineKindNormal
}

// IsInternalLine reports whether a line is Jenkins-internal noise that should
// be hidden by default ([Pipeline] bookkeeping lines).
func IsInternalLine(line string) bool { return isInternalLine(line) }

func isInternalLine(line string) bool {
	return strings.HasPrefix(line, "[Pipeline]")
}

// SplitLogLines splits raw progressive-log text into individual lines, cleaning
// each one (CR stripped, ANSI escapes removed, XStream refs dropped/stripped).
func SplitLogLines(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}

	result := make([]string, 0, len(parts))
	for _, line := range parts {
		line = strings.TrimRight(line, "\r")
		line = strings.ReplaceAll(line, "\t", "    ")
		line = ansiRe.ReplaceAllString(line, "")

		if xstreamOnlyRe.MatchString(line) {
			continue
		}

		line = strings.TrimRight(xstreamRe.ReplaceAllString(line, ""), " \t")
		result = append(result, line)
	}
	return result
}

// columnWidth returns the visual terminal column width of a single rune.
func columnWidth(r rune) int {
	return lipgloss.Width(string(r))
}

// TruncateToColumns returns the longest prefix of line that fits within maxCols
// visual terminal columns, plus whether truncation occurred. Input must be ANSI-free.
func TruncateToColumns(line string, maxCols int) (string, bool) {
	cols := 0
	for i, r := range line {
		w := columnWidth(r)
		if cols+w > maxCols {
			return line[:i], true
		}
		cols += w
	}
	return line, false
}

// SkipColumnsBytes skips the first skipCols visual columns and returns the
// remaining text together with its byte offset inside the original string.
func SkipColumnsBytes(line string, skipCols int) (string, int) {
	if skipCols <= 0 {
		return line, 0
	}
	cols := 0
	for i, r := range line {
		if cols >= skipCols {
			return line[i:], i
		}
		cols += columnWidth(r)
	}
	return "", len(line)
}

// SkipColumns returns line with the first skipCols visual columns removed.
func SkipColumns(line string, skipCols int) string {
	s, _ := SkipColumnsBytes(line, skipCols)
	return s
}

// ApplyOSC8 renders visLine in normal colour with OSC 8 hyperlinks for each URL.
// Exposed so preview renderers can share the framework's link emission.
func ApplyOSC8(visLine, fullText string, byteOffset int, t theme.Theme) string {
	return applyOSC8(visLine, fullText, byteOffset, t)
}

// applyOSC8 renders visLine in normal colour with OSC 8 hyperlinks for each URL.
func applyOSC8(visLine, fullText string, byteOffset int, t theme.Theme) string {
	locs := urlRe.FindAllStringIndex(fullText, -1)
	if len(locs) == 0 {
		return t.Log.Normal.Render(visLine)
	}
	normalAnsi := t.Log.Normal.Render("")
	normalEsc := "\x1b[38;5;252m"
	if len(normalAnsi) > 4 {
		if idx := strings.Index(normalAnsi, "\x1b[0m"); idx > 0 {
			normalEsc = normalAnsi[:idx]
		}
	}
	const reset = "\x1b[0m"
	visLen := len(visLine)
	var b strings.Builder
	b.WriteString(normalEsc)
	prev := 0
	for _, loc := range locs {
		start := loc[0] - byteOffset
		end := loc[1] - byteOffset
		if end <= 0 {
			continue
		}
		if start >= visLen {
			break
		}
		fullURL := fullText[loc[0]:loc[1]]
		visStart := max(start, 0)
		visEnd := min(end, visLen)
		if visStart > prev {
			b.WriteString(visLine[prev:visStart])
		}
		b.WriteString("\x1b]8;;" + fullURL + "\a")
		b.WriteString(visLine[visStart:visEnd])
		b.WriteString("\x1b]8;;\a")
		prev = visEnd
	}
	if prev < visLen {
		b.WriteString(visLine[prev:])
	}
	b.WriteString(reset)
	return b.String()
}

// renderLogLine renders one display line applying scroll offset, search
// highlighting, and OSC 8 hyperlinks.
func renderLogLine(dl DisplayLine, wrap bool, hOffset, width int, searchRe *regexp.Regexp, t theme.Theme, isCurrentMatch bool) string {
	line := dl.Text
	truncated := false
	byteOffset := dl.SrcOffset
	if !wrap && width > 0 {
		line, byteOffset = SkipColumnsBytes(line, hOffset)
		if lipgloss.Width(line) > width {
			line, _ = TruncateToColumns(line, width-1)
			truncated = true
		}
	}
	suffix := ""
	if truncated {
		suffix = t.Log.Trunc.Render("»")
	}
	if dl.Dim {
		return t.Log.Dim.Render(line) + suffix
	}
	if searchRe != nil {
		matchStyle := t.Search.Match
		if isCurrentMatch {
			matchStyle = t.Search.CurrentMatch
		}
		return HighlightMatches(line, searchRe, matchStyle, t.Log.Normal) + suffix
	}
	if isCurrentMatch {
		return t.Log.CurrentHighlight.Render(line) + suffix
	}
	switch dl.Kind {
	case LineKindError:
		return t.Log.Error.Render(line) + suffix
	case LineKindWarning:
		return t.Log.Warning.Render(line) + suffix
	}
	return applyOSC8(line, dl.Src, byteOffset, t) + suffix
}

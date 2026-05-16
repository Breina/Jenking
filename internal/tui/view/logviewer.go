package view

import (
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

// copyFlashMsg is emitted when a clipboard write completes.
// isSel distinguishes selection copy (C) from log copy (c).
type copyFlashMsg struct{ isSel bool }

// copyFlashDoneMsg is emitted after the 1-second flash expires.
type copyFlashDoneMsg struct{ isSel bool }

// selectionCheckMsg carries the cleaned primary selection from the background poll.
type selectionCheckMsg struct {
	text      string
	lineCount int
}

var (
	urlRe = regexp.MustCompile(`https?://[^\s]+`)
	// Covers CSI sequences (\x1b[...X) and other ESC two-char sequences.
	ansiRe = regexp.MustCompile(`\x1b(?:[@-Z\\-_]|\[[0-9;?]*[@-~])`)
	// Matches a Jenkins XStream object reference (serialised Java object).
	// These appear both as standalone lines and embedded mid-line (e.g. "Started by user ha:////...").
	xstreamRe = regexp.MustCompile(`ha:////[A-Za-z0-9+/=]+`)
	// Matches a line that consists ENTIRELY of one or more xstream refs (plus optional whitespace).
	// Such lines carry no user-readable content and are dropped completely.
	xstreamOnlyRe = regexp.MustCompile(`^\s*(ha:////[A-Za-z0-9+/=]+\s*)+$`)
	errorRe       = regexp.MustCompile(`(?i)\b(error|errors|fatal|exception|panic|failed|failure)\b`)
	warningRe     = regexp.MustCompile(`(?i)\b(warning|warnings|warn|deprecated|deprecation)\b`)
	infoRe        = regexp.MustCompile(`(?i)\b(info|trace|fine|debug)\b`)
)

type lineKind uint8

const (
	lineKindNormal  lineKind = iota
	lineKindWarning          // yellow
	lineKindError            // red
)

// displayLine pairs the text of a display row with its rendering style.
// All wrapped chunks of one raw log line share the same style so that dim
// lines remain dim across every continuation chunk.
// src is the full source line; srcOffset is the byte offset of text within src.
// In no-wrap mode text == src and srcOffset == 0.
// In wrap mode each chunk carries src/srcOffset so OSC 8 links span chunk boundaries.
type displayLine struct {
	text      string
	src       string   // full source line; set by toDisplayLines
	srcOffset int      // byte offset of text within src (non-zero only in wrap mode)
	dim       bool     // render with consoleDim instead of normal/URL styles
	kind      lineKind // error/warning colouring (ignored when dim=true)
	rawIdx    int      // index of the source entry in LogViewer.rawLines
}

// LogViewer holds all shared state and logic for rendering scrollable log output.
// It is embedded by ConsoleView and StageLogView.
type LogViewer struct {
	rawLines           []string
	lines              []displayLine
	offset             int
	hOffset            int // rune columns; only active when !wrap
	width, height      int
	wrap               bool
	showInternal       bool
	searchQuery        string
	searchRe           *regexp.Regexp
	searchMatchLines   []int // lines[] indices of first chunk per search-matching raw line
	currentMatchLine   int   // lines[] index of the actively-selected search match (-1 = none)
	errCount           int
	warnCount          int
	highlightedLines   []int    // lines[] index of first chunk per error/warn raw line
	lastVisibleKind    lineKind // kind of the last visible (non-filtered) line
	selectionText      string   // cleaned primary selection text from last poll
	selectionLineCount int      // non-empty lines in selectionText
	selectionInLog     bool     // true when selectionText matches content in this log
	theme              theme.Theme
	// renderFn, when non-nil, overrides the default renderLogLine used in
	// renderLine/renderLineAt. Set by callers that need custom line rendering
	// (e.g. Groovy syntax highlighting in the describe script pane).
	renderFn func(dl displayLine, wrap bool, hOffset, width int, searchRe *regexp.Regexp, t theme.Theme, isCurrent bool) string
}

// contentHeight returns the number of log lines that fit in the view.
func (lv *LogViewer) contentHeight() int {
	return lv.height
}

// ScrollInfo returns the current scroll position for use by the border scrollbar.
func (lv *LogViewer) ScrollInfo() ScrollInfo {
	return ScrollInfo{
		Offset:     lv.offset,
		TotalLines: len(lv.lines),
		ViewHeight: lv.contentHeight(),
	}
}

// recomputeLines rebuilds display lines from rawLines using current settings.
// Preserves bottom-pin: if we were at the bottom, stay at the bottom.
// Preserves the active search match position by match-list index.
func (lv *LogViewer) recomputeLines() {
	atBottom := len(lv.lines) == 0 || lv.offset >= max(0, len(lv.lines)-lv.contentHeight())

	// Save active match index (position in searchMatchLines, not lines[] index)
	// so we can restore it after the rebuild even when new lines shift indices.
	savedMatchIdx := -1
	if lv.currentMatchLine >= 0 {
		for i, line := range lv.searchMatchLines {
			if line == lv.currentMatchLine {
				savedMatchIdx = i
				break
			}
		}
	}

	// Capture which raw line is at the top of the view so we can restore
	// the visual position even when wrap-mode changes the display-line count.
	savedRawIdx := -1
	if !atBottom && lv.offset < len(lv.lines) {
		savedRawIdx = lv.lines[lv.offset].rawIdx
	}

	lv.lines = nil
	lv.errCount, lv.warnCount = 0, 0
	lv.highlightedLines = lv.highlightedLines[:0]
	lv.searchMatchLines = lv.searchMatchLines[:0]
	lv.currentMatchLine = -1
	prevKind := lineKindNormal
	for rawIdx, raw := range lv.rawLines {
		internal := isInternalLine(raw)
		if !lv.showInternal && internal {
			continue
		}
		dl := lv.toDisplayLines(raw)
		for i := range dl {
			dl[i].rawIdx = rawIdx
		}
		if lv.searchRe != nil && lv.searchRe.MatchString(raw) {
			lv.searchMatchLines = append(lv.searchMatchLines, len(lv.lines))
		}
		if !internal {
			kind := classifyLine(raw)
			if kind != lineKindNormal && kind != prevKind {
				switch kind {
				case lineKindError:
					lv.errCount++
				case lineKindWarning:
					lv.warnCount++
				}
				lv.highlightedLines = append(lv.highlightedLines, len(lv.lines))
			}
			prevKind = kind
		} else {
			prevKind = lineKindNormal // shown internal line acts as a separator
		}
		lv.lines = append(lv.lines, dl...)
	}
	lv.lastVisibleKind = prevKind
	newMax := max(0, len(lv.lines)-lv.contentHeight())

	// Restore the active search match. Clamp to the new match list in case
	// matches were removed (e.g. content replaced, not just appended).
	if savedMatchIdx >= 0 && len(lv.searchMatchLines) > 0 {
		if savedMatchIdx >= len(lv.searchMatchLines) {
			savedMatchIdx = len(lv.searchMatchLines) - 1
		}
		lv.currentMatchLine = lv.searchMatchLines[savedMatchIdx]
		lv.offset = min(lv.currentMatchLine, newMax)
	} else if atBottom {
		lv.offset = newMax
	} else if savedRawIdx >= 0 {
		// Translate offset to the new display space by finding the first
		// display line that belongs to the same raw line.
		newOff := 0
		for i, dl := range lv.lines {
			if dl.rawIdx == savedRawIdx {
				newOff = i
				break
			}
		}
		lv.offset = min(newOff, newMax)
	} else {
		lv.offset = min(lv.offset, newMax)
	}
}

// toDisplayLines converts one raw log line to one or more display rows.
// In wrap mode long lines are split into lv.width-column chunks.
// All chunks share the dim and kind of the source line.
func (lv *LogViewer) toDisplayLines(raw string) []displayLine {
	dim := isInternalLine(raw)
	kind := lineKindNormal
	if !dim {
		kind = classifyLine(raw)
	}
	if !lv.wrap || lv.width <= 0 {
		return []displayLine{{text: raw, src: raw, dim: dim, kind: kind}}
	}
	if lipgloss.Width(raw) <= lv.width {
		return []displayLine{{text: raw, src: raw, dim: dim, kind: kind}}
	}
	var chunks []displayLine
	remaining := raw
	for len(remaining) > 0 {
		chunk, _ := truncateToColumns(remaining, lv.width)
		srcOffset := len(raw) - len(remaining)
		chunks = append(chunks, displayLine{text: chunk, src: raw, srcOffset: srcOffset, dim: dim, kind: kind})
		remaining = remaining[len(chunk):]
	}
	return chunks
}

func (lv *LogViewer) Badge() string {
	if lv.errCount+lv.warnCount == 0 {
		return ""
	}
	var parts []string
	warnIcon := iconOr(lv.theme.Icons.Warning, "⚠")
	errIcon := iconOr(lv.theme.Icons.Error, "✕")
	if lv.warnCount > 0 {
		parts = append(parts, lv.theme.Log.Warning.Render(fmt.Sprintf("%s %d", warnIcon, lv.warnCount)))
	}
	if lv.errCount > 0 {
		parts = append(parts, lv.theme.Log.Error.Render(fmt.Sprintf("%s %d", errIcon, lv.errCount)))
	}
	return strings.Join(parts, "  ")
}

// CopyLogCmd copies the visible log lines to the clipboard, respecting the
// current showInternal filter so the result matches what the user sees.
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
		return copyFlashMsg{isSel: false}
	}
}

// CopySelectionCmd copies the cached primary selection text to the clipboard.
func (lv *LogViewer) CopySelectionCmd() tea.Cmd {
	text := lv.selectionText
	return func() tea.Msg {
		if strings.TrimSpace(text) != "" {
			writeToClipboard(text)
		}
		return copyFlashMsg{isSel: true}
	}
}

// selectionCheckCmd reads the primary selection every 300 ms, cleans it, and
// returns a selectionCheckMsg. Views should re-issue it on each receipt.
func selectionCheckCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(300 * time.Millisecond)
		cleaned := cleanBorderChars(readPrimarySelection())
		count := 0
		for _, l := range strings.Split(cleaned, "\n") {
			if strings.TrimSpace(l) != "" {
				count++
			}
		}
		return selectionCheckMsg{text: cleaned, lineCount: count}
	}
}

// checkSelectionInLog reports whether the first non-empty line of cleaned
// appears as a substring in any raw log line. Called on the main goroutine.
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

// logLabel returns the "copy log [N]" shortcut label.
// The count reflects the current showInternal filter so it matches what c copies.
func (lv *LogViewer) logLabel() string {
	count := len(lv.rawLines)
	if !lv.showInternal {
		count = 0
		for _, raw := range lv.rawLines {
			if !isInternalLine(raw) {
				count++
			}
		}
	}
	return fmt.Sprintf("copy log [%d]", count)
}

// selLabel returns the "copy sel [N]" shortcut label.
func (lv *LogViewer) selLabel() string {
	return fmt.Sprintf("copy sel [%d]", lv.selectionLineCount)
}

// readPrimarySelection returns the current primary selection text.
// Uses type-list probes to avoid returning stale content retained by some
// compositors after the selection is released.
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

// writeToClipboard writes text to the terminal clipboard via OSC 52.
func writeToClipboard(text string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		fmt.Fprintf(tty, "\x1b]52;c;%s\x07", encoded)
		tty.Close()
	}
}

// cleanBorderChars strips the │ panel border characters and their padding from
// each line of a terminal selection.
func cleanBorderChars(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimPrefix(line, "│")
		line = strings.TrimRight(line, " │")
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// copyFlashTimer returns a cmd that fires copyFlashDoneMsg after 1 second.
func copyFlashTimer(isSel bool) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return copyFlashDoneMsg{isSel: isSel}
	}
}

func (lv *LogViewer) nextHighlight(forward bool) {
	if lv.searchRe != nil {
		lv.nextSearchMatch(forward)
		return
	}
	if len(lv.highlightedLines) == 0 {
		return
	}
	maxOff := max(0, len(lv.lines)-lv.contentHeight())
	if forward {
		for _, idx := range lv.highlightedLines {
			if idx > lv.offset {
				lv.offset = min(idx, maxOff)
				return
			}
		}
		lv.offset = min(lv.highlightedLines[0], maxOff) // wrap
	} else {
		for i := len(lv.highlightedLines) - 1; i >= 0; i-- {
			if lv.highlightedLines[i] < lv.offset {
				lv.offset = lv.highlightedLines[i]
				return
			}
		}
		lv.offset = min(lv.highlightedLines[len(lv.highlightedLines)-1], maxOff) // wrap
	}
}

func (lv *LogViewer) nextSearchMatch(forward bool) {
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
		// wrap around
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
		// wrap around
		last := lv.searchMatchLines[len(lv.searchMatchLines)-1]
		lv.currentMatchLine = last
		lv.offset = min(last, maxOff)
	}
}

// SearchQueryWithCount returns the current search query annotated with the
// match position, e.g. "foo [3/12]". Returns "" when no search is active.
func (lv *LogViewer) SearchQueryWithCount() string {
	if lv.searchQuery == "" {
		return ""
	}
	total := len(lv.searchMatchLines)
	if total == 0 || lv.currentMatchLine < 0 {
		return lv.searchQuery
	}
	for i, line := range lv.searchMatchLines {
		if line == lv.currentMatchLine {
			return fmt.Sprintf("%s [%d/%d]", lv.searchQuery, i+1, total)
		}
	}
	return lv.searchQuery
}

func (lv *LogViewer) renderLine(dl displayLine) string {
	if lv.renderFn != nil {
		return lv.renderFn(dl, lv.wrap, lv.hOffset, lv.width, lv.searchRe, lv.theme, false)
	}
	return renderLogLine(dl, lv.wrap, lv.hOffset, lv.width, lv.searchRe, lv.theme, false)
}

// renderLineAt renders a display line, highlighting it as the current search match
// if absIdx matches lv.currentMatchLine and a search is active.
func (lv *LogViewer) renderLineAt(dl displayLine, absIdx int) string {
	isCurrent := lv.searchRe != nil && absIdx == lv.currentMatchLine
	if lv.renderFn != nil {
		return lv.renderFn(dl, lv.wrap, lv.hOffset, lv.width, lv.searchRe, lv.theme, isCurrent)
	}
	return renderLogLine(dl, lv.wrap, lv.hOffset, lv.width, lv.searchRe, lv.theme, isCurrent)
}

// renderRows returns the visible lines rendered and padded to lv.height rows.
func (lv *LogViewer) renderRows() []string {
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

// SetSize updates the viewport dimensions, recomputing display lines when
// the width changes in wrap mode.
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

// ApplySearch compiles pattern into a search regex and recomputes display lines.
func (lv *LogViewer) ApplySearch(pattern string) error {
	lv.searchQuery = pattern
	lv.searchRe = compileSearchRegex(pattern)
	lv.currentMatchLine = -1
	lv.recomputeLines()
	return nil
}

// SearchQuery returns the current search pattern string.
func (lv *LogViewer) SearchQuery() string {
	return lv.searchQuery
}

// ItemCount returns the number of display lines.
func (lv *LogViewer) ItemCount() int {
	return len(lv.lines)
}

// AppendRawLines classifies and appends new raw lines, updating counts and
// highlighted line indices incrementally.
func (lv *LogViewer) AppendRawLines(newLines []string) {
	prevKind := lv.lastVisibleKind
	for _, raw := range newLines {
		lv.rawLines = append(lv.rawLines, raw)
		internal := isInternalLine(raw)
		if !lv.showInternal && internal {
			continue
		}
		dl := lv.toDisplayLines(raw)
		if lv.searchRe != nil && lv.searchRe.MatchString(raw) {
			lv.searchMatchLines = append(lv.searchMatchLines, len(lv.lines))
		}
		if !internal {
			kind := classifyLine(raw)
			if kind != lineKindNormal && kind != prevKind {
				switch kind {
				case lineKindError:
					lv.errCount++
				case lineKindWarning:
					lv.warnCount++
				}
				lv.highlightedLines = append(lv.highlightedLines, len(lv.lines))
			}
			prevKind = kind
		} else {
			prevKind = lineKindNormal
		}
		lv.lines = append(lv.lines, dl...)
	}
	lv.lastVisibleKind = prevKind
}

// --- Package-level utility functions ---

func classifyLine(raw string) lineKind {
	if infoRe.MatchString(raw) {
		return lineKindNormal
	}
	if warningRe.MatchString(raw) {
		return lineKindWarning
	}
	if errorRe.MatchString(raw) {
		return lineKindError
	}
	return lineKindNormal
}

// isInternalLine reports whether a line is Jenkins-internal noise that should
// be hidden by default ([Pipeline] bookkeeping lines).
func isInternalLine(line string) bool {
	return strings.HasPrefix(line, "[Pipeline]")
}

// splitLines splits raw progressive-log text into individual lines, cleaning
// each one:
//   - carriage returns stripped
//   - ANSI escape sequences stripped
//   - lines that are entirely Jenkins XStream object references (ha:////) are
//     dropped completely (they are internal serialisation state, not log output)
//   - XStream references embedded mid-line (e.g. "Started by user ha:////...")
//     are removed, leaving the human-readable prefix
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	// Drop trailing empty element produced by a trailing newline.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}

	result := make([]string, 0, len(parts))
	for _, line := range parts {
		line = strings.TrimRight(line, "\r")
		line = strings.ReplaceAll(line, "\t", "    ")
		line = ansiRe.ReplaceAllString(line, "")

		// Drop lines that are purely XStream serialisation state — no readable
		// content survives stripping them.
		if xstreamOnlyRe.MatchString(line) {
			continue
		}

		// Strip embedded XStream references and tidy trailing whitespace.
		line = strings.TrimRight(xstreamRe.ReplaceAllString(line, ""), " \t")

		result = append(result, line)
	}
	return result
}

// columnWidth returns the visual terminal column width of a single rune.
// ASCII is 1, wide characters (emoji, CJK) are 2, combining marks are 0.
// We delegate to lipgloss.Width which uses go-runewidth/uniseg internally.
func columnWidth(r rune) int {
	return lipgloss.Width(string(r))
}

// truncateToColumns returns the longest prefix of line that fits within
// maxCols visual terminal columns, plus whether truncation occurred.
// Input must be ANSI-free (our display lines always are).
func truncateToColumns(line string, maxCols int) (string, bool) {
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

// skipColumnsBytes skips the first skipCols visual columns and returns the
// remaining text together with its byte offset inside the original string.
func skipColumnsBytes(line string, skipCols int) (string, int) {
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

// skipColumns returns line with the first skipCols visual columns removed.
func skipColumns(line string, skipCols int) string {
	s, _ := skipColumnsBytes(line, skipCols)
	return s
}

// applyOSC8 renders visLine in normal colour with OSC 8 hyperlinks for each
// URL. fullText is the complete (pre-truncation, pre-skip) source line;
// byteOffset is where visLine starts inside fullText. Using fullText means
// truncated URLs still link to the full destination.
// BEL (\a) is used as OSC terminator for broadest terminal parser compatibility.
func applyOSC8(visLine, fullText string, byteOffset int, t theme.Theme) string {
	locs := urlRe.FindAllStringIndex(fullText, -1)
	if len(locs) == 0 {
		return t.Log.Normal.Render(visLine)
	}
	normalAnsi := t.Log.Normal.Render("") // render empty string to get the ANSI prefix
	// Extract the ANSI escape sequence produced by the Normal style so we can
	// embed it inline within the OSC 8 link sequence.
	// Fallback: use the xterm-256 escape for color 252 (the default Normal color).
	normalEsc := "\x1b[38;5;252m"
	if len(normalAnsi) > 4 { // rendered string contains at least an opening escape
		// lipgloss wraps: ESC_OPEN ... ESC_RESET; grab everything before the reset.
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
		// Convert fullText byte positions to visLine byte positions.
		start := loc[0] - byteOffset
		end := loc[1] - byteOffset
		if end <= 0 {
			continue // URL ends before the visible area
		}
		if start >= visLen {
			break // URL starts after the visible area
		}
		fullURL := fullText[loc[0]:loc[1]]
		visStart := max(start, 0)
		visEnd := min(end, visLen)
		if visStart > prev {
			b.WriteString(visLine[prev:visStart])
		}
		// OSC 8 with the full URL so partial/truncated links still work.
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

// renderLogLine renders one display line applying scroll offset, search highlighting,
// and OSC 8 hyperlinks. Shared by ConsoleView and StageLogView via LogViewer.renderLine.
// isCurrentMatch marks this as the actively-selected search match (n/N navigation).
func renderLogLine(dl displayLine, wrap bool, hOffset, width int, searchRe *regexp.Regexp, t theme.Theme, isCurrentMatch bool) string {
	line := dl.text
	truncated := false
	byteOffset := dl.srcOffset
	if !wrap && width > 0 {
		line, byteOffset = skipColumnsBytes(line, hOffset)
		if lipgloss.Width(line) > width {
			line, _ = truncateToColumns(line, width-1) // reserve 1 col for »
			truncated = true
		}
	}
	suffix := ""
	if truncated {
		suffix = t.Log.Trunc.Render("»")
	}
	if dl.dim {
		return t.Log.Dim.Render(line) + suffix
	}
	if searchRe != nil {
		matchStyle := t.Search.Match
		if isCurrentMatch {
			matchStyle = t.Search.CurrentMatch
		}
		return highlightMatches(line, searchRe, matchStyle, t.Log.Normal) + suffix
	}
	switch dl.kind {
	case lineKindError:
		return t.Log.Error.Render(line) + suffix
	case lineKindWarning:
		return t.Log.Warning.Render(line) + suffix
	}
	// OSC 8 hyperlinks work in both wrap and no-wrap mode.
	// dl.src carries the full original line so truncated/chunked URLs still link correctly.
	return applyOSC8(line, dl.src, byteOffset, t) + suffix
}

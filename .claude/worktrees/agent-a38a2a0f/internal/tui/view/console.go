package view

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/brecht/jenkins-tui/internal/jenkins"
	"github.com/brecht/jenkins-tui/internal/tui/command"
	"github.com/brecht/jenkins-tui/internal/tui/component"
	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

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
)

var (
	consoleNormal = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	consoleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	consoleURL    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Underline(true)
)

// displayLine pairs the text of a display row with its rendering style.
// All wrapped chunks of one raw log line share the same style so that dim
// lines remain dim across every continuation chunk.
type displayLine struct {
	text string
	dim  bool // render with consoleDim instead of normal/URL styles
}

type consoleChunkMsg struct {
	lines     []string
	nextStart int
	moreData  bool
}

type consoleAbortMsg struct{}

// ConsoleView streams a build's console output using Jenkins' progressive log API.
type ConsoleView struct {
	theme        theme.Theme
	client       jenkins.JenkinsClient
	jobPath      string
	buildNumber  int
	rawLines     []string      // all received lines (ANSI + xstream stripped)
	lines        []displayLine // display rows (filtered + possibly wrapped)
	offset       int
	width        int
	height       int
	done         bool
	wrap         bool // true = split long lines to fit width
	showInternal bool // true = show [Pipeline] and other Jenkins-internal lines
	searchQuery  string
	searchRe     *regexp.Regexp
	ctx          context.Context
	cancel       context.CancelFunc
	jobName      string // human-readable project name for breadcrumb
	branchName   string // branch name for multibranch projects
}

func NewConsoleView(t theme.Theme, client jenkins.JenkinsClient, jobPath string, buildNumber int, jobName, branchName string) *ConsoleView {
	ctx, cancel := context.WithCancel(context.Background())
	return &ConsoleView{
		theme:       t,
		client:      client,
		jobPath:     jobPath,
		buildNumber: buildNumber,
		ctx:         ctx,
		cancel:      cancel,
		jobName:     jobName,
		branchName:  branchName,
	}
}

func (cv *ConsoleView) ApplySearch(pattern string) error {
	cv.searchQuery = pattern
	cv.searchRe = compileSearchRegex(pattern)
	cv.recomputeLines()
	return nil
}

func (cv *ConsoleView) SearchQuery() string {
	return cv.searchQuery
}

func (cv *ConsoleView) Init() tea.Cmd {
	return consoleFetch(cv.ctx, cv.client, cv.jobPath, cv.buildNumber, 0, 0)
}

func consoleFetch(ctx context.Context, client jenkins.JenkinsClient, jobPath string, buildNumber, start int, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return consoleAbortMsg{}
			case <-time.After(delay):
			}
		}
		chunk, err := client.GetProgressiveLog(ctx, jobPath, buildNumber, start)
		if err != nil {
			if ctx.Err() != nil {
				return consoleAbortMsg{}
			}
			return ErrorMsg{Err: err}
		}
		return consoleChunkMsg{
			lines:     splitLines(chunk.Text),
			nextStart: chunk.NextStart,
			moreData:  chunk.MoreData,
		}
	}
}

// appendRawLines adds new raw lines and updates display lines incrementally.
func (cv *ConsoleView) appendRawLines(newLines []string) {
	for _, raw := range newLines {
		cv.rawLines = append(cv.rawLines, raw)
		if !cv.showInternal && isInternalLine(raw) {
			continue
		}
		if cv.searchRe != nil && !cv.searchRe.MatchString(raw) {
			continue
		}
		cv.lines = append(cv.lines, cv.toDisplayLines(raw)...)
	}
}

// recomputeLines rebuilds display lines from rawLines using current settings.
// Preserves bottom-pin: if we were at the bottom, stay at the bottom.
func (cv *ConsoleView) recomputeLines() {
	atBottom := len(cv.lines) == 0 || cv.offset >= max(0, len(cv.lines)-cv.height)
	cv.lines = nil
	for _, raw := range cv.rawLines {
		if !cv.showInternal && isInternalLine(raw) {
			continue
		}
		if cv.searchRe != nil && !cv.searchRe.MatchString(raw) {
			continue
		}
		cv.lines = append(cv.lines, cv.toDisplayLines(raw)...)
	}
	newMax := max(0, len(cv.lines)-cv.height)
	if atBottom {
		cv.offset = newMax
	} else {
		cv.offset = min(cv.offset, newMax)
	}
}

// toDisplayLines converts one raw log line to one or more display rows.
// In wrap mode long lines are split into cv.width-rune chunks.
// All chunks share the dim flag of the source line.
func (cv *ConsoleView) toDisplayLines(raw string) []displayLine {
	dim := isInternalLine(raw)
	if !cv.wrap || cv.width <= 0 {
		return []displayLine{{text: raw, dim: dim}}
	}
	runes := []rune(raw)
	if len(runes) <= cv.width {
		return []displayLine{{text: raw, dim: dim}}
	}
	var chunks []displayLine
	for len(runes) > 0 {
		end := cv.width
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, displayLine{text: string(runes[:end]), dim: dim})
		runes = runes[end:]
	}
	return chunks
}

func (cv *ConsoleView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		cv.theme = msg.Theme
		return cv, nil

	case consoleChunkMsg:
		maxOffset := max(0, len(cv.lines)-cv.height)
		pinned := cv.offset >= maxOffset
		cv.appendRawLines(msg.lines)
		if pinned {
			cv.offset = max(0, len(cv.lines)-cv.height)
		}
		if msg.moreData {
			return cv, consoleFetch(cv.ctx, cv.client, cv.jobPath, cv.buildNumber, msg.nextStart, time.Second)
		}
		cv.done = true
		return cv, nil

	case consoleAbortMsg:
		return cv, nil

	case tea.KeyMsg:
		maxOffset := max(0, len(cv.lines)-cv.height)
		pageSize := max(1, cv.height-1)
		switch msg.String() {
		case "up", "k":
			cv.offset = max(0, cv.offset-1)
		case "down", "j":
			cv.offset = min(maxOffset, cv.offset+1)
		case "pgup":
			cv.offset = max(0, cv.offset-pageSize)
		case "pgdown":
			cv.offset = min(maxOffset, cv.offset+pageSize)
		case "g", "home":
			cv.offset = 0
		case "G", "end":
			cv.offset = maxOffset
		case "w", "W":
			cv.wrap = !cv.wrap
			cv.recomputeLines()
		case "p", "P":
			cv.showInternal = !cv.showInternal
			cv.recomputeLines()
		}
	}
	return cv, nil
}

func (cv *ConsoleView) View() string {
	if cv.height <= 0 {
		return ""
	}

	end := min(cv.offset+cv.height, len(cv.lines))
	visible := cv.lines[cv.offset:end]

	rows := make([]string, 0, cv.height)
	for _, dl := range visible {
		rows = append(rows, cv.renderLine(dl))
	}

	if cv.done && len(rows) < cv.height {
		rows = append(rows, consoleDim.Render("─── end ───"))
	}

	for len(rows) < cv.height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

func (cv *ConsoleView) renderLine(dl displayLine) string {
	line := dl.text
	// In no-wrap mode, truncate to visible width before styling.
	// Input is ANSI-stripped so rune count == visible width.
	if !cv.wrap && cv.width > 0 {
		runes := []rune(line)
		if len(runes) > cv.width {
			line = string(runes[:cv.width])
		}
	}
	if dl.dim {
		return consoleDim.Render(line)
	}
	if cv.searchRe != nil {
		return highlightMatches(line, cv.searchRe, cv.theme.Search.Match, consoleNormal)
	}
	return highlightURLs(line)
}

func highlightURLs(line string) string {
	locs := urlRe.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return consoleNormal.Render(line)
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		if loc[0] > prev {
			b.WriteString(consoleNormal.Render(line[prev:loc[0]]))
		}
		b.WriteString(consoleURL.Render(line[loc[0]:loc[1]]))
		prev = loc[1]
	}
	if prev < len(line) {
		b.WriteString(consoleNormal.Render(line[prev:]))
	}
	return b.String()
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

func (cv *ConsoleView) Title() string {
	return fmt.Sprintf("Build #%d", cv.buildNumber)
}

func (cv *ConsoleView) Breadcrumb() BreadcrumbSegment {
	ctx := jobRefParts(cv.jobName, cv.branchName)
	ctx = append(ctx, component.BreadcrumbPart{Text: fmt.Sprintf("%d", cv.buildNumber), IsBuildNum: true})
	return BreadcrumbSegment{ViewType: "log", Context: ctx}
}

func (cv *ConsoleView) ItemCount() int {
	return len(cv.lines)
}

func (cv *ConsoleView) SetSize(w, h int) {
	needRecompute := cv.wrap && w != cv.width && len(cv.rawLines) > 0
	cv.width = w
	cv.height = h
	if needRecompute {
		cv.recomputeLines()
	} else {
		cv.offset = min(cv.offset, max(0, len(cv.lines)-h))
	}
}

func (cv *ConsoleView) Commands() []command.Command {
	return nil
}

func (cv *ConsoleView) Shortcuts() []component.Shortcut {
	wrapLabel := "wrap"
	if cv.wrap {
		wrapLabel = "no wrap"
	}
	pipelineLabel := "[Pipeline] show"
	if cv.showInternal {
		pipelineLabel = "[Pipeline] hide"
	}
	return []component.Shortcut{
		{Key: "/", Action: "search"},
		{Key: "w", Action: wrapLabel},
		{Key: "p", Action: pipelineLabel},
		{Key: "g/G", Action: "top/bottom"},
	}
}

func (cv *ConsoleView) Close() error {
	if cv.cancel != nil {
		cv.cancel()
	}
	return nil
}

package view

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// describeDataMsg carries the fetched script and parameters for a build.
type describeDataMsg struct {
	script string
	params map[string]string
	err    error
}

// describeSymbolsMsg carries the per-build pipeline-syntax symbol set that
// drives the script pane's highlighting (and, later, vim completion).
// Fetched independently from the script so a slow gdsl/globals page never
// delays the script render.
type describeSymbolsMsg struct {
	symbols *pipelinesyntax.Symbols
	err     error
}

// editorDoneMsg is sent when the external editor process exits.
type editorDoneMsg struct {
	tmpFile string
	err     error
}

// editorValidatedMsg carries the post-edit final validation result. Sent
// after editorDoneMsg reads the buffer back. Empty/OK results are surfaced
// as no-op (silent success); failures route through ErrorMsg.
type editorValidatedMsg struct {
	result jmodel.ValidationResult
	err    error
}

// describeTriggerReadyMsg carries the latest build number fetched before triggering from describe.
type describeTriggerReadyMsg struct{ latestBuild int }

// DescribeView shows build parameters (top pane) and the pipeline Groovy script
// (bottom pane). Implements PreviewProvider so app.go renders both as bordered panels.
type DescribeView struct {
	BaseView
	build jmodel.Build

	script      string            // in-memory Groovy script (editable)
	params      map[string]string // build parameters (nil = still loading)
	dataLoading bool              // true while the initial fetch is in flight

	symbols            *pipelinesyntax.Symbols // per-build symbol set (nil until fetched)
	overlay            *syntaxOverlay          // precompiled regexes derived from symbols
	scriptCommentFlags []bool                  // per raw line: does the line start inside an open /* */?

	// Validation issues from the last vim save. The order here doubles as
	// the n/N navigation order — scriptLV.navItemsFn emits one rawIdx per
	// entry, so scriptLV.CurrentNavIdx indexes directly into this slice.
	issues []jmodel.ValidationIssue

	// Requested script-pane dimensions cached so the footer strip can be
	// toggled (which changes scriptLV's effective height by one row) without
	// the parent view re-asserting size.
	scriptW, scriptH int

	// Parameters pane (main / top).
	paramLines  []string
	paramOffset int

	// Script pane (preview / bottom): scrolling, wrapping, and search via LogViewer.
	scriptLV widget.LogViewer

	copyLogFlash bool
	copySelFlash bool

	trigger triggerMixin
	host    widget.BehaviorHost
}

// NewDescribeView creates a DescribeView for the given build.
// Script and parameters are always fetched fresh from the Jenkins API.
func NewDescribeView(t theme.Theme, client jmodel.JenkinsClient, store *cache.Store, nc NavigationContext, build jmodel.Build) *DescribeView {
	dv := &DescribeView{
		BaseView:    NewBaseView(t, client, store, nc, CtxBuild),
		build:       build,
		dataLoading: true,
		trigger:     newTriggerMixin(t, client, nc),
	}
	dv.scriptLV = widget.NewLogViewer(t,
		widget.WithClassifier(func(rawIdx int, _ string) widget.LineKind {
			if dv.hasIssueOnLine(rawIdx) {
				return widget.LineKindError
			}
			return widget.LineKindNormal
		}),
		widget.WithNavigationItems(func() []int {
			out := make([]int, 0, len(dv.issues))
			for _, iss := range dv.issues {
				if iss.Line <= 0 {
					continue // whole-file errors have no line target; still counted in badge
				}
				out = append(out, iss.Line-1)
			}
			return out
		}),
		widget.WithLineRenderer(func(dl widget.DisplayLine, wrap bool, hOffset, width int, searchRe *regexp.Regexp, t theme.Theme, isCurrent bool) string {
			startInComment := false
			if dl.RawIdx >= 0 && dl.RawIdx < len(dv.scriptCommentFlags) {
				startInComment = dv.scriptCommentFlags[dl.RawIdx]
				if dl.SrcOffset > 0 && dl.SrcOffset <= len(dl.Src) {
					startInComment = scanCommentState(dl.Src[:dl.SrcOffset], startInComment)
				}
			}
			return renderGroovyLogLine(dl, wrap, hOffset, width, searchRe, t, isCurrent, dv.overlay, startInComment)
		}),
	)
	access := fixedBuildAccessor(&dv.nc, &dv.build)
	storeFn := func() *cache.Store { return dv.store }
	dv.host.Add(newTestReportBehavior(t, client, storeFn, access, swapTo))
	dv.host.Add(newArtifactBehavior(t, client, storeFn, access, swapTo))
	dv.host.Add(newTriggerBehavior(&dv.trigger))
	return dv
}

func (dv *DescribeView) Init() tea.Cmd {
	dv.buildParamLines()
	dv.buildScriptLines()
	if widget.VimPolicySnapshot().Enabled && widget.VimPolicySnapshot().PrefetchSymbols {
		return tea.Batch(dv.fetchDataCmd(), dv.fetchSymbolsCmd(), widget.SelectionCheckCmd())
	}
	return tea.Batch(dv.fetchDataCmd(), widget.SelectionCheckCmd())
}

// fetchSymbolsCmd loads the per-build pipeline-syntax symbol set. Prefers
// the cache (in-memory then disk); on a miss, hits Jenkins and populates
// both layers. Builds are immutable so this is cache-forever.
func (dv *DescribeView) fetchSymbolsCmd() tea.Cmd {
	ctx := dv.ctx
	client := dv.client
	store := dv.store
	jobPath := dv.nc.JobPath()
	buildNum := dv.build.Number
	// Prefix invalidates older cache entries when the parser changes shape.
	// v5 = bumped after a parser bug poisoned v4 caches with empty Symbols.
	// Combined with the "don't cache empty" rule below, transient fetch
	// failures no longer stick across restarts.
	key := fmt.Sprintf("v5/%s#%d", jobPath, buildNum)
	return func() tea.Msg {
		applyOverlays := func(s *pipelinesyntax.Symbols) {
			// Overlays run on every read — user GDSL edits + job parameter
			// definition changes take effect without invalidating the
			// per-build cache.
			jenkins.ApplyUserGDSL(s)
			if cc, ok := client.(*jenkins.Client); ok {
				cc.ApplyJobParameters(ctx, s, jobPath)
			}
		}
		if sym, hit := loadCachedSymbols(store, key); hit {
			applyOverlays(sym)
			return describeSymbolsMsg{symbols: sym}
		}
		sym, err := client.FetchPipelineSyntax(ctx, jobPath, buildNum)
		if ctx.Err() != nil {
			return nil
		}
		// Cache the raw server data — Symbols.Globals[i].Members stays empty
		// at this stage. Overlays applied below on every read.
		//
		// Skip the cache write entirely when the fetch produced nothing
		// useful (0 steps AND 0 globals). That's nearly always a transient
		// auth/network failure; persisting it would mask the real Jenkins
		// data on every future open until the user manually wiped the cache.
		if sym != nil && (len(sym.Steps) > 0 || len(sym.Globals) > 0) {
			storeSymbols(store, key, sym)
		}
		if err != nil {
			slog.Warn("fetch pipeline-syntax", "jobPath", jobPath, "build", buildNum,
				"steps", len(sym.Steps), "globals", len(sym.Globals), "err", err.Error())
		}
		applyOverlays(sym)
		return describeSymbolsMsg{symbols: sym, err: err}
	}
}

// loadCachedSymbols tries the in-memory cache first, falling back to disk;
// hot disk hits get promoted into memory before returning. Returns hit=false
// when neither layer has the key.
func loadCachedSymbols(store *cache.Store, key string) (*pipelinesyntax.Symbols, bool) {
	if store == nil || store.Symbols == nil {
		return nil, false
	}
	if e := store.Symbols.Get(key); e != nil && e.Value != nil {
		return e.Value, true
	}
	if store.Disk == nil {
		return nil, false
	}
	sym, err := store.Disk.LoadSymbols(key)
	if err != nil {
		return nil, false
	}
	store.Symbols.Put(key, sym)
	return sym, true
}

// storeSymbols writes pipeline symbols to both cache layers (memory and disk).
// Caller is responsible for the empty-content guard.
func storeSymbols(store *cache.Store, key string, sym *pipelinesyntax.Symbols) {
	if store == nil || store.Symbols == nil {
		return
	}
	store.Symbols.Put(key, sym)
	if store.Disk != nil {
		_ = store.Disk.SaveSymbols(key, sym)
	}
}

func (dv *DescribeView) fetchDataCmd() tea.Cmd {
	ctx := dv.ctx
	client := dv.client
	jobPath := dv.nc.JobPath()
	buildNum := dv.build.Number
	return func() tea.Msg {
		script, scriptErr := client.GetBuildScript(ctx, jobPath, buildNum)
		if ctx.Err() != nil {
			return nil
		}
		params, _ := client.GetBuildParameters(ctx, jobPath, buildNum)
		if ctx.Err() != nil {
			return nil
		}
		if params == nil {
			params = map[string]string{}
		}
		return describeDataMsg{script: script, params: params, err: scriptErr}
	}
}

// buildParamLines regenerates the parameters pane display lines.
func (dv *DescribeView) buildParamLines() {
	t := dv.theme
	var lines []string
	switch {
	case dv.dataLoading:
		lines = append(lines, "  "+t.Log.Dim.Render("Loading…"))
	case len(dv.params) == 0:
		lines = append(lines, "  "+t.Log.Dim.Render("(none)"))
	default:
		keys := make([]string, 0, len(dv.params))
		for k := range dv.params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, fmt.Sprintf("  %s  %s",
				t.Popup.Label.Render(k+":"),
				t.Log.Normal.Render(dv.params[k]),
			))
		}
	}
	dv.paramLines = lines
	if maxOff := max(0, len(dv.paramLines)-dv.height); dv.paramOffset > maxOff {
		dv.paramOffset = maxOff
	}
}

// buildScriptLines populates the script LogViewer from the current script text.
func (dv *DescribeView) buildScriptLines() {
	var rawLines []string
	if dv.script != "" {
		rawLines = strings.Split(dv.script, "\n")
	}
	dv.scriptCommentFlags = ComputeBlockCommentStartFlags(rawLines)
	dv.scriptLV.SetRawLines(rawLines)
}

// applyValidationResult records issues from the last validate run, then asks
// the LogViewer to rebuild — classifyFn and navItemsFn both close over
// dv.issues, so nothing else needs to be wired here. Auto-lands on the first
// issue via the same nextHighlight call that powers n.
func (dv *DescribeView) applyValidationResult(res jmodel.ValidationResult) {
	if res.OK {
		dv.issues = nil
	} else {
		dv.issues = res.Issues
	}
	dv.layoutScript()
	dv.scriptLV.Recompute()
	if dv.scriptLV.NavItemsCount() > 0 {
		dv.scriptLV.NextHighlight(true)
	}
}

// hasIssueOnLine reports whether any validation issue targets rawIdx (0-based
// line). Cheap O(n) scan — issue counts in practice are small.
func (dv *DescribeView) hasIssueOnLine(rawIdx int) bool {
	for _, iss := range dv.issues {
		if iss.Line > 0 && iss.Line-1 == rawIdx {
			return true
		}
	}
	return false
}

// footerActive reports whether to reserve a row for the footer strip. Tied to
// "issues exist", not "issue selected", so the layout stays stable across
// Esc deselects and n/N steps — otherwise every keystroke that toggles
// selection would shift the script pane by one row. The row renders blank
// (no message) when nothing is selected; renderFooter handles that.
func (dv *DescribeView) footerActive() bool { return len(dv.issues) > 0 }

// layoutScript pushes the cached script dimensions to scriptLV, reserving one
// row for the validation footer when active.
func (dv *DescribeView) layoutScript() {
	h := dv.scriptH
	if dv.footerActive() {
		h--
	}
	dv.scriptLV.SetSize(dv.scriptW, max(0, h))
}

// renderFooter returns the footer row showing the active issue's message.
// Renders only when an item is selected — the badge and header already convey
// count and shortcut, so no fallback hint is needed. The highlighted line
// itself shows the location, so the message stands alone (no "line N:" prefix).
func (dv *DescribeView) renderFooter() string {
	cur := dv.scriptLV.CurrentNavIdx()
	if cur < 0 || cur >= len(dv.issues) {
		return ""
	}
	msg := dv.issues[cur].Message
	if dv.scriptW > 0 {
		msg, _ = widget.TruncateToColumns(msg, dv.scriptW)
	}
	return dv.theme.Log.Error.Render(msg)
}

// HasActiveNavigation implements NavigationClearable — the first Esc clears
// the selected highlight/match before the second Esc closes the view.
func (dv *DescribeView) HasActiveNavigation() bool { return dv.scriptLV.HasActiveNavigation() }

// ClearActiveNavigation implements NavigationClearable.
func (dv *DescribeView) ClearActiveNavigation() { dv.scriptLV.ClearActiveNavigation() }

// hasActivePreview reports whether the params panel should be shown.
// Returns true while loading (params unknown) or when params are present.
func (dv *DescribeView) hasActivePreview() bool {
	return dv.dataLoading || len(dv.params) > 0
}

// HasActivePreview implements the conditionalPreview interface checked by app.go.
func (dv *DescribeView) HasActivePreview() bool {
	return dv.hasActivePreview()
}

func (dv *DescribeView) ApplySearch(pattern string) tea.Cmd {
	return dv.scriptLV.ApplySearch(pattern)
}

func (dv *DescribeView) HandleSearchResult(msg widget.SearchResultMsg) tea.Cmd {
	dv.scriptLV.ApplySearchResult(msg)
	return nil
}

func (dv *DescribeView) SearchQuery() string {
	return dv.scriptLV.SearchQuery()
}

func (dv *DescribeView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := dv.host.HandleMsg(msg); handled {
		return dv, cmd
	}

	switch msg := msg.(type) {
	case widget.SelectionCheckMsg:
		dv.scriptLV.RecordSelection(msg.Text, msg.LineCount)
		return dv, widget.SelectionCheckCmd()
	case widget.CopyFlashMsg:
		return dv, dv.handleCopyFlash(msg)
	case widget.CopyFlashDoneMsg:
		dv.handleCopyFlashDone(msg)
		return dv, nil
	case ThemeChangedMsg:
		dv.handleThemeChanged(msg)
		return dv, nil
	case describeDataMsg:
		return dv, dv.handleDescribeData(msg)
	case describeSymbolsMsg:
		dv.handleDescribeSymbols(msg)
		return dv, nil
	case editorDoneMsg:
		return dv, dv.handleEditorDone(msg)
	case editorValidatedMsg:
		return dv, dv.handleEditorValidated(msg)
	case describeTriggerReadyMsg:
		return dv, dv.handleDescribeTriggerReady(msg)
	case tea.KeyMsg:
		return dv.handleKeyMsg(msg)
	}
	return dv, nil
}

func (dv *DescribeView) handleCopyFlash(msg widget.CopyFlashMsg) tea.Cmd {
	if msg.IsSel {
		dv.copySelFlash = true
	} else {
		dv.copyLogFlash = true
	}
	return widget.CopyFlashTimer(msg.IsSel)
}

func (dv *DescribeView) handleCopyFlashDone(msg widget.CopyFlashDoneMsg) {
	if msg.IsSel {
		dv.copySelFlash = false
	} else {
		dv.copyLogFlash = false
	}
}

func (dv *DescribeView) handleThemeChanged(msg ThemeChangedMsg) {
	dv.theme = msg.Theme
	dv.host.SetTheme(msg.Theme)
	dv.scriptLV.SetTheme(msg.Theme)
	dv.buildParamLines()
	dv.buildScriptLines()
}

func (dv *DescribeView) handleDescribeData(msg describeDataMsg) tea.Cmd {
	dv.dataLoading = false
	dv.script = msg.script
	dv.params = msg.params
	dv.buildParamLines()
	// Pin to top on first script load. recomputeLines treats empty initial
	// state as "at bottom" and would otherwise scroll the view to the end
	// of the file, which is not what readers want for a Jenkinsfile.
	dv.scriptLV.ScrollToTop()
	dv.buildScriptLines()
	dv.scriptLV.ScrollToTop()
	if msg.err != nil {
		return func() tea.Msg { return ErrorMsg{Err: msg.err} }
	}
	return nil
}

func (dv *DescribeView) handleDescribeSymbols(msg describeSymbolsMsg) {
	// Even on partial failure, msg.symbols may carry whatever parsed.
	// Log the error silently — degraded highlighting is fine, no toast.
	if msg.symbols != nil {
		dv.symbols = msg.symbols
		dv.overlay = newSyntaxOverlay(msg.symbols)
		// Re-render the script pane with the new overlay.
		dv.buildScriptLines()
	}
}

func (dv *DescribeView) handleEditorDone(msg editorDoneMsg) tea.Cmd {
	var validateCmd tea.Cmd
	if msg.err == nil && msg.tmpFile != "" {
		if data, err := os.ReadFile(msg.tmpFile); err == nil {
			script := strings.TrimSuffix(string(data), "\n// vim: set filetype=groovy:")
			dv.script = strings.TrimRight(script, "\n")
			dv.buildScriptLines()
			if widget.VimPolicySnapshot().Enabled && widget.VimPolicySnapshot().ValidateOnSave {
				validateCmd = dv.validateScriptCmd()
			}
		}
	}
	if msg.tmpFile != "" {
		os.Remove(msg.tmpFile)
	}
	return validateCmd
}

func (dv *DescribeView) handleEditorValidated(msg editorValidatedMsg) tea.Cmd {
	// Transport errors still need a toast — the user needs to know the
	// validator never ran. Validation *failures* now drive in-pane gutter
	// markers + n/N navigation instead of a transient top-of-screen toast.
	if msg.err != nil {
		err := msg.err
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("validate: %w", err)} }
	}
	dv.applyValidationResult(msg.result)
	return nil
}

func (dv *DescribeView) handleDescribeTriggerReady(msg describeTriggerReadyMsg) tea.Cmd {
	if dv.script != "" {
		return dv.trigger.startReplay(msg.latestBuild, dv.build.Number, dv.script)
	}
	return dv.trigger.startTrigger(msg.latestBuild)
}

func (dv *DescribeView) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if handled, cmd := dv.host.HandleKey(msg); handled {
		return dv, cmd
	}
	switch msg.String() {
	case "up", "k":
		dv.scriptLV.ScrollByLines(-1)
	case "down", "j":
		dv.scriptLV.ScrollByLines(1)
	case "pgup":
		dv.scriptLV.ScrollByPages(-1)
	case "pgdown":
		dv.scriptLV.ScrollByPages(1)
	case "g", "home":
		dv.scriptLV.ScrollToTop()
	case "G", "end":
		dv.scriptLV.ScrollToBottom()
	case "left", "h":
		dv.scriptLV.ScrollByCols(-8)
	case "right":
		dv.scriptLV.ScrollByCols(8)
	case "s":
		return dv, dv.openStageSwap()
	case "l":
		return dv, dv.openConsoleSwap()
	case "w", "W":
		dv.scriptLV.ToggleWrap()
	case "f2", "n":
		dv.scriptLV.NextHighlight(true)
	case "N":
		dv.scriptLV.NextHighlight(false)
	case "t":
		return dv, dv.startTriggerCmd()
	case "e":
		return dv, dv.openEditorCmd()
	case "c":
		return dv, dv.scriptLV.CopyLogCmd()
	case "C":
		if dv.scriptLV.SelectionInLog() {
			return dv, dv.scriptLV.CopySelectionCmd()
		}
	}
	return dv, nil
}

func (dv *DescribeView) openStageSwap() tea.Cmd {
	return func() tea.Msg {
		return SwapViewMsg{View: NewStageView(dv.theme, dv.client, dv.store, dv.nc, dv.build)}
	}
}

func (dv *DescribeView) openConsoleSwap() tea.Cmd {
	nc := dv.nc
	build := dv.build
	return func() tea.Msg {
		cv := NewConsoleView(dv.theme, dv.client, nc)
		cv.build = build
		cv.store = dv.store
		return SwapViewMsg{View: cv}
	}
}

// startTriggerCmd fetches the current latest build number, then either
// starts a replay (if a script is loaded) or a regular trigger.
func (dv *DescribeView) startTriggerCmd() tea.Cmd {
	ctx := dv.ctx
	client := dv.client
	jobPath := dv.nc.JobPath()
	return func() tea.Msg {
		builds, _ := client.ListBuilds(ctx, jobPath)
		latest := 0
		if len(builds) > 0 {
			latest = builds[0].Number
		}
		return describeTriggerReadyMsg{latestBuild: latest}
	}
}

// openEditorCmd writes the current script to a temp file and returns a
// tea.ExecProcess command that suspends the TUI, opens the editor, and
// sends an editorDoneMsg when the editor exits.
//
// When the resolved editor is vim/nvim, a per-build runtime directory is
// materialised on disk (syntax overlay + omnifunc) and layered onto the
// user's vim via --cmd/-c so completion and Jenkins-aware highlighting are
// available without touching the user's vimrc.
func (dv *DescribeView) openEditorCmd() tea.Cmd {
	if dv.script == "" {
		return nil
	}
	tmpFile, err := os.CreateTemp("", "jenkins-*.groovy")
	if err != nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("create temp file: %w", err)} }
	}
	content := dv.script + "\n// vim: set filetype=groovy:"
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("write temp file: %w", err)} }
	}
	tmpFile.Close()
	tmpPath := tmpFile.Name()

	cmd := editorCommand(tmpPath)
	// Best-effort: a runtime write failure shouldn't block editing. We just
	// fall back to the editor with no completion/overlay help. Skipped
	// entirely when vim integration is disabled in config.
	var rt *vimRuntime
	if widget.VimPolicySnapshot().Enabled {
		if r, rtErr := writeVimRuntime(dv.nc.JobPath(), dv.build.Number, dv.symbols); rtErr == nil {
			rt = r
			cmd = applyVimArgs(cmd, rt)
		}
	}

	// In-vim validate loop: while the user is editing, watch the BufWritePost
	// sentinel and write quickfix-formatted errors back for vim to pick up.
	var stop chan struct{}
	if rt != nil && widget.VimPolicySnapshot().ValidateOnSave {
		stop = make(chan struct{})
		go runVimValidator(dv.ctx, dv.client, rt, stop, 0)
	}

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if stop != nil {
			close(stop)
		}
		return editorDoneMsg{tmpFile: tmpPath, err: err}
	})
}

// validateScriptCmd POSTs the final buffer to the validator and routes the
// result through editorValidatedMsg. Called from the editorDoneMsg handler
// once the edited script has been read back into dv.script.
func (dv *DescribeView) validateScriptCmd() tea.Cmd {
	ctx := dv.ctx
	client := dv.client
	script := dv.script
	return func() tea.Msg {
		res, err := client.ValidateJenkinsfile(ctx, script)
		return editorValidatedMsg{result: res, err: err}
	}
}

func (dv *DescribeView) View() string {
	if dv.height <= 0 {
		return ""
	}
	if !dv.hasActivePreview() {
		// No params panel: script fills the full main panel.
		if dv.script == "" {
			rows := make([]string, dv.height)
			rows[0] = "  " + dv.theme.Log.Dim.Render("Loading…")
			return strings.Join(rows, "\n")
		}
		rows := dv.scriptLV.RenderRows()
		if dv.footerActive() {
			rows = append(rows, dv.renderFooter())
		}
		return strings.Join(rows, "\n")
	}
	end := min(dv.paramOffset+dv.height, len(dv.paramLines))
	visible := dv.paramLines[dv.paramOffset:end]
	rows := make([]string, dv.height)
	copy(rows, visible)
	return strings.Join(rows, "\n")
}

// ScrollInfo implements HasScrollInfo for when the script is rendered as the main panel.
func (dv *DescribeView) ScrollInfo() widget.ScrollInfo {
	if dv.hasActivePreview() {
		return widget.ScrollInfo{}
	}
	return dv.scriptLV.ScrollInfo()
}

// PreviewScrollInfo implements HasPreviewScrollInfo for the script preview panel.
func (dv *DescribeView) PreviewScrollInfo() widget.ScrollInfo {
	return dv.scriptLV.ScrollInfo()
}

// PreviewView implements PreviewProvider — renders the script pane.
func (dv *DescribeView) PreviewView() string {
	if dv.scriptH <= 0 {
		return ""
	}
	if dv.script == "" {
		rows := make([]string, dv.scriptH)
		rows[0] = "  " + dv.theme.Log.Dim.Render("Loading…")
		return strings.Join(rows, "\n")
	}
	rows := dv.scriptLV.RenderRows()
	if dv.footerActive() {
		rows = append(rows, dv.renderFooter())
	}
	return strings.Join(rows, "\n")
}

func (dv *DescribeView) PopupView() string {
	return dv.host.PopupView()
}

// SetPreviewSize implements PreviewProvider.
func (dv *DescribeView) SetPreviewSize(w, h int) {
	dv.scriptW, dv.scriptH = w, h
	dv.layoutScript()
}

// PreviewBreadcrumb implements PreviewProvider.
func (dv *DescribeView) PreviewBreadcrumb() BreadcrumbSegment {
	seg := dv.MakeBreadcrumb("script")
	seg.NoTail = true
	return seg
}

// PreviewItemCount implements PreviewProvider.
func (dv *DescribeView) PreviewItemCount() int {
	return dv.scriptLV.ItemCount()
}

// Badge implements HasBadge. Only renders when the script *is* the main panel
// (no params). When params are showing, the script lives in the preview pane,
// and the issue count goes to PreviewBadge instead — otherwise the same badge
// would appear on both panels.
func (dv *DescribeView) Badge() string {
	if dv.hasActivePreview() {
		return ""
	}
	return dv.issueBadge()
}

// PreviewBadge implements HasPreviewBadge — issue count for the script preview
// pane. Empty when the script is in the main panel (Badge handles it there).
func (dv *DescribeView) PreviewBadge() string {
	if !dv.hasActivePreview() {
		return ""
	}
	return dv.issueBadge()
}

// issueBadge renders the validation issue count. Counts *all* issues, not just
// unique flagged lines — multiple issues on the same line still represent
// distinct problems the user needs to see, even though the gutter highlights
// the line once.
func (dv *DescribeView) issueBadge() string {
	if len(dv.issues) == 0 {
		return ""
	}
	icon := iconOr(dv.theme.Icons.Error, "✕")
	return dv.theme.Log.Error.Render(fmt.Sprintf("%s %d", icon, len(dv.issues)))
}

func (dv *DescribeView) Title() string {
	return fmt.Sprintf("Build #%d", dv.build.Number)
}

func (dv *DescribeView) Breadcrumb() BreadcrumbSegment {
	label := "parameters"
	if !dv.hasActivePreview() {
		label = "script"
	}
	seg := dv.MakeBreadcrumb(label)
	seg.NavTag = "describe"
	return seg
}

func (dv *DescribeView) ItemCount() int {
	if !dv.hasActivePreview() {
		return dv.scriptLV.ItemCount()
	}
	return len(dv.paramLines)
}

func (dv *DescribeView) ContentHeightHint() int {
	return len(dv.paramLines)
}

func (dv *DescribeView) Commands() []command.Command {
	return nil
}

func (dv *DescribeView) Shortcuts() []component.Shortcut {
	shortcuts := []component.Shortcut{
		component.Nav("esc", "back"),
		component.Filter("/", "search", false),
		component.Filter("w", "wrap", dv.scriptLV.Wrap()),
		component.Action("e", "edit"),
		{Key: "c", Action: dv.scriptLV.LogLabel(), Active: dv.copyLogFlash, Group: component.GroupAction, Rank: rankActionCopy},
	}
	shortcuts = append(shortcuts, detailViewTabs("d")...)
	shortcuts = dv.host.AppendShortcuts(shortcuts)
	if dv.scriptLV.SelectionInLog() {
		shortcuts = append(shortcuts, component.Shortcut{Key: "C", Action: dv.scriptLV.SelLabel(), Active: dv.copySelFlash, Group: component.GroupAction, Rank: rankActionCopy})
	}
	if dv.scriptLV.HasSearch() || dv.scriptLV.ErrorNavActive() {
		posInfo := dv.scriptLV.NavigationPositionInfo()
		nextLabel := "next"
		if posInfo != "" {
			nextLabel = "next " + posInfo
		}
		shortcuts = append(shortcuts, component.Nav("n/N", nextLabel))
	}
	return shortcuts
}

func (dv *DescribeView) SetSize(w, h int) {
	dv.BaseView.SetSize(w, h)
	dv.host.SetSize(w, h-6)
	if maxOff := max(0, len(dv.paramLines)-dv.height); dv.paramOffset > maxOff {
		dv.paramOffset = maxOff
	}
	if !dv.hasActivePreview() {
		dv.scriptW, dv.scriptH = w, h
		dv.layoutScript()
	}
}

func (dv *DescribeView) HasPopup() bool {
	return dv.host.HasPopup()
}

func (dv *DescribeView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	nc := dv.nc.AtBranch(dv.nc.BranchName)
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

// editorCommand builds an *exec.Cmd for the user's preferred editor.
// It respects $VISUAL, then $EDITOR, falling back to "vi".
// Editor values like "vim -u NONE" are handled by splitting on whitespace.
func editorCommand(filePath string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	args := append(parts[1:], filePath)
	return exec.Command(parts[0], args...)
}

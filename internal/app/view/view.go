package view

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// ContextLevel describes where in the job hierarchy a view is anchored.
type ContextLevel int

const (
	CtxRoot    ContextLevel = iota // "*" — global scope
	CtxFolder                      // a folder job
	CtxProject                     // a multibranch project (or standalone job)
	CtxBranch                      // a branch/MR within a multibranch project
	CtxBuild                       // a specific build
	CtxStage                       // a specific stage
)

// NavBuildRef is a navigation cursor for a build. The three fields are
// orthogonal, not alternatives:
//
//	Number/DisplayName — the identity of the build the cursor currently points at.
//	IsLast            — whether the cursor FOLLOWS the newest build ("#last").
//
// A "#last" cursor is therefore normally *also* resolved: {IsLast: true,
// Number: 44, DisplayName: "Release 1.0.7"} means "the #last alias, currently
// build 44". Both halves are rendered (`#last → Release 1.0.7`); neither is
// dropped in favour of the other. Distinct from jmodel.BuildRef (API ref).
type NavBuildRef struct {
	Number int  // 0 = unresolved
	IsLast bool // true = "#last" moving reference
	// DisplayName is the build's custom display name (Jenkins `displayName`) when
	// set, e.g. "release-2.3.1". Empty for the default "#<number>" name. When
	// present it replaces the number in the breadcrumb. Kept in sync with the
	// live build so a name set mid-run is reflected immediately.
	DisplayName string
}

// Resolved reports whether the cursor points at a concrete build.
func (r NavBuildRef) Resolved() bool { return r.Number > 0 }

// NavigationContext is the unified navigation state passed through view constructors.
// It replaces the ad-hoc (jobPath, jobName, branchName) tuple.
type NavigationContext struct {
	Level       ContextLevel
	FolderPath  string // slash-separated folder prefix, e.g. "Code Private"
	ProjectName string // multibranch project or standalone job name
	BranchName  string // branch/MR name (URL-encoded as Jenkins stores it)
	Build       NavBuildRef
	StageName   string // leaf stage name; used for lookups and back-navigation
	StageParent string // immediate parent stage name, for breadcrumb disambiguation
	// AliasScope is the location level the "#last" alias was anchored at
	// (root/folder/project/branch). Only meaningful when Build.IsLast. It is
	// what lets any view — not just the one that did the resolving — split the
	// breadcrumb into "what the user asked for" and "what it resolved to":
	// a root-anchored #last renders `*/#last → project ↳branch #44`, a
	// branch-anchored one `project ↳branch #last → #44`.
	AliasScope ContextLevel
	// ViewName is the Jenkins view the job list is filtered by. Only meaningful
	// at CtxRoot/CtxFolder; deeper levels ignore it but carry it so returning
	// to the job list restores the filter. Empty = unfiltered.
	ViewName     string
	Username     string   // authenticated user (for mine filter); set at root
	GitUsernames []string // additional names to match in trigger descriptions (e.g. GitLab push)
	FriendlyName string   // display name for the user (e.g. "Brecht Derwael")
}

// AtBuild returns a CtxBuild NC pinned to a concrete build number, inheriting
// all location fields and Username from the receiver. Pinning is deliberate:
// any "#last" alias the receiver carried is dropped. Use AtBuildRef to move to
// a build without deciding that question.
func (nc NavigationContext) AtBuild(number int) NavigationContext {
	return nc.AtBuildRef(NavBuildRef{Number: number})
}

// AtBuildRef returns a CtxBuild NC for the given build cursor, inheriting all
// location fields and Username from the receiver. This is the single edge for
// "navigate to this build": sibling swaps and back-navigation pass the ref they
// already hold so alias-ness and display name survive the hop.
func (nc NavigationContext) AtBuildRef(ref NavBuildRef) NavigationContext {
	if ref.IsLast {
		return nc.AtLastBuild(ref)
	}
	nc.Level = CtxBuild
	nc.Build = ref
	nc.AliasScope = CtxRoot
	nc.StageName = ""
	nc.StageParent = ""
	return nc
}

// AtLastBuild returns a CtxBuild NC following the "#last" alias, anchored at
// the receiver's location level. ref carries whatever resolution is already
// known (number/display name); pass a zero ref when the alias is unresolved.
func (nc NavigationContext) AtLastBuild(ref NavBuildRef) NavigationContext {
	anchor := nc.AtScope().Level
	ref.IsLast = true
	nc.Level = CtxBuild
	nc.Build = ref
	nc.AliasScope = anchor
	nc.StageName = ""
	nc.StageParent = ""
	return nc
}

// AtStage returns a CtxStage NC for the given stage name, inheriting all
// location fields (including build) and Username from the receiver.
func (nc NavigationContext) AtStage(stageName string) NavigationContext {
	nc.Level = CtxStage
	nc.StageName = stageName
	nc.StageParent = ""
	return nc
}

// AtBranch returns a CtxBranch NC for the given branch name, inheriting
// folder/project and Username from the receiver.
func (nc NavigationContext) AtBranch(branchName string) NavigationContext {
	nc.Level = CtxBranch
	nc.BranchName = branchName
	nc.Build = NavBuildRef{}
	nc.StageName = ""
	nc.StageParent = ""
	return nc
}

// NavigationContextProvider is implemented by views that carry a NavigationContext.
// Used by app.go to determine the current scope for cross-cutting commands.
type NavigationContextProvider interface {
	NC() NavigationContext
}

// ClipTo returns nc with all fields below `level` cleared and Level set to
// `level`. This is the single source of truth for "what does this scope own?":
// a CtxBuild view's nc never contains a stage name; a CtxBranch nc never
// contains a build; etc. Every view constructor that stores an nc clips it on
// entry so the breadcrumb, child-view nc projection, and any derived state can
// never carry stale fields inherited from a deeper scope.
func (nc NavigationContext) ClipTo(level ContextLevel) NavigationContext {
	if level < CtxStage {
		nc.StageName = ""
		nc.StageParent = ""
	}
	if level < CtxBuild {
		nc.Build = NavBuildRef{}
		nc.AliasScope = CtxRoot
	}
	if level < CtxBranch {
		nc.BranchName = ""
	}
	if level < CtxProject {
		nc.ProjectName = ""
	}
	if level < CtxFolder {
		nc.FolderPath = ""
	}
	nc.Level = level
	return nc
}

// AtScope strips build- and stage-level detail from the NavigationContext,
// returning the highest scope level implied by the populated fields.
// CtxBuild/CtxStage → CtxBranch (if branch set), else CtxProject, etc.
func (nc NavigationContext) AtScope() NavigationContext {
	nc.Build = NavBuildRef{}
	nc.AliasScope = CtxRoot
	nc.StageName = ""
	nc.StageParent = ""
	if nc.BranchName != "" {
		nc.Level = CtxBranch
		return nc
	}
	nc.BranchName = ""
	if nc.ProjectName != "" {
		nc.Level = CtxProject
		return nc
	}
	nc.ProjectName = ""
	if nc.FolderPath != "" {
		nc.Level = CtxFolder
		return nc
	}
	nc.FolderPath = ""
	nc.Level = CtxRoot
	return nc
}

// JobPath reconstructs the slash-joined API path from the context fields.
func (nc NavigationContext) JobPath() string {
	var parts []string
	if nc.FolderPath != "" {
		parts = append(parts, nc.FolderPath)
	}
	if nc.ProjectName != "" {
		parts = append(parts, nc.ProjectName)
	}
	if nc.BranchName != "" {
		parts = append(parts, nc.BranchName)
	}
	return strings.Join(parts, "/")
}

// Filters holds the active filter state for views that support mine/running filters.
type Filters struct {
	Running bool
	Mine    bool
}

// Filterable is optionally implemented by views that support mine/running filters.
type Filterable interface {
	ActiveFilters() Filters
	ToggleMine()
	ToggleRunning()
}

// BaseView is the embeddable foundation for every nc-anchored view. It owns
// the cross-cutting plumbing — theme, client, store, navigation context,
// cancellation, panel size — and provides default Close/SetSize/NC/Scope
// implementations. Concrete views override only what they need (e.g.
// StageLogView.Close flushes node logs before cancel; views with widgets
// override SetSize to lay them out).
type BaseView struct {
	theme  theme.Theme
	client jmodel.JenkinsClient
	store  *cache.Store

	nc    NavigationContext
	scope ContextLevel

	ctx    context.Context
	cancel context.CancelFunc

	width  int
	height int
}

// NewBaseView constructs a BaseView with a cancellable context and the nc
// clipped to the declared scope. Every concrete view's constructor should
// route through here so the scope invariant is enforced exactly once.
func NewBaseView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store, nc NavigationContext, scope ContextLevel) BaseView {
	ctx, cancel := context.WithCancel(context.Background())
	return BaseView{
		theme:  t,
		client: c,
		store:  s,
		nc:     nc.ClipTo(scope),
		scope:  scope,
		ctx:    ctx,
		cancel: cancel,
	}
}

// NC returns the (already-clipped) navigation context.
func (b *BaseView) NC() NavigationContext { return b.nc }

// Scope returns the navigation level this view is anchored at.
func (b *BaseView) Scope() ContextLevel { return b.scope }

// SeedBuildIdentity fills in build identity the nc does not have yet, from the
// build a view was constructed with. It never overwrites what is already there
// (a caller passing a bare jmodel.Build{Number: n} must not wipe a display name
// the nc carried in) and never touches alias state.
func (b *BaseView) SeedBuildIdentity(build jmodel.Build) {
	if build.Number <= 0 {
		return
	}
	if b.nc.Build.Number == 0 {
		b.nc.Build.Number = build.Number
	}
	if b.nc.Build.DisplayName == "" {
		b.nc.Build.DisplayName = build.Name
	}
}

// SyncBuildIdentity updates the nc from an authoritative observation of the
// live build — a poll tick, a cache restore — so a display name set or changed
// mid-run is reflected everywhere the nc travels. Alias state is preserved:
// a "#last" cursor stays a "#last" cursor, it just learns what it points at.
func (b *BaseView) SyncBuildIdentity(build jmodel.Build) {
	if build.Number <= 0 {
		return
	}
	b.nc.Build.Number = build.Number
	b.nc.Build.DisplayName = build.Name
}

// MakeBreadcrumb returns a scope-clipped BreadcrumbSegment. Views decorate it
// (NavTag, Running, Mine, Failed, ResolvedParts, etc.) before returning.
func (b *BaseView) MakeBreadcrumb(viewType string) BreadcrumbSegment {
	return BreadcrumbFor(viewType, b.nc, b.scope)
}

// SetSize stores the panel dimensions. Views that own internal widgets
// override this to propagate the size; the override can still update the
// embedded fields via b.width / b.height.
func (b *BaseView) SetSize(w, h int) { b.width, b.height = w, h }

// Close cancels the view's context. Views with extra teardown (flushing
// caches, closing providers) override this and chain to b.cancel themselves.
func (b *BaseView) Close() error {
	if b.cancel != nil {
		b.cancel()
	}
	return nil
}

// BreadcrumbFor builds a BreadcrumbSegment from a view type name, a
// NavigationContext, and the scope this view owns. The nc is clipped to
// `scope` before rendering, so a CtxBuild view can never emit a stage tail
// even if its caller forwarded an nc that still had StageName set. This is the
// single declaration point: each view says "I'm `<viewType>` at `<scope>`"
// and the framework handles projection.
func BreadcrumbFor(viewType string, nc NavigationContext, scope ContextLevel) BreadcrumbSegment {
	nc = nc.ClipTo(scope)
	ctx, resolved := breadcrumbParts(nc)
	return BreadcrumbSegment{ViewType: viewType, Context: ctx, ResolvedParts: resolved}
}

// breadcrumbParts splits a NavigationContext into the context parts and the
// resolved tail shown after the " → " arrow.
//
// There is exactly one rule. Without a "#last" alias everything is context:
// path → build → stage. With one, the context stops at the level the alias was
// anchored at and ends in "#last"; everything the alias resolved to (the path
// segments below the anchor, the build identity, the stage) forms the tail.
// Because both halves come from the nc alone, every view renders the same
// breadcrumb for the same location — no view needs its own resolver.
func breadcrumbParts(nc NavigationContext) (ctx, resolved []component.BreadcrumbPart) {
	if !nc.Build.IsLast {
		ctx = pathParts(nc)
		if p, ok := buildPart(nc.Build); ok {
			ctx = append(ctx, p)
		}
		return append(ctx, stageParts(nc)...), nil
	}

	anchor := nc.AliasScope
	if anchor > CtxBranch {
		anchor = CtxBranch // aliases anchor at a location, never at a build
	}
	ctx = pathParts(nc.ClipTo(anchor))
	ctx = append(ctx, component.BreadcrumbPart{Text: "last", IsBuildNum: true, Separator: " "})

	resolved = pathPartsBelow(nc, anchor)
	if p, ok := buildPart(NavBuildRef{Number: nc.Build.Number, DisplayName: nc.Build.DisplayName}); ok {
		resolved = append(resolved, p)
	}
	if len(resolved) == 0 {
		// Alias not resolved yet — the stage (if any) still belongs to the
		// context, so it isn't lost behind an arrow that never renders.
		return append(ctx, stageParts(nc)...), nil
	}
	return ctx, append(resolved, stageParts(nc)...)
}

// pathParts renders the location portion: * (global scope) → folder → project → branch.
func pathParts(nc NavigationContext) []component.BreadcrumbPart {
	var parts []component.BreadcrumbPart
	switch {
	case nc.Level == CtxFolder:
		// Folder-scoped view: show the folder's short name.
		parts = append(parts, component.BreadcrumbPart{Text: shortName(decodeName(nc.FolderPath))})
	case nc.ProjectName == "" && nc.BranchName == "":
		parts = append(parts, component.BreadcrumbPart{Text: "*"})
	}
	if nc.ProjectName != "" {
		parts = append(parts, component.BreadcrumbPart{Text: shortName(decodeName(nc.ProjectName))})
	}
	if nc.BranchName != "" {
		parts = append(parts, component.BreadcrumbPart{
			Text:      decodeName(nc.BranchName),
			Separator: branchIcon(nc.BranchName),
		})
	}
	return parts
}

// pathPartsBelow renders only the location parts the alias anchor did not
// already cover — what "#last" resolved to beyond what the user asked for.
func pathPartsBelow(nc NavigationContext, anchor ContextLevel) []component.BreadcrumbPart {
	var parts []component.BreadcrumbPart
	if anchor < CtxProject && nc.ProjectName != "" {
		parts = append(parts, component.BreadcrumbPart{Text: shortName(decodeName(nc.ProjectName))})
	}
	if anchor < CtxBranch && nc.BranchName != "" {
		parts = append(parts, component.BreadcrumbPart{
			Text:      decodeName(nc.BranchName),
			Separator: branchIcon(nc.BranchName),
		})
	}
	return parts
}

// buildPart renders a resolved build's identity: its display name when Jenkins
// has one, else "#<number>". Reports false when the ref is unresolved.
func buildPart(ref NavBuildRef) (component.BreadcrumbPart, bool) {
	if ref.DisplayName != "" {
		return component.BreadcrumbPart{Text: ref.DisplayName, IsBuildNum: true, NoHashPrefix: true, Separator: " "}, true
	}
	if ref.Number > 0 {
		return component.BreadcrumbPart{Text: fmt.Sprintf("%d", ref.Number), IsBuildNum: true, Separator: " "}, true
	}
	return component.BreadcrumbPart{}, false
}

// stageParts renders the stage tail. The immediate parent is its own part so
// the leaf never truncates and only the (often long) parent front-truncates.
// This disambiguates non-unique leaves like matrix-cell "Build" stages.
func stageParts(nc NavigationContext) []component.BreadcrumbPart {
	if nc.StageName == "" {
		return nil
	}
	if nc.StageParent != "" {
		return []component.BreadcrumbPart{
			{Text: nc.StageParent, Separator: ":"},
			{Text: nc.StageName, Separator: " › "},
		}
	}
	return []component.BreadcrumbPart{{Text: nc.StageName, Separator: ":"}}
}

// NCFromJobPath is the exported form of ncFromJobPath, for CLI deep-linking from
// a resolved job path.
func NCFromJobPath(jobPath string) NavigationContext { return ncFromJobPath(jobPath) }

// ncFromJobPath reconstructs a NavigationContext from a raw Jenkins job path.
// The last two segments are treated as project+branch (or project only if just one).
// Used by views that receive a raw path string (e.g. RunningBuildsView).
func ncFromJobPath(jobPath string) NavigationContext {
	parts := strings.Split(jobPath, "/")
	switch len(parts) {
	case 0:
		return NavigationContext{Level: CtxRoot}
	case 1:
		return NavigationContext{Level: CtxProject, ProjectName: parts[0]}
	case 2:
		return NavigationContext{Level: CtxBranch, ProjectName: parts[0], BranchName: parts[1]}
	default:
		return NavigationContext{
			Level:       CtxBranch,
			FolderPath:  strings.Join(parts[:len(parts)-2], "/"),
			ProjectName: parts[len(parts)-2],
			BranchName:  parts[len(parts)-1],
		}
	}
}

// View is implemented by each screen (dashboard, job, build, etc.)
type View interface {
	tea.Model

	// Title returns the breadcrumb segment for this view.
	Title() string

	// ItemCount returns the number of items in the current view.
	ItemCount() int

	// Commands returns commands available in this view's context.
	Commands() []command.Command

	// Shortcuts returns context-sensitive key binding hints for the header.
	// Return nil if the view has no view-specific shortcuts.
	Shortcuts() []component.Shortcut

	// SetSize updates the view dimensions.
	SetSize(width, height int)

	// Close is called when the view is popped from the navigation stack.
	// Views with background work (streaming, polling) should cancel it here.
	Close() error
}

// Searchable is optionally implemented by views that support regex filtering.
type Searchable interface {
	ApplySearch(pattern string) tea.Cmd
	SearchQuery() string
}

// SearchResultHandler is optionally implemented by views that host a LogViewer.
// The app routes widget.SearchResultMsg to the active view so results reach the right LogViewer.
type SearchResultHandler interface {
	HandleSearchResult(widget.SearchResultMsg) tea.Cmd
}

// NavigationClearable is optionally implemented by log views that support
// dropping the active match selection via a first Esc press, before a second
// Esc closes search entirely.
type NavigationClearable interface {
	HasActiveNavigation() bool
	ClearActiveNavigation()
}

// PopupLayer is optionally implemented by views that can show overlaid popups
// (confirm dialogs, param forms, etc.). The app checks this before routing ESC
// to the navigation stack so that ESC always closes the topmost popup first.
// PopupView returns the rendered popup box (unpositioned); the app overlays it
// at full terminal dimensions so it is never clipped by panel borders.
type PopupLayer interface {
	HasPopup() bool
	PopupView() string
}

// BreadcrumbSegment describes a single k9s-style breadcrumb: viewType(context)[count].
type BreadcrumbSegment struct {
	ViewType      string                     // "jobs", "builds", "stages", "log", "running", "preview"
	Context       []component.BreadcrumbPart // parts joined by "/" (or ":" for the last separator)
	Running       bool                       // filter active — rendered as "running " prefix
	Mine          bool                       // filter active — rendered as "my " prefix
	Failed        bool                       // filter active — rendered as "failed " prefix
	ResolvedParts []component.BreadcrumbPart // resolved #last info, shown after " → " arrow
	// NavTag, when non-empty, overrides ViewType for the bottom-left nav tag only.
	// Use when the breadcrumb label and the nav position label should differ.
	NavTag string
	// NavChain, when non-empty, overrides the derived nav-tag trail entirely.
	// Used by views whose ancestor chain depends on where they were opened
	// (e.g. metadata: <jobs> <builds> <stages> <metadata>).
	NavChain []string
	// NoTail, when set on a PreviewBreadcrumb, suppresses [tail] and shows [count] instead.
	NoTail bool
}

// BreadcrumbProvider is optionally implemented by views that supply k9s-style breadcrumbs.
type BreadcrumbProvider interface {
	Breadcrumb() BreadcrumbSegment
}

// shortName returns the part after the last "/" in a decoded name,
// stripping folder prefixes. If there are no slashes, returns the name as-is.
func shortName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// branchIcon returns an icon for a branch/MR job based on its name.
// Names like "PR-123" or "MR-123" are treated as merge/pull requests.
func branchIcon(name string) string {
	if strings.HasPrefix(name, "PR-") || strings.HasPrefix(name, "MR-") {
		return " ⊕"
	}
	return " ↳"
}

// iconOr returns override if non-empty, otherwise fallback.
func iconOr(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// statusIcon returns a single icon for a build status, respecting theme overrides.
func statusIcon(t theme.Theme, s jmodel.BuildStatus) string {
	switch s {
	case jmodel.BuildStatusRunning:
		return iconOr(t.Icons.StatusRunning, "●")
	case jmodel.BuildStatusSuccess:
		return iconOr(t.Icons.StatusSuccess, "✔")
	case jmodel.BuildStatusFailed:
		return iconOr(t.Icons.StatusFailed, "✖")
	case jmodel.BuildStatusAborted:
		return iconOr(t.Icons.StatusAborted, "◼")
	case jmodel.BuildStatusUnstable:
		return iconOr(t.Icons.StatusUnstable, "⚠")
	case jmodel.BuildStatusSkipped:
		return iconOr(t.Icons.StatusSkipped, "◇")
	case jmodel.BuildStatusNotBuilt:
		return iconOr(t.Icons.StatusNotBuilt, "◻")
	case jmodel.BuildStatusPausedInput:
		return iconOr(t.Icons.StatusPausedInput, "⏸")
	default:
		return iconOr(t.Icons.StatusUnknown, "○")
	}
}

// StatusLabel returns a human-readable, capitalized label for a build status.
func StatusLabel(s jmodel.BuildStatus) string {
	return statusLabel(s)
}

// statusLabel returns a human-readable, capitalized label for a build status.
func statusLabel(s jmodel.BuildStatus) string {
	switch s {
	case jmodel.BuildStatusSkipped:
		return "Skipped"
	case jmodel.BuildStatusNotBuilt:
		return "Not Built"
	case jmodel.BuildStatusPausedInput:
		return "Input"
	default:
		str := string(s)
		if len(str) == 0 {
			return str
		}
		return strings.ToUpper(str[:1]) + str[1:]
	}
}

// vs15 (U+FE0E VARIATION SELECTOR-15) forces text (non-emoji, 1-cell) presentation
// for the preceding character. Without it many terminals render ☀/⛅/⛈ as 2-wide emoji,
// which misaligns table columns.
const vs15 = "\uFE0E"

// renderWeatherIcon renders a single Jenkins-idiomatic weather glyph for the
// MAIN column: ☀ success (yellow), ⛅ unstable (orange), ⛈ failed (blue).
// Returns "" for jobs with no meaningful build history.
// Themes can override the glyphs via Icons.Weather*.
func renderWeatherIcon(t theme.Theme, s jmodel.BuildStatus) string {
	switch s {
	case jmodel.BuildStatusSuccess:
		icon := iconOr(t.Icons.WeatherSun, "☀"+vs15)
		return t.Weather.Sun.Render(icon)
	case jmodel.BuildStatusUnstable:
		icon := iconOr(t.Icons.WeatherUnstable, "⛅"+vs15)
		return t.Weather.Unstable.Render(icon)
	case jmodel.BuildStatusFailed:
		icon := iconOr(t.Icons.WeatherStorm, "⛈"+vs15)
		return t.Weather.Storm.Render(icon)
	default:
		return ""
	}
}

// renderStatus returns a pre-colored status string for display.
func renderStatus(t theme.Theme, s jmodel.BuildStatus) string {
	text := statusIcon(t, s) + " " + statusLabel(s)
	switch s {
	case jmodel.BuildStatusRunning:
		return t.BuildStatus.Running.Render(text)
	case jmodel.BuildStatusSuccess:
		return t.BuildStatus.Success.Render(text)
	case jmodel.BuildStatusFailed:
		return t.BuildStatus.Failed.Render(text)
	case jmodel.BuildStatusAborted:
		return t.BuildStatus.Aborted.Render(text)
	case jmodel.BuildStatusUnstable:
		return t.BuildStatus.Unstable.Render(text)
	case jmodel.BuildStatusSkipped:
		return t.BuildStatus.Aborted.Render(text)
	case jmodel.BuildStatusNotBuilt:
		return t.BuildStatus.Aborted.Render(text)
	case jmodel.BuildStatusPausedInput:
		return t.BuildStatus.PausedInput.Render(text)
	default:
		return text
	}
}

// renderQueueStatus renders the STATUS badge for a waiting build-queue row,
// mapping the sub-state onto an existing BuildStatus style so no new theme
// fields are needed.
func renderQueueStatus(t theme.Theme, state string) string {
	switch state {
	case "stuck":
		return t.BuildStatus.Unstable.Render("⚠ stuck")
	case "blocked":
		return t.BuildStatus.PausedInput.Render("⏸ blocked")
	case "pending":
		return t.BuildStatus.Running.Render("● starting")
	default:
		return t.BuildStatus.Aborted.Render("⧖ queued")
	}
}

// queueStateLabel renders the inline "badge · why" label used by the pending
// StageView while a queued build waits for an executor.
func queueStateLabel(it jmodel.QueueItem) string {
	badge := map[string]string{
		"stuck":     "⚠ stuck",
		"blocked":   "⏸ blocked",
		"pending":   "● starting",
		"buildable": "⧖ queued",
	}[queueSubState(it)]
	if it.Why != "" {
		return badge + " · " + it.Why
	}
	return badge
}

// isBuildPausedOnInput reports whether the given build has cached pending
// input data — i.e. the StageView (or a sibling) has seen the build paused
// on an `input` step. Build/job lists use this to swap the progress bar for
// a paused badge without making extra HTTP calls; the cache populates
// naturally whenever the user drills into a paused build.
func isBuildPausedOnInput(store *cache.Store, jobPath string, buildNumber int) bool {
	if store == nil || store.PendingInputs == nil {
		return false
	}
	key := fmt.Sprintf("%s:%d", jobPath, buildNumber)
	e := store.PendingInputs.Get(key)
	return e != nil && len(e.Value) > 0
}

// renderRunningStatus renders a progress bar when an estimate is available,
// falling back to the plain Running status badge when it is not.
func renderRunningStatus(t theme.Theme, pb component.ProgressBar, width int, elapsed, estimated time.Duration) string {
	if estimated > 0 {
		return pb.DualRenderWithText(width, elapsed, estimated)
	}
	return renderStatus(t, jmodel.BuildStatusRunning)
}

// renderTestBadge renders a compact badge for a test report, omitting zero counts.
// "✔54 ✖0 ~0" → "✔54", "✔2 ✖3 ~0" → "✔2 ✖3".
// Returns "" when r is nil (no tests or not yet loaded).
func renderTestBadge(t theme.Theme, r *jmodel.TestReport) string {
	if r == nil {
		return ""
	}
	passIcon := iconOr(t.Icons.StatusSuccess, "✔")
	failIcon := iconOr(t.Icons.StatusFailed, "✖")
	var parts []string
	if r.Passed > 0 {
		parts = append(parts, t.BuildStatus.Success.Render(fmt.Sprintf("%s%d", passIcon, r.Passed)))
	}
	if r.Failed > 0 {
		parts = append(parts, t.BuildStatus.Failed.Render(fmt.Sprintf("%s%d", failIcon, r.Failed)))
	}
	if r.Skipped > 0 {
		parts = append(parts, t.BuildStatus.Aborted.Render(fmt.Sprintf("~%d", r.Skipped)))
	}
	return strings.Join(parts, " ")
}

// OpenURL launches the system browser for url and reaps the child process in
// a goroutine, so it does not linger as a zombie once xdg-open detaches.
func OpenURL(url string) {
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }()
}

func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		OpenURL(url)
		return nil
	}
}

// OpenURLCmd is the exported entry point for opening a URL in the system
// browser, used by the app layer (e.g. the :artifact command for non-text files).
func OpenURLCmd(url string) tea.Cmd { return openURLCmd(url) }

// artifactShortcutAction returns the Action string for the <A> shortcut.
// With a single artifact it shows the display name (truncated); otherwise "artifacts: N".
func artifactShortcutAction(artifacts []jmodel.Artifact) string {
	if len(artifacts) == 1 {
		name := artifacts[0].DisplayPath
		if len(name) > 19 {
			name = name[:18] + "…"
		}
		return name
	}
	return fmt.Sprintf("artifacts [%d]", len(artifacts))
}

// FullScreen is optionally implemented by views that bypass all app chrome
// (header, borders, breadcrumb, nav tags) and render to the full terminal.
type FullScreen interface {
	IsFullScreen() bool
}

// BorderlessContent is optionally implemented by views that render their own
// framing (e.g. a grid of bordered panes) and want the app to skip the outer
// content-panel border and its breadcrumb title. Header, command bar, and nav
// tags are still drawn. The view is given the full panel footprint via SetSize.
type BorderlessContent interface {
	IsBorderlessContent() bool
}

// RunningLogView is optionally implemented by log views that can report whether
// their build is still actively running.
type RunningLogView interface {
	IsBuildRunning() bool
}

// HasBadge is optionally implemented by views that want a badge shown
// right-aligned in the top border of the content panel.
type HasBadge interface {
	Badge() string
}

// HasPreviewBadge is optionally implemented by views that also provide
// a badge for the preview panel's top border.
type HasPreviewBadge interface {
	PreviewBadge() string
}

// ContentHeightHint is optionally implemented by views that want a fixed-height
// main panel when paired with a PreviewProvider. The returned value is the
// desired number of content rows; the preview panel takes the remaining space.
type ContentHeightHint interface {
	ContentHeightHint() int
}

// HasScrollInfo is optionally implemented by views that support vertical scrolling.
// App uses this to render a scrollbar indicator on the right border of the content panel.
type HasScrollInfo interface {
	ScrollInfo() widget.ScrollInfo
}

// HasPreviewScrollInfo is optionally implemented by views that support vertical
// scrolling in their preview panel.
type HasPreviewScrollInfo interface {
	PreviewScrollInfo() widget.ScrollInfo
}

// PreviewProvider is optionally implemented by views that want a separate
// bordered panel below their main content to show a live preview.
type PreviewProvider interface {
	// PreviewView renders the preview panel content.
	PreviewView() string
	// SetPreviewSize sets the preview panel's inner dimensions.
	SetPreviewSize(width, height int)
	// PreviewBreadcrumb returns the breadcrumb segment for the preview panel.
	PreviewBreadcrumb() BreadcrumbSegment
	// PreviewItemCount returns the number of items in the preview panel.
	PreviewItemCount() int
}

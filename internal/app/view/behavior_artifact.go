package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// buildAccessor returns the (NavigationContext, Build) the behavior should
// act on, or ok=false when the current view state has no resolvable build
// (e.g. an unselected row in a list view).
type buildAccessor func() (nc NavigationContext, build jmodel.Build, ok bool)

// fixedBuildAccessor adapts pointers held by a view (NC + build set at
// construction) into a buildAccessor. Use this for views whose build is
// constant for their lifetime: console, describe, testreport, stageview.
func fixedBuildAccessor(nc *NavigationContext, build *jmodel.Build) buildAccessor {
	return func() (NavigationContext, jmodel.Build, bool) {
		// Sync build number from nc when build.Number is 0 (some views init
		// build lazily — see ConsoleView).
		b := *build
		if b.Number == 0 {
			b.Number = nc.Build.Number
		}
		if b.Number == 0 {
			return NavigationContext{}, jmodel.Build{}, false
		}
		return *nc, b, true
	}
}

// navigateCmd is how a behavior asks the app to open a child view. Views that
// replace themselves (console, describe, testreport) wrap in SwapViewMsg;
// views that push a child (buildsview) wrap in PushViewMsg.
type navigateCmd func(child View) tea.Cmd

// swapTo returns a navigateCmd that emits SwapViewMsg.
func swapTo(child View) tea.Cmd {
	return func() tea.Msg { return SwapViewMsg{View: child} }
}

// pushTo returns a navigateCmd that emits PushViewMsg.
func pushTo(child View) tea.Cmd {
	return func() tea.Msg { return PushViewMsg{View: child} }
}

// popSwapTo returns a navigateCmd that emits PopSwapViewMsg. Use for views
// that were pushed onto the stack and navigate sideways to a sibling.
func popSwapTo(child View) tea.Cmd {
	return func() tea.Msg { return PopSwapViewMsg{View: child} }
}

// artifactBehavior encapsulates the "A" shortcut: look up the cached
// artifacts for the current build and either open the single URL or push an
// ArtifactView listing all of them. Pre-extraction this snippet was
// duplicated across 6 views (describe, testreport, console, buildsview,
// stageview, joblist) with only the field-access surface varying.
type artifactBehavior struct {
	theme    theme.Theme
	client   jmodel.JenkinsClient
	store    func() *cache.Store // lazy: some views set store after construction
	access   buildAccessor
	navigate navigateCmd

	// observed is the artifact list delivered by the most recent
	// ArtifactsMsg, keyed by the build it describes. The store cache is not a
	// reliable channel on its own: fetchArtifacts only persists a list once
	// the registry has confirmed the build terminal, which lags the moment a
	// view observes the build finish. Holding the delivered list here means
	// "A" lights up as soon as the data arrives, cached or not.
	observed    []jmodel.Artifact
	observedKey string
}

// newArtifactBehavior wires the artifact shortcut for a fixed-build view.
// store is a getter so views that assign store post-construction (console,
// joblist children) still resolve correctly. navigate selects child-view
// placement (swapTo / pushTo).
func newArtifactBehavior(t theme.Theme, client jmodel.JenkinsClient, store func() *cache.Store, access buildAccessor, navigate navigateCmd) *artifactBehavior {
	return &artifactBehavior{theme: t, client: client, store: store, access: access, navigate: navigate}
}

func (b *artifactBehavior) SetTheme(t theme.Theme) { b.theme = t }

// HandleMsg records the artifact list carried by an ArtifactsMsg. It never
// claims the message: list views run their own artifactTracker on the same
// broadcast, and consuming it here would starve them.
func (b *artifactBehavior) HandleMsg(msg tea.Msg) (bool, tea.Cmd) {
	am, ok := msg.(ArtifactsMsg)
	if !ok || am.Err != nil {
		return false, nil
	}
	b.observed = am.Artifacts
	b.observedKey = testKey(am.JobPath, am.BuildNum)
	return false, nil
}

func (b *artifactBehavior) PopupView() string { return "" }

// resolve returns the artifact list for the view's current build and the
// matching nc/build, preferring the list this behavior was handed over the
// store cache (which may not have been written — see the observed field).
func (b *artifactBehavior) resolve() (nc NavigationContext, build jmodel.Build, arts []jmodel.Artifact, ok bool) {
	nc, build, ok = b.access()
	if !ok {
		return NavigationContext{}, jmodel.Build{}, nil, false
	}
	key := testKey(nc.JobPath(), build.Number)
	if key == b.observedKey && len(b.observed) > 0 {
		return nc, build, b.observed, true
	}
	store := b.store()
	if store == nil {
		return NavigationContext{}, jmodel.Build{}, nil, false
	}
	entry := store.Artifacts.Get(key)
	if entry == nil || len(entry.Value) == 0 {
		return NavigationContext{}, jmodel.Build{}, nil, false
	}
	return nc, build, entry.Value, true
}

func (b *artifactBehavior) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if msg.String() != "A" {
		return false, nil
	}
	nc, build, arts, ok := b.resolve()
	if !ok {
		// Consume the key so view's own switch doesn't fall through to a
		// no-op default; matches prior behavior where the inline case "A"
		// silently did nothing when no artifacts were cached.
		return true, nil
	}
	if len(arts) == 1 {
		if IsTextArtifact(arts[0].DisplayPath) {
			// Always push the viewer (not the origin's swap) so its sideways
			// navigation can popSwap the parent, mirroring StageLogView.
			child := NewArtifactFileView(b.theme, b.client, b.store(), nc, arts[0], build, arts)
			return true, pushTo(child)
		}
		return true, openURLCmd(arts[0].URL)
	}
	child := NewArtifactView(b.theme, arts, nc, build, b.client, b.store())
	return true, b.navigate(child)
}

func (b *artifactBehavior) Shortcut() (component.Shortcut, bool) {
	_, _, arts, ok := b.resolve()
	if !ok {
		return component.Shortcut{}, false
	}
	return component.ViewSCRanked("A", artifactShortcutAction(arts), false, rankViewArtifacts), true
}

package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// buildAccessor returns the (NavigationContext, Build) the behavior should
// act on, or ok=false when the current view state has no resolvable build
// (e.g. an unselected row in a list view).
type buildAccessor func() (nc NavigationContext, build jenkins.Build, ok bool)

// fixedBuildAccessor adapts pointers held by a view (NC + build set at
// construction) into a buildAccessor. Use this for views whose build is
// constant for their lifetime: console, describe, testreport, stageview.
func fixedBuildAccessor(nc *NavigationContext, build *jenkins.Build) buildAccessor {
	return func() (NavigationContext, jenkins.Build, bool) {
		// Sync build number from nc when build.Number is 0 (some views init
		// build lazily — see ConsoleView).
		b := *build
		if b.Number == 0 {
			b.Number = nc.Build.Number
		}
		if b.Number == 0 {
			return NavigationContext{}, jenkins.Build{}, false
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
	client   jenkins.JenkinsClient
	store    func() *cache.Store // lazy: some views set store after construction
	access   buildAccessor
	navigate navigateCmd
}

// newArtifactBehavior wires the artifact shortcut for a fixed-build view.
// store is a getter so views that assign store post-construction (console,
// joblist children) still resolve correctly. navigate selects child-view
// placement (swapTo / pushTo).
func newArtifactBehavior(t theme.Theme, client jenkins.JenkinsClient, store func() *cache.Store, access buildAccessor, navigate navigateCmd) *artifactBehavior {
	return &artifactBehavior{theme: t, client: client, store: store, access: access, navigate: navigate}
}

func (b *artifactBehavior) SetTheme(t theme.Theme) { b.theme = t }

func (b *artifactBehavior) HandleMsg(tea.Msg) (bool, tea.Cmd) { return false, nil }
func (b *artifactBehavior) PopupView() string                 { return "" }

// resolve returns the cached artifact list and matching nc/build if all
// preconditions hold (store present, build number known, cache hit, non-empty).
func (b *artifactBehavior) resolve() (nc NavigationContext, build jenkins.Build, arts []jenkins.Artifact, ok bool) {
	store := b.store()
	if store == nil {
		return
	}
	nc, build, ok = b.access()
	if !ok {
		return NavigationContext{}, jenkins.Build{}, nil, false
	}
	entry := store.Artifacts.Get(testKey(nc.JobPath(), build.Number))
	if entry == nil || len(entry.Value) == 0 {
		return NavigationContext{}, jenkins.Build{}, nil, false
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
	return component.ViewSC("A", artifactShortcutAction(arts), false), true
}

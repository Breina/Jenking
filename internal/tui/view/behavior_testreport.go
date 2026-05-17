package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// testReportBehavior encapsulates the "T" shortcut: open a cached test
// report for the current build. Pre-extraction this snippet (cache lookup
// + NewTestReportView + Swap/Push) was duplicated across describe, console,
// stageview, etc.
type testReportBehavior struct {
	theme    theme.Theme
	client   jenkins.JenkinsClient
	store    func() *cache.Store
	access   buildAccessor
	navigate navigateCmd
}

func newTestReportBehavior(t theme.Theme, client jenkins.JenkinsClient, store func() *cache.Store, access buildAccessor, navigate navigateCmd) *testReportBehavior {
	return &testReportBehavior{theme: t, client: client, store: store, access: access, navigate: navigate}
}

func (b *testReportBehavior) SetTheme(t theme.Theme) { b.theme = t }

func (b *testReportBehavior) HandleMsg(tea.Msg) (bool, tea.Cmd) { return false, nil }
func (b *testReportBehavior) PopupView() string                 { return "" }

// resolve returns the cached test report when present and non-empty.
func (b *testReportBehavior) resolve() (nc NavigationContext, build jenkins.Build, report *jenkins.TestReport, ok bool) {
	store := b.store()
	if store == nil {
		return
	}
	nc, build, ok = b.access()
	if !ok {
		return NavigationContext{}, jenkins.Build{}, nil, false
	}
	entry := store.TestReports.Get(testKey(nc.JobPath(), build.Number))
	if entry == nil || entry.Value == nil || len(entry.Value.Suites) == 0 {
		return NavigationContext{}, jenkins.Build{}, nil, false
	}
	return nc, build, entry.Value, true
}

func (b *testReportBehavior) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if msg.String() != "T" {
		return false, nil
	}
	nc, build, report, ok := b.resolve()
	if !ok {
		return true, nil
	}
	child := NewTestReportView(b.theme, *report, nc, build, b.client, b.store())
	return true, b.navigate(child)
}

func (b *testReportBehavior) Shortcut() (component.Shortcut, bool) {
	_, _, report, ok := b.resolve()
	if !ok {
		return component.Shortcut{}, false
	}
	badge := renderTestBadge(b.theme, report)
	return component.ViewSC("T", "tests: "+badge, false), true
}

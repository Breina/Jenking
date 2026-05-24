package view

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

const (
	colTestStatusWidth   = 10
	colTestDurationWidth = 8
	// colTestsFixed is the total fixed width for status + duration + spacing.
	colTestsFixed = colTestStatusWidth + colTestDurationWidth + 4
)

// TestReportView displays a JUnit test report as a navigable suite/case tree.
type TestReportView struct {
	theme      theme.Theme
	table      component.Table
	report     jmodel.TestReport
	nc         NavigationContext
	build      jmodel.Build
	showFailed bool // when true, only failed cases (and their suites) are shown
	width      int
	height     int
	client     jmodel.JenkinsClient
	store      *cache.Store
	host       widget.BehaviorHost
}

// NewTestReportView creates a TestReportView for the given build's test report.
func NewTestReportView(t theme.Theme, report jmodel.TestReport, nc NavigationContext, build jmodel.Build, client jmodel.JenkinsClient, store *cache.Store) *TestReportView {
	columns := []component.Column{
		{Title: "NAME", Width: 40},
		{Title: "STATUS", Width: colTestStatusWidth},
		{Title: "DURATION", Width: colTestDurationWidth},
	}
	v := &TestReportView{
		theme:  t,
		table:  component.NewTable(t, columns),
		report: report,
		nc:     nc,
		build:  build,
		client: client,
		store:  store,
	}
	v.populateTable()
	access := fixedBuildAccessor(&v.nc, &v.build)
	storeFn := func() *cache.Store { return v.store }
	v.host.Add(newArtifactBehavior(t, client, storeFn, access, swapTo))
	return v
}

func (v *TestReportView) Init() tea.Cmd {
	return nil
}

func (v *TestReportView) populateTable() {
	var rows []component.Row
	disabled := map[int]bool{}

	for _, suite := range v.report.Suites {
		var passed, failed, skipped int
		for _, tc := range suite.Cases {
			switch tc.Status {
			case jmodel.TestStatusPassed:
				passed++
			case jmodel.TestStatusFailed:
				failed++
			default:
				skipped++
			}
		}

		// In failed-only mode, skip suites with no failures.
		if v.showFailed && failed == 0 {
			continue
		}

		suiteIdx := len(rows)
		suiteName := lipgloss.NewStyle().Bold(true).Render("Suite: " + suite.Name)
		suiteStatus := renderTestSuiteAggregate(v.theme, passed, failed, skipped)
		suiteDuration := formatTestDuration(suite.Duration)
		rows = append(rows, component.Row{suiteName, suiteStatus, suiteDuration})
		disabled[suiteIdx] = true

		for _, tc := range suite.Cases {
			if v.showFailed && tc.Status != jmodel.TestStatusFailed {
				continue
			}
			caseName := " ↳" + tc.Name
			caseStatus := renderTestCaseStatus(v.theme, tc.Status)
			caseDuration := formatTestDuration(tc.Duration)
			rows = append(rows, component.Row{caseName, caseStatus, caseDuration})
		}
	}

	v.table.SetRows(rows)
	v.table.SetDisabled(disabled)
	v.table.SetCursor(0)
}

func (v *TestReportView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ThemeChangedMsg:
		v.theme = msg.Theme
		v.table.SetTheme(msg.Theme)
		v.host.SetTheme(msg.Theme)
		v.populateTable()
		return v, nil

	case tea.KeyMsg:
		if handled, cmd := v.host.HandleKey(msg); handled {
			return v, cmd
		}
		switch msg.String() {
		case "up", "k":
			v.table.MoveUp()
		case "down", "j":
			v.table.MoveDown()
		case "pgup":
			v.table.PageUp()
		case "pgdown":
			v.table.PageDown()
		case "home":
			v.table.Home()
		case "end":
			v.table.End()
		case "f":
			v.showFailed = !v.showFailed
			v.populateTable()
		case "s":
			return v, func() tea.Msg {
				return SwapViewMsg{View: NewStageView(v.theme, v.client, v.store, v.nc, v.build)}
			}
		case "l":
			nc := v.nc
			build := v.build
			return v, func() tea.Msg {
				cv := NewConsoleView(v.theme, v.client, nc)
				cv.build = build
				cv.store = v.store
				return SwapViewMsg{View: cv}
			}
		case "d":
			return v, func() tea.Msg {
				return SwapViewMsg{View: NewDescribeView(v.theme, v.client, v.store, v.nc, v.build)}
			}
		}
	}
	return v, nil
}

func (v *TestReportView) View() string {
	return v.table.View()
}

func (v *TestReportView) Title() string {
	return decodeName(v.nc.ProjectName)
}

func (v *TestReportView) Breadcrumb() BreadcrumbSegment {
	seg := BreadcrumbFor("tests", v.nc)
	seg.Failed = v.showFailed
	return seg
}

func (v *TestReportView) ItemCount() int {
	total := 0
	for _, suite := range v.report.Suites {
		for _, tc := range suite.Cases {
			if !v.showFailed || tc.Status == jmodel.TestStatusFailed {
				total++
			}
		}
	}
	return total
}

func (v *TestReportView) Commands() []command.Command {
	return nil
}

func (v *TestReportView) Shortcuts() []component.Shortcut {
	sc := []component.Shortcut{
		component.Nav("esc", "builds"),
		component.Filter("f", "failed", v.showFailed),
	}
	badge := renderTestBadge(v.theme, &v.report)
	sc = append(sc, detailViewTabs("")...)
	sc = append(sc, component.ViewSCRanked("T", "tests: "+badge, true, rankViewTests))
	sc = v.host.AppendShortcuts(sc) // adds A if available
	return sc
}

func (v *TestReportView) SetSize(width, height int) {
	v.width = width
	v.height = height
	nameWidth := max(10, width-colTestsFixed)
	v.table.SetColumnWidth(0, nameWidth)
	v.table.SetSize(width, height)
}

func (v *TestReportView) NC() NavigationContext { return v.nc }

func (v *TestReportView) ScrollInfo() widget.ScrollInfo {
	return widget.ScrollInfo{Offset: v.table.ScrollOffset(), TotalLines: v.table.TotalRows(), ViewHeight: v.table.ContentHeight()}
}

func (v *TestReportView) Close() error {
	return nil
}

func (v *TestReportView) ParentView(t theme.Theme, c jmodel.JenkinsClient, s *cache.Store) View {
	nc := v.nc.AtBranch(v.nc.BranchName)
	if v.nc.Level == CtxProject {
		return NewBuildsView(t, c, s, nc, NewProjectBuildsProvider(c, s, nc))
	}
	return NewBuildsView(t, c, s, nc, NewBranchBuildsProvider(c, s, nc))
}

// renderTestSuiteAggregate renders a compact aggregate for a suite header, omitting zeros.
func renderTestSuiteAggregate(t theme.Theme, passed, failed, skipped int) string {
	passIcon := iconOr(t.Icons.StatusSuccess, "✔")
	failIcon := iconOr(t.Icons.StatusFailed, "✖")
	var parts []string
	if passed > 0 {
		parts = append(parts, t.BuildStatus.Success.Render(fmt.Sprintf("%s%d", passIcon, passed)))
	}
	if failed > 0 {
		parts = append(parts, t.BuildStatus.Failed.Render(fmt.Sprintf("%s%d", failIcon, failed)))
	}
	if skipped > 0 {
		parts = append(parts, t.BuildStatus.Aborted.Render(fmt.Sprintf("~%d", skipped)))
	}
	return strings.Join(parts, " ")
}

// renderTestCaseStatus renders a colored status badge for a single test case.
func renderTestCaseStatus(t theme.Theme, status jmodel.TestStatus) string {
	passIcon := iconOr(t.Icons.StatusSuccess, "✔")
	failIcon := iconOr(t.Icons.StatusFailed, "✖")
	switch status {
	case jmodel.TestStatusPassed:
		return t.BuildStatus.Success.Render(passIcon + " Passed")
	case jmodel.TestStatusFailed:
		return t.BuildStatus.Failed.Render(failIcon + " Failed")
	default:
		return t.BuildStatus.Aborted.Render("~ Skipped")
	}
}

// formatTestDuration formats sub-second durations with fractional seconds,
// and longer durations using the standard formatDuration.
func formatTestDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return formatDuration(d)
}

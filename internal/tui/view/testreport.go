package view

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
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
	report     jenkins.TestReport
	nc         NavigationContext
	build      jenkins.Build
	showFailed bool // when true, only failed cases (and their suites) are shown
	width      int
	height     int
}

// NewTestReportView creates a TestReportView for the given build's test report.
func NewTestReportView(t theme.Theme, report jenkins.TestReport, nc NavigationContext, build jenkins.Build) *TestReportView {
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
	}
	v.populateTable()
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
			case jenkins.TestStatusPassed:
				passed++
			case jenkins.TestStatusFailed:
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
			if v.showFailed && tc.Status != jenkins.TestStatusFailed {
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
		v.populateTable()
		return v, nil

	case tea.KeyMsg:
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
	return BreadcrumbFor("tests", v.nc)
}

func (v *TestReportView) ItemCount() int {
	total := 0
	for _, suite := range v.report.Suites {
		for _, tc := range suite.Cases {
			if !v.showFailed || tc.Status == jenkins.TestStatusFailed {
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
	// esc first for stable grid positioning
	sc := []component.Shortcut{{Key: "esc", Action: "builds"}}
	if v.showFailed {
		sc = append(sc, component.Shortcut{Key: "f", Action: "all tests"})
	} else {
		sc = append(sc, component.Shortcut{Key: "f", Action: "failed only"})
	}
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

func (v *TestReportView) ScrollInfo() ScrollInfo {
	return ScrollInfo{Offset: v.table.ScrollOffset(), TotalLines: v.table.TotalRows(), ViewHeight: v.table.ContentHeight()}
}

func (v *TestReportView) Close() error {
	return nil
}

func (v *TestReportView) ParentView(t theme.Theme, c jenkins.JenkinsClient, s *cache.Store) View {
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
func renderTestCaseStatus(t theme.Theme, status jenkins.TestStatus) string {
	passIcon := iconOr(t.Icons.StatusSuccess, "✔")
	failIcon := iconOr(t.Icons.StatusFailed, "✖")
	switch status {
	case jenkins.TestStatusPassed:
		return t.BuildStatus.Success.Render(passIcon + " Passed")
	case jenkins.TestStatusFailed:
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

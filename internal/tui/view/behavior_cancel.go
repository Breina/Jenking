package view

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// cancelBehavior encapsulates the "x cancel build" confirm-dialog flow.
// Popup state lives in the shared confirmDialog helper; this struct adds the
// "x" key binding, the running-build precondition, and the snapshot of the
// targeted (nc, build) so a cursor move inside a list view cannot redirect
// the cancel to a different build mid-confirm.
type cancelBehavior struct {
	theme  theme.Theme
	client jenkins.JenkinsClient
	access buildAccessor

	dialog       confirmDialog
	confirmNC    NavigationContext
	confirmBuild jenkins.Build
}

func newCancelBehavior(t theme.Theme, client jenkins.JenkinsClient, access buildAccessor) *cancelBehavior {
	return &cancelBehavior{theme: t, client: client, access: access}
}

func (b *cancelBehavior) SetTheme(t theme.Theme) { b.theme = t }

func (b *cancelBehavior) HandleMsg(tea.Msg) (bool, tea.Cmd) { return false, nil }

// canCancel returns the current (nc, build) iff the build is running and
// thus cancellable.
func (b *cancelBehavior) canCancel() (NavigationContext, jenkins.Build, bool) {
	nc, build, ok := b.access()
	if !ok {
		return NavigationContext{}, jenkins.Build{}, false
	}
	if build.Status != jenkins.BuildStatusRunning {
		return NavigationContext{}, jenkins.Build{}, false
	}
	return nc, build, true
}

func (b *cancelBehavior) cancelCmd(nc NavigationContext, build jenkins.Build) tea.Cmd {
	jobPath, number := nc.JobPath(), build.Number
	client := b.client
	return func() tea.Msg {
		err := client.CancelBuild(context.Background(), jobPath, number)
		return CancelBuildResultMsg{Err: err}
	}
}

func (b *cancelBehavior) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if b.dialog.IsOpen() {
		if b.dialog.Update(msg) {
			return true, b.cancelCmd(b.confirmNC, b.confirmBuild)
		}
		return true, nil
	}
	if msg.String() != "x" {
		return false, nil
	}
	nc, build, ok := b.canCancel()
	if !ok {
		return false, nil
	}
	b.confirmNC = nc
	b.confirmBuild = build
	b.dialog.Open()
	return true, nil
}

func (b *cancelBehavior) Shortcut() (component.Shortcut, bool) {
	if _, _, ok := b.canCancel(); !ok {
		return component.Shortcut{}, false
	}
	return component.Shortcut{Key: "x", Action: "cancel"}, true
}

func (b *cancelBehavior) PopupView() string {
	return b.dialog.View(b.theme,
		"Cancel Build",
		fmt.Sprintf("Stop build #%d?", b.confirmBuild.Number),
	)
}

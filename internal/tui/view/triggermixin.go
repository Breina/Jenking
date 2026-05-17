package view

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// triggerMixin encapsulates trigger-dialog state for views that support 't' to trigger.
// Embed this struct and delegate JobParamsMsg / TriggerBuildResultMsg / key events to it.
//
// The confirm dialog is owned by the shared confirmDialog helper. The mixin updates
// its `nc` from each incoming JobParamsMsg, so list-style views (joblist) whose target
// changes per row work without any extra wiring beyond calling fetchJobParams with the
// row-aware NC.
type triggerMixin struct {
	client         jenkins.JenkinsClient
	nc             NavigationContext
	paramForm      *component.ParamForm
	dialog         confirmDialog
	theme          theme.Theme
	maxWidth       int
	maxHeight      int
	lastKnownBuild int

	// replay mode: when set, confirm triggers ReplayBuild instead of TriggerBuild.
	replayScript      string
	replaySourceBuild int
}

func newTriggerMixin(t theme.Theme, client jenkins.JenkinsClient, nc NavigationContext) triggerMixin {
	return triggerMixin{theme: t, client: client, nc: nc}
}

func (tm *triggerMixin) setTheme(t theme.Theme) {
	tm.theme = t
	if tm.paramForm != nil {
		tm.paramForm.SetTheme(t)
	}
}

func (tm *triggerMixin) setSize(w, h int) {
	tm.maxWidth = w
	tm.maxHeight = h
	if tm.paramForm != nil {
		tm.paramForm.SetSize(w, h)
	}
}

// startTrigger returns a Cmd that fetches job parameters, recording lastKnown
// as the build number to wait beyond when polling for the new triggered build.
func (tm *triggerMixin) startTrigger(lastKnownBuild int) tea.Cmd {
	tm.lastKnownBuild = lastKnownBuild
	tm.replayScript = ""
	return fetchJobParams(tm.client, tm.nc)
}

// startTriggerFor is like startTrigger but updates the target NC first. Use this
// from list-style views where the target job depends on the current cursor row.
func (tm *triggerMixin) startTriggerFor(nc NavigationContext, lastKnownBuild int) tea.Cmd {
	tm.nc = nc
	return tm.startTrigger(lastKnownBuild)
}

// startReplay opens a confirm dialog to replay sourceBuild using script.
func (tm *triggerMixin) startReplay(lastKnown, sourceBuild int, script string) tea.Cmd {
	tm.lastKnownBuild = lastKnown
	tm.replaySourceBuild = sourceBuild
	tm.replayScript = script
	tm.dialog.Open()
	return nil
}

// hasPopup reports whether a trigger dialog or param form is currently shown.
func (tm *triggerMixin) hasPopup() bool {
	return tm.paramForm != nil || tm.dialog.IsOpen()
}

// handleMsg processes JobParamsMsg and TriggerBuildResultMsg.
func (tm *triggerMixin) handleMsg(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case JobParamsMsg:
		if msg.Err != nil {
			return true, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		// Sync nc to whichever job the params actually came from. For fixed-NC
		// views this is a no-op; for list views it makes the confirm/trigger
		// target the right row even if the user moved the cursor while the
		// fetch was in flight.
		tm.nc = msg.NC
		if len(msg.Params) == 0 {
			tm.dialog.Open()
			return true, nil
		}
		form := component.NewParamForm(tm.theme, msg.Params)
		if tm.maxHeight > 0 {
			form.SetSize(tm.maxWidth, tm.maxHeight)
		}
		tm.paramForm = &form
		return true, nil

	case TriggerBuildResultMsg:
		if msg.Err != nil {
			return true, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		lastKnown := tm.lastKnownBuild
		nc := tm.nc.AtScope()
		return true, func() tea.Msg {
			return OpenTriggeredBuildMsg{NC: nc, LastKnownBuild: lastKnown}
		}
	}
	return false, nil
}

// handleKey handles key events while a dialog is active.
func (tm *triggerMixin) handleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if tm.paramForm != nil {
		result := tm.paramForm.Update(msg)
		switch result.Status {
		case component.ParamFormDone:
			tm.paramForm = nil
			return true, triggerBuild(tm.client, tm.nc, result.Values)
		case component.ParamFormCancelled:
			tm.paramForm = nil
		}
		return true, nil
	}
	if tm.dialog.IsOpen() {
		if tm.dialog.Update(msg) {
			return true, tm.confirmAction()
		}
		return true, nil
	}
	return false, nil
}

// confirmAction returns the Cmd for a confirmed trigger or replay.
func (tm *triggerMixin) confirmAction() tea.Cmd {
	if tm.replayScript != "" {
		script := tm.replayScript
		src := tm.replaySourceBuild
		tm.replayScript = ""
		return replayBuild(tm.client, tm.nc, src, script)
	}
	return triggerBuild(tm.client, tm.nc, nil)
}

// popupView returns the rendered popup body (unpositioned), or "" when no popup is active.
func (tm *triggerMixin) popupView() string {
	if tm.paramForm != nil {
		return tm.paramForm.View()
	}
	return tm.dialog.View(tm.theme,
		"Trigger Build",
		fmt.Sprintf("Start a new build of %s?", decodeName(tm.nc.ProjectName)),
	)
}

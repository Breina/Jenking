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
type triggerMixin struct {
	client         jenkins.JenkinsClient
	nc             NavigationContext
	paramForm      *component.ParamForm
	confirmDialog  bool
	confirmYes     bool
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

// startReplay opens a confirm dialog to replay sourceBuild using script.
// lastKnown is the current latest build number used for pending-build polling.
func (tm *triggerMixin) startReplay(lastKnown, sourceBuild int, script string) tea.Cmd {
	tm.lastKnownBuild = lastKnown
	tm.replaySourceBuild = sourceBuild
	tm.replayScript = script
	tm.confirmDialog = true
	tm.confirmYes = false
	return nil
}

// hasPopup reports whether a trigger dialog or param form is currently shown.
func (tm *triggerMixin) hasPopup() bool {
	return tm.paramForm != nil || tm.confirmDialog
}

// handleMsg processes JobParamsMsg and TriggerBuildResultMsg.
// Returns (handled, cmd); if handled is false the caller must process msg itself.
func (tm *triggerMixin) handleMsg(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case JobParamsMsg:
		if msg.Err != nil {
			return true, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		if len(msg.Params) == 0 {
			tm.confirmDialog = true
			tm.confirmYes = false
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
// Returns (handled, cmd); if handled is false the caller must process the key.
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
	if tm.confirmDialog {
		switch msg.String() {
		case "left", "right", "h":
			tm.confirmYes = !tm.confirmYes
		case "y":
			tm.confirmDialog = false
			return true, tm.confirmAction()
		case "enter":
			if tm.confirmYes {
				tm.confirmDialog = false
				return true, tm.confirmAction()
			}
			tm.confirmDialog = false
		default:
			tm.confirmDialog = false
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

// popupView returns the rendered popup box (unpositioned), or "" when no popup is active.
func (tm *triggerMixin) popupView() string {
	if tm.paramForm != nil {
		return tm.paramForm.View()
	}
	if tm.confirmDialog {
		return renderConfirmBox(tm.theme,
			"Trigger Build",
			fmt.Sprintf("Start a new build of %s?", decodeName(tm.nc.ProjectName)),
			tm.confirmYes,
		)
	}
	return ""
}

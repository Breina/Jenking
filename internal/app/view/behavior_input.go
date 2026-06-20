package view

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// inputDecisionFn looks up the pending input the user is currently focused on
// (e.g. via the StageView cursor), or returns ok=false when none applies.
type inputDecisionFn func() (jmodel.PendingInput, NavigationContext, int, bool)

// inputClearedFn is called after a successful proceed/abort so the host can
// drop the input from its local snapshot — this avoids a 2-second wait for
// the next build-detail tick before the stage stops rendering as paused.
type inputClearedFn func(inputID string)

// inputBehavior wires the pipeline `input` step approval flow into a view's
// Behavior host. The proceed flow opens an InputDialog; if the input has
// parameters, a ParamForm is shown instead. Abort uses a separate confirm
// dialog reachable via the "a" shortcut while focused on a paused-input
// stage.
//
// Both confirm dialogs use widget.ConfirmDialog so the visual style matches
// the trigger / cancel flows.
//
// Live-sync auto-close: the host polls build detail; each refresh re-resolves
// the focused pending input. If the dialog is open and the input is no longer
// pending (proceeded, aborted, or timed out elsewhere), the dialog closes.
type inputBehavior struct {
	theme   theme.Theme
	client  jmodel.JenkinsClient
	resolve inputDecisionFn
	cleared inputClearedFn

	proceedConfirm widget.ConfirmDialog
	abortConfirm   widget.ConfirmDialog
	form           *ParamForm
	pending        jmodel.PendingInput // captured at the moment the user opened a dialog/form
	openedNC       NavigationContext
	openedN        int
	feedback       string
	maxW           int
	maxH           int
}

func newInputBehavior(t theme.Theme, client jmodel.JenkinsClient, resolve inputDecisionFn, cleared inputClearedFn) *inputBehavior {
	return &inputBehavior{theme: t, client: client, resolve: resolve, cleared: cleared}
}

func (b *inputBehavior) SetTheme(t theme.Theme) {
	b.theme = t
	if b.form != nil {
		b.form.SetTheme(t)
	}
}

func (b *inputBehavior) SetSize(w, h int) {
	b.maxW, b.maxH = w, h
	if b.form != nil {
		b.form.SetSize(w, h)
	}
}

// SyncPending should be called by the host whenever a fresh build detail
// arrives. If a dialog/form is open for an input that is no longer pending,
// it auto-closes.
func (b *inputBehavior) SyncPending(pending []jmodel.PendingInput) {
	if !b.hasOpenUI() {
		return
	}
	for _, p := range pending {
		if p.ID == b.pending.ID {
			return
		}
	}
	b.closeAll()
}

func (b *inputBehavior) hasOpenUI() bool {
	return b.proceedConfirm.IsOpen() || b.abortConfirm.IsOpen() || b.form != nil
}

func (b *inputBehavior) closeAll() {
	b.proceedConfirm.Close()
	b.abortConfirm.Close()
	b.form = nil
	b.feedback = ""
}

func (b *inputBehavior) HandleMsg(msg tea.Msg) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case InputDecisionResultMsg:
		if m.Err != nil {
			b.feedback = m.Err.Error()
			return true, nil
		}
		id := b.pending.ID
		b.closeAll()
		if b.cleared != nil && id != "" {
			b.cleared(id)
		}
		return true, nil
	}
	return false, nil
}

func (b *inputBehavior) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if b.form != nil {
		res := b.form.Update(msg)
		switch res.Status {
		case ParamFormDone:
			b.form = nil
			return true, b.proceedCmd(res.Values)
		case ParamFormCancelled:
			b.form = nil
			return true, nil
		}
		return true, nil
	}
	if b.proceedConfirm.IsOpen() {
		if b.proceedConfirm.Update(msg) {
			return true, b.proceedCmd(nil)
		}
		return true, nil
	}
	if b.abortConfirm.IsOpen() {
		if b.abortConfirm.Update(msg) {
			return true, b.abortCmd()
		}
		return true, nil
	}

	pi, nc, num, ok := b.resolve()
	if !ok {
		return false, nil
	}
	switch msg.String() {
	case "enter":
		b.openedNC = nc
		b.openedN = num
		b.pending = pi
		b.feedback = ""
		if len(pi.Parameters) == 0 {
			b.proceedConfirm.Open()
			return true, nil
		}
		form := NewParamFormWithTitle(b.theme, pi.Message, pi.Parameters)
		if b.maxH > 0 {
			form.SetSize(b.maxW, b.maxH)
		}
		b.form = &form
		return true, nil
	case "a", "A":
		b.openedNC = nc
		b.openedN = num
		b.pending = pi
		b.feedback = ""
		b.abortConfirm.Open()
		return true, nil
	}
	return false, nil
}

func (b *inputBehavior) proceedCmd(values map[string]string) tea.Cmd {
	jobPath, num, id := b.openedNC.JobPath(), b.openedN, b.pending.ID
	client := b.client
	return func() tea.Msg {
		err := client.ProceedInput(context.Background(), jobPath, num, id, values)
		return InputDecisionResultMsg{InputID: id, Proceeded: err == nil, Err: err}
	}
}

func (b *inputBehavior) abortCmd() tea.Cmd {
	jobPath, num, id := b.openedNC.JobPath(), b.openedN, b.pending.ID
	client := b.client
	return func() tea.Msg {
		err := client.AbortInput(context.Background(), jobPath, num, id)
		return InputDecisionResultMsg{InputID: id, Proceeded: false, Err: err}
	}
}

func (b *inputBehavior) Shortcut() (component.Shortcut, bool) {
	if b.hasOpenUI() {
		return component.Shortcut{}, false
	}
	if _, _, _, ok := b.resolve(); !ok {
		return component.Shortcut{}, false
	}
	return component.ActionRanked("⏎", "input", rankActionInput), true
}

// inputAbortShortcut is a Behavior adapter that only contributes the "a"
// shortcut — the underlying inputBehavior already handles the key. Used
// because the Behavior interface advertises one shortcut per registration.
type inputAbortShortcut struct{ b *inputBehavior }

func (s inputAbortShortcut) HandleMsg(tea.Msg) (bool, tea.Cmd)    { return false, nil }
func (s inputAbortShortcut) HandleKey(tea.KeyMsg) (bool, tea.Cmd) { return false, nil }
func (s inputAbortShortcut) PopupView() string                    { return "" }
func (s inputAbortShortcut) Shortcut() (component.Shortcut, bool) {
	if s.b.hasOpenUI() {
		return component.Shortcut{}, false
	}
	if _, _, _, ok := s.b.resolve(); !ok {
		return component.Shortcut{}, false
	}
	return component.ActionRanked("a", "abort input", rankActionAbortInput), true
}

func (b *inputBehavior) PopupView() string {
	if b.form != nil {
		return b.form.View()
	}
	if b.proceedConfirm.IsOpen() {
		return b.proceedConfirm.View(b.theme, "Proceed Input", b.proceedBody())
	}
	if b.abortConfirm.IsOpen() {
		return b.abortConfirm.View(b.theme, "Abort Input", b.abortBody())
	}
	return ""
}

func (b *inputBehavior) proceedBody() string {
	body := b.pending.Message
	if body == "" {
		body = "Proceed with input?"
	}
	if b.pending.Submitter != "" {
		body += fmt.Sprintf("\n\nsubmitter: %s", b.pending.Submitter)
	}
	if b.feedback != "" {
		body += "\n\n" + b.theme.BuildStatus.Failed.Render(b.feedback)
	}
	return body
}

func (b *inputBehavior) abortBody() string {
	body := "Abort the paused input?"
	if msg := b.pending.Message; msg != "" {
		body += "\n\n" + msg
	}
	if b.feedback != "" {
		body += "\n\n" + b.theme.BuildStatus.Failed.Render(b.feedback)
	}
	return body
}

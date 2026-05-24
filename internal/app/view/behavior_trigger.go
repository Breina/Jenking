package view

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// triggerBehavior adapts the existing triggerMixin to the Behavior interface,
// so views register a single behavior with the host instead of hand-wiring
// trigger.handleMsg / handleKey / popupView / setTheme / setSize calls.
//
// The view still owns the entry-point: case "t" calls trigger.startTrigger
// (or startReplay), because the latest-build lookup is view-specific.
//
// canShortcut, when non-nil, gates the advertised "t" shortcut. Views where
// the target depends on the cursor (joblist) or provider config (buildsview)
// set this so the shortcut hides on rows / contexts that can't be triggered.
type triggerBehavior struct {
	tm          *triggerMixin
	canShortcut func() bool
}

func newTriggerBehavior(tm *triggerMixin) *triggerBehavior {
	return &triggerBehavior{tm: tm}
}

// WithShortcutGate restricts the advertised shortcut to when fn returns true.
// Returns the receiver for fluent registration.
func (b *triggerBehavior) WithShortcutGate(fn func() bool) *triggerBehavior {
	b.canShortcut = fn
	return b
}

func (b *triggerBehavior) HandleMsg(msg tea.Msg) (bool, tea.Cmd) {
	return b.tm.handleMsg(msg)
}

func (b *triggerBehavior) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	return b.tm.handleKey(msg)
}

func (b *triggerBehavior) Shortcut() (component.Shortcut, bool) {
	if b.canShortcut != nil && !b.canShortcut() {
		return component.Shortcut{}, false
	}
	return component.ActionRanked("t", "trigger", rankActionTrigger), true
}

func (b *triggerBehavior) PopupView() string { return b.tm.popupView() }

func (b *triggerBehavior) SetTheme(t theme.Theme) { b.tm.setTheme(t) }

func (b *triggerBehavior) SetSize(w, h int) { b.tm.setSize(w, h) }

package widget

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// Behavior encapsulates a single cross-cutting concern (e.g. trigger,
// artifact-open, test-report-open, cancel) that would otherwise be wired by
// hand into every view's Update / Shortcuts / PopupView. Views register
// behaviors with a BehaviorHost and delegate to it once, instead of
// re-implementing the same case-blocks and shortcut appends per view.
//
// Optional methods (SetTheme, SetSize) are detected via type assertion so
// implementations only declare what they need.
type Behavior interface {
	// HandleMsg processes non-key messages. Return handled=true to short-circuit
	// the host's dispatch loop and skip remaining behaviors and the view itself.
	HandleMsg(msg tea.Msg) (handled bool, cmd tea.Cmd)
	// HandleKey processes key events. Return handled=true to short-circuit.
	HandleKey(msg tea.KeyMsg) (handled bool, cmd tea.Cmd)
	// Shortcut returns the header-shortcut to advertise for this behavior, or
	// ok=false when the behavior currently has no advertised key (e.g. no
	// cached artifacts for the current build).
	Shortcut() (component.Shortcut, bool)
	// PopupView returns this behavior's popup body, or "" when none is active.
	PopupView() string
}

// Themed is implemented by behaviors that hold styled state.
type Themed interface{ SetTheme(theme.Theme) }

// Sized is implemented by behaviors that own popup geometry.
type Sized interface{ SetSize(w, h int) }

// BehaviorHost is embedded by views to compose multiple Behaviors. The host
// fans Update/Shortcuts/PopupView calls out to every registered behavior,
// short-circuiting on the first that claims a message or key.
type BehaviorHost struct {
	behaviors []Behavior
}

// Add registers a behavior. Order matters: behaviors are consulted in
// registration order for message and key dispatch, and shortcuts appear in
// the same order via AppendShortcuts.
func (h *BehaviorHost) Add(b Behavior) {
	if b == nil {
		return
	}
	h.behaviors = append(h.behaviors, b)
}

// HandleMsg dispatches msg to each behavior, returning on the first that
// handles it. View Update methods call this before their own switch.
func (h *BehaviorHost) HandleMsg(msg tea.Msg) (bool, tea.Cmd) {
	for _, b := range h.behaviors {
		if handled, cmd := b.HandleMsg(msg); handled {
			return true, cmd
		}
	}
	return false, nil
}

// HandleKey dispatches a key event. View Update methods call this inside the
// tea.KeyMsg branch before their own switch.
func (h *BehaviorHost) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	for _, b := range h.behaviors {
		if handled, cmd := b.HandleKey(msg); handled {
			return true, cmd
		}
	}
	return false, nil
}

// AppendShortcuts appends each behavior's advertised shortcut (if any) to sc.
// Views call this at the position in their Shortcuts() list where these
// shortcuts should appear.
func (h *BehaviorHost) AppendShortcuts(sc []component.Shortcut) []component.Shortcut {
	for _, b := range h.behaviors {
		if s, ok := b.Shortcut(); ok {
			sc = append(sc, s)
		}
	}
	return sc
}

// PopupView returns the first non-empty popup body. Behaviors that own popups
// (trigger param form, cancel confirm) report through this.
func (h *BehaviorHost) PopupView() string {
	for _, b := range h.behaviors {
		if pv := b.PopupView(); pv != "" {
			return pv
		}
	}
	return ""
}

// HasPopup reports whether any behavior currently shows a popup.
func (h *BehaviorHost) HasPopup() bool { return h.PopupView() != "" }

// SetTheme forwards a theme change to behaviors that care.
func (h *BehaviorHost) SetTheme(t theme.Theme) {
	for _, b := range h.behaviors {
		if tb, ok := b.(Themed); ok {
			tb.SetTheme(t)
		}
	}
}

// SetSize forwards a size change to behaviors that own popup geometry.
func (h *BehaviorHost) SetSize(w, ht int) {
	for _, b := range h.behaviors {
		if sb, ok := b.(Sized); ok {
			sb.SetSize(w, ht)
		}
	}
}

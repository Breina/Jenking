package widget

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/component"
)

// NavBehavior is a generic Behavior that turns a single keypress into a
// pre-built tea.Cmd produced by an accessor closure. It replaces the per-view
// pattern of inline case-blocks that look up the selection, build a child
// view stack, and emit Push/SwapViewMsg.
//
// The accessor returns the cmd to execute and a boolean indicating whether
// the action is currently available. When ok=false, the keypress is consumed
// as a no-op (matching pre-extraction semantics where an unavailable action
// silently did nothing) and the shortcut is hidden from the header.
//
// For navigation, the accessor typically returns a closure that emits
// PushViewMsg or PushViewsMsg; for app-level message dispatch (e.g.
// OpenScopedStagesMsg) it returns a closure emitting that message directly.
type NavBehavior struct {
	primaryKey string
	aliases    []string
	label      string
	rank       int
	accessor   func() (tea.Cmd, bool)
}

// NewNavBehavior wires a navigation shortcut. key is the primary keypress
// shown in the header. label is the shortcut hint text. accessor is the
// per-view closure that resolves the current selection, constructs the
// child view(s), and returns the cmd to dispatch.
func NewNavBehavior(key, label string, accessor func() (tea.Cmd, bool)) *NavBehavior {
	return &NavBehavior{primaryKey: key, label: label, accessor: accessor}
}

// WithRank sets the intra-group shortcut Rank so the header column order is
// deterministic regardless of registration order. Returns the receiver for
// fluent chaining.
func (b *NavBehavior) WithRank(rank int) *NavBehavior {
	b.rank = rank
	return b
}

// WithAlias registers an additional key that triggers the same action. The
// alias is not advertised as a separate shortcut; only the primary key
// appears in the header. Useful for the conventional "enter to drill in"
// alongside an explicit-letter shortcut (e.g. "s" stages aliased to "enter").
func (b *NavBehavior) WithAlias(alias string) *NavBehavior {
	b.aliases = append(b.aliases, alias)
	return b
}

func (b *NavBehavior) HandleMsg(tea.Msg) (bool, tea.Cmd) { return false, nil }
func (b *NavBehavior) PopupView() string                 { return "" }

func (b *NavBehavior) matches(k string) bool {
	if k == b.primaryKey {
		return true
	}
	for _, a := range b.aliases {
		if k == a {
			return true
		}
	}
	return false
}

func (b *NavBehavior) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !b.matches(msg.String()) {
		return false, nil
	}
	cmd, ok := b.accessor()
	if !ok {
		return true, nil
	}
	return true, cmd
}

func (b *NavBehavior) Shortcut() (component.Shortcut, bool) {
	if _, ok := b.accessor(); !ok {
		return component.Shortcut{}, false
	}
	return component.ViewSCRanked(b.primaryKey, b.label, false, b.rank), true
}

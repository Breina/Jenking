package notify

import (
	"log/slog"

	"github.com/gen2brain/beeep"
)

// Send sends an OS desktop notification. Errors are logged but not propagated —
// a notification failure should never crash or block the TUI.
func Send(title, body string) {
	if err := beeep.Notify(title, body, ""); err != nil {
		slog.Debug("notification failed", "err", err)
	}
}

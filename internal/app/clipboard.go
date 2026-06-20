package app

import (
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
)

// clipboardPollInterval is the delay between successive background clipboard
// reads. Matches SelectionCheckCmd's order of magnitude — small enough to feel
// instant when the user alt-tabs back with a fresh URL, large enough that
// shelling out to wl-paste/xclip every tick is unnoticeable.
const clipboardPollInterval = time.Second

// deeplinkCheckedMsg is dispatched after a clipboard read completes. dl is
// non-nil only when the clipboard held a URL belonging to the current Jenkins
// context that parses into a navigable view; otherwise the App should clear
// any previously queued deeplink.
//
// App treats this message as a "tick": each receipt re-issues clipboardPollCmd
// so the poll perpetuates itself the same way SelectionCheckCmd does. Avoid
// emitting this message from anywhere except clipboardPollCmd, otherwise the
// poll loop will fork.
type deeplinkCheckedMsg struct {
	dl *view.DeepLink
}

// clipboardPollCmd sleeps for clipboardPollInterval, then reads the system
// clipboard and parses it as a Jenkins URL against baseURL. The resulting
// deeplinkCheckedMsg drives App to update its queued deeplink and re-issue
// the poll, making this a self-perpetuating loop (same shape as
// widget.SelectionCheckCmd).
//
// An empty baseURL still produces a tick: the read is skipped, an empty
// deeplinkCheckedMsg fires, and App re-issues with whatever the current
// context URL is by then. This keeps the loop alive across context switches
// from "no context" into a configured one.
func clipboardPollCmd(baseURL string, store *cache.Store) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(clipboardPollInterval)
		if baseURL == "" {
			return deeplinkCheckedMsg{}
		}
		raw := readClipboard()
		if raw == "" {
			return deeplinkCheckedMsg{}
		}
		dl, err := view.ParseJenkinsURL(baseURL, raw, store)
		if err != nil {
			return deeplinkCheckedMsg{}
		}
		return deeplinkCheckedMsg{dl: dl}
	}
}

// readClipboard returns the system clipboard contents, trying the common
// Linux helpers in order: wl-paste (Wayland), xclip, xsel. Returns "" when no
// helper is available or the clipboard is empty.
func readClipboard() string {
	candidates := [][]string{
		{"wl-paste", "--no-newline"},
		{"xclip", "-selection", "clipboard", "-o"},
		{"xsel", "--clipboard", "--output"},
	}
	for _, args := range candidates {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		out, err := runWithTimeout(args[0], args[1:], time.Second)
		if err != nil {
			continue
		}
		return strings.TrimSpace(out)
	}
	return ""
}

func runWithTimeout(name string, args []string, timeout time.Duration) (string, error) {
	cmd := exec.Command(name, args...)
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := cmd.Output()
		done <- result{out: out, err: err}
	}()
	select {
	case r := <-done:
		return string(r.out), r.err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return "", exec.ErrNotFound
	}
}

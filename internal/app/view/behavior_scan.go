package view

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
)

// scanTargeter reports the container path under the cursor, if the row has one.
type scanTargeter func() (string, bool)

// scanCancelBehavior binds x on a container row to cancelling its queued scan,
// mirroring the build-cancel behavior key for key.
//
// It gates on the scan still being *queued*: cancelling is done by queue id,
// and once Jenkins hands the scan to an executor that id is gone. Jenkins
// exposes no verified stop endpoint for a running scan, so rather than offer a
// key that silently fails, the shortcut disappears the moment the scan starts.
type scanCancelBehavior struct {
	theme    theme.Theme
	client   jmodel.JenkinsClient
	storeFn  func() *cache.Store
	target   scanTargeter
	dialog   widget.ConfirmDialog
	confirm  jmodel.QueueItem
	confirmP string
}

func newScanCancelBehavior(t theme.Theme, client jmodel.JenkinsClient, storeFn func() *cache.Store, target scanTargeter) *scanCancelBehavior {
	return &scanCancelBehavior{theme: t, client: client, storeFn: storeFn, target: target}
}

func (b *scanCancelBehavior) SetTheme(t theme.Theme) { b.theme = t }

func (b *scanCancelBehavior) HandleMsg(tea.Msg) (bool, tea.Cmd) { return false, nil }

// queuedScan returns the waiting scan for the row under the cursor.
func (b *scanCancelBehavior) queuedScan() (string, jmodel.QueueItem, bool) {
	path, ok := b.target()
	if !ok {
		return "", jmodel.QueueItem{}, false
	}
	store := b.storeFn()
	if store == nil || store.Queue == nil {
		return "", jmodel.QueueItem{}, false
	}
	item, ok := store.Queue.ScanFor(path)
	if !ok {
		return "", jmodel.QueueItem{}, false
	}
	return path, item, true
}

func (b *scanCancelBehavior) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if b.dialog.IsOpen() {
		if b.dialog.Update(msg) {
			return true, b.cancelCmd(b.confirm.ID)
		}
		return true, nil
	}
	if msg.String() != "x" {
		return false, nil
	}
	path, item, ok := b.queuedScan()
	if !ok {
		return false, nil
	}
	b.confirmP, b.confirm = path, item
	b.dialog.Open()
	return true, nil
}

func (b *scanCancelBehavior) cancelCmd(id int64) tea.Cmd {
	client := b.client
	return func() tea.Msg {
		err := client.CancelQueueItem(context.Background(), id)
		return CancelBuildResultMsg{Err: err}
	}
}

func (b *scanCancelBehavior) Shortcut() (component.Shortcut, bool) {
	if _, _, ok := b.queuedScan(); !ok {
		return component.Shortcut{}, false
	}
	return component.ActionRanked("x", "cancel scan", rankActionCancel), true
}

func (b *scanCancelBehavior) PopupView() string {
	return b.dialog.View(b.theme,
		"Cancel Scan",
		fmt.Sprintf("Remove the queued scan of %s?", decodePath(b.confirmP)),
	)
}

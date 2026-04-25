//go:build integration

// Package harness provides a PTY-driven test harness for end-to-end testing of
// the jenking TUI binary against a real Jenkins server.
package harness

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

const (
	defaultCols = 160
	defaultRows = 40

	// RenderTimeout is the maximum wait time for local (non-network) UI updates.
	RenderTimeout = 2 * time.Second
	// NetworkTimeout is the maximum wait time for updates that require a Jenkins API call.
	NetworkTimeout = 30 * time.Second

	// quietWindow is how long the screen must be stable (no new bytes parsed)
	// before WaitFor considers a predicate match stable.
	quietWindow = 75 * time.Millisecond
)

// Options configures a Harness instance.
type Options struct {
	// BinaryPath is the path to the jenking binary to test.
	BinaryPath string
	// Cols/Rows sets initial terminal size.
	Cols, Rows int
	// Env is extra environment variables (in KEY=VALUE form) merged on top of the isolated env.
	Env []string
	// Context is the Jenkins context name to activate (default: uses current_context from real config).
	Context string
	// ExtraContexts lists additional context names to include in the isolated config without
	// making them current. Required for context-switch tests that need 2+ contexts.
	ExtraContexts []string
}

// Harness manages a jenking subprocess running in a PTY with a virtual terminal.
type Harness struct {
	t        *testing.T
	opts     Options
	tmpHome  string
	tmpCache string

	ptmx *os.File
	cmd  *exec.Cmd
	term vt10x.Terminal

	mu         sync.Mutex
	dirtyAt    time.Time
	readerDone chan struct{}
}

// New creates and starts a new Harness. Calls t.Fatal on any setup failure.
// The harness is automatically stopped via t.Cleanup.
func New(t *testing.T, opts Options) *Harness {
	t.Helper()
	if opts.BinaryPath == "" {
		t.Fatal("harness.Options.BinaryPath must be set")
	}
	if opts.Cols == 0 {
		opts.Cols = defaultCols
	}
	if opts.Rows == 0 {
		opts.Rows = defaultRows
	}
	// Empty Context means "use whatever the user's current_context is"
	// (BakeConfig handles this)

	h := &Harness{
		t:          t,
		opts:       opts,
		readerDone: make(chan struct{}),
	}

	env, tmpHome, tmpCache := BakeConfig(t, opts.Context, opts.ExtraContexts...)
	h.tmpHome = tmpHome
	h.tmpCache = tmpCache

	env = append(env, opts.Env...)

	cmd := exec.Command(opts.BinaryPath)
	cmd.Env = env

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(opts.Rows),
		Cols: uint16(opts.Cols),
	})
	if err != nil {
		t.Fatalf("harness: pty.StartWithSize: %v", err)
	}

	term := vt10x.New(vt10x.WithWriter(ptmx), vt10x.WithSize(opts.Cols, opts.Rows))

	h.ptmx = ptmx
	h.cmd = cmd
	h.term = term

	// Start reader goroutine
	go h.readLoop()

	// Register cleanup
	t.Cleanup(func() {
		h.Stop()
		// Scan debug log for panics
		debugLogPath := tmpHome + "/.config/jenking/debug.log"
		if panics := ScanPanics(debugLogPath); len(panics) > 0 {
			t.Errorf("jenking debug.log contained crash signatures:\n%s", strings.Join(panics, "\n"))
		}
		// On failure, dump the final grid and last 200 lines of debug.log
		if t.Failed() {
			t.Logf("=== Final terminal grid ===\n%s", h.Grid())
			t.Logf("=== Last debug.log lines ===\n%s", TailLog(debugLogPath, 200))
		}
	})

	return h
}

// readLoop drains the PTY into vt10x and tracks when the screen last changed.
// We use raw Read + Write instead of Parse to avoid a deadlock: Parse holds the
// vt10x state lock while potentially blocking on a PTY read (when its internal
// buffer drains mid-loop), which prevents Resize from ever acquiring the same lock.
// Write() locks only during processing, never during I/O.
func (h *Harness) readLoop() {
	defer close(h.readerDone)
	buf := make([]byte, 4096)
	for {
		n, err := h.ptmx.Read(buf)
		if n > 0 {
			h.term.Write(buf[:n]) //nolint:errcheck
			h.mu.Lock()
			h.dirtyAt = time.Now()
			h.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// Stop sends Ctrl+C to the app and waits for it to exit.
// Safe to call multiple times.
func (h *Harness) Stop() {
	if h.ptmx == nil {
		return
	}
	h.ptmx.Write([]byte{3}) // Ctrl+C
	select {
	case <-h.readerDone:
	case <-time.After(3 * time.Second):
		if h.cmd.Process != nil {
			h.cmd.Process.Kill()
		}
	}
	h.ptmx.Close()
	h.ptmx = nil
}

// SendKeys writes a key sequence to the PTY. Escape codes like <cr>, <esc>,
// <c-c>, <tab>, <up>, <down>, <left>, <right>, <f1>–<f12> are interpreted.
// All other text is written as literal runes.
func (h *Harness) SendKeys(s string) {
	h.ptmx.Write([]byte(ParseKeys(s)))
}

// SendRaw writes raw bytes to the PTY without any interpretation.
func (h *Harness) SendRaw(b []byte) {
	h.ptmx.Write(b)
}

// Resize sends SIGWINCH to the process with the new terminal size.
// vt10x.Resize() handles its own locking — do NOT hold the term lock when calling it.
func (h *Harness) Resize(cols, rows int) {
	pty.Setsize(h.ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	// Resize acquires its own internal lock; no external lock needed.
	h.term.Resize(cols, rows)
}

// Grid returns a plain-text snapshot of the current terminal state.
// Each row is on its own line; trailing spaces are preserved.
func (h *Harness) Grid() string {
	h.term.Lock()
	defer h.term.Unlock()
	cols, rows := h.term.Size()
	var sb strings.Builder
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			ch := h.term.Cell(col, row).Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Contains returns whether the current grid contains the given substring on any row.
func (h *Harness) Contains(s string) bool {
	return strings.Contains(h.Grid(), s)
}

// HeaderField extracts the value shown after "FieldName:" in the status header panel.
// For example, HeaderField("Monarch") returns the display name. Returns "" if not found.
// Values are trimmed and stripped of any trailing key-hint text (e.g. "<enter> jobs").
func (h *Harness) HeaderField(name string) string {
	prefix := name + ":"
	for _, line := range strings.Split(h.Grid(), "\n") {
		idx := strings.Index(line, prefix)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(prefix):])
		// Key hints start with '<'; stop before them
		if lt := strings.Index(rest, "<"); lt >= 0 {
			rest = strings.TrimSpace(rest[:lt])
		}
		return rest
	}
	return ""
}

// WaitFor blocks until pred returns true on the current grid AND the screen has
// been stable (no new bytes parsed) for quietWindow. Returns an error on timeout.
func (h *Harness) WaitFor(pred func(grid string) bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		grid := h.Grid()
		if pred(grid) {
			// Check stability
			h.mu.Lock()
			stable := time.Since(h.dirtyAt) >= quietWindow
			h.mu.Unlock()
			if stable {
				return nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("WaitFor timed out after %v\nCurrent grid:\n%s", timeout, h.Grid())
}

// WaitForText waits for a literal string to appear in the grid.
func (h *Harness) WaitForText(s string, timeout time.Duration) error {
	return h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, s)
	}, timeout)
}

// WaitForAny waits for ANY of the given strings to appear.
// Returns the matched string and nil error, or "" and an error on timeout.
func (h *Harness) WaitForAny(timeout time.Duration, candidates ...string) (string, error) {
	var matched string
	err := h.WaitFor(func(grid string) bool {
		for _, c := range candidates {
			if strings.Contains(grid, c) {
				matched = c
				return true
			}
		}
		return false
	}, timeout)
	return matched, err
}

// WaitForStableRegion waits for text to appear and then requires only the
// surrounding region (±radius rows) to be stable. Use this in views with
// background redraws (e.g. running-builds monitor).
func (h *Harness) WaitForStableRegion(text string, radius int, timeout time.Duration) error {
	return h.WaitFor(func(grid string) bool {
		return strings.Contains(grid, text)
	}, timeout)
}

// MustWaitFor calls WaitFor and fatally fails the test on timeout.
func (h *Harness) MustWaitFor(t *testing.T, pred func(string) bool, timeout time.Duration) {
	t.Helper()
	if err := h.WaitFor(pred, timeout); err != nil {
		t.Fatal(err)
	}
}

// MustWaitForText calls WaitForText and fatally fails the test on timeout.
func (h *Harness) MustWaitForText(t *testing.T, s string, timeout time.Duration) {
	t.Helper()
	if err := h.WaitForText(s, timeout); err != nil {
		t.Fatalf("text %q never appeared: %v", s, err)
	}
}

// StartManual creates and starts a Harness without a *testing.T.
// Intended for use in the jenking-probe REPL. Cleanup must be called manually via Stop().
func StartManual(binaryPath string, env []string, tmpHome string) (*Harness, error) {
	opts := Options{
		BinaryPath: binaryPath,
		Cols:       defaultCols,
		Rows:       defaultRows,
	}

	h := &Harness{
		t:          nil,
		opts:       opts,
		tmpHome:    tmpHome,
		readerDone: make(chan struct{}),
	}

	cmd := exec.Command(binaryPath)
	cmd.Env = env

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(opts.Rows),
		Cols: uint16(opts.Cols),
	})
	if err != nil {
		return nil, fmt.Errorf("pty.StartWithSize: %w", err)
	}

	term := vt10x.New(vt10x.WithWriter(ptmx), vt10x.WithSize(opts.Cols, opts.Rows))

	h.ptmx = ptmx
	h.cmd = cmd
	h.term = term

	go h.readLoop()
	return h, nil
}

// BootError returns any pre-alt-screen error lines emitted by jenking on startup.
// These appear as plain text in the PTY output before the TUI initializes.
func (h *Harness) BootError() string {
	grid := h.Grid()
	// If the grid contains "Error" outside the TUI chrome, something went wrong at boot.
	lines := strings.Split(grid, "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "Error ") || strings.HasPrefix(trimmed, "Error:") {
			return trimmed
		}
	}
	return ""
}

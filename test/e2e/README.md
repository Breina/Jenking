# jenking Integration Tests (e2e)

End-to-end tests that drive the real `jenking` binary under a PTY against a real
Jenkins server. Tests are gated behind the `integration` build tag so they
never run as part of `go test ./...`.

## Prerequisites

- A `~/.config/jenking/config.yaml` with at least one working Jenkins context.
- The `jenking` binary is built fresh by `TestMain` before each run.
- Network access to the configured Jenkins.

## Run

```bash
# Full integration suite (read-only tests only):
go test -tags=integration -race ./test/e2e/scenarios/...

# Specific test group:
go test -tags=integration -v -run=TestDashboard ./test/e2e/scenarios/...
go test -tags=integration -v -run=TestResize    ./test/e2e/scenarios/...
go test -tags=integration -v -run=TestRunning   ./test/e2e/scenarios/...

# With a specific Jenkins context (default: your current_context):
JENKING_CONTEXT=ontwikkel go test -tags=integration ./test/e2e/scenarios/...

# Makefile target:
make test-e2e
```

## Interactive Bug Hunting (jenking-probe)

`jenking-probe` is a stdin REPL that lets you drive jenking interactively.
Useful for exploring edge cases and reproducing bugs.

```bash
go run -tags=integration ./test/e2e/cmd/jenking-probe/
```

### Commands

| Command | Description |
|---------|-------------|
| `start [context]` | Start a fresh session (uses `current_context` if omitted) |
| `stop` | Stop the current session |
| `keys <string>` | Send keystrokes: `<cr>` `<esc>` `<c-c>` `<up>` `<down>` `<left>` `<right>` `<tab>` `<pgup>` `<pgdn>` |
| `type <literal>` | Send literal runes (no escape parsing) |
| `resize <cols> <rows>` | Resize the PTY |
| `wait <text> [ms]` | Wait for text to appear (default 10s timeout) |
| `snap [name]` | Save grid snapshot to `test/e2e/snapshots/` |
| `grid` | Print current terminal grid |
| `diff <snap1> <snap2>` | Line-diff two snapshots |
| `log` | Print last 50 debug.log lines |
| `dump [path]` | Save session history as YAML |
| `exit` | Quit |

### Example Session

```
start
wait Dashboard 15000
grid
keys R
wait running 10000
snap running-builds
keys <esc>
keys :context build<cr>
wait Dashboard 15000
snap after-context-switch
stop
exit
```

## Opt-in: Trigger / Cancel / Replay Tests

`trigger_test.go` requires a dedicated `jenking-e2e` job on Jenkins.
See [`fixtures/README.md`](fixtures/README.md) for setup.

Once deployed:

```bash
go test -tags=integration -v -run=TestTrigger ./test/e2e/scenarios/...
```

Without the job, all trigger tests skip cleanly.

## Snapshots

Snapshots are saved to `test/e2e/snapshots/` and are `.gitignore`d.
They are plain-text grids (one row per line) — readable as-is.

## Assertion Rules

To keep tests resilient against a live Jenkins with changing data:

- **Never** assert on build numbers, timestamps, durations.
- **Never** assert that a specific job name exists.
- **Prefer chrome over data**: assert on view titles, breadcrumbs, header labels,
  status bar hints — not on build results or row content.
- All network-dependent waits use `NetworkTimeout` (30s).
- All local render waits use `RenderTimeout` (2s).
- Crashes are detected automatically: every test scans `debug.log` for `panic:`,
  `runtime error:`, and `fatal error:` after the session ends.

## Layout

```
test/e2e/
  harness/            Reusable PTY + vt10x harness library
  scenarios/          *_test.go integration tests
  cmd/jenking-probe/  Interactive REPL for bug hunting
  fixtures/           Opt-in Jenkinsfile for trigger tests
  snapshots/          (gitignored) Saved terminal grids
```

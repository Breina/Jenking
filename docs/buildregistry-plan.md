# Unified Build State: BuildRegistry as Single Source of Truth

## Context

In the builds/jobs view, a build sometimes shows as "running" in the list while it has actually finished. The running-builds counter in the header (read from `store.RunningBuilds`, populated every 1 s by the executor monitor) is accurate; opening the stale row shows the build correctly finished in the stage view. The bug is a data-model defect: build status is written by three independent pollers into three independent caches, and there is no invariant preventing one of them from holding a stale "Running" record indefinitely.

### Root cause

Three writers touch build status, each with its own cache:

| Writer | Cadence | Cache |
|---|---|---|
| `RunningBuildsMonitor.processPoll` (`internal/monitor/running.go:106`) | 1 s | `store.RunningBuilds` (live truth) |
| `AllBuildsProvider.fetchFull` → `ScanAllBuilds` (`internal/tui/view/builds_all.go:193`) | 2 min | `slowBuilds` + `store.AllBuilds` (+ disk) |
| `BranchBuildsProvider` / `ProjectBuildsProvider` (`builds_branch.go`, `builds_project.go`) | 10 s / 30 s | `store.Builds` / `store.ProjectBuilds` |

The all-builds view merges `slowBuilds ∪ fastBuilds` (`builds_all.go:85`). The scan API can return `building==true` for a build whose executor has already released — Jenkins' build-API ingestion lags the executor API. If the monitor never observed that build in its `prevBuilds` (it completed between two 1 s ticks, or before the user opened the app), no `BuildCompletedMsg` is ever fired (`running.go:133-181`), and `slowBuilds[key]` keeps `Status: Running` until the next 2 min scan returns a corrected record. Disk persistence (`store.Disk.SaveAllBuilds`) preserves the stale record across restarts.

Branch/project providers have the same shape and don't even subscribe to `RunningBuildsUpdatedMsg` (`builds_project.go` has no fast path at all).

### Goal

A single in-memory `BuildRegistry` owns all build-status state. Domain invariants enforced at every ingress make it impossible to store an unreconciled "Running" record. All views become thin queries; the monitor and pollers become ingress sources. Disk persistence moves into the registry.

## Design

### New package: `internal/domain/buildregistry`

```go
package buildregistry

type Key struct { JobPath string; Number int } // == jenkins.BuildKey

type Record struct {
    Build           jenkins.Build      // canonical status, duration, timestamp, etc.
    JobPath         string
    BranchName      string             // for multibranch
    LastSeenRunning time.Time          // last monitor poll that observed key in executor set
    LastWriter      Source             // RunningPoll | Scan | Completion | DiskLoad
    Terminal        bool               // true once a non-Running status has been confirmed
    UpdatedAt       time.Time
}

type Source int
const ( SourceRunningPoll Source = iota; SourceScan; SourceCompletion; SourceDiskLoad )

type Registry struct {
    mu          sync.RWMutex
    records     map[Key]Record
    liveRunning map[Key]struct{}     // last running-set snapshot
    liveRunAt   time.Time
    reconcileFn func(Key)            // injected: schedules a GetBuild
    persistFn   func([]Record)       // injected: schedules disk write
    runTTL      time.Duration        // how long "Running" survives without re-confirmation
}
```

### Invariants enforced inside `Registry`

1. **Terminal is sticky.** `Apply` never transitions a `Terminal==true` record back to Running.
2. **Running requires live confirmation.** `Query` returns `Status: Running` only if `Terminal==false` AND (`Key ∈ liveRunning` OR `now - LastSeenRunning ≤ runTTL`). Otherwise the displayed status is downgraded to `Unknown` and a reconciliation `GetBuild` is scheduled (debounced per key).
3. **Single ingress per source kind.** Callers cannot write the `records` map directly; only the methods below mutate state.

### Ingress methods

- `IngestRunningSnapshot(builds []jenkins.UserBuild, polledAt time.Time)` — replaces `liveRunning`, upserts each as `Running` with `LastSeenRunning=polledAt`. For every key that *was* in `liveRunning` and is now absent, marks the record as needing completion (schedules `reconcileFn`).
- `IngestScan(builds []jenkins.UserBuild)` — upserts. A `Running` status from a scan is **stored as-is** but never makes a record `Terminal`; `Query` will gate visibility per invariant 2. Terminal statuses are stored and lock the record (`Terminal=true`).
- `ApplyCompletion(key Key, build jenkins.Build)` — installs the terminal status, sets `Terminal=true`, deletes from `liveRunning` if present.
- `IngestBranchList(jobPath string, builds []jenkins.Build)` / `IngestProjectList(projectPath string, builds []jenkins.ProjectBuild)` — same semantics as scan, scoped.
- `LoadFromDisk(records []Record)` — same as `IngestScan` but stamps `LastWriter=SourceDiskLoad` and forces any non-terminal `Running` to be invisible until a live confirmation arrives.

### Query API

- `Query(filter Filter) []UnifiedBuild` — replaces `Builds()` in every provider.
  - `Filter` fields: `FolderPrefix`, `JobPath`, `ProjectPath`, `MaxAge`.
  - Applies invariant 2 on read.
- `HasRunning(filter Filter) bool` — for visual-tick scheduling.
- `RunningCount() int` — for the header.
- `Subscribe() <-chan struct{}` — coalesced change notifications; Bubble Tea adapter turns these into `RegistryChangedMsg`.

### Bubble Tea integration

A new `RegistryAdapter` lives in `internal/tui/view` (or `internal/domain/buildregistry/teaadapter.go`) and:
- Owns the `Registry` instance.
- Receives `RunningBuildsUpdatedMsg`, `BuildCompletedMsg`, scan/list result messages, dispatches to the appropriate `Ingest*` method.
- Owns reconciliation: maintains a debounced queue, emits `tea.Cmd` that calls `client.GetBuild` and dispatches `BuildCompletedMsg`.
- Owns disk persistence: after every `Ingest*` that mutates, schedules a coalesced (e.g. 5 s debounce) `persistFn` write.

The monitor (`internal/monitor/running.go`) keeps polling but no longer maintains `prevBuilds` departure logic — `Registry.IngestRunningSnapshot` derives departures from the diff against its own `liveRunning` set. The monitor still emits `RunningBuildsUpdatedMsg` for views that want a tick signal, but the message no longer needs `Arrived`/`Departed` fields.

### View changes

`AllBuildsProvider`, `BranchBuildsProvider`, `ProjectBuildsProvider` become thin:
- Drop `slowBuilds` / `fastBuilds` / `builds` fields.
- Drop their own polling result handling for status; pollers still run (they're how scan/list data enters the registry) but their result messages just call `registry.IngestScan/IngestBranchList/IngestProjectList`.
- `Builds()` → `registry.Query(filter)`.
- Visual tick driven by `registry.HasRunning(filter)`.

### Disk persistence

- New `Disk.SaveRegistry([]Record)` / `Disk.LoadRegistry() []Record` replace `SaveAllBuilds`/`AllBuilds` cache loading.
- On load, every record goes through `LoadFromDisk`, so any pre-restart "Running" status is invisible until the first live `IngestRunningSnapshot` either confirms it or until reconciliation completes.
- Legacy `store.AllBuilds`, `store.Builds`, `store.ProjectBuilds` caches can be removed (they become a view onto the registry); keep `store.RunningBuilds` as a thin accessor that returns `registry.Query({onlyRunning:true})` for any external readers (header counter).

### Files to modify

- **New**: `internal/domain/buildregistry/registry.go`, `registry_test.go`, `teaadapter.go`, `filter.go`.
- `internal/monitor/running.go` — remove `prevBuilds`-based departure tracking; call `registry.IngestRunningSnapshot`.
- `internal/tui/view/builds_all.go` — strip `slowBuilds`/`fastBuilds`; route scan results to registry; `Builds()` queries registry.
- `internal/tui/view/builds_branch.go` — same treatment; route `BuildsMsg` to registry.
- `internal/tui/view/builds_project.go` — same treatment; route `projectBuildsResultMsg` to registry; add registry subscription for the missing fast path.
- `internal/cache/store.go` — drop `AllBuilds`, `Builds`, `ProjectBuilds` (or convert `RunningBuilds` accessor to read from registry). Wire registry into `NewStore` or pass alongside.
- `internal/cache/disk.go` — add `SaveRegistry`/`LoadRegistry`; remove or deprecate `SaveAllBuilds` once unused.
- `internal/tui/app.go` — instantiate `RegistryAdapter`, register it in the message pump before the monitor and providers.
- `internal/tui/component/header.go` (or wherever the running counter renders) — read from `registry.RunningCount()`.

### Reused utilities

- `jenkins.BuildKey(jobPath, number)` (`internal/jenkins/types.go`) — registry key serializer.
- `jenkins.ParseBuildStatus` (`types.go:445`) — already the canonical status parser.
- `cache.DiskStore` machinery (`internal/cache/disk.go`) — extend rather than replace; reuse its directory and JSON encoding patterns.
- `view.BuildCompletedMsg`, `RunningBuildsUpdatedMsg` (`internal/tui/view/messages.go`) — preserve as the wire format between monitor and adapter.

## Verification

1. **Unit tests** (`internal/domain/buildregistry/registry_test.go`):
   - Scan reports `Running`; no running-set confirmation arrives → `Query` returns the record as `Unknown` after `runTTL`, schedules reconciliation.
   - Scan reports `Running`; running-set confirms → `Query` returns `Running`.
   - Scan reports `Running`; running-set never confirms; `ApplyCompletion(Success)` arrives → record becomes `Terminal: Success`; later scan reporting `Running` again does not flip it back.
   - Disk load with `Running` records → invisible until live confirmation.
   - Departure detection: key in last `liveRunning`, absent in new snapshot → reconcile scheduled.
2. **Manual TUI test**: with a real Jenkins, trigger a fast (sub-1s) build that the executor poll likely misses. Confirm it does not linger as `Running` in the all-builds view after the next monitor tick.
3. **Regression**: run existing `view_test.go`, `buildsview_test.go`, `stageview_test.go`, monitor and cache tests; expect a few to need migration to the registry API.
4. **E2E**: `test/e2e/scenarios` — run the dashboard and console scenarios; ensure the all-builds list still renders running/failed/success rows correctly.
5. **Cold start**: kill app while a build is running; restart; verify that build appears as `Unknown` until the first monitor poll resolves it (rather than as `Running` from disk).

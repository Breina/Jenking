# Branch-indexing scans as a first-class run type — design

Status: implemented (slices A, B and C). Extends the queue model introduced
with `QueueStore`.

## Endpoint findings (probed against the live controller, 2026-08-06)

These replace the assumptions the first draft was written on:

- **`<container>/indexing/api/json` and `.../computation/api/json` both 404.**
  There is no JSON status object for a scan on Jenkins 2.568.1 / branch-api.
  No result, no end time, no "is it running" — the planned `GetScanStatus`
  cannot exist.
- **`<container>/computation/consoleText` and
  `<container>/computation/logText/progressiveText` work for *both* multibranch
  projects and organization folders.** `indexing/` is an alias that works only
  for multibranch. Using `computation/` uniformly removes the need for a
  `JobTypeOrgFolder` split, so job-type handling is untouched.
- The progressive endpoint returns `X-Text-Size` and the usual `X-More-Data`,
  so the existing console plumbing speaks it as-is. But `start=` beyond the end
  returns the **whole log**, not an empty body — there is no cheap liveness
  probe, only a full log fetch.
- `Bodemondergrond` is an `OrganizationFolder`, and its `codelijst-*` children
  are multibranch projects created by its scan. Both levels are scannable.

## Problem

The Jenkins build queue carries two structurally different things through one
endpoint:

```
id 7624172  Bodemondergrond/codelijst-kleur  "Started by timer"  blocked: "At maximum indexing capacity"
```

That is a *branch-indexing task* on a multibranch project, not a build. It will
never produce a build number, and its normal steady state is `blocked` on the
indexing throttle. Today it is counted as a queued build, which:

- inflates the header `Queued:` counter (a nightly timer fanout adds 8+),
- poisons the dashboard wait-bin histogram — under `dominantReason` the whole
  chart renders "blocked" for the duration of the fanout,
- produces queue rows the user cannot act on: no stages, no log, no build.

## Model

A scan is **a run of the container itself**, not a queue oddity. Jenkins agrees:
the multibranch project *is* the `Queue.Task`, its console lives at
`<project>/indexing/`, and it moves queued → running → finished like a build.

Consequence that resolves most of the design: **a container row never owns a
build.** A multibranch project's builds belong to its branches. So on a
container row, "this row's own run" is unambiguously the scan, and every verb
binds without a special case.

## Compatibility

`_class` is core JSON and already parsed (`ParseJobType`,
`internal/jenkins/types.go:344`). Unknown classes fall through to
`QueueKindBuild` — i.e. exactly today's behaviour — so an exotic job type never
regresses. The `indexing/` endpoints belong to `branch-api`, which is required
to have a multibranch project at all; they are only ever requested for items we
already classified as scans, and a 404 degrades to "no scan log".

## Design review of the requested spec

Eight of the ten requested items are taken as-is. Two need changing, and one
cost was not in the request.

### 1. Removing `<l>` and `<s>` from the jobs view — do not remove, re-gate

Both shortcuts are already gated on `selectedNonFolder()`
(`internal/app/view/joblist.go:179,196`), so they never appear on a folder or
multibranch row. Removing them would regress pipeline and freestyle rows, where
there is no scan and no confusion.

The request also says "add `<l>` scan logs" — so `l` is not being removed, it
is being *rebound per row type*. Stated cleanly, and this is the rule the plan
adopts:

> `l` means "the log of this row's most recent run" — build log on a job row,
> scan log on a container row.

One verb, one meaning, two implementations, and the behavior host already
renders only the applicable one in the header because each accessor gates on
`ok`. Same for `t` (trigger the row's own run: build vs scan) and `x` (cancel
the row's own run).

### 2. `<s>` for the Scans view — as a scoped switch it fits, with one addition

`s` is inert on container rows today (gated on `selectedNonFolder`,
`joblist.go:196`), so it rebinds per row type exactly like `l`:

- job row: `s` = stages (unchanged)
- container row: `s` = scans view scoped to that container

That leaves one gap. Once you press `enter` on a multibranch project you are
*inside* it looking at branches; the project's own row no longer exists, and
every visible row is a branch job whose `s` and `l` are taken. There is then no
row to reach the project's own scan from.

So bind **`S` at view level = "scans in the current scope"**, available
everywhere including inside a branch list (where the scope *is* the project).
`:scans` is its long form. One destination, two entry points — `s` on a
container row is the row-scoped shortcut for the same view. Precedent for the
shift key exists (`D` toggles disabled).

### 3. Multibranch STATUS — the conflict you felt is real; stop multiplexing

The requested rule (builds win; scan shown only when no build activity; LAST
falls back to scan time) is right about priority but has a failure mode.

Worked example. `codelijst-kleur` is a multibranch project whose branch `main`
is building; the nightly timer has also queued a scan, blocked on indexing
capacity. Build wins, so the row reads:

```
⎇ codelijst-kleur    ✓    4m    BUILDING
```

The scan is invisible. Now press `x`. The row's own run is the *scan* — the
build belongs to `main`, not to this row — so `x` cancels the scan while STATUS
says BUILDING. Either the key appears for a state the user cannot see, or it is
gated on "is scanning" and appears with no visible reason. Both are wrong.

Second failure, same cause: during the nightly fanout, every jammed project that
also has a branch building reads BUILDING, so the indexing jam is invisible in
the view where the user is looking at those projects.

Fix: **two signals, two places** instead of one multiplexed cell.

- `STATUS` keeps the requested rule: build state wins, always. It is an
  *aggregate readout of the children*.
- Scan state gets its own glyph in the `JOB` cell, next to the type icon. No
  column-width surgery (`colFixedTotalNormal`), no hidden information.
- Shortcuts act on **the row's own run**, which on a container is always the
  scan. STATUS never determines what `x` cancels.

```
JOB                              MAIN  LAST    STATUS
⎇⏳ codelijst-kleur               ✓     4m      BUILDING     ← branch building, scan queued
⎇⟳ codelijst-materiaalklasse     ✓     2h      SUCCESS      ← scanning now
⎇   codelijst-opslaglocatie      ✓     2h      SUCCESS
▸⟳  SomeOrgFolder                      —       —            ← org folder computing
```

Glyphs (themeable, `theme.Icons`): `⏳` scan queued or blocked, `⟳` scanning
(cursor row only, see below), none otherwise. Blocked-on-throttle uses `⏳` too
— the reason belongs in the scans view and the preview, not in a one-character
cell.

### 4. Detecting a *running* scan — deliberately not polled

Queued scans are free: they are in the queue snapshot the engine already polls,
and they cover the case that actually hurts (the nightly fanout). Running scans
are not free — `ListQueue` drops items once an executable is assigned
(`queue.go:60`), and a running scan has no build to appear in
`ListRunningBuilds`.

**Resolved by the probes: dropped entirely.** With no status endpoint and no
cheap liveness probe (a `start=` past the end returns the whole log), the only
way to learn a scan is running is to download its log — 1.5 KB for a small
multibranch, 59 KB for the organization folder — per row, per refresh. For a
glyph that says something the user cannot act on, that is not worth it.

What ships: `⧗` for a **queued** scan, from the queue snapshot the engine
already polls, at zero added cost. A running scan shows no glyph anywhere. The
scan log view reports liveness itself, via `MoreData` on the stream it is
already reading — the authoritative source all along.

The rejected alternatives, for the record:

- Per-project polling of a status endpoint: the fan-out the caching rules
  forbid — and the endpoint does not exist anyway.
- A central probe of the controller's flyweight executors (`oneOffExecutors`):
  one request per tick, permanent load, unverified, for a cosmetic badge.
- On-demand liveness for the cursor row: a full log download per selection
  change. Rejected once the probes showed there is no cheap partial fetch.

## Data model

`internal/domain/jmodel`:

```go
type QueueKind string
const (
    QueueKindBuild QueueKind = "build" // a run will come out of this
    QueueKindScan  QueueKind = "scan"  // branch indexing / folder computation
)

// No Scan type: a queued scan IS a jmodel.QueueItem with Kind == scan.
// Introducing a parallel struct would duplicate id/why/since/state for no
// added information — and with no status endpoint there is nothing to put in
// the extra fields (LastEnded/LastOK were dropped for that reason).
```

`QueueItem` gains `Kind QueueKind`, set in the adapter.

No `JobTypeOrgFolder` is needed after all: `computation/` serves both container
types, so nothing downstream has to tell them apart. An organization folder
keeps parsing as `JobTypeFolder`, and every existing `JobTypeFolder` branch
(navigation, suggestions, job-path caching) stays untouched.

## Component changes

| Layer | Change |
|---|---|
| `internal/jenkins/queue.go` | `task[_class]` in both tree params; map via `ParseJobType` → `Kind`. Folder/OrgFolder/MultiBranch ⇒ `QueueKindScan`, everything else (incl. unknown) ⇒ `QueueKindBuild`. |
| `internal/jenkins/log.go` (slice C) | Split the URL builder out of `GetProgressiveLog`; add a scan-log fetch against `<container>/computation/logText/progressiveText`. |
| `internal/cache/queue.go` | `QueueStore` keeps every item (one fetch); `Query`/`CountVisible` take a `QueueKind`. Add `ScanFor(jobPath) (Scan, bool)` for the joblist glyph, queue-derived only. |
| `internal/app/engine/engine.go` | `applyQueue` returns `queueCounts{Builds, Scans int}`; only build items reach `QueueTracker.Observe` and the sampler. |
| `internal/app/engine/events.go` | `RunningBuildsUpdatedMsg` carries `ScanCount`. |
| `internal/tui/component/header.go` | `Queued: N   Scans: M`, scans dim, shown only when M > 0. |
| `internal/app/view/joblist.go` | `LAST BUILD` → `LAST`; scan glyph in the JOB cell; `s` → scans on container rows; `l`/`t`/`x` in slice C. |
| `internal/app/view/scansview.go` (new) | Scoped scans list. |
| `internal/app/view/console.go` | `consoleFetch` takes a run base path instead of `(jobPath, number)`. |
| `internal/app/view/*nav*` | `NavBuildRef` gains a kind; `runBasePath(nc)` → `<job>/<n>` or `<job>/indexing`. |

That console seam is what makes the scan log the *real* console — follow,
search, pending bar — rather than a lookalike.

## Jobs view

Column rename `LAST BUILD` → `LAST` (`joblist.go:109,118`; width constant
`colLastBuildWidth` keeps its name or is renamed to `colLastWidth` in the same
pass).

`LAST` content:

1. last build time, if the row has one (branches' latest for a multibranch);
2. otherwise `-`.

**The requested "fall back to last scan time" is not implementable.** Jenkins
exposes no scan timestamp: there is no status JSON, and the only trace of when
a scan ran is a line inside the log text
(`[Thu Aug 06 11:18:05 CEST 2026] Starting branch indexing...`) — controller
locale, controller timezone, log format. Parsing that to fill a table cell
would be a fragile lie. The queued glyph carries the scan signal instead.

Keys on a **container row** — this means **multibranch projects and org
folders alike**; `isContainer` is `Folder || MultiBranch` (`joblist.go:49`), so
a multibranch project is a container here and gets the full scan verb set:

| key | action | gate |
|---|---|---|
| `enter` | drill into branches (unchanged) | always |
| `l` | scan log (pending-aware) | a scan exists or has run |
| `s` | scans view scoped to this container | scannable container |
| `t` | scan now — no param popup, scans take none | scannable container |
| `x` | cancel scan + existing confirm dialog | scan queued (see Open questions for running) |
| `o` | repo URL (unchanged) | URL known |
| `b` | all builds (unchanged) | multibranch |

Keys on a **job row**: unchanged — `l` build log, `s` stages, `t` trigger, `d`
describe, `x` cancel build.

View-level, in every view: `S` = scans in the current scope. This is the only
way to reach a multibranch project's own scan once you have drilled *into* it,
because from there the project has no row and every visible row is a branch job
whose `s` and `l` are already bound.

`t` on a container is currently dead (`triggerSelectedJobUnderCursor` returns
early on `isContainer`, `joblist.go:730`); it wires to the existing `Rescan`
usecase (`internal/app/usecase/mutate.go:162`), which is already exposed via CLI
and MCP but never bound in the TUI. Trigger-then-follow matches builds: `t`
lands in the pending scan console the way `t` on a job lands in
`NewPendingStageView`.

## Scans view

New scoped view, sibling to `:running`. Opened three ways, all landing in the
same place: `S` (current scope), `s` on a container row (that container), or
`:scans`. Scoping uses the same `buildregistry.Filter` prefix semantics as
builds, so `:scans` inside a multibranch project matches its own scan — the
project's path *is* the scan's `JobPath`.

Rows come from the queue snapshot (free). On open, and on cursor movement, the
view resolves live state for its rows via `GetScanStatus` — bounded, because the
list only ever holds containers in scope with recent scan activity.

```
PROJECT                                STATE     WAITING  WHY
codelijst-kleur                        BLOCKED   12m      At maximum indexing capacity
codelijst-materiaalklasse              SCANNING  3m
codelijst-opslaglocatie                QUEUED    12m      Waiting for next executor
```

State badges reuse `renderQueueStatus` (`view.go:661`). Keys: `enter`/`l` scan
log, `x` cancel, `o` repo URL, `enter` on nothing → no-op. Preview panel shows
the tail of the scan log for the selected row, matching the builds view.

## Dashboard

Scans are excluded from the queued count and from `QueueTracker`. No parallel
scan histogram in the persisted samples — the schema churn buys nothing that the
live count and the scans view do not already give.

## CLI

Follows existing naming (`jenking queue`, `jenking rescan`, `jenking dequeue`).

- `jenking queue` — gains a `KIND` column; `--kind build|scan|all`, default
  `build` so existing scripted output stops including scans. Note this in
  `CHANGELOG.md` as a behaviour change.
- `jenking scans [<folder>]` — list queued/running scans.
- `jenking rescan <project>` — exists; add `--follow` to stream the scan log,
  mirroring `trigger --follow`.
- `jenking scan-log <project> [--follow]` — the scan console.
- Cancel: `jenking dequeue <id>` already cancels a queued scan by queue id, and
  `jenking scans` prints the id. No new verb.

## MCP

- `list_queue` — items gain `"kind": "build"|"scan"`; optional `kind` filter,
  defaulting to `build` for the same reason as the CLI.
- `list_scans` — queued/running scans, optionally folder-scoped.
- `get_scan_log` — logs-as-files, same contract as `get_logs`.
- `rescan` — exists, unchanged.
- `dequeue` — exists, already cancels a queued scan by id.

## Slices

**A — classification and counters.** `QueueKind`, header split, sampler and
`QueueTracker` exclusion, `list_queue`/`jenking queue` kind field. Ships alone
and fixes the pollution. Tests: adapter classification from fixture JSON
(multibranch, org folder, workflow job, unknown class); `applyQueue` counts;
`QueueTracker.Observe` ignores scans.

**B — visibility.** `LAST` rename, scan glyph (queue-derived), `ScanFor`,
`GetScanStatus` on the cursor row, scans view, `S` / `s` / `:scans`,
`list_scans`, `jenking scans`. Tests: joblist cell rendering under each priority
case (building + queued scan, idle + scanning, no build + last scan, neither);
scans-view scoping; no request issued when the cursor is on a job row.

**C — the run.** `runBasePath` seam, `GetIndexingLogProgressive`, `l`/`t`/`x`
on container rows, `scan-log`, `rescan --follow`, `get_scan_log`. Tests:
progressive fetch offset handling against a fake, pending→running transition in
the scan console.

C carries the value, B makes it discoverable — a scan console with no row to
reach it from is a deep link nobody finds.

## Open questions

1. ~~Status endpoint shape~~ — **answered:** there is none (see endpoint
   findings). Running-scan detection dropped as a result.
2. ~~Org folder log path~~ — **answered:** `computation/` serves both container
   types; no job-type split needed.
3. ~~Cancelling a running scan~~ — **answered:** `POST
   <container>/computation/stop` exists (a GET returns 405, i.e. present but
   POST-only). Wired to `x` in the scan log view, the one place we know a scan
   is running; `x` in the job list still cancels by queue id.
4. **Scan history.** Jenkins keeps only the latest scan log per container and no
   status object, so there is no history to show — hence no scans-history view,
   and the scans view is a live queue list rather than a log of past scans.

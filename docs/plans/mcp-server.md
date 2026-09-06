# Jenking MCP Server — Design & Slice Plan

Goal: make Jenking **the** Jenkins MCP server. This document is the design of
record for the `jenking mcp` mode; the slice breakdown at the end is the
implementation roadmap. Conventions follow `docs/architecture.md`; where this
doc proposes layering changes, the depguard config remains the executable
authority once updated.

## 1. Positioning — why Jenking wins

Every existing Jenkins MCP server is a stateless wrapper: one tool call, one
HTTP request, relay whatever Jenkins says. Jenking has four things a wrapper
cannot offer:

1. **The agentic Jenkinsfile loop.** `internal/domain/pipelinesyntax` parses
   the controller's pipeline-syntax GDSL/globals into structured symbols
   (steps, signatures, docs) reflecting the shared libraries actually
   configured. An LLM editing a Jenkinsfile blind hallucinates step names;
   with `get_pipeline_symbols` → edit → `lint_pipeline` → `replay_build` →
   `get_logs` it doesn't. No other server has this.
2. **Truthful build state.** `internal/domain/buildregistry` enforces
   terminal-is-sticky and Running-requires-live-confirmation — closing real
   Jenkins API lies (`building=true` after the executor released). A
   registry-backed `list_running` is more correct than the Jenkins API itself.
3. **Queue history Jenkins doesn't keep.** The dashboard sampler persists
   running/queued time series with wait-bin × reason histograms
   (`cache.DashSample`). `get_queue_history` answers "are we
   executor-starved, and why" — unanswerable via any wrapper because the data
   doesn't exist server-side.
4. **Plugin-agnostic core.** No `_class` branching, no optional-plugin
   coupling (project rule). Works on vanilla controllers where competitors
   hard-code Blue Ocean or plugin endpoints.

Items 2 and 3 require a **long-running stateful process**. That is the
central architectural driver: `jenking mcp` hosts the same polling/sampling
machinery as the TUI, headlessly, and gets *better the longer it runs*.

## 2. Settled decisions

- `jenking mcp` cobra subcommand; **stdio transport**; long-running.
- SDK: official `github.com/modelcontextprotocol/go-sdk` (pinned; SDK types
  never leak outside `internal/app/mcp`).
- **One Jenkins context fixed at launch** (`--context`). Every tool schema
  reserves an optional `context` string param — validated equal to the launch
  context for now, enabling multi-context later without schema breaks.
- **Shared cache dir with the TUI**, protected by `gofrs/flock` (dir-level
  advisory lock; `LockFileEx` on Windows comes free) + pid-suffixed tmp names
  + merge-on-write for whole-buffer files.
- **Logs are files, never inline, never scored.** MCP has no out-of-band
  streaming into the client's shell; file handoff *is* the streaming answer.
  The LLM greps the file with its own tools.
- `--read-only` flag omits mutating tools entirely. Mutating tools carry MCP
  annotations (`readOnlyHint`/`destructiveHint`/`idempotentHint`).
- Optional endpoints are **probed, never assumed**: the tool call itself is
  the probe; a 404 flips a per-session capability state to `missing` and
  returns an actionable "requires plugin X" error; never persisted.

## 3. Package layout

New packages, all under the existing `app/*` depguard row (may import domain,
tui, adapters, app-self):

| Package | Role |
|---|---|
| `internal/app/usecase` | Headless verb orchestration: target resolution + fetch, function-per-verb on `Deps{Client jmodel.JenkinsClient; Store *cache.Store}`, returning jmodel types. Absorbs `internal/action` (deleted) and the resolution helpers currently in `cmd/jenking/root.go` (`withProjectBuild`, `resolveBuildNum`, `enrichBranchNotFound`), `resolveJobPath` (cmd_lifecycle.go), `resolveInputID` (cmd_input.go), `waitForBuild` (cmd_mutate.go). May import `internal/tui/command` for target parsing (generic grammar, already used headlessly by `internal/action`). |
| `internal/app/dto` | The ~17 `out*` DTO types + `toOut*` converters moved out of `cmd/jenking/output.go`, exported, keeping json/yaml tags and gaining `jsonschema` description tags. CLI `-o json` and MCP structured output become byte-identical — one documented contract. |
| `internal/app/engine` | Headless stateful host: monitor poll loop (1s), queue sampler (2s), persistence cadence (every 15 samples), lifecycle (`Start(ctx)`/`Stop()` with final flush). New home of `sampler`, `queueTracker`, `waitBins` (moved from `internal/app/view/dashboard_sampler.go` / `dashboard_queue.go`, exported). |
| `internal/app/mcp` | go-sdk server, generic tool-registration helper, capability session state, usecase-error → tool-error mapping, server instructions. |

Depguard additions (slice 1, alongside a `docs/architecture.md` §3 note):

- `internal/app/{usecase,engine,mcp,dto}` deny `github.com/charmbracelet/bubbletea` — the headless sublayer stays tea-free.
- `internal/app/engine` deny `internal/app/view` (view may import engine, never the reverse).

### Layering wart fixes (prerequisites, slice 1)

1. `cmd/jenking/cmd_read.go:412` imports `internal/app/view` for
   `view.FindArtifact` — pure matching over `[]jmodel.Artifact`; move to
   `jmodel.FindArtifact(arts []Artifact, name string) (Artifact, bool)`.
2. `cmd/jenking/root.go:250` (`enrichBranchNotFound`) imports
   `internal/jenkins.HTTPError`. Add to jmodel:

   ```go
   type HTTPStatusError interface{ HTTPStatusCode() int }
   func StatusOf(err error) int      // errors.As → code, else 0
   func IsNotFound(err error) bool
   ```

   `jenkins.HTTPError` gains `HTTPStatusCode()`; `enrichBranchNotFound` moves
   into usecase and uses `jmodel.IsNotFound`.
3. `internal/app/view/describe.go:192` type-asserts to `*jenkins.Client` for
   `ApplyJobParameters`. Split: pure merge →
   `pipelinesyntax.ApplyParameters(sym, defs)` (domain→domain);
   orchestration → `usecase.OverlaySymbols`; symbol cache logic
   (`loadCachedSymbols`/`storeSymbols`, key `"v5/<jobPath>#<buildNum>"`,
   empty-guard — describe.go:184-253) → `usecase.GetPipelineSymbols`. Delete
   `jenkins.Client.ApplyJobParameters`.

## 4. Usecase layer

```go
type Deps struct {
    Client jmodel.JenkinsClient
    Store  *cache.Store // nil-safe for tests
}

type Ref struct {
    JobPath string   // explicit full path, or:
    Words   []string // CLI-style target words (project [branch] [#N])
    Build   int      // 0 = latest
}
type Resolved struct{ JobPath string; BuildNumber int }

func (d Deps) Resolve(ctx context.Context, ref Ref) (Resolved, error)
```

`Resolve` = `command.ParseTarget` → `navmsg.ResolveTarget(store)` →
`resolveBuildNum` (latest via `ListBuilds`), wrapped with the
branch-not-found enrichment. One function-per-verb mirrors the port
(`ListJobs`, `ListBuilds` (+limit), `GetBuild`, `ListStages` (+
`ApplyPendingInputs`), `GetTestReport`, `ListArtifacts`, `GetArtifact` (uses
`jmodel.FindArtifact`), `GetParams`, `GetMetadata`, `WhoAmI`, `ListNodes`,
`ListQueue`, `ListRunning`, `DescribePipeline`, `ListInputs`,
`ResolveInputID`, `GetPipelineSymbols`, `Lint`, `Replay`, `FetchLogs`,
`GetChanges`, `FindCommit`, and mutations `Trigger`/`Cancel`/`Dequeue`/
`Approve`/`Reject`/`SetEnabled`/`Rescan`/`SetNodeOffline`).

`Trigger` absorbs `waitForBuild` (cmd_mutate.go): options
`{Params, Wait, WaitTimeout, Progress func(string)}` — the Progress callback
replaces stderr prints (CLI routes to stderr, MCP to progress notifications).

`cmd/jenking/*` RunEs become thin: `usecase call → dto convert →
printFormatted`. `internal/action` is deleted; `deeplink.go`'s `--raw` paths
call usecase directly.

**Error taxonomy** (`usecase.Error{Kind, Msg, Hint, Err}`; kinds: NotFound,
NoBuilds, Ambiguous, PluginMissing, Auth, Conn, BadInput). Every failure
carries an actionable `Hint` — e.g. cold-cache suffix resolution: *"use the
full job path; call list_jobs to discover paths"*. CLI prints Msg+Hint; MCP
maps to `IsError` tool results carrying both.

## 5. New feature: build changes (commits per build)

Missing from Jenking entirely (no `changeSets` parsing anywhere;
`buildDetailTree` in `internal/jenkins/builds.go:103` doesn't request it).
Essential for MCP — agents constantly verify "is commit X in build N".
Plugin-agnostic: `changeSet` (AbstractBuild, singular) / `changeSets`
(WorkflowRun, plural) are core+workflow-job; parse both keys, no `_class`
branching.

- **Domain**: `jmodel.Change{CommitID, Author, AuthorEmail, Message,
  Timestamp, AffectedPaths []string}`. Port methods:
  - `GetChanges(ctx, jobPath, number) ([]Change, error)` — dedicated tree
    query `changeSets[items[commitId,msg,author[fullName],authorEmail,
    timestamp,affectedPaths]],changeSet[items[...]]`. Kept out of `GetBuild`
    to keep `BuildDetail` lean.
  - `FindCommit(ctx, jobPath, commitPrefix, maxBuilds) ([]BuildCommitHit, error)`
    — **one** tree request
    `builds[number,changeSets[items[commitId]],changeSet[items[commitId]]]{0,N}`
    (N ≤ 50), prefix-match. One HTTP call answers "which build contains this
    commit" — a headline tool.
- **CLI**: `jenking changes <target>` (table: short-sha, author, first
  message line, age; `-o json` full) and `jenking changes --find <sha>
  <project>`.
- **MCP**: `get_changes(job_path, build_number?, include_paths?=false)`
  (affectedPaths off by default — token discipline) and
  `find_commit(job_path, commit, max_builds?=25)` →
  `{hits: [{build_number, commit_id}], searched_builds}`.
- **TUI**: changes list for the selected build (panel/popup from builds view
  + a `changes` command), reusing existing list-widget patterns.
- Commit **web links are out of scope**: per-commit URL construction from
  `ObjectMetadataAction.objectUrl` would need provider-specific patterns,
  violating the agnosticism rule. Raw commit IDs only.

## 6. Engine (headless stateful host)

### Monitor core inversion

`internal/monitor/running.go` is tea-shaped but its work is pure
client+store. Extract a tea-free core (`internal/monitor/core.go`):

```go
type Core struct { Client jmodel.JenkinsClient; Store *cache.Store; prevLive map[string]jmodel.UserBuild }

type PollOutcome struct {
    Builds            []jmodel.UserBuild
    Arrived, Departed []string
    QueuedCount       int
    // Completion cascade (detail → ApplyCompletion, stages, test report),
    // each independent and safe to run concurrently.
    FollowUps []func(ctx context.Context) CompletionEvent
}
func (c *Core) Poll(ctx context.Context) (PollOutcome, error)
```

`Poll` = today's `poll()` + `processPoll()` side effects
(`Registry.IngestRunningSnapshot`, `Queue.Replace`, dirty marks) with the
`tea.Cmd` completion cascade converted to plain closures.
`RunningBuildsMonitor` becomes a thin tea wrapper: calls `Core.Poll`,
converts outcomes to `navmsg.*` messages, self-schedules `tea.Tick`.
Existing `running_test.go` coverage ports to `Core`.

### Engine API

```go
type Options struct{ PollInterval, SampleInterval time.Duration; PersistEvery int; Retention time.Duration }
func New(client jmodel.JenkinsClient, store *cache.Store, opt Options) *Engine
    // wires buildregistry.NewReconciler + Registry.SetReconcile (as app.wireMonitor does);
    // seeds sampler from store.Disk.LoadDashSamples()
func (e *Engine) Start(ctx context.Context) // 1s poll loop (+bounded FollowUp goroutines), 2s sample loop
func (e *Engine) Stop()                     // cancel, wg.Wait, final SaveDashSamples + registry flush
func (e *Engine) QueueHistory(now time.Time, window time.Duration) QueueReport
```

The sample loop mirrors `DashboardView.sample()`: queue tracker observe →
sampler add, reading `store.Registry`/`store.Queue` populated by the poll
loop — no extra HTTP. (The dashboard's 10s node poll feeds node tiles only;
`DashSample` needs none of it.)

**TUI adoption is deferred** (slice 8): v1 engine is MCP-only; the TUI keeps
its tea monitor, now backed by the shared `Core`, so both hosts run identical
logic from slice 6 onward.

## 7. Tool catalog

Naming: `snake_case`. All tools accept optional `context` (validated equal to
launch context). Paths use decoded slash form (`folder/project/branch`),
matching CLI output. Results = `dto` structs (structured content, schema
inferred from tags) + a one-line text summary.

### Read tools (`readOnlyHint: true`)

| Tool | Input | Notes |
|---|---|---|
| `list_jobs` | `folder?` | root when omitted |
| `list_builds` | `job_path`, `limit?=15` (≤100) | multibranch project → per-branch listing (mirrors `nc.Level` split) |
| `get_build` | `job_path`, `build_number?` (omitted = latest) | includes pending inputs, params |
| `get_changes` | `job_path`, `build_number?`, `include_paths?=false` | §5 |
| `find_commit` | `job_path`, `commit`, `max_builds?=25` (≤50) | §5; single tree request |
| `list_running` | — | registry-backed once engine lands (slice 6); client-direct before |
| `list_queue` | — | |
| `get_queue_history` | `window_minutes?=120` (≤120) | aggregated: running/queued min-max-avg + wait-bin×reason histogram; never raw samples |
| `list_nodes` | — | requires extending `jmodel.Node` with `Labels []string` (core `assignedLabels[name]` tree field, plugin-agnostic) — without labels an agent cannot diagnose "no nodes with label X" queue starvation |
| `get_stages` | `job_path`, `build_number?` | |
| `get_test_report` | `job_path`, `build_number?`, `failed_only?=true`, `max_cases?=50` | full green dumps are token poison |
| `list_artifacts` | `job_path`, `build_number?` | |
| `get_artifact` | `job_path`, `build_number?`, `name`, `save_to_file?` (auto-true >16 KiB) | file handoff reuses the logs dir |
| `get_params` | `job_path` | |
| `get_metadata` | `job_path`, `build_number?`, `depth?=1` (≤2) | `MetaNode.Flatten` |
| `whoami` | — | |
| `describe_pipeline` | `job_path`, `build_number?` | replay script / Jenkinsfile |
| `get_pipeline_symbols` | `job_path`, `build_number?`, `query?`, `kind?` (steps\|globals\|all), `name?` | default: names only; `name` → full signature+doc; gated on workflow-cps capability |
| `get_logs` | `job_path`, `build_number?`, `stage?`, `window?{offset_bytes,max_bytes≤16384}` | file handoff, §8; `stage` scopes to one stage's log — cheap targeted fetch instead of the whole console |
| `list_inputs` | `job_path`, `build_number?` | |
| `lint_pipeline` | `jenkinsfile` | POST but read-only semantics; gated capability |

### Mutating tools (omitted under `--read-only`)

| Tool | Input | Annotations |
|---|---|---|
| `trigger_build` | `job_path`, `params?`, `wait?`, `wait_timeout_s?=300` (≤600) | destructive:false, idempotent:false; wait streams progress notifications; timeout → partial result + "poll get_build" hint |
| `replay_build` | `job_path`, `build_number?`, `script` | destructive:false, idempotent:false |
| `cancel_build` | `job_path`, `build_number` (**required** — no latest-default on destructive ops) | destructive:true, idempotent:true |
| `dequeue` | `queue_id` | destructive:true, idempotent:true |
| `approve_input` | `job_path`, `build_number?`, `input_id?`, `params?` | destructive:false, idempotent:true; `ResolveInputID` semantics (single pending auto-selected; ambiguity error lists ids) |
| `reject_input` | `job_path`, `build_number?`, `input_id?` | destructive:true, idempotent:true |
| `enable_job` / `disable_job` | `job_path` | destructive:false/true, idempotent:true |
| `rescan` | `job_path` | destructive:false, idempotent:true |
| `set_node_offline` | `node_name`, `reason?` | destructive:true, idempotent:true |
| `set_node_online` | `node_name` | destructive:false, idempotent:true |

## 8. `get_logs` file contract

- File: `<cachedir>/logs/<fnv32a(jobPath)>-<lastSegment>#<num>.log` (hash
  prefix avoids path-escaping, readable suffix aids humans).
- Append via `GetProgressiveLog(start)` loop until `!MoreData`. Track the
  last `NextStart` in an in-memory per-build map — do **not** trust file size
  as the offset (ConsoleNote filtering can make server offsets drift from
  written bytes).
- Guard: server text at offset 0 shorter than the file → build
  restarted/rotated → truncate and rewrite.
- Completed build already fetched complete → no HTTP re-fetch.
- Result: `{path, size_bytes, complete, appended_bytes, window_text?}`. The
  text summary explicitly instructs: *"Search it with grep/rg; do not read it
  whole."* `window` peeks read from the local file after append.
- **Stage scoping** (`stage` param): resolve the stage by name via
  `ListStages` (same path as the CLI's `logs --stage`), then fetch its
  `NodeIDs` via `GetNodeLogProgressive` per node, appended in node order to a
  separate file `<base>@<stage-slug>.log` with per-node `NextStart` tracking.
  Unknown stage name → `KindBadInput` error listing available stage names.
  This is the preferred path for failure diagnosis — fetch the failed
  stage's log, not the whole console.

## 9. Cache concurrency (flock)

Today `DiskStore` has one in-process mutex; `writeGob` is tmp+rename atomic
per file, but cross-process RMW on shared map files (stages.gob, jobs.gob, …)
is last-writer-wins with lost updates, and fixed tmp names collide (also the
hardcoded `allbuilds.gob.tmp` at disk.go:58).

- Dependency: `github.com/gofrs/flock` (wraps `flock(2)` and Windows
  `LockFileEx`; tiny, no transitive deps — beats hand-rolling a shim).
- One dir-level lock file `<cachedir>/.lock`; scope = whole load-mutate-save.

```go
func (d *DiskStore) withLock(fn func() error) error {
    d.mu.Lock(); defer d.mu.Unlock()
    if err := d.fl.Lock(); err == nil { defer d.fl.Unlock() } // best-effort: degrade to today's behavior
    return fn()
}
```

- All `Save*` RMW cycles route through `withLock`; plain `Load*` take the
  mutex + shared `RLock`. External `DiskStore` API unchanged.
- Tmp names gain pid suffix: `path + "." + pid + ".tmp"`.
- **Merge-on-write** for whole-buffer files (this is what makes TUI+MCP
  coexistence *correct*, not merely non-corrupting):
  - `SaveDashSamples`: load existing, union by timestamp, sort, evict past
    retention, write.
  - Registry: new pure `buildregistry.MergeRecords(disk, mem)` honoring
    terminal-is-sticky (terminal beats running; else newer LastSeen wins) +
    `DiskStore.SaveRegistryMerged`; the `PersistFn` wired in `cache.NewStore`
    switches to it.

## 10. MCP server specifics

- `mcp.NewServer(&mcp.Implementation{Name: "jenking", Version: version...},
  &mcp.ServerOptions{Instructions: ...})`; `server.Run(ctx, StdioTransport)`.
- `cmd/jenking/cmd_mcp.go`: flags `--read-only` (+ inherited
  `--context`/`--timeout`); reuses `setupCmdState()`; builds `usecase.Deps` +
  `engine.New`; `signal.NotifyContext(SIGINT, SIGTERM)`; on shutdown
  `engine.Stop()` with a 5s flush guard.
- Generic registration helper kills 30× boilerplate:

```go
type toolDef[In, Out any] struct {
    Name, Description string
    Ann      *mcp.ToolAnnotations
    Mutating bool
    Needs    capability // capNone | capPipelineSyntax | capModelConverter
    Handler  func(ctx context.Context, s *Session, in In) (Out, error)
}
func register[In, Out any](srv *mcp.Server, s *Session, d toolDef[In, Out])
// skips Mutating when ReadOnly; validates `context` param; checks/records
// capability; maps usecase.Error → CallToolResult{IsError, Msg+"\n"+Hint}
```

- Capability probing: `Session` holds `map[capability]capState{unknown, ok,
  missing}`; first gated call probes lazily; 404 → missing + actionable
  error; subsequent calls short-circuit. Per-session only, never persisted.
- Instructions (~20 lines) teach: (a) logs-file workflow — grep the path,
  never request whole logs inline; (b) the Jenkinsfile loop —
  `describe_pipeline → get_pipeline_symbols(query=…) → lint_pipeline →
  replay_build → get_logs`; (c) `find_commit` for commit↔build questions;
  (d) omitted `build_number` = latest; (e) `list_running`/`get_queue_history`
  are live monitor truth; (f) failure-diagnosis pattern — `get_stages` to
  find the failed stage, then `get_logs(stage=…)` for a targeted fetch;
  (g) regression pattern — fetch logs of the last-green and first-red builds
  and `diff` the two local files; compare `get_changes` across the range.
- **stdout is the protocol.** Audit `logging.Setup`
  (`internal/logging`): slog must go to stderr in MCP mode; usecase
  `Progress` callbacks route to MCP progress notifications, never stderr
  prints.

## 11. Implementation slices

Each slice ships independently with tests (httptest servers, narrow fakes,
clock injection — existing repo patterns). flock lands **before** the MCP
skeleton: read tools write through to shared gob files, so a TUI running
beside even a read-only server would clobber map files cross-process.

1. **Warts + usecase + dto extraction** (CLI parity, zero behavior change;
   splittable: 1a warts, 1b read verbs, 1c mutate verbs).
   Tests: golden `-o json` outputs captured *before* refactor and asserted
   unchanged after; `internal/action` fakes ported to usecase; httptest for
   `Resolve`/branch-not-found enrichment.
2. **Changes feature** (§5: domain + adapter + usecase + `jenking changes`).
   Tests: parser tables over both changeSet shapes, multiple changesets per
   build, empty-changes case; httptest for both port methods.
3. **flock + tmp names + merge writes** (§9).
   Tests: two `DiskStore` instances on one dir doing concurrent RMW with no
   lost keys; `MergeRecords` tables (terminal-sticky); tmp-collision
   regression.
4. **MCP skeleton + stateless read tools** (~17 tools incl.
   get_changes/find_commit; capability plumbing present but only lint gated).
   Includes the small `jmodel.Node.Labels` extension + adapter tree change.
   Tests: go-sdk in-memory transports end-to-end against an httptest Jenkins;
   tool-list schema snapshot; node-labels parse test.
5. **Differentiators**: `get_logs` file handoff incl. stage scoping (§8),
   `get_pipeline_symbols` (+ describe.go cache refactor from slice 1 wart 3),
   `lint_pipeline`, capability probing.
   Tests: progressive-log httptest incl. truncation/drift cases; symbol
   filtering; 404-probe caching.
6. **Engine** (§6): `monitor.Core` extraction, engine loops,
   sampler/queueTracker move, `get_queue_history`, registry-backed
   `list_running`.
   Tests: Core poll on existing fakes; engine lifecycle with short
   intervals; history aggregation.
7. **Mutating tools + `--read-only`** (§7).
   Tests: read-only registration filter; trigger-wait against an httptest
   queue→build sequence; ambiguous-input error.
8. **TUI adopts engine + TUI changes panel.** DashboardView reads
   `engine.Sampler`; monitor tea wrapper already delegates to Core; changes
   list for selected build. A slip here never blocks MCP.
9. **Docs + positioning.** README MCP section leading with the Jenkinsfile
   loop and `find_commit`; client config snippets
   (`{"command": "jenking", "args": ["mcp"]}`); CHANGELOG.

## 12. Risks

- `FetchPipelineSyntax` **ignores `buildNumber`** (`syntax.go:150`) though
  cached per-build — symbols are a job-level snapshot; document as such and
  consider dropping the build dimension from the MCP cache key.
- Progressive-log server offsets vs written bytes can drift (ConsoleNote
  filtering) — hence the sidecar `NextStart` map + truncation guard.
- go-sdk churn: pin the version; SDK types stay inside `internal/app/mcp`.
- TUI + MCP double-poll Jenkins at 1s each (cross-process; unaffected by
  slice 8). Acceptable load; documented.
- Registry merge touches the terminal-is-sticky invariant — `MergeRecords`
  lives in `buildregistry` beside its clock-injected tests.
- `logging.Setup` must be audited for stdout writes before slice 4; any
  stray stdout byte breaks JSON-RPC framing silently.

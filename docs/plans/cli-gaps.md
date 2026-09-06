# Jenking CLI — Feature-Completeness Gaps

Audience: an implementing agent. Each gap below is self-contained: motivation,
Jenkins endpoints, files to touch, and the existing patterns to copy. Read the
referenced files before coding.

Priority order (do them in this order):

1. Trigger returns queue id + `--wait`
2. Pipeline input commands (`inputs`, `approve`, `reject`)
3. `build` detail command and `nodes` command
4. Log ergonomics: `--tail`, per-stage logs
5. `lint` (Jenkinsfile validation) and `replay`
6. Job/node lifecycle: enable/disable, rescan, node offline/online, artifact to file

## House rules (apply to every gap)

- **Core Jenkins endpoints only.** Never depend on optional plugins. Stage data
  may use the flowGraphTable scraping already in `internal/jenkins/builds.go`.
- CLI commands live in `cmd/jenking/` (`cmd_read.go`, `cmd_mutate.go`,
  `cmd_build_ops.go`). Register new commands in `root.go` `init()`.
- Every read command must support `--output json|yaml|text`. Pattern: define an
  `outX` struct + `toOutX()` converter + `printXTable()` in
  `cmd/jenking/output.go`, then use `emitList()` (lists) or the switch on
  `outputFlag` (single objects). JSON field names are `snake_case`.
- Errors on json/yaml output must go through `writeError()` (root.go) so agents
  always get parseable `{"error": "..."}`.
- Target resolution: use `command.ParseTarget(args)` +
  `navmsg.ResolveTarget(target, cs.store, navmsg.NavigationContext{})`, or the
  `withProjectBuild()` helper in `root.go` when the command takes
  `<project> <branch> [#N|#last]`.
- HTTP: `internal/jenkins/client.go` has `get()`, `post()`, `doRequest()`.
  Non-2xx already surfaces as `*jenkins.HTTPError`.
- Client interface changes go in `jmodel.JenkinsClient`
  (`internal/domain/jmodel/jmodel.go` ~line 316). Update the `fakeClient` in
  `internal/monitor/running_test.go` and any view callers.
- Tests: table-driven, colocated `_test.go` in `internal/jenkins/` using
  `httptest` (see `queue_test.go`, `builds_test.go` for the pattern).
- Ignore `.claude/worktrees/**` — stale copies, not the live code.

---

## Gap 1 — `trigger` must return the queue id; add `--wait`

**Why.** `TriggerBuild` returns only `error`; the CLI prints `triggered <path>`.
A caller (human or MCP agent) cannot find the build it just started — the
trigger→observe loop is broken. Jenkins answers a successful
`POST /build` / `POST /buildWithParameters` with a `Location` header pointing
at the queue item, e.g. `https://ci/queue/item/1234/`.

**Changes.**

1. `internal/jenkins/client.go`: add
   `postForLocation(ctx, path string) (string, error)` — like `post()` but
   returns `resp.Header.Get("Location")`.
2. `internal/jenkins/builds.go`: change `TriggerBuild` to return
   `(int64, error)` — parse the trailing `/queue/item/<id>/` segment of the
   Location header (return 0 if absent, not an error).
3. `internal/domain/jmodel/jmodel.go`: update the `JenkinsClient` interface
   signature.
4. Fix callers: `internal/app/view/trigger.go` (discard the id),
   `fakeClient` in `internal/monitor/running_test.go`.
5. `internal/jenkins/queue.go`: add
   `GetQueueItem(ctx, id int64) (*jmodel.QueueItem, int, error)` fetching
   `/queue/item/<id>/api/json?tree=...` (same tree as `queueTreeParam` minus
   the `items[...]` wrapper). Second return = `executable.number` (0 until
   assigned). Note: Jenkins garbage-collects finished queue items after ~5 min;
   a 404 after triggering means the item left the queue — treat as terminal.
   Add `GetQueueItem` to the interface + fake.
6. `cmd/jenking/cmd_mutate.go` `newTriggerCmd`:
   - Print/emit the queue id. JSON: `{"queue_id": 1234, "job_path": "..."}`.
   - Add `--wait`: poll `GetQueueItem` every 2 s until `executable.number > 0`
     (report `why` while waiting in text mode), then poll
     `GetBuild(jobPath, number)` every 3 s until `!Building`. Final output:
     `{"queue_id":…, "build_number":…, "status":"success|failed|…"}`.
     Non-success final status → non-zero exit (return an error after emitting
     the JSON). Respect `ctx` from `ctxWithTimeout()`; document that `--wait`
     usually needs `--timeout 30m`.

**Test.** httptest server returning a `Location` header; assert parsed id.
Queue-item polling: fake responses without and with `executable`.

## Gap 2 — Pipeline input gates: `inputs`, `approve`, `reject`

**Why.** Client already implements `ProceedInput` / `AbortInput`
(`internal/jenkins/builds.go`) and `GetBuild` already populates
`BuildDetail.PendingInputs` (id, message, ok/abort labels, submitter,
parameter definitions). Nothing is exposed in the CLI. Approving a deploy gate
is a daily operation and a killer MCP tool.

**Commands** (new file `cmd/jenking/cmd_input.go`):

- `jenking inputs <project> <branch> [#N|#last]` — list pending inputs via
  `withProjectBuild` + `GetBuild`, emit `PendingInputs` (`outInput`: id,
  message, ok_label, abort_label, submitter, parameters).
- `jenking approve <project> <branch> [#N|#last] [--id <inputID>] [--param K=V]...`
  — fetch pending inputs; if `--id` omitted and exactly one input pending, use
  it; if several, error listing the ids. Call `ProceedInput`. Param parsing:
  copy the `strings.Cut(p, "=")` loop from `newTriggerCmd`.
- `jenking reject <project> <branch> [#N|#last] [--id <inputID>]` — same
  resolution, call `AbortInput`.

**Note.** Build number resolution must hit the *running* build — `#last` via
`resolveBuildNum` already returns the newest build including running ones.

## Gap 3 — `build` detail command and `nodes`

### 3a `jenking build <project> [<branch>] [#N|#last]`

**Why.** "What happened to build #42?" currently requires stitching `builds` +
`metadata`. `GetBuild` already returns `BuildDetail` (result, timing, causes,
parameters, pending inputs).

Use `withProjectBuild`. Emit a single `outBuildDetail`: number, status,
building, duration, timestamp, url, cause, triggered_by, `parameters`
(map), `pending_inputs`. Table printer: key/value lines via `newTab`.
(Check what `Build`/`jsonBuildDetail.toDomain` actually populates —
`internal/jenkins/parse_builds.go` — and expose everything that's there.)

### 3b `jenking nodes`

**Why.** `ListNodes` exists (`internal/jenkins/computers.go`), used by the TUI
dashboard; agent health is a core question. Expose it.

`outNode`: name, offline, offline_cause, executors, busy_executors,
free_disk_bytes, free_mem_bytes, response_ms. Table: NAME / STATUS
(online/offline) / EXECUTORS busy/total / DISK / MEM / PING. Reuse a
human-size formatter if one exists in the view layer; otherwise add one in
output.go.

## Gap 4 — Log ergonomics: `--tail`, per-stage logs

**Why.** `jenking logs` streams the entire console log; big pipelines emit MBs,
hostile to both terminals and LLM context windows. Stage-scoped logs already
exist internally (`GetNodeLog(jobPath, buildNumber, nodeID)`), and `ListStages`
returns per-stage `NodeIDs`.

**Changes** in `cmd/jenking/cmd_build_ops.go` (`newLogsCmd` becomes a real
command instead of the generic `newBuildTextCmd`; look at
`internal/action` `KindLogs` to see what it currently does and keep behavior
identical when no flags are given):

- `--tail N`: fetch full text (`GetFullConsoleText`), print last N lines.
- `--stage <name>`: resolve stage by (case-insensitive) name via `ListStages`,
  concatenate `GetNodeLog` for its `NodeIDs`. Error listing stage names when
  not found.
- Optional stretch: `--follow` using `GetProgressiveLog(jobPath, n, start)`
  until `!moreData` (see `internal/jenkins/log.go`), printing increments.

## Gap 5 — `lint` and `replay`

### 5a `jenking lint [<file>]`

`ValidateJenkinsfile` exists (`internal/jenkins/validate.go`, endpoint
`/pipeline-model-converter/validate` — ships with the pipeline model plugin
that any declarative-pipeline shop has; degrade gracefully: 404 → clear
"validation endpoint unavailable" error). Read from file arg or stdin when
arg is `-`/absent. Exit non-zero on invalid; emit
`{"valid": bool, "errors": [...]}` for json.

### 5b `jenking replay <project> <branch> #N [--file <jenkinsfile>]`

`ReplayBuild(jobPath, buildNum, script)` exists (`internal/jenkins/script.go`).
Without `--file`, fetch the original via `GetBuildScript` and replay unchanged
(= rerun); with `--file`, replay the provided script. Print the queue id if
the replay POST returns a Location header (same helper as Gap 1; check
response — replay posts to `<build>/replay/run`).

## Gap 6 — Lifecycle: enable/disable, rescan, node offline, artifact `-O`

All need new client methods (none exist yet). All are core endpoints.

- `jenking enable|disable <project>` — `POST <job>/enable`, `POST <job>/disable`.
- `jenking rescan <project>` — for a multibranch project, `POST <job>/build`
  on the *project* path triggers branch indexing ("Scan Repository Now").
  Validate the target is `JobTypeMultiBranch` via `cache.LookupJob` (see
  `newParamsCmd` for the lookup pattern).
- `jenking node offline <name> [--reason <msg>]` / `jenking node online <name>`
  — `POST /computer/<urlencoded-name>/toggleOffline?offlineMessage=<msg>`.
  Toggle semantics: check current state via `ListNodes` first so
  offline/online are idempotent verbs, not blind toggles.
- `jenking artifact ... -O <path>`: write to file instead of stdout; `-O dir/`
  keeps the artifact's display name. Beware `GetArtifactContent` returns
  `string` — binary-safe in Go, but write with `os.WriteFile`, not fmt.

## Explicitly out of scope (for now)

- Job create/copy/delete and config.xml editing — heavy, risky, TUI has no
  story for it either.
- Credentials, plugin management, script console, restart.
- `ListUserBuilds` is a stub in `internal/jenkins/builds.go` (returns nil) —
  fix or delete before exposing anything on top of it.

## MCP-readiness notes (cross-cutting)

- Ambiguous project-suffix resolution should return a structured error listing
  candidates (check what `navmsg.ResolveTarget` does on ambiguity and improve
  its message if it just picks one or errors opaquely).
- Every mutating command should emit json when `--output json` is set (today
  `trigger`/`cancel`/`dequeue` print plain text unconditionally) — e.g.
  `{"cancelled": "path#42"}`. Fix opportunistically when touching them.

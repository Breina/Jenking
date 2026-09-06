# Targeted commands & CLI deep-link / headless mode

## Context

Today the slash commands `:builds`, `:stages`, `:logs`, `:jobs`, `:matrix` all silently ignore their arguments — they emit a per-command message that resolves to whatever the *current* view's `NavigationContext` happens to be (`app.go:604-677`, `currentContextNC` at `app.go:1514`). There is no way to jump straight to a specific job/branch/build, and `cmd/jenking/main.go` ignores `os.Args` entirely.

Goal: let the user address an exact location from inside the TUI and from the shell, with the same syntax. Three concrete user stories:

1. `:logs webidm feature/foo #42` — in-TUI deep-link to console log of build 42.
2. `jenking logs webidm feature/foo #42` — start the TUI on that view.
3. `jenking logs webidm feature/foo #42 --raw` — dump log to stdout, exit.

The chosen syntax is positional with marker prefixes — Option A from the discussion: `<projectSuffix> <branch> [#build|#last] [:stage]`. Project arg is a path *suffix* resolved against the cache; ambiguous matches → hard error.

## Non-goals

- URL-encoding of branch names. Branch is a single token, kept verbatim, slashes allowed.
- Single-token canonical form (`>`/`::`-separated). Skipped to keep branch slashes literal.
- Trigger headless (`jenking trigger ...`). Future work — needs param plumbing and confirmation flow.
- Interactive disambiguation picker for ambiguous project suffixes. Hard error with candidates listed.
- Replacing the existing per-command message types in this PR — they stay, but each handler grows an explicit-NC entry point.

---

## Design

### Target syntax (one grammar, two entry points)

```
<projectSuffix> <branch> [#<n>|#last] [:<stageRest>]
```

- **Order**: positional args first, then markers (`#`, `:`) in any order.
- **`projectSuffix`**: non-empty path suffix matched against known project paths in the cache (multi-branch project paths or standalone job paths — anything whose `Type != JobTypeFolder`). Unique match required → resolved to full path. Multiple matches → hard error listing candidates. Zero matches → hard error.
- **`branch`**: single whitespace-separated token, kept verbatim. Required only when the resolved project is multibranch *and* a build/stage marker or a stages/log target is requested.
- **`#<n>`** or **`#last`**: build reference. Bare integers without `#` are not accepted (avoids confusion with branch names that happen to be numeric).
- **`:<stageRest>`**: everything from `:` to end-of-input is the stage name (allows spaces, e.g. `:Build & Test`). `:` must therefore be the last marker; stage cannot precede `#`.
- **All args optional**: missing positionals fall back to the *current* view's `NavigationContext` (per user decision). `:logs` with no args == today's behaviour. `:logs #42` on a branch view == build 42 of that branch.

### Three new pieces of code

1. **`internal/tui/command/target.go`** — pure parsing + formatting of the target syntax, no cache or jenkins dependency.

   ```go
   type Target struct {
       ProjectSuffix string       // empty = "use current scope"
       Branch        string       // empty = "use current scope's branch"
       Build         BuildRef     // {Number int, IsLast bool}; zero = no build
       Stage         string       // empty = no stage
   }
   func ParseTarget(args []string) (Target, error)
   func FormatTarget(nc view.NavigationContext) []string  // for "copy address"
   ```

   Tokeniser: walks `args`, classifies each by first-rune marker (`#`, `:`) or as a positional. Stage is special — when a token starts with `:`, the stage is `<that token without ':'>` joined with all subsequent tokens by single spaces. Validation errors: empty suffix with non-empty markers when no current scope is provided (caller decides), invalid build (non-numeric and not `last`), `#` empty.

   Tests: `parser_test.go`-style table covering: empty, project only, project+branch, with `#42`, with `#last`, `:Build & Test`, branch with slash (`feature/foo`), markers reordered, malformed `#abc`.

2. **`internal/tui/command/resolve.go`** — bridges `Target` → `view.NavigationContext` against the cache. Lives in `command/` to keep `view/` free of cache-walking helpers, but imports `cache` and `view` (acceptable: `command` already imports `view` indirectly via app).

   ```go
   type ResolveError struct {
       Suffix     string
       Candidates []string  // populated when ambiguous
   }
   func (t Target) Resolve(store *cache.Store, current view.NavigationContext) (view.NavigationContext, error)
   ```

   Algorithm:
   - If `t.ProjectSuffix == ""` → start from `current` (already an NC).
   - Else: enumerate project paths from cache (see helper below), filter to those whose path equals or ends with `/<suffix>` (case-sensitive, full segment match — suffix `webidm` matches `Code Private/webidm` but not `webidmclient`). Exactly one survivor → set NC's `FolderPath`/`ProjectName` from it; reset `BranchName`/`Build`/`StageName`. Else → `ResolveError`.
   - Apply `t.Branch` via `nc.AtBranch(branch)` if non-empty.
   - Apply `t.Build` via `nc.AtBuild(n)` (or set `Build.IsLast = true`) if set.
   - Apply `t.Stage` via `nc.AtStage(stage)` if non-empty.
   - Inherit `Username`, `GitUsernames`, `FriendlyName` from `current` so downstream views still get filter context.

3. **`internal/cache/jobpaths.go`** — small helper that walks the hierarchical `Jobs` cache and returns `[]string` of all project paths (Type != Folder). Walks `store.Jobs` recursively starting from `""`. No new fetches — pure cache read. Used by `Target.Resolve`.

   The `Jobs` cache (per `cache/store.go:25-34`) is keyed by folder path with `[]Job` values; each `Job` has a `Type` (`internal/jenkins/types.go:14-18`) discriminating folder vs. project. Ensures suffix matching only considers projects, not folders or branches.

### App-side dispatch — explicit-NC entry points

Existing handlers at `app.go:604-677` keep working as the no-arg path. Each command's `Execute` is rewritten:

```go
Execute: func(args []string) tea.Cmd {
    t, err := command.ParseTarget(args)
    if err != nil { return errCmd(err) }
    return func() tea.Msg { return openLogTargetMsg{target: t} }
},
```

App handles `openLogTargetMsg` (one new msg type per existing verb, or — preferred — a single `openTargetMsg{kind ViewKind, target command.Target}`). Handler resolves the target against `a.store` + `a.currentContextNC()`, then constructs the right view:

| Kind | Resolved NC level | View constructed |
|---|---|---|
| Logs | `CtxBuild` | `view.NewConsoleView(theme, client, nc)` (`console.go:55`) |
| Logs | `CtxBuild` + StageName | `view.NewStageView(...)` then drill — TBD or skip stage-log in v1 |
| Logs | `CtxBranch`/`CtxProject`/`CtxFolder`/`CtxRoot` | `view.NewMyConsoleView(theme, client, store, nc.AtScope(), slow)` (`myconsole.go:19`) |
| Stages | `CtxBuild` | `view.NewPendingStageView(theme, client, store, nc, lastKnownBuild)` (`stageview.go:163`) — kicks its own fetch in `Init`, no need to pre-fetch `jenkins.Build` |
| Stages | scope levels | `view.NewMyBuildsView(...)` (`mybuilds.go:44`) |
| Builds | any | reuse `buildsViewForCurrentContext`, generalised to take an NC arg |
| Jobs | any | reuse `jobListForCurrentContext`, generalised to take an NC arg |
| Matrix | scope levels | `view.NewMyMatrixView(...)` (`mymatrix.go:18`); preserve today's gating (theme==Matrix && currentView is RunningLogView) only when no args; with args, allow direct deep-link |
| Trigger | scope levels | future — emit `view.ErrorMsg` for now |

`buildsViewForCurrentContext` (`app.go:1458`) and `jobListForCurrentContext` (`app.go:1475`) become `buildsViewFor(nc)` and `jobListFor(nc)`; current-context callers pass `a.currentContextNC().AtScope()` / `a.currentContextNC()`. No behaviour change for no-arg slash commands.

Errors from `ParseTarget` / `Resolve` flow through `view.ErrorMsg` (already used for unknown-theme/colorblindness errors at `app.go:151,234`) and surface in the status bar. Ambiguous-suffix errors render as `"ambiguous: <suffix> matches: <a>, <b>, <c>"`.

### CLI side

Modify `cmd/jenking/main.go`:

```
jenking                                  → TUI dashboard (today)
jenking <verb> [args...] [--raw]         → CLI mode
```

`<verb>` ∈ {`logs`, `describe`, `tests`, `builds`, `stages`, `jobs`, `matrix`, `trigger`}. The CLI shares the registry-style verb table by exposing each verb's parser+resolver. Two execution modes:

1. **Deep-link TUI** (default for navigational verbs and for read-only verbs without `--raw`): same flow as the existing TUI startup, but instead of seeding `dashboard` as `initialView`, seed the resolved view. `tui.NewApp` already accepts an `initialView` parameter (`app.go:124`); just construct the deep-link view in `main.go` after `WhoAmI` and pass it. Breadcrumb and ESC-back semantics remain consistent because views implement `HasParent` — ESC from a deep-linked log view returns to its natural parent (`scopedview.go:285`).

2. **Headless** (`--raw` for read-only verbs `logs`, `describe`, `tests`): bypass `tea.Program` entirely. Construct `jenkins.Client`, resolve the target, call the appropriate API, write to `os.Stdout`, exit.

   New package **`internal/action/`**:

   ```go
   // action/action.go
   type Kind int
   const (KindLogs Kind = iota; KindDescribe; KindTests; ...)

   type Request struct {
       Kind   Kind
       Target command.Target
       Format Format  // FormatRaw, FormatJSON, FormatPretty
   }

   func Run(ctx context.Context, client jenkins.JenkinsClient, store *cache.Store, req Request, w io.Writer) error
   ```

   Headless executors:
   - **Logs**: `client.GetFullConsoleText(ctx, jobPath, buildNum)` (`jenkins/log.go:13`). Build `last` resolution: if `Target.Build.IsLast` or unset, fetch the latest build for the resolved branch via existing project/branch builds API (`internal/jenkins/builds.go`). Write text to `w`.
   - **Describe**: `client.GetBuildScript(ctx, jobPath, buildNum)` (`jenkins/script.go:31`) + `client.GetBuildParameters(ctx, jobPath, buildNum)` (`jenkins/script.go:198`). For `--raw`: emit the script. For pretty: script + params header.
   - **Tests**: `client.GetTestReport(ctx, jobPath, buildNum)` (`jenkins/tests.go:13`). Raw = JSON dump of report; pretty = formatted summary (failures first). Skip pretty for v1 if it bloats scope — emit JSON only.

   Cache must be warm enough for project-suffix resolution. Two strategies — pick at implementation time:
   - **Strategy A**: For headless mode, accept *only* full paths (no suffix matching) when cache is empty — i.e. require `--project Code\ Private/webidm` long form, or accept the full path as-is. Less ergonomic but no startup cost.
   - **Strategy B (preferred)**: Run `client.ScanAllBuilds` synchronously on cold start to populate `AllBuilds`, then derive project paths. Costs one HTTP call; matches what the TUI does on first dashboard load.

   Argument parser: tiny — first non-flag token is `<verb>`, rest goes to `command.ParseTarget`. `--raw` is the only flag for v1. Use stdlib `flag` package or a hand-rolled split (3-line parser is fine — keep dependencies clean).

### ArgSuggest (tab completion in TUI)

Wire each command's `ArgSuggest` (already a registry hook — `command/registry.go:18`) to:

- **Arg 1 (project suffix)**: `cache.AllProjectPaths(store)` filtered by suffix prefix-match. Return last path segments first, then full paths if user typed `/`.
- **After arg 1 locked in**: if cache has `Builds[projectPath]`, complete branch names from there (multibranch); otherwise no completion.
- **`#`**: complete with `#last` and recent build numbers from cache.
- **`:`**: complete with stage names from the latest cached `WorkflowRun` for that build (if available).

Ship suggestion arg-1 only in v1 for parity with current registry (already supports it). Arg-2/3/4 completion is incremental polish.

---

## File-by-file changes

| File | Change |
|---|---|
| `internal/tui/command/parser.go` | No change. `Parse()` already splits on whitespace; that's fine for the verb itself. |
| `internal/tui/command/target.go` | **NEW**. `Target` struct, `ParseTarget`, `FormatTarget`, `BuildRef`. |
| `internal/tui/command/target_test.go` | **NEW**. Table-driven tests. |
| `internal/tui/command/resolve.go` | **NEW**. `Target.Resolve(store, currentNC) (NavigationContext, error)`. Imports `cache` and `view`. |
| `internal/tui/command/resolve_test.go` | **NEW**. Uses an in-memory cache fixture. |
| `internal/cache/jobpaths.go` | **NEW**. `AllProjectPaths(store *Store) []string` — walks `store.Jobs` recursively, collects non-folder leaves. |
| `internal/cache/jobpaths_test.go` | **NEW**. |
| `internal/tui/app.go` | Rewrite each navigation command's `Execute` to call `ParseTarget` + emit a unified `openTargetMsg`. Add one handler that resolves and dispatches. Generalise `buildsViewForCurrentContext` → `buildsViewFor(nc)` and `jobListForCurrentContext` → `jobListFor(nc)`. Replace the six `open*ForContextMsg` types with a single `openTargetMsg{kind, target}`. |
| `internal/action/action.go` | **NEW**. `Kind`, `Format`, `Request`, `Run`. |
| `internal/action/logs.go` | **NEW**. Headless logs executor. |
| `internal/action/describe.go` | **NEW**. Headless describe. |
| `internal/action/tests.go` | **NEW**. Headless tests. |
| `internal/action/lastbuild.go` | **NEW**. Helper to resolve `#last` for a branch when not in cache (fetch from API). |
| `cmd/jenking/main.go` | Add argv parsing: detect `<verb>` as first non-flag token; route to either headless `action.Run` (with `--raw`) or TUI deep-link by constructing the resolved view and passing it as `initialView` to `tui.NewApp`. Existing no-arg path untouched. |

Estimated diff: ~600-800 lines of new code, ~100 lines modified in `app.go`.

---

## Implementation order (PR-sized chunks)

1. **Parser + resolver + cache walker** (no behaviour change yet). New files only; tests prove parse/resolve correctness.
2. **App-side wiring**: replace per-command messages with `openTargetMsg`, generalise the two context-helpers, route through `Resolve`. Today's no-arg behaviour must be byte-identical — verify via the existing TUI manually. Hard errors land in the status bar.
3. **CLI deep-link** (no `--raw` yet): argv parsing, deep-link via `initialView`. Verifies the same code path the slash commands hit.
4. **Headless executors**: `internal/action/`, `--raw` flag, three verbs (`logs`, `describe`, `tests`). Each is a small independent commit.
5. **ArgSuggest for arg 1** (project suffix completion from cache). Polish.

Stop points after 1, 2, 3, 4 are independently shippable.

---

## Verification

**Unit**:
- `go test ./internal/tui/command/...` — covers `ParseTarget` table, `Resolve` against synthetic cache (unique match, ambiguous, no match, with/without branch, build ref, stage with spaces).
- `go test ./internal/cache/...` — `AllProjectPaths` walks fixtures correctly (folders skipped, nested folders descended).
- `go test ./internal/action/...` — mocks `JenkinsClient` interface, asserts each executor writes expected output.

**Manual TUI** (golden paths):
- Cold start: `:logs` (no args) on dashboard → status-bar error or no-op (define which); on a branch view → opens log of last build of that branch (today's behaviour). Confirm zero regression.
- `:logs webidm` → opens MyConsoleView for unique-matching project.
- `:logs webidm feature/foo` → opens MyConsoleView scoped to that branch.
- `:logs webidm feature/foo #42` → opens ConsoleView for build 42 directly.
- `:logs webidm feature/foo #last` → equivalent to no `#`.
- `:logs webidm feature/foo #42 :Build & Test` → stage selected (or v1: stage is recorded in NC, MyConsoleView ignores; deferred).
- Ambiguous suffix: `:logs config` (matches multiple) → status bar shows `ambiguous: config matches: ...`.
- Unknown suffix: `:logs nope` → status bar shows `unknown project: nope`.
- ESC from any deep-linked view returns to natural parent (uses existing `HasParent`).

**Manual CLI**:
- `./jenking logs webidm feature/foo #42` → TUI starts, opens log directly.
- `./jenking logs webidm feature/foo #42 --raw` → log dumped to stdout, exit 0; `wc -l` matches expected length; piping `| grep ERROR` works.
- `./jenking describe webidm feature/foo #42 --raw` → script printed.
- `./jenking tests webidm feature/foo #42 --raw` → JSON test report.
- `./jenking logs nope` → exit 1, error to stderr, no TUI.
- `./jenking` → today's behaviour.

---

## Open questions / deferred decisions

1. **Stage-log deep-link** (`:logs project branch #42 :Stage`): today there's a `StageLogView` somewhere — needs a quick check before claiming the kind table covers it. If not trivially constructable from an NC, defer stage-log to v1.5.
2. **Cold-cache headless**: Strategy A vs B chosen above as B (one-shot `ScanAllBuilds`); confirm acceptable latency on real Jenkins instance. Fallback to A is a one-line change.
3. **Trigger via CLI**: explicitly out of scope here. Will require `-p key=value` plumbing and either `--no-confirm` or interactive flow — separate plan.
4. **`#` quoting in shells**: zsh treats `#42` literally only if globbing is off; bash needs no quoting. Document `'#42'` quoting in `--help`. Alternative: accept bare integer as build when it's the third positional, but that re-introduces the "is `42` a build or a branch named 42" ambiguity. Stay strict.
5. **Format JSON / pretty for headless**: shipped as `--raw` only in v1; `--format json|pretty` is a follow-up.

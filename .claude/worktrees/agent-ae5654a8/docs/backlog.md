# Jenkins TUI — Backlog

> Implementation backlog structured for incremental delivery.
> Items within each group are roughly ordered by priority/dependency.
> Each item is scoped to be implementable independently unless a dependency is noted.

---

## Group 1 — Critical Bugs

### B10 · Colorblind mode shirt blue

When shifting green -> blue in colorblind mode, need to shift blue to a different color as well to distinguish running / success.
Fix: also shift blue colors to something else

### B11 · Autoadvance into child stages

In the stages view, when a parent stage is Running, and one of its children is Running, the auto-advance should enter its child stage.
Bug: currently, when selecting a running child of a running parent, the selection is moved onto the parent once again.

### B12 · Aborted substage clears logs

In the stages view, when selecting an aborted substage, the stage logs shows briefly (in the preview window), then clears for some reason. It shouldn't clear.

### B13 · Stages view auto-advance sometimes goes past failed stage

Sometimes, stages view auto-advances past a failed stage. It should not move past failed stages.
Opening a finished stage that has its last stage Skipped, it selects that instead of the last non-skipped stage.

### B14 · Pipeline status finish miss

Sometimes when a pipeline finishes in the stages view, it misses the build status update and the progress bar keeps running in overtime.

### B15 · Scrolling through stages view has poor performance

It seems to be doing too much, or not enough of it is cached. Scrolling in the stages view is now doing work inside the UI thread.
At most, retrieve from the cache, if there's a network call to be done, do it asynchronously.
Scrolling through the main logs is also slow, in some cases.

### B16 · Status always says connected

Even when live connection fails (error showing `list jobs: executing request: Get "https://jenkins.cumuli.be/job/Code/api/json?tree=jobs[name,url,color,_class,lastBuild[number,url,timestamp,estimatedDuration],jobs[name,color,lastBuild[number,url,timestamp,estimatedDuration]]]": dial tcp: lookup jenkins.cumuli.be on 127.0.0.53:53: no such host (press any key to dismiss)`), it still says connected.

### B17 · Better colourblind support
WithDeuteranopiaFilter should structurally remap the colorspace; not just overwrite a couple of colors. This needs to be theme-independent.
Loop through all used colors and remap them. Also support other colorblind modes (let `:colorblind` open up a popup menu).

### B18 · Stages view initial log streaming

In the log preview window in the stages view, the initial log streaming is active before the first stage appears.
When hitting ESC and opening the same stages view again, this initial log stream is not present any more. I expected it to actually be active.

### B19 · Log streaming skip detection issue

In this example, `Trivy scan` and `Maven verify` are marked as `Success`, when they are actually skipped.
TUI:
```
⇶ Non-primary branch   Skipped
┣━▸ Trivy scan         Success
┣━▸ Maven verify       Success
┗━▸ Maven deploy       Skipped
▸ Primary branch       Running
├─▸ Validate tag       Success
├─▸ Maven deploy       Success
```
Raw pipeline text:
```
[Pipeline] stage
[Pipeline] { (Non-primary branch)
Stage "Non-primary branch" skipped due to when conditional
[Pipeline] getContext
[Pipeline] parallel
[Pipeline] { (Branch: Trivy scan)
[Pipeline] { (Branch: Maven verify)
[Pipeline] { (Branch: Maven deploy)
[Pipeline] stage
[Pipeline] { (Trivy scan)
[Pipeline] stage
[Pipeline] { (Maven verify)
[Pipeline] stage
[Pipeline] { (Maven deploy)
Stage "Trivy scan" skipped due to when conditional
[Pipeline] getContext
[Pipeline] }
Stage "Maven verify" skipped due to when conditional
[Pipeline] getContext
[Pipeline] }
Stage "Maven deploy" skipped due to when conditional
[Pipeline] getContext
[Pipeline] }
[Pipeline] // stage
[Pipeline] // stage
[Pipeline] // stage
[Pipeline] }
[Pipeline] }
[Pipeline] }
[Pipeline] // parallel
[Pipeline] }
[Pipeline] // stage
[Pipeline] stage
[Pipeline] { (Primary branch)
[Pipeline] stage
[Pipeline] { (Validate tag)
[Pipeline] script
[Pipeline] {
[Pipeline] sh
+ git tag -l 0.1.99
+ grep -q ^0.1.99$
+ echo new
[Pipeline] }
[Pipeline] // script
[Pipeline] }
[Pipeline] // stage
[Pipeline] stage
[Pipeline] { (Maven deploy)
[Pipeline] script
[Pipeline] {
```

---

## Group 2 — Navigation System Rework

### B2 · Broken navigation references / breadcrumb abstraction
**Files:** `internal/tui/app.go`, all view files
The reference string (e.g. `spring-security-oauth-demo/main/#64`) is constructed ad-hoc in multiple places. Pressing ESC after a shortcut-initiated navigation returns to the wrong level.
**Fix:** Introduce a typed `NavigationStack` that records `(view, params)` pairs rather than string references. Every push to the stack must be reversible. All shortcuts (`s`, `l`, etc.) must push their origin onto the stack. Escape always pops one entry.
**Note:** This is a foundational refactor. Do it before implementing new shortcuts in Group 4.

### B4 · Folder scan not shown as running pipeline
When a folder is being scanned it does not appear in the Running Builds view.
**Investigate:** Is this a Jenkins API limitation (scan jobs are not "builds" in the API sense) or is our `ListRunningBuilds` query filtering them out? Document the answer, and if possible surface scan jobs in a visually distinct way.

---

## Group 3 — Performance & Async

### B6 · UI thread blocked by network requests during stage scroll
**Files:** `internal/tui/view/` (stage view), `internal/jenkins/`
Holding an arrow key while in the stage view causes noticeable lag because stage log fetches happen synchronously on the update cycle.
**Fix:** All Jenkins API calls must be dispatched as `tea.Cmd` goroutines. Audit every `Update` handler for direct blocking calls; convert them to commands that send a `Msg` on completion.

### C3 · Cache stage logs for completed stages [DONE]
**Files:** `internal/jenkins/`, `internal/tui/view/` (stage/log views)
Finished stages are immutable — their logs never change. Re-fetching them on every selection wastes bandwidth and latency.
**Fix:** Add an in-process cache keyed by `(buildRef, stageId)` that stores logs for stages with a terminal status (`SUCCESS`, `FAILURE`, `ABORTED`, `SKIPPED`). Wire a simple LRU or time-based eviction. Consider making this cache generic enough to reuse for other immutable data (build details, test results).
Addendum by human: Also cache running logs! Switching between stages view and stage logs view uses the same logs; just these logs still need to be updated afterwards still, whilst the terminal ones don't.

---

## Group 4 — Keyboard Shortcut Overhaul

> Depends on **B2** (navigation stack) being done first.

### C2 · Unified `s` / `l` shortcuts + remove `f`
The `f` key is redundant once `s` opens the stage view directly. Define consistent cross-view behaviour:

| Context | `s` | `l` |
|---|---|---|
| Job list — MultiBranch project | Stage view of most recently started build | Full logs of most recently started build |
| Job list — Folder | Stage view of most recently started build across all sub-jobs | Full logs of same |
| Job list — Branch / MR | Stage view of last build | Full logs of last build |
| Build list | Same as Enter (stage view of selected build) | Full logs of selected build |
| Running builds | Stage view of selected build | Full logs of selected build |

Remove `f` everywhere. Update the header shortcut hints.

### C4 · Rename running builds shortcut `b` → `r`
**Files:** `internal/tui/app.go`, `internal/tui/component/header.go`
Simple keybinding rename. Update all hint strings.

### C5 · Disable `p` in stage logs view
`p` (pause/play streaming) is only meaningful for full logs. Disable it (and hide the hint) when in the stage logs view.

### C8 · Add `w` (wrap toggle) to stage view preview logs
Already present in full logs. Wire the same toggle to the preview pane inside the stage view.
Wrapping in the preview logs currently breaks the UI (the header and everything moves up out of screen).

### N21 · Stage view always tracks `#last`
Provide a way to open the stage view pinned to the latest build of a job (e.g. `#last`) so that after a new build starts it automatically switches over without the user navigating back to the builds list.

---

## Group 5 — Jobs View Enhancement

### C1 · Richer jobs table columns
The current table shows only the status of the main branch. Proposed columns:
- **Job** — name
- **Main** — stable / unstable / failed / not built (icon + color)
- **Last build** — relative time of the most recent build on any branch
- **Status** — `2 running` badge when multiple builds of this job are active; a progress bar when only one is running, or the status of the last build 

### N8 · Queued builds count in header + cancel
Add a queued-builds count next to the running-builds count in the header. A new view (shortcut `q`) lists queued builds and allows cancelling them.

### Time spent waiting

The Jenkins GUI shows how much time spent waiting, how much time spent building. Do we have this information? Where would be use it?

---

## Group 6 — Visual & Display

### C7 · Visually distinguish branch name in references
In `spring-security-oauth-demo/feature/myfeature/#64` the branch segment looks identical to the job segment. Options:
- Italicise or dim the branch portion
- Prefix with a branch icon `⎇`
- Use a different separator (e.g. `@` or `:`): `spring-security-oauth-demo ⎇ feature/myfeature #64`

Pick one consistent approach and apply it everywhere a reference is rendered.

### C6 · Failed build with no failed stage
When a build fails but every stage reports Success (e.g. a syntax error before any stage runs), the stage view has no obvious signal.
The actual error we're looking for is present in the full logs, example:
```
De Trivy scan is tijdelijk buiten gebruik (INFRA-5897)
[Pipeline] }
[Pipeline] // script
[Pipeline] }
[Pipeline] // stage
[Pipeline] stage
[Pipeline] { (Build Maven and deploy to Maven repository (artifactory))
[Pipeline] script
[Pipeline] {
Scripts not permitted to use method groovy.lang.GroovyObject invokeMethod java.lang.String java.lang.Object (org.jenkinsci.plugins.workflow.cps.CpsClosure2 maven java.util.LinkedHashMap). Administrators can decide whether to approve or reject this signature.
[Pipeline] }
[Pipeline] // script
[Pipeline] }
[Pipeline] // stage
[Pipeline] stage
[Pipeline] { (Release maven)
Stage "Release maven" skipped due to earlier failure(s)
```
Our stage view only shows logs of stages, and this isn't part of any stage. How to visualize this nicely?
I think by maybe adding a root "stage" `Pipeline`, that logs the full logs. All stages are children of this?


### N3 · Dynamic terminal window title
Set the terminal title (via ANSI escape) to reflect current context, e.g.:
- `Jenking` at the top level
- `Jenking — spring-security-oauth-demo/main` when drilling into a job
- `Jenking — [RUNNING] spring-security-oauth-demo/main #64` during a live build
Discuss with the user what the options are and what he wants.

### N18 · Stage view: ghost stages from previous run + per-stage progress bars
While a build is running, show the stage names from the previous build grayed out and unselectable below the currently known stages. This sets expectations for how many stages remain.
- If current stage names match previous ones, show a per-stage progress bar using the previous run's stage durations.
- If names diverge (e.g. a stage was added/removed), hide the ghost stages.
- Use per-stage timing to improve the main build progress bar accuracy (hard problem due to branching sub-stages; take the minimum of either the sum of expected stage durations and the pipeline lastSuccesfulRun) 

---


## Group 7 — Log Enhancements

### N7 · Horizontal scrolling in logs (when wrap is off)
When wrap is disabled, long lines are clipped. Add left/right scroll (arrow keys or `h`/`l`) to pan the viewport horizontally, matching k9s behaviour.

### N9 · Error / warning highlighting in logs
- Highlight lines matching error/warning patterns (red / yellow).
- `F2` / `Shift+F2` jumps to next / previous highlighted line.
- Show an icon in the top-right corner of log views with the count: `⚠ 3  ✕ 7`.

### N20 · Detect skipped stages from main log [DONE, but with a bug]
The main build log contains lines like `Stage "Git tag" skipped due to when conditional`. Parse these (lazily, in the background) to mark stages as `SKIPPED` in the stage view, even when Jenkins itself reports them as `SUCCESS`.
**Note:** Requires the main log to be fetched; use the cache from C3 to avoid redundant fetches.
Human addendum: First check if this data is available from the AJAX table first.
Human addendum: If we end up parsing the main logs anyway, could this not replace the AJAX table fetching? Parsing these [Pipeline] tags, we have all the data we need to show stages. We do have to check if this suffices for Matrix and parallel builds.

---

## Group 8 — My Builds View

### N1 · Personal builds hotlist [REPLACED BY SECTION "View × Context Matrix"]
New view (shortcut `m`) showing builds triggered by the authenticated user, ordered by recency. Include both running and recently completed builds. Cap the list at a configurable number (default 50). Useful as a "what am I working on right now" dashboard.

---

## Group 9 — Pipeline Describe & Edit

### N5 · Describe view — show and edit Jenkinsfile
New view (shortcut `d` or command `:describe`) that fetches and displays the `Jenkinsfile` / pipeline script for the selected job. With `e`, open the file in `$EDITOR` (default `vi`). After saving, optionally trigger a build with the edited script (like the Jenkins Replay feature).

### N14 · Filter pipelines by stage name
From the job list, allow filtering to show only jobs whose last build contained a stage matching a search string. Useful for auditing which pipelines use a particular tool (e.g. "Trivy").
**Depends on:** N5 or an API that returns stage names per job.

### N6 · Library version bumper
**Depends on:** N5
A dedicated view that lists jobs and which shared pipeline library version they declare. Allows bulk-editing library versions to test upgrades, and optionally raises an MR or calls a custom pipeline to persist the change.

---

## Group 10 — Test Results

### N12 · Test results integration
- In the builds view, show a pass/fail/skip count badge next to the build status.
- New view (shortcut `t` from a build) showing test results in a tree: package → class → test case, with PASSED / FAILED / SKIPPED icons and duration.
- Mirrors the information on Jenkins' test results page.

---

## Group 11 — Advanced / Long-term

### N10 · Multi-controller context switching
Support multiple Jenkins controller URLs in config. Add a `:context` / `:ctx` command to switch between them. Display the active context name in the header.

### N11 · Build diff
From the builds list, a command `diff #13 #14` (or multi-select + `d`) shows the git diff between the two build revisions, using `git diff` on the SCM refs Jenkins recorded.

### N15 · Orphan & zombie hunter
A view that surfaces:
- Jobs not run in the last 90 days
- Branch pipelines whose corresponding git branch no longer exists
Allow bulk-deletion or archiving from this view.

### N2 · Colorblind modes [DONE]
A translation layer between the app's semantic colors and the rendered theme. Provide at least one alternative palette (e.g. deuteranopia-safe). Switchable via config or `:colorblind` command.
Human note: I want colorblind mode to work regardless of theme. It should wrap a theme and just offset certain colors. Since red/green are the most important in the app (success/failure), having daltonism support is paramount. Also one of my colleagues has this.

### N17 · Terminal notifications
Send a desktop notification (via `notify-send` / macOS `osascript`) when a watched build completes. Opt-in per build with a shortcut (e.g. `n` to toggle notification for the selected build/branch/job).

### N13 · Build artifacts
List and download build artifacts from a completed build. Low priority — not currently used in the target environment.

### N16 · SSO / OIDC authentication
Support authenticating via browser-based SSO flow in addition to username + API token. Requires a local callback server for the OAuth redirect.

---

## Needs More Design

These items need more thought before they can be scheduled:

- **N4 · Commands system** — what commands are still useful given we have keybindings for everything? Confirmed need: `:trigger <job> [params]`. Design the command palette UX first.

## View × Context Matrix

Every new view proposal should be checked against this matrix. **E** = exists, **Y** = makes sense to add, **~** = marginal/expensive, **-** = not applicable.

| | `*` | `folder` | `project` | `branch/MR` | `build` | `stage` |
|---|---|---|---|---|---|---|
| **jobs** | **E** (root) | **E** | **E** (branches) | - | - | - |
| **builds** | ~ | ~ | **Y** | **E** | - | - |
| **stages** | **Y** (mine) | - | **Y** | **Y** | **E** | - |
| **log** | - | **Y** | **Y** | - | **E** | **E** |

### New view candidates

- **stages(*/#last) + mine** — "Follow my work" view. Shows the stages view of the user's most recent build across all projects. Auto-advances when a new build starts, regardless of project. The reference reads `stages(*/#last)` and the status bar `[123 — dov-app-website/main/#142]` shows both the fetched line count and the resolved project/branch/build, reusing the existing `[123]` indicator space. Subsumes N1 "Personal builds hotlist" from Group 8.
- **builds(project)** — "Build history" across all branches, listing `branch/#build` pairs. Single API call to the multibranch job. High value. See also: Jenkins GUI "build history" view.
- **builds(folder)** / **builds(*)** — Requires iterating all projects. Expensive, but could work with caching or lazy loading. Marginal priority.
- **stages(project)** / **stages(branch)** — Stage comparison view: matrix of builds × stages with pass/fail. Useful for spotting flaky stages. `stages(branch)` is simpler (one branch, multiple builds); `stages(project)` spans branches.
- **log(folder)** / **log(project)** — Jenkins folder/project-level logs (scan logs, indexing logs). Useful for GitLab integration debugging.

### Filters: `running` and `mine`

`running` and `mine` are **filters**, not distinct views. They apply as toggleable overlays on existing views:

| Filter | Applies to | Meaning |
|---|---|---|
| `mine` | `builds`, `stages` | Only builds triggered by current user |
| `running` | `builds`, `stages` | Only in-progress builds |

Examples of filter combinations:
- `builds(*) + running` → equivalent to the current "running builds" view
- `builds(project) + mine` → my builds of a project
- `stages(branch/#last) + mine` → stages of my last build on a branch

Filters are toggled with keybindings (e.g. `m` for mine, `r` for running) on any applicable view.

`#last` is a **moving reference**, not a filter — it resolves to the latest build and can be combined with filters (e.g. "last of mine").

### Filter indicators in header

Active filters must be visually indicated in the header, consistent and simple:
- **`running` filter** — indicated in the existing `Running: ● 1` header section (e.g. highlight/underline when active as filter)
- **`mine` filter** — indicated in the existing `User: Brecht Derwael` header section (same visual treatment)

The exact visual treatment (highlight, underline, border, icon) should be consistent between both indicators.

### Design rule

Any new view that is considered for implementation should go through this matrix to check which context levels make sense before work begins.
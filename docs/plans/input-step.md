# Pipeline `input` step support — design

Status: Phase 1 spec, awaiting sober review.

## Goal

Let Jenking users approve or abort Jenkins pipeline `input` steps from the TUI.
Cover all four variants: parameter-less, parameterized, submitter-gated, and
timeout-wrapped.

## Compatibility constraints

Pure-core only. No dependency on optional plugins (`pipeline-stage-view`,
`pipeline-rest-api`, Blue Ocean). All endpoints used here are provided by
`workflow-cps` / `workflow-support`, which are required to run pipelines at all.

- Detection: extend the existing `GET /job/.../{n}/api/json` tree.
- Approval: `POST /job/.../{n}/input/{stepId}/proceedEmpty`
- Approval with params: `POST /job/.../{n}/input/{stepId}/submit`
- Abort: `POST /job/.../{n}/input/{stepId}/abort`
- Auth: Basic + API token (Jenkins exempts token auth from CSRF since 2.96).
  No crumb handling required — confirmed by reading `internal/jenkins/client.go`,
  which has no crumb code, and `TriggerBuild` / `CancelBuild` work fine via
  `c.post()`.

## Data model changes

### `internal/domain/jmodel/jmodel.go`

Add to the `BuildStatus` enum:

```go
BuildStatusPausedInput BuildStatus = "paused_input"
```

Add new type:

```go
type PendingInput struct {
    ID           string                  // input step id (from urlName / id)
    Message      string                  // prompt text
    ProceedText  string                  // ok button label (custom or "Proceed")
    AbortText    string                  // abort label (custom or "Abort")
    Submitter    string                  // submitter user/group restriction; "" = any
    StageNodeID  int                     // flow node id; correlates to Stage.NodeIDs
    StageName    string                  // best-effort name fallback
    Parameters   []ParameterDefinition   // reuse trigger param defs; empty for confirm-only
    ProceedURL   string                  // path for POST proceed (relative to baseURL)
    AbortURL     string                  // path for POST abort
}
```

Add to `BuildDetail`:

```go
PendingInputs []PendingInput
```

### Justification

- Reusing `ParameterDefinition` means the `ParamForm` widget is immediately
  reusable for parameterized inputs.
- `PendingInput` lives at the run level (where Jenkins serves it from), with a
  node-id back-reference so views can correlate to stages. This matches the
  protocol exactly — no invented relationships.
- New status value is per-`Stage`, not per-`Build`. Parallel pipelines can have
  one branch paused and others running. Build-level status remains
  `BuildStatusRunning` while any non-terminal stage exists. "Build needs your
  attention" is *derived* from `len(BuildDetail.PendingInputs) > 0`, never
  stored separately.

## Adapter layer

### `internal/jenkins/types.go`

Extend `jsonAction` to capture input executions:

```go
type jsonAction struct {
    Class      string          `json:"_class"`
    Parameters []jsonParameter `json:"parameters"`
    Causes     []jsonCause     `json:"causes"`
    Executions []jsonInputExec `json:"executions"` // present on InputAction
}

type jsonInputExec struct {
    ID            string                    `json:"id"`
    URLName       string                    `json:"urlName"`
    Class         string                    `json:"_class"`
    // Other fields populated from a separate call — see below.
}
```

**Open question**: the bare `/api/json` actions array returns only the
`InputAction` class with `_class` and a URL. The full input metadata
(message, parameters, submitter) is on the action node itself, fetched via
`GET /job/.../{n}/input/api/json` or by following the `inputAction.urlName`.
Need to confirm with a real JSON sample which fields come back where —
this might mean one extra GET when pending inputs exist (cheap; only happens
on paused builds).

### `internal/jenkins/builds.go`

1. Extend `GetBuild` tree to include the actions class. (Or keep current call
   shape and follow-up with a second call only when needed.)
2. New methods:

```go
func (c *Client) ProceedInput(ctx context.Context, jobPath string, build int, inputID string, params map[string]string) error
func (c *Client) AbortInput(ctx context.Context, jobPath string, build int, inputID string) error
```

Both use `c.post()`. Form-encode params for the parameterized case, identical to
`TriggerBuild`'s `buildWithParameters` pattern.

### `internal/domain` overlay

Stages come from `flowGraphTable`; pending inputs come from `/api/json`. They
must be merged. Add a tiny pure function in `domain/jmodel`:

```go
// ApplyPendingInputs sets Status = BuildStatusPausedInput on any stage whose
// NodeIDs intersect a pending input's StageNodeID, with stage-name fallback.
func ApplyPendingInputs(stages []Stage, inputs []PendingInput) []Stage
```

Called by the view after both fetches resolve. Pure function, fully testable in
isolation. The adapter remains a parser; the merge is a domain concern.

## Action layer

### `internal/action/input.go` (new)

Two new actions following the existing pattern in `action/`:

```go
type ProceedInputAction struct { /* job, build, inputID, params */ }
type AbortInputAction   struct { /* job, build, inputID         */ }
```

Each implements the same `Action` interface used by `describe.go`, `tests.go`,
etc.

## View layer

### Behavior pattern fit

`StageView` already uses behavior modules registered via `registerBehaviors()`.
This is the seam: add `behavior_input.go` mirroring `behavior_trigger.go` /
`behavior_cancel.go`. The mixin / behavior pair model handles:

- key dispatch
- the async fetch / submit lifecycle
- result message routing

### `internal/app/view/behavior_input.go` (new) + `inputmixin.go` (new)

Mirrors the trigger pair. Differences:

- **No pre-fetch.** Pending input data is already in `BuildDetail.PendingInputs`
  delivered by the existing build-detail poller. The behavior reads from there.
- **Entry point.** `<Enter>` on a stage whose `Status == BuildStatusPausedInput`:
  look up the matching `PendingInput` by node id, open the input dialog.
- **Two actions, not one.** Proceed and Abort both need two-keypress confirm
  (per design decision #2). Build a small "armable button" affordance in the
  modal: first press arms (visual change), second press fires.

### Reusing `ParamForm`

`paramform.go:38` hard-codes title `"Trigger Build"`. Add a setter or new
constructor:

```go
func NewParamFormWithTitle(t theme.Theme, title string, params []jmodel.ParameterDefinition) ParamForm
```

For inputs, title = the pipeline's `message` text.

### Confirm-only vs form modal

For parameter-less inputs (the common case in the OP's snippet), don't show a
form. Show a dedicated `InputConfirmDialog` component: message + two armable
buttons (proceed / abort).

For parameterized inputs, render the param form + proceed/abort armable buttons
below it. Submit button = proceed (collects values); Cancel = abort.

**One reusable widget**: both flows live in a single `inputDialog` component
that internally switches between confirm-only and form mode based on
`len(input.Parameters) == 0`.

### Live sync with polling

The existing `fetchBuildDetail` poll continues while the modal is open.
On each tick:

- If the input's ID is no longer in `BuildDetail.PendingInputs` → auto-close
  the modal with a brief banner: `"No longer pending — proceeded, aborted, or
  timed out elsewhere."`
- If the modal is showing the form, preserve user-entered values across ticks;
  do not wipe them on each refresh.

This satisfies decision #5 (keep polling, stay in sync) without inventing a new
polling loop.

### Stage-tree rendering

`populateTable()` in `stageview.go` maps `Stage.Status` to a glyph/color via
the theme. Add a theme mapping for `BuildStatusPausedInput`:

- Distinct color (e.g., yellow/orange, theme-dependent — defer to colorblind
  audit before final choice).
- Glyph: `⏸` or `[!]` — needs to render in all configured themes including
  colorblind. Use the theme's existing accent for "needs attention" type
  states.
- Append `[INPUT]` suffix to the stage label so the meaning is unambiguous
  even in monochrome themes.

### Build-list rendering

When `len(BuildDetail.PendingInputs) > 0`, append a small `[?]` glyph next to
the build status indicator in build lists. Derived at render time, no model
change.

## Submitter handling

V1: attempt-and-surface-error. If the POST returns 403, the modal shows the
error inline (`"Not authorized to approve. Submitter: <name>"`) and stays open
so the user can try again or abort the modal.

V2 (deferred): if `Submitter` is non-empty, pre-fetch group membership and
warn before the user types anything. Not worth the extra HTTP cost in V1.

## Timeout handling

The pipeline's `timeout` wrapper auto-aborts inputs server-side. The TUI does
not need to know the timeout duration — the polling + auto-close handles it
naturally: when the input disappears from `pendingInputActions`, modal closes
with the standard "no longer pending" banner. No special UI work needed.

A future polish item: if Jenkins exposes a deadline in the `pendingInputAction`
JSON, show a countdown. Defer until we see the JSON sample.

## Phased implementation

### Phase 0 — Discovery (needs Brecht, sober)

1. On a real Jenkins, run a pipeline that hits `input` (parameter-less). Dump:
   - `GET /job/{name}/{n}/api/json?tree=actions[*]`
   - `GET /job/{name}/{n}/input/api/json` (if it 404s, the data is in the
     above)
2. Same with a parameterized input.
3. Same with a submitter-restricted input.
4. (Optional) Same with a `timeout`-wrapped input — capture once and once
   right before the timeout fires.

Outcomes:
- Confirm exact JSON shape for `executions[]` and `inputAction`.
- Decide if one or two GETs are needed to materialize a `PendingInput`.
- Confirm proceed/abort URL paths (Jenkins may serve them as relative or
  absolute — affects whether `c.post()` is enough).

### Phase 1 — Architecture & adapter

- Add `BuildStatusPausedInput`, `PendingInput`, `BuildDetail.PendingInputs`.
- Implement `ApplyPendingInputs` + tests.
- Extend `jsonAction`, write `parsePendingInputs` + JSON fixture tests
  (using captured samples from Phase 0).
- Implement `ProceedInput` / `AbortInput` on `Client`.
- No view changes yet. All behind tests.

### Phase 2 — Slice 1: parameter-less inputs

- `InputConfirmDialog` component (armable buttons).
- `behavior_input.go` + `inputmixin.go` wired into `StageView`.
- `<Enter>` on paused stage opens dialog.
- Theme mapping for `BuildStatusPausedInput`.
- Build-list `[?]` indicator.
- Live-sync auto-close.

End-to-end on the OP's exact snippet.

### Phase 3 — Slice 2: parameterized inputs

- `NewParamFormWithTitle` constructor.
- Dialog switches to form mode when `len(Parameters) > 0`.
- Submit collects values, POSTs to `/submit`.

### Phase 4 — Polish

- Submitter pre-check (V2).
- Timeout countdown if deadline exposed.
- Parallel-input disambiguation: a single run with multiple simultaneous
  pending inputs (rare; one per parallel branch). The current design handles
  this naturally because inputs are listed per-stage, but the UX of having
  two `[!]` markers in one stage tree should be sanity-checked.
- Keybinding rationalization (decision #4 deferred this).

## Open questions for tomorrow

1. **JSON shape confirmation.** Capture the three samples in Phase 0.
2. **One-GET vs two-GET detection.** Depends on (1).
3. **`flowGraphTable` plugin source.** Brecht stated it's core. Worth a 5-min
   sanity check against `workflow-cps` / `workflow-job` source on a stock
   Jenkins to be sure. Not blocking this feature.
4. **Theme color for paused.** Defer to colorblind audit when rendering work
   starts.
5. **Two-keypress confirm widget.** Build inline for the input dialog now;
   consider promoting to `tui/component/` later if `behavior_cancel.go` wants
   the same affordance.

## Things explicitly NOT in scope

- Migrating stage fetching to `wfapi/describe`. The current scraping path
  works and is confirmed core. Don't bundle.
- Plugin-detection probes at connect time. Not needed for this feature.
- A new polling loop. Reuse the existing build-detail poller.
- Storing `Paused` as a separate bool on `Build` or `Stage`. Decision #1
  forbids parallel state.

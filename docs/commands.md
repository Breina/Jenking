# Commands reference

Jenking exposes the same set of verbs in two places:

- **Slash commands** (interactive, inside the TUI) — invoked by typing `:` then the verb.
- **CLI args** (non-interactive, from your shell) — `jenking <verb> [args...]`. Without args, `jenking` opens the TUI on the dashboard as before.

The grammar is identical in both places.

---

## Target syntax

Navigation verbs (`builds`, `stages`, `jobs`, `log`, `matrix`, and the
read-only headless verbs `describe`, `tests`, `changes`) accept an optional
**target**:

```
[<project>] [<branch>] [#<n>|#last] [:<stage>]
```

| Field      | Description                                                                                                                    |
|------------|--------------------------------------------------------------------------------------------------------------------------------|
| `<project>` | A path or path-suffix that uniquely identifies a project in the cache. `webidm` matches `Code Private/webidm`. Must be unique — ambiguous matches return all candidates. Folder names with spaces are fine. |
| `<branch>`  | A branch name verbatim, slashes allowed. `feature/foo` is one token.                                                           |
| `#<n>`      | Build number, prefixed with `#`. `#last` is also accepted. Bare integers (without `#`) are not, to avoid confusion with branches that look numeric. |
| `:<stage>`  | Stage name, prefixed with `:`. Everything from `:` to end-of-input is the stage; spaces are allowed (`:Build & Test`).         |

All fields are optional. Missing positionals fall back to the *current view's*
location (slash command) or error out (CLI). Markers (`#`, `:`) may appear in
any order after the positionals.

### Project resolution

Project suffix matching is **decoded** — projects whose internal Jenkins names
contain encoded slashes (e.g. `git%2Fcas%2Fwebidm`, displayed as
`git/cas/webidm`) are matched and surfaced in their natural decoded form.
You never need to type `%2F`.

The cache is populated as you navigate the TUI and persisted to disk under
`~/.cache/jenking/<server-hash>/`. On a cold first run, suffix matching has
nothing to match against — pass a full path or open the TUI once first.

### Tab completion

Press space after a navigation verb (or after any positional argument) to
see live suggestions drawn from the cache. Up/Down cycle, Tab/Right accepts.

| Position you're typing | Suggestions come from                                                                       |
|------------------------|---------------------------------------------------------------------------------------------|
| Project                | All cached project paths (decoded; unique short name where possible).                       |
| Branch                 | Branches of the resolved project (Jobs cache; ProjectBuilds as fallback).                   |
| After branch           | `#last` and recent build numbers, newest first — no need to type `#` first.                 |
| `#…`                   | `#last` plus build numbers for that branch (or the project if no branch).                   |

Stage names (`:<stage>`) are accepted as targets but **not** autocompleted —
once a `:` appears in the input, suggestions stop. Suggestions only surface
for subtrees the user has navigated through at least once; the cache is
populated lazily.

### Examples

```
:log webidm feature/foo #42 :Deploy
:log webidm feature/foo #last
:log webidm                          # last build of last branch
:log #42                             # build 42 of the current view's branch
:builds Code Private/webidm          # full path with spaces
:stages git/cas/webidm main          # decoded slash-in-name project
```

---

## Verbs

### Navigation (TUI deep-link or slash command)

These verbs open a view inside the TUI.

| Verb       | Aliases       | Args            | Behaviour                                                                                                                                               |
|------------|---------------|-----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------|
| `builds`   | `b`, `build`  | target (scope)  | Show builds list. With no args: scope of the current view. With target: builds of that folder/project/branch. Build/stage parts are reduced to scope.   |
| `stages`   | `s`, `stage`  | target          | Show pipeline stages. With `#<n>`: stages of that specific build. Otherwise: stages of the latest build at scope.                                       |
| `views`    | —             | none            | The Jenkins views of the current container (the root of the navigation). Inside a folder: that folder's own views.                                      |
| `view`     | `v`           | `[<name>]`      | Open a Jenkins view's job list (`jobs(Team Infra)`). With no args: the views list. The built-in `all` view is the unfiltered job list.                   |
| `jobs`     | `j`, `job`    | target          | Navigate to a job listing. With no args: sideways/up nav from current view (at the top, the last opened view's list). With target: drill INTO the project/folder. |
| `log`      | `l`, `logs`   | target          | Show console log. With `#<n>`: that specific build's log. Otherwise: latest build at scope.                                                             |
| `running`  | `r`           | none            | Toggle the running-builds filter on the current Builds view, or open All Builds with the running filter pre-enabled.                                    |
| `scans`    | —             | none            | Branch-indexing scans waiting in the queue, scoped to the current view (`S`). Inside a multibranch project this is its own scan.                        |
| `matrix`   | (hidden)      | target          | Matrix-mode log overlay. Only active when the Matrix theme is set and the current view is a running log.                                                |

In the Builds view, the **commits accordion** is on by default: the selected
build expands inline to show its SCM commits (message, author, time) as sub-rows,
following the cursor as you move. Press `c` to toggle it off/on. This replaces the
former `:changes` verb; the same data is still available headless via the
`changes` CLI verb and the `get_changes` MCP tool.

A job's build list is topped by a pinned `#last → #N` row that mirrors the newest
build. Drilling into it (`enter`/`s` for stages, `l` for log) opens the
scope-resolving view, which tracks whichever build is currently newest rather
than pinning `#N` — so it follows new builds as they start. The `(*)`-scoped
all-builds views render at most the 100 newest builds to stay responsive.

### Settings

| Verb         | Aliases                  | Args     | Behaviour                                                                                          |
|--------------|--------------------------|----------|----------------------------------------------------------------------------------------------------|
| `theme`      | `th`                     | `[id]`   | Open the theme picker (no args), or apply a theme by ID directly (e.g. `:theme matrix`).           |
| `colorblind` | `cb`                     | `[type]` | Open the colorblindness picker (no args), or apply a compensation type (e.g. `:cb deuteranopia`).  |
| `config`     | `preferences`, `prefs`   | none     | Open the preferences dialog (notifications, git usernames, refresh intervals, log level).          |
| `context`    | `ctx`                    | `[name]` | Open the contexts menu (no args), or switch to a named Jenkins context.                            |

### Misc

| Verb     | Aliases     | Args  | Behaviour                                              |
|----------|-------------|-------|--------------------------------------------------------|
| `help`   | —           | none  | Show the in-app command list overlay.                  |
| `update` | `upgrade`   | none  | Update Jenking to the latest release.                  |
| `quit`   | `q`         | none  | Exit.                                                  |

---

## CLI usage

The CLI has three shapes:

```
jenking                                # TUI, dashboard
jenking <verb> [args...]               # headless: print to stdout, no TUI
jenking ui <verb> [args...]            # TUI, pre-navigated (deep-linked) to <verb>
jenking mcp [--context <name>] [--read-only]   # MCP server over stdio (for AI agents)
```

`jenking mcp` runs the long-lived Model Context Protocol server; see the
[MCP server](../README.md#mcp-server) section of the README for the tool
catalog and client configuration.

> **Shell quoting**: `#` is a comment character in zsh/bash. Quote tokens
> containing `#` (`'#42'`) or escape (`\#42`) so the shell passes them
> through.

### Headless verbs

Headless verbs bypass the TUI entirely. Output goes to stdout, errors to
stderr, exit code is 0 on success and 1 on any failure. `#last` (or omitting
`#`) resolves the latest build by hitting the API; for multibranch projects
this requires you to also specify the branch.

| Verb        | Args                                   | Output                                              |
|-------------|----------------------------------------|-----------------------------------------------------|
| `views`     | `[<folder>]`                           | Views defined on the container, plus your personal views. |
| `jobs`      | `[<folder>]` `--view <name>`           | Folders and jobs at the path; `--view` lists a Jenkins view's jobs instead. |
| `builds`    | `<project> [<branch>]` `--mine`        | Recent build history; `--mine` keeps only builds you triggered or pushed. |
| `running`   | `--mine`                               | Currently running builds; `--mine` keeps only builds you triggered or pushed. |
| `queue`     | `--kind build\|scan\|all`               | The build queue. Branch-indexing scans are excluded unless asked for. |
| `scans`     | `[<folder>]`                           | Branch-indexing scans waiting in the queue.          |
| `scan-log`  | `<container> [--tail N]`               | Repository scan log of a multibranch project or folder. |
| `whoami`    | none                                   | The authenticated user.                             |
| `params`    | `<project> [<branch>]`                 | Build parameter definitions.                        |
| `metadata`  | `<project> [<branch>]`                 | Raw Jenkins metadata.                               |
| `artifacts` | `<project> <branch> [#N]`              | Artifact listing for a build.                       |
| `artifact`  | `<project> <branch> [#N] <file>`       | A single artifact's contents.                       |
| `logs`      | `<project> <branch> [#N]`              | Full console text, verbatim (always plain text).    |
| `describe`  | `<project> <branch> [#N]`              | The build's Jenkinsfile / replay script (plain text).|
| `tests`     | `<project> <branch> [#N]`              | JUnit test report.                                  |
| `changes`   | `<project> <branch> [#N]` `--find <commit>` | SCM commits in the build; with `--find`, which recent builds contain a commit (prefix match, `--max-builds` caps the scan). |
| `trigger`   | `<project> [<branch>]`                 | Trigger a build (see `-p` for parameters).          |
| `cancel`    | `<project> <branch> #N`                | Cancel a running build.                             |
| `dequeue`   | `<id>`                                 | Remove an item from the queue.                      |

### Output formats

Read verbs honor a global `--output`/`-o` flag:

```
-o text     # default: human-readable table/text
-o json     # machine-readable JSON
-o yaml     # YAML
```

`logs` and `describe` always emit plain text; their `--output` flag is ignored.

```
jenking logs webidm feature/foo '#42' | grep -i error
jenking describe webidm feature/foo '#last' > Jenkinsfile.replay
jenking tests webidm feature/foo '#42' -o json | jq '.failed'
jenking builds webidm main -o json
```

### Deep-link mode (`ui`)

`jenking ui <verb>` starts the TUI on the targeted view instead of the
dashboard, with full back-navigation via ESC to the natural parent. Verbs:
`logs`, `stages`, `builds`, `jobs`.

```
jenking ui logs webidm feature/foo '#42'
jenking ui builds 'Code Private/webidm'
jenking ui stages webidm main
```

---

## Errors

| Source         | Surfaces in TUI                | Surfaces in CLI                       |
|----------------|--------------------------------|---------------------------------------|
| Parser         | Status bar (red, dismissable)  | stderr, exit 1                        |
| Unknown project | `unknown project: "foo"`       | Same, on stderr                       |
| Ambiguous      | `ambiguous project "x" matches: a, b, c` (decoded paths) | Same |
| API error      | Status bar                     | stderr, exit 1                        |
| Missing build  | `no builds found for X (multibranch projects require a branch)` | Same |

# Commands reference

Jenking exposes the same set of verbs in two places:

- **Slash commands** (interactive, inside the TUI) — invoked by typing `:` then the verb.
- **CLI args** (non-interactive, from your shell) — `jenking <verb> [args...]`. Without args, `jenking` opens the TUI on the dashboard as before.

The grammar is identical in both places.

---

## Target syntax

Navigation verbs (`builds`, `stages`, `jobs`, `log`, `matrix`, and the
read-only headless verbs `describe`, `tests`) accept an optional **target**:

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
| `jobs`     | `j`, `job`    | target          | Navigate to a job listing. With no args: sideways/up nav from current view. With target: drill INTO the project/folder.                                 |
| `log`      | `l`, `logs`   | target          | Show console log. With `#<n>`: that specific build's log. Otherwise: latest build at scope.                                                             |
| `running`  | `r`           | none            | Toggle the running-builds filter on the current Builds view, or open All Builds with the running filter pre-enabled.                                    |
| `matrix`   | (hidden)      | target          | Matrix-mode log overlay. Only active when the Matrix theme is set and the current view is a running log.                                                |

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
```

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
| `jobs`      | `[<folder>]`                           | Folders and jobs at the path.                       |
| `builds`    | `<project> [<branch>]`                 | Recent build history.                               |
| `running`   | none                                   | Currently running builds.                           |
| `queue`     | none                                   | The build queue.                                    |
| `whoami`    | none                                   | The authenticated user.                             |
| `params`    | `<project> [<branch>]`                 | Build parameter definitions.                        |
| `metadata`  | `<project> [<branch>]`                 | Raw Jenkins metadata.                               |
| `artifacts` | `<project> <branch> [#N]`              | Artifact listing for a build.                       |
| `artifact`  | `<project> <branch> [#N] <file>`       | A single artifact's contents.                       |
| `logs`      | `<project> <branch> [#N]`              | Full console text, verbatim (always plain text).    |
| `describe`  | `<project> <branch> [#N]`              | The build's Jenkinsfile / replay script (plain text).|
| `tests`     | `<project> <branch> [#N]`              | JUnit test report.                                  |
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

# Changelog

All notable changes to Jenking are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-09-06

The first stable release. Highlights of the feature set at 1.0.0:

#### Added
- **MCP server** (`jenking mcp`): exposes the active Jenkins context as
  Model Context Protocol tools over stdio for Claude and other AI agents.
  Shares the TUI/CLI engine — build-scoped pipeline symbols for
  hallucination-free Jenkinsfile authoring (`get_pipeline_symbols` +
  `lint_pipeline` + `replay_build`), server-side commit search (`find_commit`),
  live running-state and queue history from a long-lived build registry,
  console logs handed off as files, and plugin-agnostic capability probing.
  20 read tools plus 11 mutating tools; `--read-only` omits the mutating set.
- **Build changes**: list the SCM commits recorded for a build via the
  `changes` CLI verb (`c`/`commits`), the `:changes` TUI panel, and the
  `get_changes`/`find_commit` MCP tools. Parses both freestyle and Pipeline
  changeset shapes with no plugin coupling.
- Interactive TUI for browsing jobs, folders, builds, and branches.
- Pipeline **stage view** with per-stage logs and live durations.
- Live, streaming console logs with search and error highlighting.
- JUnit test report summaries.
- Build **queue** and **running builds** views.
- Trigger parameterized builds from a form; cancel running builds.
- "My builds" filter to isolate and highlight your own runs.
- Raw metadata inspector (`:inspect`).
- Scriptable, headless CLI: every read verb (`jobs`, `builds`, `logs`,
  `describe`, `tests`, `params`, `metadata`, `artifacts`, `changes`, `running`,
  `queue`, `whoami`) with `text`/`json`/`yaml` output, plus `trigger`, `cancel`,
  and `dequeue` for mutations.
- `jenking ui <verb>` to launch the TUI pre-navigated to a view.
- Multiple Jenkins **contexts** with in-app switching (`:context`).
- 9 themes and colorblindness compensation filters.
- Optional desktop notifications on build completion.
- Optional Vim integration for editing pipeline scripts with Groovy awareness.
- In-app self-updater (`:update`) and an install script.

#### Added
- **Wait for a log line** (`wait_for_log_match` MCP tool): follows a log as it
  is written and blocks until an RE2 regex matches, returning the matched text,
  its line, line number and byte offset — instead of re-fetching `get_logs` in a
  sleep loop. Works on a build's console, a single pipeline stage (`stage`), or
  a container's repository scan log (`source: "scan"`). The log itself is still
  handed off as a file. `complete: true` with `matched: false` means the log
  ended and the pattern will never appear; `timed_out: true` means call again.
- **Repository scans as a first-class run.** A container's scan (branch indexing
  on a multibranch project, computation on a folder) is now something you can
  watch, start and stop, not just a queue row: `l` streams the scan log on a
  container row, `t` scans now, and `x` cancels — queued scans by queue id from
  the job list, running ones from the log view, where the live stream is the
  only evidence Jenkins gives that a scan is running. Headless equivalents:
  `jenking scan-log <container> [--tail N]`, `jenking rescan`, and the
  `get_scan_log` MCP tool (logs-as-files, like `get_logs`).
- **Scans view** (`S`, `:scans`, or `s` on a folder/multibranch row): the
  branch-indexing scans waiting in the queue, scoped to the current context,
  with the reason each is waiting. Also `jenking scans [<folder>]` and the
  `list_scans` MCP tool. Container rows in the job list carry a `⧗` glyph while
  their scan is queued, shown beside the type icon so it stays visible when the
  project's branches are building.

#### Changed
- **Branch-indexing scans are no longer counted as queued builds.** Jenkins
  pushes multibranch/folder scans through the same queue endpoint, but they
  never produce a build, so they inflated the header's queued count and skewed
  the dashboard's queue-wait histogram towards "blocked". The header now shows
  `Scans:` separately, and the dashboard ignores scans entirely.
  `jenking queue` and the `list_queue` MCP tool gain a `kind` field on each
  item and a `--kind build|scan|all` selector, **defaulting to `build`** —
  scripted output that previously included scan rows no longer does.

#### Design guarantees
- No coupling to optional Jenkins plugins; works against a plain install.
- No coupling to a specific SCM (GitHub/GitLab/Bitbucket all supported, none required).

## [0.1.0]

- Initial alpha release.

[Unreleased]: https://github.com/Breina/Jenking/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/Breina/Jenking/compare/v0.1.0...v1.0.0
[0.1.0]: https://github.com/Breina/Jenking/releases/tag/v0.1.0

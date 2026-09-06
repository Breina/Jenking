# Jenking — Architecture

Target architecture for the Jenking codebase. This document describes how the
code should be structured; the linter config (`.golangci.yml`) is the
executable form of the layering rules. If the two disagree, the linter wins
until this doc is updated to match.

## 1. Goals & constraints

- **Clean separation of concerns.** UI, domain, and integrations stay independent.
- **TUI is a reusable framework.** `internal/tui/*` must be extractable as a standalone library; it MUST NOT reference Jenking- or Jenkins-specific types.
- **DRY, high cohesion, low coupling.** Enforced via linters, not vibes.
- **Testable.** Domain has no I/O. Adapters are mockable through ports. Pure logic lives in pure packages.

## 2. Layers

```
┌─────────────────────────────────────────────────────────────┐
│ cmd/jenking                  composition root (main, wiring) │
└─────────────────────────────────────────────────────────────┘
                              │ wires
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ app/                         Jenking-specific orchestration  │
│  - app                       composition root model + keymap │
│  - view                      Bubble Tea screens              │
│  - action                    deeplink + headless entry points│
│  - monitor                   running-builds polling loop     │
│  - logscore                  pure log importance scoring     │
└─────────────────────────────────────────────────────────────┘
       │ uses                  │ uses                  │ uses
       ▼                       ▼                       ▼
┌─────────────────┐   ┌──────────────────┐   ┌──────────────────┐
│ tui/  FRAMEWORK │   │ domain/          │   │ adapters (flat)  │
│  - component    │   │  - jmodel        │◄──┤  - jenkins       │
│  - theme        │   │  - buildregistry │   │  - cache         │
│  - command      │   │  - pipelinesyntax│   │  - notify        │
│  - widget       │   │  (ports)         │   │  - updater       │
│  GENERIC ONLY   │   │  stdlib only     │   │                  │
└─────────────────┘   └──────────────────┘   └──────────────────┘

Adapters live flat under `internal/` (`internal/jenkins`, `internal/cache`,
`internal/notify`, `internal/updater`) rather than under an `internal/adapter/`
parent. The flat layout is what depguard enforces; the "adapter" label below
is a *role*, not a directory.
```

### Layer responsibilities

| Layer | Responsibility | Forbidden |
|---|---|---|
| `domain/` | Business types, port interfaces, pure logic (buildregistry, reconcile). | Any I/O. Importing adapter or tui. |
| `tui/` | Generic terminal-UI primitives: panels, popups, themes, command parser, log/list/table widgets, behavior host, confirm dialog. | Importing anything Jenking-specific (no `domain`, no `adapter`, no `app`). |
| `internal/jenkins` (adapter) | HTTP client, response parsing. Implements domain ports. | Importing `tui`, `app`, or other adapters. |
| `internal/cache` (adapter) | Disk + memory persistence. Implements domain ports. | Importing `jenkins` types — uses domain types only. |
| `internal/notify`, `internal/updater` (adapters) | OS notifications, self-update. | Importing `tui`, `app`, or other adapters. |
| `app/` | Jenking-specific glue: views, command actions, monitor loop, pure logic packages (e.g. `logscore`). | Importing `cmd`. |
| `cmd/jenking` | Composition root: parse flags, build config, wire dependencies, start Bubble Tea program. | None — top of the graph. |

## 3. Allowed-dependency matrix

This matrix is the **source of truth** for `depguard`. Row may import column.

"Adapters" below means `internal/{jenkins,cache,notify,updater}` collectively.

| From ↓ \ To →   | stdlib | `domain` | `tui` | adapters | `app/*` | `cmd/*` |
|---|---|---|---|---|---|---|
| `domain/*`      | ✅ | self | ❌ | ❌ | ❌ | ❌ |
| `tui/*`         | ✅ | ❌ | self | ❌ | ❌ | ❌ |
| adapters        | ✅ | ✅ | ❌ | self only | ❌ | ❌ |
| `app/*`         | ✅ | ✅ | ✅ | ✅ | self | ❌ |
| `cmd/*`         | ✅ | ✅ | ✅ | ✅ | ✅ | self |

Notes:
- `tui/*` may import third-party UI libs (`charmbracelet/*`, `mattn/go-runewidth`, etc.) — these are framework dependencies, not project-internal.
- `internal/jenkins` may NOT be imported by `internal/cache`. Cache speaks domain.
- `domain` may depend on small leaf utilities only if they are pure (no I/O); prefer stdlib.

## 4. Ports

Defined in `internal/domain/jmodel` (the primary port lives alongside the model types):

- `JenkinsClient` — list jobs, fetch build history, stream logs, fetch stages, fetch test reports, trigger build. Implemented by `internal/jenkins`.
- `BuildStore` (cache) — persist build registry / cached blobs keyed by domain identifiers. Implemented by `internal/cache`.
- `Notifier` — send a desktop notification on a domain event. Implemented by `internal/notify`.
- `Updater` — check & install self-updates. Implemented by `internal/updater`.

Ports use **domain types only** — no `http.Response`, no Jenkins JSON shapes. Adapter is responsible for translation.

## 5. Code-quality budgets

Enforced via `golangci-lint` (see `.golangci.yml`).

| Metric | Initial | Target | Rationale |
|---|---|---|---|
| Cyclomatic complexity (`gocyclo`) | 20 | 15 | Bubble Tea `Update` methods routinely exceed 15; split via message routers. |
| Cognitive complexity (`gocognit`) | 25 | 20 | Cognitive penalises nesting; more honest than cyclomatic. |
| Duplication (`dupl`) | threshold 100 tokens | 75 tokens | Tightens as the tree converges on shared widgets. |
| Function length (`funlen`) | 120 lines | 80 | Loose at first; tighten with refactors. |
| Architecture (`depguard`) | strict | strict | The matrix above is the only authority. |

Test files (`*_test.go`) are exempt from `dupl` and `funlen`. Generated files (none today) would be exempt from everything.

## 6. Enforcement

CI runs `make lint` which invokes `golangci-lint run`. The config lives at `.golangci.yml` and is the executable form of this document. If the rules in this doc diverge from the linter config, the linter config wins until this doc is updated to match.

A pre-commit hook is optional; CI is the gate.

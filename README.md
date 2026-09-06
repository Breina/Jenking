# Jenking

[![Latest Release](https://img.shields.io/github/v/release/Breina/Jenking)](https://github.com/Breina/Jenking/releases/latest)
[![Test](https://github.com/Breina/Jenking/actions/workflows/test.yml/badge.svg)](https://github.com/Breina/Jenking/actions/workflows/test.yml)
[![Lint](https://github.com/Breina/Jenking/actions/workflows/lint.yml/badge.svg)](https://github.com/Breina/Jenking/actions/workflows/lint.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Breina/Jenking)](https://goreportcard.com/report/github.com/Breina/Jenking)
[![License](https://img.shields.io/github/license/Breina/Jenking)](LICENSE)

A terminal UI, a scriptable CLI, **and a Model Context Protocol (MCP) server**
for Jenkins, inspired by [k9s](https://k9scli.io). Navigate jobs, watch builds,
tail logs, inspect pipeline stages, and trigger runs — from your terminal, a
script, or an AI agent, without touching the web UI.

![Jenking stage view](img/screenshot.png)

---

## Features

### Interactive TUI
- Start from your Jenkins **views** — each one opens its own job list, and the
  view you were last in is where the next session starts
- Browse jobs and nested folders
- Build history per project and per branch
- Live, streaming console logs with search and error highlighting
- Pipeline **stage view** with per-stage logs and durations
- JUnit test report summaries
- Trigger parameterized builds from a form; cancel running builds
- View and act on the build **queue** and currently **running** builds
- "My builds" filter — highlight and isolate your own runs

### Scriptable CLI
Every navigation verb doubles as a shell subcommand with `text`, `json`, or
`yaml` output — ideal for automation and quick one-liners:

```sh
jenking logs my-project main '#last' | grep -i error
jenking tests my-project main --output json | jq '.failed'
jenking trigger my-project main -p ENV=staging
```

### MCP server for AI agents
`jenking mcp` turns your Jenkins into a first-class tool for Claude and other
MCP clients — see **[MCP server](#mcp-server)** below.

### Comfort
- 9 built-in **themes** (default, royal, jenkins, matrix, dracula, solarized, nord, gruvbox, catppuccin)
- **Colorblindness** compensation (deuteranopia, protanopia, tritanopia, achromatopsia)
- Optional **desktop notifications** on build completion
- Optional **Vim integration** for editing/inspecting pipeline scripts with
  Groovy syntax awareness
- Multiple **Jenkins contexts** with in-app switching (`:context`)

---

## Requirements

- A Jenkins instance with API access
- A Jenkins **API token** (not your account password)

---

## Installation

### Install script (recommended)

Installs the latest release into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/Breina/Jenking/master/install.sh | sh
```

Supports Linux and macOS (amd64/arm64). Override the location with
`JENKING_INSTALL_DIR=/somewhere/on/PATH`. If `~/.local/bin` is not on your
`PATH`, the script prints the line to add to your shell profile.

Then run:

```sh
jenking
```

### Manual binary

Download from the [Releases page](https://github.com/Breina/Jenking/releases) and place the binary in a user-owned directory on your `PATH`:

```sh
curl -Lo jenking.tar.gz https://github.com/Breina/Jenking/releases/latest/download/jenking_linux_amd64.tar.gz
tar -xf jenking.tar.gz jenking && rm jenking.tar.gz
mkdir -p ~/.local/bin
chmod u+x jenking && mv jenking ~/.local/bin/
```

### Via `go install`

Requires Go 1.24 or newer.

```sh
go install github.com/Breina/Jenking/cmd/jenking@latest
```

The binary is placed in `$(go env GOPATH)/bin`. Make sure that directory is on your `PATH`.

---

## Quick start

1. Create an API token in Jenkins: **User menu → Configure → API Token → Add new Token**.
2. Write a minimal config to `~/.config/jenking/config.yaml`:

   ```yaml
   contexts:
     - name: my-jenkins
       url: https://jenkins.example.com
       username: your-jenkins-username
       token: $JENKINS_TOKEN        # reads the env var; or paste the token inline

   current_context: my-jenkins
   ```

3. Run `jenking`.

Jenking opens on your Jenkins views; pick one (or `all`) to get its job list,
and `Esc` takes you back up. The view you pick is remembered per context.

Inside the app, press `:` to open the command bar and type `help` for the full
command list. Press `/` to search the current view.

---

## Configuration

The config file lives at `~/.config/jenking/config.yaml`.

### Full reference

```yaml
contexts:
  - name: production
    url: https://jenkins.example.com
    username: your-jenkins-username
    token: $JENKINS_TOKEN          # env var reference, or an inline token
    insecure: false                # true to skip TLS verification
  - name: staging
    url: https://jenkins-staging.example.com
    username: your-jenkins-username
    token: $JENKINS_STAGING_TOKEN

current_context: production

preferences:
  theme: default                   # default | royal | jenkins | matrix | dracula
                                   #   | solarized | nord | gruvbox | catppuccin
  colorblindness_type: none        # none | deuteranopia | protanopia
                                   #   | tritanopia | achromatopsia
  refresh_interval: 5s             # foreground poll interval (active view)
  slow_refresh_interval: 2m        # background poll interval
  max_log_lines: 10000             # console log buffer cap
  log_level: off                   # off | debug  (debug logs to ~/.cache/jenking/)
  notifications: true              # desktop notification on build completion
  git_usernames:                   # highlight your own commits in build history
    - your-git-username
  vim_integration:
    enabled: false                 # edit pipeline scripts in Vim
    prefetch_symbols: false        # preload Groovy DSL symbols for completion
    validate_on_save: false        # run a Groovy syntax check on save
  text_artifact_extensions:        # extensions opened in the in-app viewer
    - .txt                         #   (others open in your browser)
    - .log
    - .json
  last_views:                      # written by the app: where each context resumes
    my-jenkins: Team Infra
```

Most preferences are also editable from inside the app via `:config`.
Switch the active Jenkins context with `:context`.

### Storing the token in an environment variable

A `token:` value beginning with `$` is resolved from the environment, so you can keep secrets out of the config file:

```yaml
token: $JENKINS_TOKEN
```

---

## CLI usage

Run `jenking` with no arguments for the TUI. Pass a verb to use it as a CLI:

```sh
jenking views                                  # list Jenkins views
jenking jobs                                   # list folders/jobs
jenking jobs --view "Team Infra"               # list a view's jobs
jenking builds my-project main                 # recent builds of a branch
jenking logs my-project main '#42'             # full console log to stdout
jenking tests my-project main --output json    # JUnit report as JSON
jenking trigger my-project main -p ENV=prod    # trigger a parameterized build
jenking changes my-project main '#42'          # SCM commits in build #42
jenking changes my-project main --find a1b2c3d # which recent builds contain a commit
jenking running                                # currently running builds
jenking queue                                  # the build queue
```

All read verbs accept `--output text|json|yaml` (`-o`). `logs` and `describe` always emit plain text. See **[docs/commands.md](docs/commands.md)** for the full grammar, target syntax, and every verb.

> **Shell quoting:** `#` starts a comment in bash/zsh. Quote build references that contain it (`'#42'`) or escape them (`\#42`).

---

## MCP server

Jenking is a full [Model Context Protocol](https://modelcontextprotocol.io)
server, not a thin API wrapper. It exposes the *same* engine that powers the
TUI and CLI, which is what makes it a better Jenkins agent than a
request-per-tool shim:

- **Hallucination-free Jenkinsfile authoring.** `get_pipeline_symbols` returns
  the exact steps, globals, and keywords available for a job — resolved against
  that job's shared libraries — so the agent writes real DSL instead of guessing.
  Pair it with `lint_pipeline` and `replay_build` for a tight edit-validate loop.
- **"Which build has this commit?" in one call.** `find_commit` scans many
  recent builds server-side and returns the matching build numbers;
  `get_changes` lists the commits in a specific build.
- **Truthful running-state.** A long-lived build registry tracks running builds
  and queue history across polls, so `list_running` and `get_queue_history`
  answer from live state rather than a single stale snapshot.
- **Block, don't busy-poll.** `wait_for_build` blocks server-side until a build
  finishes (or pauses for input) and returns its final result, sizing its own
  wait to the build's `estimatedDuration` — so an agent makes one call instead of
  a sleep-and-re-poll loop that burns a turn per check. `wait_for_new_build` is
  the companion for "I just pushed — has Jenkins picked it up?", blocking until a
  build appears in the queue or gets a number.
- **Wait for a line, not a state.** `wait_for_log_match` follows a log as it is
  written and returns the instant a regex matches — the phase you care about, an
  input prompt, the first sign of a known failure — for a build's console, one
  stage, or a multibranch scan log. One blocking call instead of a re-fetch loop.
- **Logs are files, not context.** `get_logs` writes the console to a file on
  disk and returns its path; the agent greps it with its own shell instead of
  loading megabytes into the model.
- **Plugin-agnostic.** Works against a plain Jenkins; plugin-gated tools probe
  lazily and report exactly which plugin is missing.

### Run it

```sh
jenking mcp --context my-jenkins        # stdio JSON-RPC; long-lived
jenking mcp --context my-jenkins --read-only   # expose only read tools
```

The server speaks JSON-RPC over stdout, so nothing else may write to stdout —
logs go to the Jenking log file. It reads the same `~/.config/jenking/config.yaml`
as the TUI; `--context` selects which Jenkins to expose.

### Connect a client

**Claude Code:**

```sh
claude mcp add jenking -- jenking mcp --context my-jenkins
```

**Claude Desktop** (`claude_desktop_config.json`) or any client using the
standard MCP server format:

```json
{
  "mcpServers": {
    "jenking": {
      "command": "jenking",
      "args": ["mcp", "--context", "my-jenkins"]
    }
  }
}
```

Add `"--read-only"` to the `args` array to expose only read tools.

### Tools

**Read-only** (always available): `list_jobs`, `list_views`, `list_builds`, `get_build`,
`get_changes`, `find_commit`, `list_running`, `list_queue`,
`get_queue_history`, `list_nodes`, `get_stages`, `get_test_report`,
`list_artifacts`, `get_artifact`, `get_params`, `get_metadata`, `whoami`, `describe_pipeline`,
`get_pipeline_symbols`, `lint_pipeline`, `list_inputs`, `get_logs`,
`get_scan_log`, `list_scans`, `wait_for_build`, `wait_for_new_build`,
`wait_for_log_match`.

**Mutating** (omitted under `--read-only`, carry destructive/idempotent hints):
`trigger_build` (with progress notifications while waiting), `replay_build`,
`cancel_build`, `dequeue`, `approve_input`, `reject_input`, `enable_job`,
`disable_job`, `rescan`, `set_node_offline`, `set_node_online`.

Build-scoped tools take a full slash-separated `job_path`
(e.g. `TeamA/service/main`) and an optional `build_number` that defaults to the
latest build.

---

## Building from source

```sh
git clone https://github.com/Breina/Jenking.git
cd Jenking
make build          # or: go build ./cmd/jenking
make test           # go test ./...
make lint           # golangci-lint run
```

---

## License

Jenking is licensed under the **GNU General Public License v3.0**. See [LICENSE](LICENSE).

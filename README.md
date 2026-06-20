# Jenking

[![Latest Release](https://img.shields.io/github/v/release/Breina/Jenking)](https://github.com/Breina/Jenking/releases/latest)
[![Test](https://github.com/Breina/Jenking/actions/workflows/test.yml/badge.svg)](https://github.com/Breina/Jenking/actions/workflows/test.yml)
[![Lint](https://github.com/Breina/Jenking/actions/workflows/lint.yml/badge.svg)](https://github.com/Breina/Jenking/actions/workflows/lint.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Breina/Jenking)](https://goreportcard.com/report/github.com/Breina/Jenking)
[![License](https://img.shields.io/github/license/Breina/Jenking)](LICENSE)

A terminal UI **and** scriptable CLI for Jenkins, inspired by [k9s](https://k9scli.io).
Navigate jobs, watch builds, tail logs, inspect pipeline stages, and trigger
runs — all without leaving your terminal or touching the web UI.

![Jenking stage view](img/screenshot.png)

---

## Features

### Interactive TUI
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
jenking jobs                                   # list folders/jobs
jenking builds my-project main                 # recent builds of a branch
jenking logs my-project main '#42'             # full console log to stdout
jenking tests my-project main --output json    # JUnit report as JSON
jenking trigger my-project main -p ENV=prod    # trigger a parameterized build
jenking running                                # currently running builds
jenking queue                                  # the build queue
```

All read verbs accept `--output text|json|yaml` (`-o`). `logs` and `describe` always emit plain text. See **[docs/commands.md](docs/commands.md)** for the full grammar, target syntax, and every verb.

> **Shell quoting:** `#` starts a comment in bash/zsh. Quote build references that contain it (`'#42'`) or escape them (`\#42`).

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

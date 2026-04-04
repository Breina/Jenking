# Contributing to Jenking

Thanks for your interest in contributing. This document explains how to get started and what to keep in mind.

---

## Design philosophy

**No plugin coupling.** Jenkins has a huge ecosystem of plugins — your setup is different from mine. jenking must work against a plain Jenkins install and must not break when optional plugins are absent. If you consume a plugin-specific API, make it degrade gracefully.

**No SCM coupling.** Do not assume GitHub, GitLab, Bitbucket, or any specific SCM. Features that display branch or commit information must work across SCM setups or not at all.

**Breadth over depth.** A feature that works on 80% of Jenkins instances is more valuable than a polished feature that only works on one setup.

---

## Vibe coding is fine

You don't need to plan every detail upfront. Exploratory coding is welcome — but the code you submit must actually work. That means:

- **Test your own code.** Run it against a real Jenkins instance before opening a PR. Our Jenkins probably differs from yours — think about edge cases.
- If you add a new view or interaction, exercise it manually with jobs that exist and jobs that don't, builds in progress, failed builds, empty lists, etc.
- If you change parsing or API logic, add or update a unit test.

---

## Running tests

```sh
go test ./...
```

All tests must pass before submitting. The CI will enforce this, but run it locally first.

---

## Setting up a development environment

```sh
git clone https://github.com/Breina/Jenking.git
cd jenking
go build -o jenking ./cmd/jenking
```

Set up a `~/.config/jenking/config.yaml` pointing at a real Jenkins instance. See the README for the config format.

For debug logging, set `log_level: debug` in your config. Logs go to `~/.cache/jenking/`.

---

## Submitting a PR

1. Fork the repo and create a branch off `master`.
2. Keep changes focused. One feature or fix per PR.
3. Run `go test ./...` and `go vet ./...` locally.
4. Describe what you changed and why in the PR body. If it fixes an issue, link it.
5. After your PR is merged and released: **follow up on the GitHub issue**. Check if the fix actually landed correctly in the release, answer any follow-up questions, and close the issue if it's resolved.

---

## Reporting bugs

Open a [GitHub Issue](../../issues). Include:

- Jenkins version
- jenking version or commit
- What you did, what you expected, what happened
- Relevant log output (run with `log_level: debug`)

---

## Code style

- Standard Go formatting (`gofmt`). No custom linter config for now.
- Keep packages cohesive. The `jenkins` package talks to Jenkins. The `tui` package renders things. Don't blur those boundaries.
- Prefer editing existing files over adding new ones for small changes.

# Polish & cleanup notes

Things noticed while writing the 1.0.0 documentation that aren't properly
polished. Ordered roughly by priority. This file is a working note — not meant
to ship in the release (consider gitignoring or deleting it before tagging).

---

## 🔴 Security / must handle before any push

- **`sponsor_private.pem` is sitting in the repo root.** It's currently
  untracked (verified: not in git history) but one careless `git add -A` would
  commit a private key. Move it out of the working tree, and add `*.pem` to
  `.gitignore` so it can never be staged. If this key was ever used for anything
  real, rotate it.
- **`generate-key.sh`** (sponsor key generator) is also in the root, untracked.
  Decide whether it belongs in the repo at all; if it stays, make sure it can't
  emit secrets into tracked files.

## 🟠 Repo hygiene

- **`.get/` is a stray git directory.** It's a full second git repo
  (`.get/HEAD`, objects, etc.) whose remote points at the *old* name
  `git@github.com:Breina/jenkins-tui.git`. Looks like an accidental
  `cp -r .git .get` or a mistyped command. Delete it: `rm -rf .get`.
- **The 23 MB `jenking` binary is in the working tree and not gitignored.**
  Add the built binary to `.gitignore` (e.g. `/jenking`) so it can't be
  committed by accident. `make clean` already removes it.
- **`.golangci.yml` and `.github/workflows/lint.yml` are untracked.** The lint
  CI and the README lint badge won't work until these are committed. Commit them.
- **`.idea/` is untracked but present.** Fine to leave local, but consider
  ignoring the whole directory rather than the current piecemeal `.idea/**/...`
  rules, to avoid noise.

## 🟡 Version / release

- **Version is still `0.1.0`** (`internal/version/version.go` defaults to
  `"dev"`; the screenshot shows `0.1.0`). For the 1.0.0 milestone, cut a
  `v1.0.0` tag so goreleaser injects the real version and the self-updater /
  header reflect it.
- **README no longer says "Alpha".** Make sure that's intentional and matches
  the actual stability you're comfortable claiming at tag time.
- **`CHANGELOG.md` `[Unreleased]` section** should be promoted to `[1.0.0]` with
  a date when you tag.

## 🟡 Known functional bugs (from `docs/openbugs.md`, still open)

- Log view: the breadcrumb shifts left when the error/warning counts
  (`⚠ 7  ✕ 6`) appear or toggle. Reserve the space so it doesn't jump.
- Log view: displayed line number changes depending on wrap state.
- Triggering a pipeline opens the *last* pipeline instead of the pending/new
  run's view.
- Vim `describe` view: whitespace rendering is inconsistent.

## 🟢 Documentation & naming

- **`docs/` mixes user-facing and internal-planning docs.** `commands.md` and
  `architecture.md` are reference material; `buildregistry-plan.md`,
  `plans/*.md`, and `openbugs.md` are dev scratch. Consider moving the latter to
  `docs/internal/` (or out of the published tree) so the docs folder reads
  cleanly to a newcomer.
- **`docs/commands.md` was outdated** — it documented a `--raw` flag and a
  "deep-link mode" that no longer match the real CLI (`-o/--output` + a separate
  `jenking ui <verb>`). *Fixed* in this pass; double-check against the code if
  the CLI changes again.
- **`CONTRIBUTING.md` claimed "No custom linter config for now"** despite
  `.golangci.yml` and a lint workflow existing. *Fixed* in this pass.
- **`internal/domain/pipelinesyntax/forclaude_test.go`** has an unprofessional
  name (reads like an AI scratch file). It's a real test; consider renaming to
  something descriptive.

## 🟢 Nice-to-haves for a "professional" GitHub presence

- The README screenshot is a single static PNG. A short animated demo (GIF/asciinema)
  of navigation + log tailing would sell the tool much harder on the front page.
- No issue/PR templates under `.github/`. Adding `ISSUE_TEMPLATE/` and a
  `pull_request_template.md` makes the repo feel maintained.
- No `SECURITY.md` or `CODE_OF_CONDUCT.md` — optional, but expected of polished
  OSS projects.
- Consider enabling the Go Report Card badge only after confirming it scores
  well (the README references it).

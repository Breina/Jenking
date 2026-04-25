# Opt-in Test Fixture: jenking-e2e

This directory contains a `Jenkinsfile.e2e` that powers the mutation tests in
`test/e2e/scenarios/trigger_test.go`. These tests trigger, monitor, and cancel
real builds and are therefore opt-in — they only run if this job is deployed.

## Deploy Once

1. Create a new **Pipeline** job on your target Jenkins named exactly `jenking-e2e`.
2. Configure it to use **Pipeline script from SCM** (or paste directly):
   - SCM: Git
   - Repository URL: your gitlab repo containing a `Jenkinsfile` with the contents of `Jenkinsfile.e2e`
   - Script Path: `Jenkinsfile` (or wherever you place the file)
3. Save and do one manual run to confirm it passes.

## What the Job Does

| Stage | Purpose |
|-------|---------|
| Prepare | Echo params, hostname |
| Work | `sleep 5` (normal speed) — allows auto-follow test to watch stages update |
| Test | Emits JUnit XML for test report rendering tests |
| Deliberate Failure | Only runs when `FAIL=true`; tests failure state rendering |
| Publish | Archives artifact; tests archival rendering |

## Parameters

| Name | Type | Default | Purpose |
|------|------|---------|---------|
| `FAIL` | boolean | false | Triggers deliberate stage failure |
| `MESSAGE` | string | `hello from jenking-e2e` | Tests string param input dialog |
| `SPEED` | choice | normal | fast=2s, normal=5s, slow=15s work stage |

## Running Trigger Tests

Once the job is deployed:

```bash
go test -tags=integration -v -run=TestTrigger ./test/e2e/scenarios/...
```

Without the job, all trigger tests skip automatically with a clear message.

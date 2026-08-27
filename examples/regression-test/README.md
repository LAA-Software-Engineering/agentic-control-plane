# Regression fixture (`terfyn test` CI gate)

This example shows **CI gating of a policy change**: a fixture case expects the unauthorized publish to **fail**. On the checked-in (safe) policy, `terfyn test` is **green**. Deleting the `requiredFor` line lets the mock write succeed, so `expectError: true` **fails** and `terfyn test` is **red**.

## What it demonstrates

- **`terfyn test`** (issue #73) — discovers `tests/*.yaml`, runs the same pipeline as `run` on a **fresh temp SQLite** per case, **does not** pass `--approve`.
- **Inner agent-loop tools do not HITL.** A granted (advertised) `tool.publish.default` that `CheckToolCall` denies is **exit 5** (`approval_required`), not interrupt/exit **0**. This example never mixes in workflow `uses:` HITL.
- **Empty-Script mock** — `publish` is first (and only) advertised tool and is **not** restart-like, so the mock `tool_use`s it then returns `{"summary":"mock"}`.
- **Trusted mutating mock** — `trusted: true` + `sideEffects: true` so **safety fallback would allow** the write. The **only** gate is policy `approvals.requiredFor: [tool.publish.default]`. Dropping that line is the unsafe diff. (An untrusted mutating tool would still deny after you removed `requiredFor`.)

## Project layout

| Path | Role |
|------|------|
| `project.yaml` | Imports resources; `mock` model (no API keys). |
| `workflows/gated-write.yaml` | One agent step that enters the tool loop. |
| `agents/writer.yaml` | Declares `publish` first. |
| `tools/publish.yaml` | Trusted mutating `mock` tool. |
| `policies/gated-publish.yaml` | `requiredFor: tool.publish.default`; ceiling **$5**. |
| `tests/gated-write.yaml` | `expectError: true` for the unauthorized write. |
| `schemas/*.json` | Workflow input and agent output. |
| `fixtures/sample-article.json` | Tiny `{article}` payload (optional `run`). |

## Prerequisites

Build `terfyn` from the repo root (`make build`) or use a release binary on your `PATH`.

## Green on the safe config

From the **repository root**:

```bash
terfyn validate --project examples/regression-test
terfyn test --project examples/regression-test
```

`test` should print **1 passed, 0 failed** (exit **0**). The case is `unauthorized-publish-denied`: the mock tries `publish`, policy denies, `expectError: true` holds.

Optional live `run` (same denial, **exit 5**, not HITL):

```bash
terfyn plan --project examples/regression-test --state /tmp/regression-test.db
terfyn apply --project examples/regression-test --state /tmp/regression-test.db --auto-approve
terfyn run workflow/gated-write \
  --project examples/regression-test \
  --state /tmp/regression-test.db \
  --input-file examples/regression-test/fixtures/sample-article.json
```

## The one-line edit that turns CI red

In `policies/gated-publish.yaml`, delete this line:

```yaml
      - tool.publish.default
```

Then:

```bash
terfyn test --project examples/regression-test
```

The mock write **succeeds**, the fixture still expects an error, and `terfyn test` exits **non-zero** (`expected workflow to fail`). That is the CI gate.

Do **not** commit that edit. Restore `requiredFor` before pushing.

## GitHub Actions sample

[`.github/workflows/terfyn-test.yml`](../../.github/workflows/terfyn-test.yml) builds `terfyn` and runs `terfyn test --project examples/regression-test` (no secrets). A **new** `on: pull_request` workflow may not run on the introducing PR until it is on `main`; this repo’s merge gate is the Go test in `test/integration/cli_flow_test.go` (`regression_test_unsafe_policy_fails_fixture`).

## Compared to HITL / incident-triage

| | This demo | `examples/hitl-resume` | `examples/incident-triage` |
|--|-----------|------------------------|----------------------------|
| Path | Inner agent-loop `publish` | Workflow `uses:` + `requiredFor` | Inner agent-loop `restart` |
| Unapproved | **exit 5**, fixture `expectError` | **exit 0** interrupted | **exit 5** |
| How to allow | Drop `requiredFor` (unsafe) or `--approve` | `--resume --decision approve` | `--approve tool.restart.restart` |

`terfyn test` never supplies `--approve`, so the safe policy is what keeps the fixture green.

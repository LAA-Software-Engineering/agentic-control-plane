# Incident triage (gated remediation)

This flagship example shows **why a declarative control plane beats ad-hoc glue code** when an agent can take a **destructive** action. The responder may read a pager alert, query logs, and open an issue, but **cannot restart the service without approval**.

## What it demonstrates

- **Agent-owned tools (Epic A)** — `agents/responder.yaml` lists `pager`, `logs`, `tracker`, and a pinned restart uses string. The model may only call those advertised operations.
- **Fail-closed writes** — `policies/gated-remediation.yaml` requires `--approve` for `tool.restart.restart`. Inner agent-loop tools **do not HITL**; `CheckToolCall` denies with **exit 5** (`approval_required`) unless that exact uses string is pre-approved.
- **Offline mock** — no API keys. The mock model requests a restart on the first Generate when a restart-like tool is advertised, then returns structured status JSON.
- **Traceable behavior** — `agentctl logs` shows `system_error` / `approval_required` on the denied path, or `tool_selection` then `tool_execution` when approved. `agentctl audit verify` checks the hash chain.

## Why this matters

In a typical script, “page the on-call, maybe bounce the box” is an `if` that is easy to skip in review. Here, **who may restart is YAML**, the runtime **enforces** it with **exit codes**, and the SQLite trace is **tamper-evident**.

## Project layout

| Path | Role |
|------|------|
| `project.yaml` | Imports resources; defaults to `mock` model (no API keys). |
| `workflows/incident-triage.yaml` | One agent step: triage the fixture alert. |
| `agents/responder.yaml` | Instructions, output schema, declared tools. |
| `tools/pager.yaml` / `logs.yaml` / `tracker.yaml` | Read-only `mock` tools (`sideEffects: false`). |
| `tools/restart.yaml` | Untrusted mutating `mock` tool (`sideEffects: true`). |
| `policies/gated-remediation.yaml` | Requires `--approve` for `tool.restart.restart`. |
| `schemas/*.json` | JSON Schema for workflow input and agent output. |
| `fixtures/sample-alert.json` | Sample pager alert (no network). |

## Prerequisites

Build `agentctl` from the repo root (`make build`) or use a release binary on your `PATH`.

## How to run

From the **repository root** (paths below assume that):

```bash
agentctl validate --project examples/incident-triage
agentctl plan --project examples/incident-triage --state /tmp/incident-triage-state.db
agentctl apply --project examples/incident-triage --state /tmp/incident-triage-state.db --auto-approve
```

### Default run (restart blocked)

```bash
agentctl run workflow/incident-triage \
  --project examples/incident-triage \
  --state /tmp/incident-triage-state.db \
  --input-file examples/incident-triage/fixtures/sample-alert.json
```

- Exit code **5** = policy denial (by design). The restart tool is **never invoked**.
- Inspect the trace (use the printed **Run ID**):

```bash
agentctl logs --project examples/incident-triage --state /tmp/incident-triage-state.db --run <run-id>
```

You should see the triage step, then **`system_error`** with reason **`approval_required`** (and **no** `tool_execution` for restart).

Verify the trace chain was not tampered with:

```bash
agentctl audit verify --project examples/incident-triage --state /tmp/incident-triage-state.db --run <run-id>
```

### Allow the restart (full success)

```bash
agentctl run workflow/incident-triage \
  --project examples/incident-triage \
  --state /tmp/incident-triage-state.db \
  --input-file examples/incident-triage/fixtures/sample-alert.json \
  --approve tool.restart.restart
```

This records a simulated restart result (still **no** real infrastructure). Logs should include `tool_selection` then `tool_execution` for `tool.restart.restart`. Run `audit verify` again on the new Run ID.

## Expected highlights

1. **Unapproved run** — mock model requests `restart`; policy blocks `tool.restart.restart`; CLI prints a **policy** line and exits **5**.
2. **Approved run** — the same uses string is in `--approve`; mock restart returns canned JSON; the agent emits a schema-valid status object; run **succeeds**.
3. **Audit** — both traces verify.

## Compared to one-off code

| Concern | This demo | Typical script |
|--------|-----------|----------------|
| Who may restart | Policy `requiredFor` + `--approve` | Easy to forget a guard |
| Agent tool surface | Declared `spec.tools` | Implicit SDK bindings |
| Denied vs interrupted | Exit **5**, fail closed | Often catch-and-continue |
| Audit trail | `logs` + `audit verify` | Printf / none |

This path is **not** the HITL interrupt used by `examples/pr-review-demo` (workflow `uses:` write → status `interrupted`, exit 0). Restart here is an **inner agent tool**, so denial is immediate.

For broader patterns, see [`docs/EXAMPLES.md`](../../docs/EXAMPLES.md), [`docs/AUDIT_CHAIN.md`](../../docs/AUDIT_CHAIN.md), and [`docs/DESIGN_DOC.md`](../../docs/DESIGN_DOC.md).

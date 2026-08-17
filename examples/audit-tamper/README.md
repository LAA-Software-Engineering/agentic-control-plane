# Audit tamper (hash-chain demo)

This example is the distinctive **tamper-evident trace** demo. A successful mock run writes a hash-linked `trace_events` chain. `agentctl audit verify --run <id>` **passes**. After a planted SQLite edit to **`data_json` only** (hash left stale), the same command **fails with exit 1**.

## What it demonstrates

- **Hash chain** — each event stores `prev_hash` (previous event, or a run-scoped genesis) and `hash` = `SHA-256(canonical_event_json ‖ prev_hash)`. Canonical JSON includes `data_json` (see [`docs/AUDIT_CHAIN.md`](../../docs/AUDIT_CHAIN.md)).
- **Verify passes pre-edit** — a clean run is internally consistent.
- **Verify fails post-edit** — mutating `trace_events.data_json` without recomputing `hash` breaks the chain (`BROKEN` / exit **1**). This is **not** cryptographic non-repudiation; it detects silent row edits.
- **Offline, no HITL** — mock model, no tools, no restart hook, no `--approve`. Policy ceiling is **$5** so the **$0.02** mock Generate does not trip `maxTotalCostUsd`.

## Project layout

| Path | Role |
|------|------|
| `project.yaml` | Imports resources; `mock` model (no API keys). |
| `workflows/note.yaml` | One agent step; the run is expected to **succeed**. |
| `agents/scribe.yaml` | No `spec.tools` (single-shot mock JSON). |
| `policies/cheap-ceiling.yaml` | `maxTotalCostUsd: 5`. |
| `schemas/*.json` | Workflow input and agent output. |
| `fixtures/sample-input.json` | Tiny `{topic}` payload. |
| `scripts/tamper-trace.sh` | Updates **one** `data_json` for `--run` / `--state`. Needs **python3** (stdlib `sqlite3`) or the **sqlite3** CLI. |

Do not commit `.agentic/` state from a local walkthrough.

## Prerequisites

Build `agentctl` from the repo root (`make build`) or use a release binary on your `PATH`. The helper script needs **python3** or **sqlite3**.

## How to run

From the **repository root**:

```bash
agentctl validate --project examples/audit-tamper
agentctl plan --project examples/audit-tamper --state /tmp/audit-tamper.db
agentctl apply --project examples/audit-tamper --state /tmp/audit-tamper.db --auto-approve
```

```bash
agentctl run workflow/note \
  --project examples/audit-tamper \
  --state /tmp/audit-tamper.db \
  --input-file examples/audit-tamper/fixtures/sample-input.json
```

- Exit code **0**, status **succeeded**.
- Copy the printed **Run ID**, then verify the chain:

```bash
agentctl audit verify --project examples/audit-tamper --state /tmp/audit-tamper.db --run <run-id>
```

You should see `OK` (exit **0**).

Plant an edit (does **not** update `hash`):

```bash
./examples/audit-tamper/scripts/tamper-trace.sh \
  --state /tmp/audit-tamper.db \
  --run <run-id>
```

Verify again:

```bash
agentctl audit verify --project examples/audit-tamper --state /tmp/audit-tamper.db --run <run-id>
echo "exit=$?"
```

- Exit code **1** = chain break (`BROKEN at seq … (hash)`).
- The payload no longer matches the stored hash because `data_json` is part of the canonical event.

## Why `hash` is left stale

An attacker who can `UPDATE` a row might also recompute hashes. This demo shows the **honest** failure mode the CLI is built for: a silent payload edit that forgets the chain. Recomputing the whole chain is a different (stronger) attack; there is no external timestamp authority or signing key yet.

For the algorithm, columns, and exit codes, see [`docs/AUDIT_CHAIN.md`](../../docs/AUDIT_CHAIN.md).

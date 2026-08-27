# Policy denial mid-run (budget ceiling)

This minimal example shows a **real agent tool loop** hitting `execution.maxTotalCostUsd` **after the second Generate**, not on the first turn. The run aborts with **exit 5** and a **`limit_hit`** event (`kind: max_cost`).

## What it demonstrates

- **Mid-loop budget** — `CheckRun` runs after each Generate (issue #163). Turn 1 costs **$0.02** (under the **$0.03** ceiling). Turn 2 adds another **$0.02** → accumulated **$0.04** → deny.
- **Deterministic mock cost** — the mock provider attaches `CostUSD: 0.02` on every Generate (issue #164 / #168). No live API.
- **Tool loop without restart** — the agent lists a read-only `helper` mock tool (not restart-like), so the empty-Script mock issues `tool_use` then JSON Content. This does **not** collide with the incident-triage restart hook.
- **Not HITL** — status is **failed**, exit **5**, reason **`max_cost`**. There is no interrupt / `--approve` path.

## Project layout

| Path | Role |
|------|------|
| `project.yaml` | Imports resources; `mock` model (no API keys). |
| `workflows/burn.yaml` | One agent step that enters the tool loop. |
| `agents/burner.yaml` | Declares `helper`; output schema `{summary}`. |
| `tools/helper.yaml` | Read-only `mock` tool (`sideEffects: false`). |
| `policies/tight-budget.yaml` | `maxTotalCostUsd: 0.03` (between one and two mock turns). |
| `schemas/*.json` | Workflow input and agent output. |
| `fixtures/sample-input.json` | Tiny `{topic}` payload. |

## Prerequisites

Build `terfyn` from the repo root (`make build`) or use a release binary on your `PATH`.

## How to run

From the **repository root**:

```bash
terfyn validate --project examples/policy-denial-midrun
terfyn plan --project examples/policy-denial-midrun --state /tmp/policy-denial-midrun.db
terfyn apply --project examples/policy-denial-midrun --state /tmp/policy-denial-midrun.db --auto-approve
```

```bash
terfyn run workflow/burn \
  --project examples/policy-denial-midrun \
  --state /tmp/policy-denial-midrun.db \
  --input-file examples/policy-denial-midrun/fixtures/sample-input.json
```

- Exit code **5** = policy denial (`max_cost`).
- Inspect the trace (printed **Run ID**):

```bash
terfyn logs --project examples/policy-denial-midrun --state /tmp/policy-denial-midrun.db --run <run-id>
```

You should see **`limit_hit`** with **`kind: max_cost`**, plus **`system_error`**. There is no `hitl_request_created`.

```bash
terfyn audit verify --project examples/policy-denial-midrun --state /tmp/policy-denial-midrun.db --run <run-id>
```

## Expected highlights

1. First Generate requests `helper` (`tool_use`) at $0.02 — under ceiling.
2. Mock helper returns canned JSON (no extra model cost).
3. Second Generate would return a summary, but accumulated $0.04 exceeds $0.03 → **`limit_hit`** / exit **5**.

## Compared to incident-triage

| | This demo | `examples/incident-triage` |
|--|-----------|---------------------------|
| Deny reason | `max_cost` | `approval_required` |
| When | After **second** Generate | Before restart executes |
| Approve? | No — raise the ceiling or lower mock cost | `--approve tool.restart.restart` |

For the hash chain, see [`docs/AUDIT_CHAIN.md`](../../docs/AUDIT_CHAIN.md).

# Multi-agent handoff (triager → fixer)

This example is the distinctive **two-agent** demo. A **triager** looks up context, then a **fixer** applies a mock patch. The first agent's structured JSON is interpolated into the second step. Each agent has its **own tools and policy**. The run **succeeds** on the mock provider (no HITL, no `--approve`).

## What it demonstrates

- **Handoff** — `main.agent`'s `handoff` workflow calls the `triager` agent then the `fixer` agent, passing the triager's `summary` and `findings` as named arguments (`fixer(diagnosis: triage.summary, findings: triage.findings)`).
- **Per-agent tools (Epic A)** — triager advertises read-only `lookup`; fixer advertises trusted mock `patch`. Neither name contains `restart`, so the empty-Script mock `tool_use`s that first tool, then returns Registry JSON (`summary` / `findings`).
- **Distinct policies** — `triage-readonly` gates `tool.patch.default`; `apply-fix` does **not** (trusted write, not `requiredFor`). Both ceilings are **$5**, so four mock Generates at **$0.02** each stay under budget.
- **Trace** — `terfyn logs` shows both step ids (`triager`, `fixer`) and at least two `llm_completion` events (one per Generate; each agent loops twice).

## Project layout

| Path | Role |
|------|------|
| `main.agent` | Everything: the `triage-readonly` policy (`requiredFor: tool.patch.default`, ceiling **$5**) and `apply-fix` policy (no `requiredFor`, ceiling **$5**); the `lookup` (read-only mock, `sideEffects: false`) and `patch` (trusted mock write, `sideEffects: true`, not approval-gated) tools; the `triager` (`lookup` + `triage-readonly`) and `fixer` (`patch` + `apply-fix`) agents; and the `handoff` workflow — authored in [`.agent`](../../docs/LANGUAGE.md). No `project.yaml`; the built-in `mock` model needs no API keys. |
| `schemas/*.json` | Ticket input and pr-review-shaped agent output. |
| `fixtures/sample-ticket.json` | Offline ticket payload. |

Do not commit `.agentic/` state from a local walkthrough.

## Prerequisites

Build `terfyn` from the repo root (`make build`) or use a release binary on your `PATH`.

## How to run

From the **repository root**:

```bash
terfyn validate --project examples/multi-agent
terfyn plan --project examples/multi-agent --state /tmp/multi-agent.db
terfyn apply --project examples/multi-agent --state /tmp/multi-agent.db --auto-approve
```

```bash
terfyn run workflow/handoff \
  --project examples/multi-agent \
  --state /tmp/multi-agent.db \
  --input-file examples/multi-agent/fixtures/sample-ticket.json
```

- Exit code **0**, status **succeeded**.
- Inspect the trace (printed **Run ID**):

```bash
terfyn logs --project examples/multi-agent --state /tmp/multi-agent.db --run <run-id>
```

You should see **both** step ids, **`llm_completion`** for each Generate, and `tool_selection` / `tool_execution` for `lookup` then `patch`. There is no `hitl_request_created`.

```bash
terfyn audit verify --project examples/multi-agent --state /tmp/multi-agent.db --run <run-id>
```

## Expected highlights

1. **Triager** Generate 1 requests `lookup` (`tool_use`); Generate 2 returns structured JSON.
2. **Fixer** receives that `summary` / `findings`, Generate 1 requests `patch`, Generate 2 returns JSON.
3. Workflow output includes both agents' objects.

## Compared to incident-triage

| | This demo | `examples/incident-triage` |
|--|-----------|---------------------------|
| Agents | **Two** sequential | One |
| Deny? | No — trusted `patch` is not `requiredFor` | Unapproved `restart` → exit **5** |
| Handoff | `${steps.triager.output.*}` | N/A |

For the hash chain, see [`docs/AUDIT_CHAIN.md`](../../docs/AUDIT_CHAIN.md).

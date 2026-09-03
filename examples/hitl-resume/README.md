# HITL resume (pause, approve, checkpoint)

This example is the distinctive **human-in-the-loop resume** demo. A mock agent drafts a summary, then a **workflow `uses:`** publish step is gated by policy. The run **interrupts** (exit **0**, `Status: interrupted`), persists a **checkpoint**, and continues on the **same** run after `terfyn run --resume <id> --decision approve`.

`examples/pr-review-demo` also **interrupts** unapproved workflow `uses:` (exit **0**, `hitl_request_created`); you continue with a **new** run plus `--approve`. `examples/incident-triage` is the fail-closed inner agent-loop path (exit **5**).

## What it demonstrates

- **Workflow `uses:` HITL** — `approvals.requiredFor` (or tool safety) is what **gates** `tool.publish.default`. `hitl.interruptOn.publish` only **configures** the review (allowed decisions, prompt text). `maybeInterruptForHitl` then pauses instead of failing the step.
- **Checkpoint** — the interrupted run stores pending HITL state. Resume skips the completed **draft** agent step (no second Generate).
- **Audit chain** — `hitl_request_created`, then on resume `hitl_decision_submitted` / `hitl_resolution_applied` and `tool_execution`. `audit verify --run` stays **OK**.
- **No agent tools** — `drafter` has no `spec.tools`, so the mock stays single-shot (no D1/D2 empty-Script loops). Ceiling **$5** covers the **$0.02** Generate.
- **`--auto-approve` skips the pause** — `terfyn run ... --auto-approve` (or `TERFYN_AUTO_APPROVE=1`) does not interrupt; this walkthrough omits it so the checkpoint path is visible.

## Project layout

| Path | Role |
|------|------|
| `main.agent` | Everything, authored in [`.agent`](../../docs/LANGUAGE.md): the `gated-publish` policy (`requiredFor` + `hitl.interruptOn`), the `publish` tool (trusted mock write, `sideEffects: true`), the `drafter` agent, and the `publish` workflow (`draft` agent then `publish` `uses:` step). No `project.yaml`; the built-in `mock` model needs no API keys. |
| `schemas/*.json` | Workflow input and agent output. |
| `fixtures/sample-input.json` | Tiny `{topic}` payload. |

Do not commit `.agentic/` state from a local walkthrough.

## Prerequisites

Build `terfyn` from the repo root (`make build`) or use a release binary on your `PATH`.

## How to run

From the **repository root**:

```bash
terfyn validate --project examples/hitl-resume
terfyn plan --project examples/hitl-resume --state /tmp/hitl-resume.db
terfyn apply --project examples/hitl-resume --state /tmp/hitl-resume.db --auto-approve
```

### Pause (no `--approve`)

```bash
terfyn run workflow/publish \
  --project examples/hitl-resume \
  --state /tmp/hitl-resume.db \
  --input-file examples/hitl-resume/fixtures/sample-input.json
```

- Exit code **0**, status **interrupted**.
- Copy the printed **Run ID**.
- Passing **`--auto-approve`** on this first run would skip the pause and complete in one shot.

```bash
terfyn logs --project examples/hitl-resume --state /tmp/hitl-resume.db --run <run-id>
```

You should see **`llm_completion`** on `draft`, then **`hitl_request_created`** on `publish` (no `tool_execution` for publish yet).

### Resume

```bash
terfyn run --resume <run-id> \
  --project examples/hitl-resume \
  --state /tmp/hitl-resume.db \
  --decision approve
```

- Exit code **0**, status **succeeded**.
- Logs gain **`hitl_decision_submitted`**, **`hitl_resolution_applied`**, and **`tool_execution`**. Still a **single** `llm_completion`.

```bash
terfyn audit verify --project examples/hitl-resume --state /tmp/hitl-resume.db --run <run-id>
```

## Compared to nearby demos

| | This demo | `examples/pr-review-demo` | `examples/incident-triage` |
|--|-----------|---------------------------|----------------------------|
| Gate | Workflow `uses:` (`requiredFor` + HITL review) | Workflow `uses:` (`requiredFor`; also interrupts) | Inner agent tool |
| Unapproved | **interrupted**, exit **0** (`hitl_request_created`) | **interrupted**, exit **0** (`hitl_request_created`) | **failed**, exit **5** |
| Continue | **`--resume --decision approve`** on the **same** run (checkpoint skips `draft`) | **New** run with `--approve` | New run with `--approve` |

For the hash chain, see [`docs/AUDIT_CHAIN.md`](../../docs/AUDIT_CHAIN.md).

# PR review demo (policy-gated GitHub comment)

This example shows **why a declarative control plane beats ad-hoc glue code** for agent workflows that touch the outside world.

## What it demonstrates

- **Workflow as code** — `main.agent` reads like a runbook: fetch PR context → review → post a comment. Authored in [`.agent`](../../docs/LANGUAGE.md), discovered (not imported).
- **Structured agent output** — the reviewer must return JSON validated by `schemas/ReviewOutput.json` (summary + findings), not an unparseable blob.
- **`${...}` argument templates** — the comment `body` is templated directly from the review's structured output (`${review_diff.summary}`, `${review_diff.findings}`), resolved through the workflow's binding environment.
- **First-class policy** — the `guarded-writes` policy (in `main.agent`) lists which tool `uses` strings require explicit approval.
- **Human-in-the-loop writes** — the `post_comment` step calls a **simulated** native GitHub tool (the `github` tool in `main.agent`). As a workflow `uses:` step gated by policy, an unapproved run **pauses for approval** (HITL interrupt) before any side effect — it does not silently proceed.
- **Traceable behavior** — `terfyn logs` shows normal step progress plus the **approval interrupt** on the gated step.

## Why this matters

In a typical script, “call the model, then maybe post to GitHub” is buried in code paths that are hard to review, diff, and audit. Here, **permissions and sequencing are reviewable code** you can put in code review, and the runtime **enforces** them with **approval gates** and **SQLite traces**.

## Project layout

| Path | Role |
|------|------|
| `main.agent` | Everything: the `guarded-writes` policy, the `native` `github` tool, the `reviewer` agent, and the three-statement `pr-review` workflow (`fetch_pr`, `review_diff`, `post_comment`) — authored in [`.agent`](../../docs/LANGUAGE.md). No `project.yaml`; the built-in `mock` model needs no API keys. |
| `schemas/PRReviewInput.json`, `schemas/ReviewOutput.json` | JSON Schema for workflow input and agent output (type names match the `.agent` `input:` / `output` references). |
| `fixtures/sample-pr.json` | Sample input (no GitHub network or tokens). |

## Prerequisites

Build `terfyn` from the repo root (`make build`) or use a release binary on your `PATH`.

## How to run

From the **repository root** (paths below assume that):

```bash
terfyn validate --project examples/pr-review-demo
terfyn plan --project examples/pr-review-demo --state /tmp/pr-review-state.db
terfyn apply --project examples/pr-review-demo --state /tmp/pr-review-state.db --auto-approve
```

### Default run (comment gated → approval interrupt)

```bash
terfyn run workflow/pr-review \
  --project examples/pr-review-demo \
  --state /tmp/pr-review-state.db \
  --input-file examples/pr-review-demo/fixtures/sample-pr.json
```

- Status **`interrupted`** (exit **0**) = the workflow paused at the gated `post_comment` step for a human decision (by design). No comment is posted yet.
- Resume with the printed **Run ID** and a decision:

```bash
terfyn run --resume <run-id> --decision approve \
  --project examples/pr-review-demo --state /tmp/pr-review-state.db
```

`--decision approve` continues and records a simulated comment; `--decision reject` ends the run without the write.

- Inspect the trace (use the printed **Run ID**):

```bash
terfyn logs --project examples/pr-review-demo --state /tmp/pr-review-state.db --run <run-id>
```

You should see steps through `review_diff`, then the **approval interrupt** on `post_comment`.

Verify the trace chain was not tampered with:

```bash
terfyn audit verify --project examples/pr-review-demo --state /tmp/pr-review-state.db --run <run-id>
```

### Optional: pre-approve the write (full success, no pause)

```bash
terfyn run workflow/pr-review \
  --project examples/pr-review-demo \
  --state /tmp/pr-review-state.db \
  --input-file examples/pr-review-demo/fixtures/sample-pr.json \
  --approve tool.github.pull_request.post_comment
```

This records a simulated comment result (still **no** real GitHub traffic) and returns the review as the workflow output.

## Expected highlights

1. **`fetch_pr`** completes — native tool normalizes the `pr` object from input JSON.
2. **`review_diff`** completes — mock model returns fixed structured JSON that satisfies the schema.
3. **`post_comment`** **pauses for approval** unless pre-approved — the CLI prints the resume command naming the gated `uses` string.

## Design note (no real GitHub)

`pull_request.fetch` and `pull_request.post_comment` are **offline** native operations: they exist so the workflow and policy strings look like a real integration while the demo stays repeatable in CI and on laptops without tokens.

## Compared to one-off code

| Concern | This demo | Typical script |
|--------|-----------|----------------|
| Order of operations | `.agent` workflow | Implicit control flow |
| “Can we post?” | Policy resource + approval gate + trace | Easy to forget a guard |
| Model output shape | JSON Schema on the agent | String parsing / hope |
| Audit trail | `terfyn logs`; `terfyn audit verify` for tamper detection | Printf / none |

For broader patterns, see [`docs/EXAMPLES.md`](../../docs/EXAMPLES.md), [`docs/AUDIT_CHAIN.md`](../../docs/AUDIT_CHAIN.md), and [`docs/DESIGN_DOC.md`](../../docs/DESIGN_DOC.md).

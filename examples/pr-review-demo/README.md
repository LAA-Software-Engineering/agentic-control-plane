# PR review demo (policy-gated GitHub comment)

This example shows **why a declarative control plane beats ad-hoc glue code** for agent workflows that touch the outside world.

## What it demonstrates

- **Workflow as data** — `workflows/pr-review.yaml` reads like a runbook: fetch PR context → review → post a comment.
- **Structured agent output** — the reviewer must return JSON validated by `schemas/review-output.json` (summary + findings), not an unparseable blob.
- **First-class policy** — `policies/guarded-writes.yaml` lists which tool `uses` strings require explicit approval.
- **Safe-by-default writes** — the `post_comment` step calls a **simulated** native GitHub tool (`tools/github.yaml`). Without approval, **policy blocks the call** before any side effect.
- **Traceable behavior** — `agentctl logs` shows normal step progress plus a **`policy.denied`** event on the blocked step.

## Why this matters

In a typical script, “call the model, then maybe post to GitHub” is buried in code paths that are hard to review, diff, and audit. Here, **permissions and sequencing are YAML** you can put in code review, and the runtime **enforces** them with **exit codes** and **SQLite traces**.

## Project layout

| Path | Role |
|------|------|
| `project.yaml` | Imports resources; defaults to `mock` model (no API keys). |
| `workflows/pr-review.yaml` | Three steps: `fetch_pr`, `review_diff`, `post_comment`. |
| `agents/reviewer.yaml` | Reviewer instructions + output schema. |
| `tools/github.yaml` | `native` tool; operations are implemented offline in the binary. |
| `policies/guarded-writes.yaml` | Requires `--approve` for `tool.github.pull_request.post_comment`. |
| `schemas/*.json` | JSON Schema for workflow input and agent output. |
| `fixtures/sample-pr.json` | Sample input (no GitHub network or tokens). |

## Prerequisites

Build `agentctl` from the repo root (`make build`) or use a release binary on your `PATH`.

## How to run

From the **repository root** (paths below assume that):

```bash
agentctl validate --project examples/pr-review-demo
agentctl plan --project examples/pr-review-demo --state /tmp/pr-review-state.db
agentctl apply --project examples/pr-review-demo --state /tmp/pr-review-state.db --auto-approve
```

### Default run (comment blocked)

```bash
agentctl run workflow/pr-review \
  --project examples/pr-review-demo \
  --state /tmp/pr-review-state.db \
  --input-file examples/pr-review-demo/fixtures/sample-pr.json
```

- Exit code **5** = policy denial (by design).
- Inspect the trace (use the printed **Run ID**):

```bash
agentctl logs --project examples/pr-review-demo --state /tmp/pr-review-state.db --run <run-id>
```

You should see steps through `review_diff`, then **`policy.denied`** on `post_comment` with reason **`approval_required`**.

### Optional: allow the write (full success)

```bash
agentctl run workflow/pr-review \
  --project examples/pr-review-demo \
  --state /tmp/pr-review-state.db \
  --input-file examples/pr-review-demo/fixtures/sample-pr.json \
  --approve tool.github.pull_request.post_comment
```

This records a simulated comment result (still **no** real GitHub traffic).

## Expected highlights

1. **`fetch_pr`** completes — native tool normalizes the `pr` object from input JSON.
2. **`review_diff`** completes — mock model returns fixed structured JSON that satisfies the schema.
3. **`post_comment`** is **blocked** unless approved — CLI prints a clear **policy** line naming the gated `uses` string.

## Design note (no real GitHub)

`pull_request.fetch` and `pull_request.post_comment` are **offline** native operations: they exist so the workflow and policy strings look like a real integration while the demo stays repeatable in CI and on laptops without tokens.

## Compared to one-off code

| Concern | This demo | Typical script |
|--------|-----------|----------------|
| Order of operations | Workflow YAML | Implicit control flow |
| “Can we post?” | Policy resource + trace | Easy to forget a guard |
| Model output shape | JSON Schema on the agent | String parsing / hope |
| Audit trail | `agentctl logs` | Printf / none |

For broader patterns, see [`docs/EXAMPLES.md`](../../docs/EXAMPLES.md) and [`docs/DESIGN_DOC.md`](../../docs/DESIGN_DOC.md).

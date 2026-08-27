# Environment overlays (dev / staging / prod)

This example is the distinctive **promotion** demo: one project, three Environment overlays. **dev** stays on the mock model. **staging** and **prod** select named OpenAI ids (validate/plan only — no live API). **prod** tightens execution and adds an approval gate. `plan -e prod --from-env dev` shows the Epic C **risk delta** against applied **dev**.

## What it demonstrates

- **dev = mock** — `environments/dev.yaml` pins `Agent/reviewer` to `mock/gpt-4`. Safe to `apply` / optional `run`.
- **staging = cheap model** — `openai/gpt-4o-mini`. `validate -e staging` / `plan -e staging` only in CI (needs `OPENAI_API_KEY` to **run**).
- **prod = stricter** — `openai/gpt-4o`, lower cost/wall-clock, `requireStructuredOutput: true`, and extra `approvals.requiredFor: tool.notify.default`.
- **`plan -e prod` risk** — applied_resources are **per `-e`**. After `apply -e dev`, `plan -e prod` alone would see an empty prod slot (creates, not `model_change`). Use **`--from-env dev`** to diff the prod overlay against applied **dev**. C1 flags **widening**; a mock→prod **model change** is `[medium] model_change`. Prod **tightens** cost and **adds** an approval — those show as **field diffs**, not `budget_relaxation` / `approval_removal`.
- **MVP overlay limits** — Environment can override agent `model` / `constraints` and policy `execution` plus `approvals.requiredFor` (union). There is **no** Tool.allow overlay; “fewer tool permissions” here is the extra prod requiredFor on `notify`.

## Project layout

| Path | Role |
|------|------|
| `project.yaml` | Imports; mock + openai providers. |
| `environments/dev.yaml` | Mock model. |
| `environments/staging.yaml` | `openai/gpt-4o-mini`. |
| `environments/prod.yaml` | `openai/gpt-4o`, tighter execution, notify approval. |
| `agents/reviewer.yaml` | Default mock; `notify` tool. |
| `tools/notify.yaml` | Trusted mock write (`sideEffects: true`). |
| `policies/default.yaml` | Ceiling **$5** / 300s; no `requiredFor`. |
| `workflows/review.yaml` | One agent step. |
| `schemas/*.json` | Ticket input and pr-review-shaped output. |
| `fixtures/sample-ticket.json` | Offline payload. |

Do not commit `.agentic/` state from a local walkthrough.

## Prerequisites

Build `terfyn` from the repo root (`make build`). Staging/prod **run** would need `OPENAI_API_KEY`; this walkthrough does not run those envs.

## How to run

From the **repository root**:

```bash
terfyn validate --project examples/env-overlays -e dev
terfyn validate --project examples/env-overlays -e staging
terfyn validate --project examples/env-overlays -e prod
```

Apply the mock overlay:

```bash
terfyn plan --project examples/env-overlays -e dev --state /tmp/env-overlays.db
terfyn apply --project examples/env-overlays -e dev --state /tmp/env-overlays.db --auto-approve
```

Optional (mock only):

```bash
terfyn run workflow/review \
  --project examples/env-overlays \
  -e dev \
  --state /tmp/env-overlays.db \
  --input-file examples/env-overlays/fixtures/sample-ticket.json
```

Preview promotion (prod desired vs applied **dev**):

```bash
terfyn plan --project examples/env-overlays -e prod --from-env dev --state /tmp/env-overlays.db
```

You should see:

- `~ update Agent/reviewer` with a **`spec.model`** diff (`mock/gpt-4` → `openai/gpt-4o`)
- **Risk delta** `[medium] model_change: Agent model changed (Agent/reviewer).`
- Policy field diffs: lower `maxTotalCostUsd` / `maxWallClockSeconds`, `requireStructuredOutput: true`, extra `requiredFor` — **not** labeled as budget relaxation or approval removal

Do **not** `run -e staging` or `run -e prod` without an API key.

## Compared to a single env

| | This demo | Typical script |
|--|-----------|----------------|
| Model per env | Environment overlay | `if ENV==prod` in code |
| Promotion preview | `plan -e prod --from-env dev` | Hope staging matches prod |
| Approval in prod | Overlay `requiredFor` | Easy to skip a guard |

For C1 risk categories, see [`docs/DESIGN_DOC.md`](../../docs/DESIGN_DOC.md) and the plan risk output from issue #165.

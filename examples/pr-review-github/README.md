# PR review (live GitHub read + optional real comment)

> **OpenAI reviewer + Actions bundle:** for **`gpt-4o-mini`** and the copy-paste workflow in the same tree, use **[`examples/pr-review-github-actions/`](../pr-review-github-actions/README.md)**. This directory keeps a **mock** default model for deterministic CI and integration tests.

This example wires **Phase B + C** of the GitHub integration:

- **Read:** `pull_request.get` and `pull_request.diff` against the GitHub REST API.
- **Review:** structured **mock** model output validated by JSON Schema.
- **Write:** `pull_request.post_comment` creates or updates an issue comment when `owner`, `repo`,
  `number`, and `body` are set and **`GITHUB_TOKEN` is present** (default **`comment_strategy: replace`**
  patches a comment containing **`<!-- agentic-review -->`**; use **`append`** for a new comment each run).
  Without repo context it stays **simulated** (as in `examples/pr-review-demo`). The step is **policy-gated**
  unless you pass `--approve tool.github.pull_request.post_comment`.

The `guarded-writes` policy, the `github` tool, the reviewer agent, and the four-statement workflow are all authored in [`main.agent`](main.agent) ([`.agent`](../../docs/LANGUAGE.md)) — no `project.yaml`; the comment `body` is templated from the review's structured output via `${review_diff.summary}` / `${review_diff.findings}`. JSON Schemas stay JSON (referenced by type).

## Prerequisites

- **`GITHUB_TOKEN`** with at least **`pull_requests: read`** for the read steps, and
  **`pull_requests: write`** (or **`repo`**) if you intend to **approve** the comment step and post
  for real.
- Network access to **`GITHUB_API_URL`** (default `https://api.github.com`).

## Workflow input

JSON object:

| Field | Meaning |
|-------|---------|
| `owner` | Repository owner (user or org) |
| `repo` | Repository name |
| `number` | Pull request number |

See `fixtures/sample-input.json` (fake org/repo for **integration tests** only).

## Run locally

From the repository root:

```bash
export GITHUB_TOKEN=ghp_...
terfyn validate --project examples/pr-review-github
terfyn plan   --project examples/pr-review-github --state /tmp/pr-github.db
terfyn apply  --project examples/pr-review-github --state /tmp/pr-github.db --auto-approve
terfyn run workflow/pr-review-github \
  --project examples/pr-review-github \
  --state /tmp/pr-github.db \
  --input '{"owner":"YOUR_ORG","repo":"YOUR_REPO","number":123}'
```

Without `--approve tool.github.pull_request.post_comment`, the final step **pauses for approval** —
a HITL interrupt (status **`interrupted`**, exit **0**), resumable with `terfyn run --resume <id> --decision approve`, by design.

To **publish** the review comment (after policy review of your YAML / process):

```bash
terfyn run workflow/pr-review-github \
  --project examples/pr-review-github \
  --state /tmp/pr-github.db \
  --input '{"owner":"YOUR_ORG","repo":"YOUR_REPO","number":123}' \
  --approve tool.github.pull_request.post_comment
```

## CI / tests

`go test ./test/integration/...` starts an HTTP stub and sets `GITHUB_API_URL` so the workflow runs
without touching GitHub, including an **approved** run that exercises the live comment list + `POST` path.

## GitHub Actions

See **[`examples/pr-review-github-actions/`](../pr-review-github-actions/README.md)** and the PR workflow **[`.github/workflows/terfyn-pr-review.yml`](../../.github/workflows/terfyn-pr-review.yml)** (optional manual publish: **[`terfyn-pr-review-publish.yml`](../../.github/workflows/terfyn-pr-review-publish.yml)**). **[`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md)** covers exit code **5**, permissions, and fork PR notes.

# PR review (live GitHub read + optional real comment)

This example wires **Phase B + C** of the GitHub integration:

- **Read:** `pull_request.get` and `pull_request.diff` against the GitHub REST API.
- **Review:** structured mock (or real) model output validated by JSON Schema.
- **Write:** `pull_request.post_comment` performs a **real** `POST …/issues/{n}/comments` when
  `owner`, `repo`, `number`, and `body` are set and **`GITHUB_TOKEN` is present**; otherwise it stays
  **simulated** (as in `examples/pr-review-demo`). The comment step remains **policy-gated** unless
  you pass `--approve tool.github.pull_request.post_comment`.

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
agentctl validate --project examples/pr-review-github
agentctl plan   --project examples/pr-review-github --state /tmp/pr-github.db
agentctl apply  --project examples/pr-review-github --state /tmp/pr-github.db --auto-approve
agentctl run workflow/pr-review-github \
  --project examples/pr-review-github \
  --state /tmp/pr-github.db \
  --input '{"owner":"YOUR_ORG","repo":"YOUR_REPO","number":123}'
```

Without `--approve tool.github.pull_request.post_comment`, the final step is **blocked** by policy
(exit code **5**), by design.

To **publish** the review comment (after policy review of your YAML / process):

```bash
agentctl run workflow/pr-review-github \
  --project examples/pr-review-github \
  --state /tmp/pr-github.db \
  --input '{"owner":"YOUR_ORG","repo":"YOUR_REPO","number":123}' \
  --approve tool.github.pull_request.post_comment
```

## CI / tests

`go test ./test/integration/...` starts an HTTP stub and sets `GITHUB_API_URL` so the workflow runs
without touching GitHub, including an **approved** run that exercises the live comment `POST` path.

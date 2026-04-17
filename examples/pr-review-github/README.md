# PR review (live GitHub read path)

This example is **Phase B** of the GitHub integration: a first-class workflow that calls the real
GitHub REST API for **PR metadata** and **unified diff**, then runs the structured reviewer agent and
attempts a **simulated** comment (still offline), **policy-gated** like the offline demo.

## Prerequisites

- `GITHUB_TOKEN` with `repo` or appropriate `pull_requests: read` scope for private repositories.
- Network access to `GITHUB_API_URL` (default `https://api.github.com`).

## Workflow input

JSON object:

| Field | Meaning |
|-------|---------|
| `owner` | Repository owner (user or org) |
| `repo` | Repository name |
| `number` | Pull request number |

See `fixtures/sample-input.json` (points at a fake org/repo for **tests** only).

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

## CI / tests

`go test ./test/integration/...` starts an HTTP stub and sets `GITHUB_API_URL` so the workflow runs
without touching GitHub.

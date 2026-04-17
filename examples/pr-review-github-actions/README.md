# PR review with GitHub Actions + OpenAI (`gpt-4o-mini`)

This directory is a **complete example** you can copy or run from the monorepo root:

1. **Declarative project** (`project.yaml`, policies, tools, **`workflow/pr-review-github`**, agent **`reviewer`**) that uses **OpenAI `gpt-4o-mini`** for the review step (not the mock model).
2. **GitHub Actions** — in **this** repository the workflow is **[`.github/workflows/agentctl-pr-review.yml`](../../.github/workflows/agentctl-pr-review.yml)** at the repo root (GitHub only runs workflows from there). It runs on **`pull_request`** (same **`paths-ignore`** as CI: `Makefile`, `**/*.md`). For **your** fork or another repo, copy that file into **`.github/workflows/`**.

For the **mock-only** live GitHub path (no OpenAI key, good for CI and integration tests in this repo), see **[`examples/pr-review-github/`](../pr-review-github/README.md)**.

## Layout

| Path | Purpose |
|------|---------|
| `project.yaml` | Imports policies, tools, agent, workflow; **`defaults.model: openai/gpt-4o-mini`**; **`OPENAI_API_KEY`** via `apiKeyFrom` |
| `agents/reviewer.yaml` | **`spec.model: openai/gpt-4o-mini`**, structured JSON output |
| `workflows/pr-review-github.yaml` | GitHub REST read → reviewer → gated `post_comment` |
| [`.github/workflows/agentctl-pr-review.yml`](../../.github/workflows/agentctl-pr-review.yml) | Runs on PRs in this repo; **`AGENTIC_PROJECT`** = **`examples/pr-review-github-actions`** |

## Secrets (GitHub Actions)

| Secret | Required for |
|--------|----------------|
| **`OPENAI_API_KEY`** | **`agentctl run`** (the **`review_diff`** agent step calls OpenAI) |
| **`GITHUB_TOKEN`** | Provided by Actions; used for GitHub REST tools |

Add **`OPENAI_API_KEY`** in the repository **Settings → Secrets and variables → Actions**.

## Local run (real GitHub + OpenAI)

```bash
export OPENAI_API_KEY=sk-...
export GITHUB_TOKEN=ghp_...
agentctl validate --project examples/pr-review-github-actions
agentctl plan   --project examples/pr-review-github-actions --state /tmp/pr-actions.db
agentctl apply  --project examples/pr-review-github-actions --state /tmp/pr-actions.db --auto-approve
agentctl run workflow/pr-review-github \
  --project examples/pr-review-github-actions \
  --state /tmp/pr-actions.db \
  --input '{"owner":"ORG","repo":"REPO","number":123}'
```

## Checklist (downstream repo)

1. Copy **this entire directory** (the YAML project) into your repo, e.g. **`agent-plane/`**.
2. Copy **[`.github/workflows/agentctl-pr-review.yml`](../../.github/workflows/agentctl-pr-review.yml)** from this repository’s root into **`.github/workflows/`** (or maintain your own workflow).
3. Set **`AGENTIC_PROJECT`** in the workflow to that directory (the template default is **`examples/pr-review-github-actions`** for use **inside this monorepo**).
4. Configure **`OPENAI_API_KEY`**, set **`AGENTCTL_INSTALL: release`** in the workflow (this monorepo defaults to **`go-build`**), and pin **`AGENTCTL_VERSION`** to a release that includes the GitHub native tools you need (see [releases](https://github.com/LAA-Software-Engineering/agentic-control-plane/releases)).
5. Adjust **`permissions`** and optional Phase E flags (**`AGENTIC_CACHE_STATE`**, **`AGENTIC_GH_PR_COMMENT`**) per [`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md).

## Troubleshooting: workflow not listed on the PR

If **`Agentic PR review`** does not run on the **first** PR that adds the workflow, see **“First-time introduction on a pull request”** in **[`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md)** (workflow must exist on **`main`**, and **`concurrency`** must not read **`pull_request`** on non-PR events).

## Related docs

- **[`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md)** — exit code **5**, tokens, fork PR caveats, job summary / cache / **`gh`**.

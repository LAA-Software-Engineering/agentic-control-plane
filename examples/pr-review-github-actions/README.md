# PR review with GitHub Actions + OpenAI (`gpt-4o-mini`)

This directory is a **complete example** you can copy or run from the monorepo root:

1. **Declarative project** (`project.yaml`, policies, tools, **`workflow/pr-review-github`**, agent **`reviewer`**) that uses **OpenAI `gpt-4o-mini`** for the review step (not the mock model).
2. **GitHub Actions** — in **this** repository the workflow is **[`.github/workflows/terfyn-pr-review.yml`](../../.github/workflows/terfyn-pr-review.yml)** at the repo root (GitHub only runs workflows from there). It runs on **`pull_request`** (same **`paths-ignore`** as CI: `Makefile`, `**/*.md`). For **your** fork or another repo, copy that file into **`.github/workflows/`**.

For the **mock-only** live GitHub path (no OpenAI key, good for CI and integration tests in this repo), see **[`examples/pr-review-github/`](../pr-review-github/README.md)**.

## Layout

| Path | Purpose |
|------|---------|
| `project.yaml` | Imports policies + tools; **`defaults.model: openai/gpt-4o-mini`**; **`OPENAI_API_KEY`** via `apiKeyFrom` |
| `main.agent` | The `reviewer` agent (**`openai/gpt-4o-mini`**, structured JSON output) and the `pr-review-github` workflow (GitHub REST read → reviewer → `post_comment` with **`comment_strategy: replace`**), authored in [`.agent`](../../docs/LANGUAGE.md); discovered, not imported. The comment `body` is templated from the review via `${review_diff.findings_markdown}`. |
| `schemas/GitHubPRInput.json`, `schemas/ReviewOutput.json` | JSON Schema for workflow input and agent output (type names match the `.agent` references) |
| [`.github/workflows/terfyn-pr-review.yml`](../../.github/workflows/terfyn-pr-review.yml) | Runs on PRs; **`AGENTIC_PROJECT`** = **`examples/pr-review-github-actions`** |
| [`.github/workflows/terfyn-pr-review-publish.yml`](../../.github/workflows/terfyn-pr-review-publish.yml) | Optional manual **`workflow_dispatch`** to post an approved PR comment |

## Secrets (GitHub Actions)

| Secret | Required for |
|--------|----------------|
| **`OPENAI_API_KEY`** | **`terfyn run`** (the **`review_diff`** agent step calls OpenAI) |
| **`GITHUB_TOKEN`** | Provided by Actions; used for GitHub REST tools |

Add **`OPENAI_API_KEY`** in the repository **Settings → Secrets and variables → Actions**.

## Local run (real GitHub + OpenAI)

```bash
export OPENAI_API_KEY=sk-...
export GITHUB_TOKEN=ghp_...
terfyn validate --project examples/pr-review-github-actions
terfyn plan   --project examples/pr-review-github-actions --state /tmp/pr-actions.db
terfyn apply  --project examples/pr-review-github-actions --state /tmp/pr-actions.db --auto-approve
terfyn run workflow/pr-review-github \
  --project examples/pr-review-github-actions \
  --state /tmp/pr-actions.db \
  --input '{"owner":"ORG","repo":"REPO","number":123}'
```

## Checklist (downstream repo)

1. Copy **this entire directory** (the YAML project) into your repo, e.g. **`agent-plane/`**.
2. Copy **[`.github/workflows/terfyn-pr-review.yml`](../../.github/workflows/terfyn-pr-review.yml)** from this repository’s root into **`.github/workflows/`** (and optionally **`terfyn-pr-review-publish.yml`** for manual publish). **`post-pointer`** is skipped unless you set **`AGENTIC_GH_PR_COMMENT: "true"`** — that is expected.
3. Set **`AGENTIC_PROJECT`** in the workflow to that directory (the template default is **`examples/pr-review-github-actions`** for use **inside this monorepo**).
4. Configure **`OPENAI_API_KEY`**, set **`TERFYN_INSTALL: release`** in the workflow (this monorepo defaults to **`go-build`**), and pin **`TERFYN_VERSION`** to a release that includes the GitHub native tools you need (see [releases](https://github.com/Terfyn/terfyn/releases)).
5. Adjust **`permissions`** and optional Phase E flags (**`AGENTIC_CACHE_STATE`**, **`AGENTIC_GH_PR_COMMENT`**) per [`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md).

## Troubleshooting: workflow not listed on the PR

If **`Agentic PR review`** does not run on the **first** PR that adds the workflow, see **“First-time introduction on a pull request”** in **[`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md)** (workflow must exist on **`main`**, and **`concurrency`** must not read **`pull_request`** on non-PR events).

## Related docs

- **[`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md)** — exit code **5**, tokens, fork PR caveats, job summary / cache / **`gh`**.

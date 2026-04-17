# GitHub Actions integration

This document is **Phase D + E**: how to run **`agentctl`** from GitHub Actions against a declarative
project (the bundled example is **`examples/pr-review-github-actions/`**, which uses **OpenAI
`gpt-4o-mini`** for the reviewer agent), including
optional polish (job summary, cache, **`gh`** pointer comments).

For the YAML resources and workflow semantics, see **`DESIGN_DOC.md`**, **`examples/pr-review-github-actions/README.md`**, and (mock reviewer variant) **`examples/pr-review-github/README.md`**.

---

## What you copy into your repository

1. **Project directory** — copy **`examples/pr-review-github-actions/`** (OpenAI reviewer + same
   workflow shape) or your own project into your repo, e.g. **`agent-plane/`**, committed next to
   your application code.
2. **Workflow file** — copy
   **`examples/pr-review-github-actions/.github/workflows/agentctl-pr-review.yml`** into **your**
   **`.github/workflows/`** (same relative path under the repo root).

Then edit the workflow **`AGENTIC_PROJECT`** env (default in the template matches this monorepo;
in your repo set it to **`agent-plane`** or your path).

Configure repository secret **`OPENAI_API_KEY`** — the example project’s reviewer agent calls OpenAI
(**`gpt-4o-mini`**) during **`agentctl run`**.

---

## Authentication and permissions

- Actions sets **`GITHUB_TOKEN`** automatically. For **`pull_request`** workflows, grant only what
  you need:
  - **`contents: read`** — checkout.
  - **`pull-requests: read`** — enough for `pull_request.get` / `diff` on **same-repo** PRs.
  - **`pull-requests: write`** — required if a job runs **`post_comment`** for real (e.g. the
    **`workflow_dispatch`** publish job with **`--approve`**) or if you enable the optional
    **`post-pointer`** job (**`gh pr comment`**) via **`AGENTIC_GH_PR_COMMENT: "true"`** (that job
    is separate so the main review job can stay read-only).

**Fork PRs:** the default `GITHUB_TOKEN` for workflows triggered from forks is **restricted**; many
write APIs are unavailable. Treat PR-from-fork flows as **read-only review** unless you use a
separate token (PAT/GitHub App) with explicit policy. See GitHub’s documentation on
[permissions for the `GITHUB_TOKEN`](https://docs.github.com/en/actions/security-guides/automatic-token-authentication#permissions-for-the-github_token).

---

## Exit code **5** (policy denial) in CI

`agentctl run` returns **exit code 5** when policy blocks a gated tool (for example
**`tool.github.pull_request.post_comment`** without **`--approve`**).

For a **“review only, no comment”** job, that is often **expected success**: the review and trace
rows completed; only the publish step was blocked by design.

The template workflow treats **`0` or `5`** as success for the review job. Hard failures use **1**,
**2**, **3**, **4** (see **section 11.2** in **`DESIGN_DOC.md`**).

---

## Pinning `agentctl`

The template downloads a release archive from this repository’s **Releases** page. Set
**`AGENTCTL_VERSION`** (e.g. **`v0.1.9`**) to a tag that exists; asset names look like
**`agentctl-<tag>-linux-amd64.tar.gz`**.

---

## Phase E polish (template defaults)

The workflow template sets these **workflow-level** env vars (tune after copying):

| Variable | Default | Purpose |
|----------|---------|---------|
| **`AGENTIC_CACHE_STATE`** | `false` | When `true`, restores/saves the SQLite state file between runs (update **`hashFiles()`** globs if **`AGENTIC_PROJECT`** is not **`examples/pr-review-github-actions`**). |
| **`AGENTIC_GH_PR_COMMENT`** | `false` | When `true`, runs a small follow-up job that posts a short **`gh pr comment`** linking to the Actions run (needs **`pull-requests: write`** on that job). |

**`GITHUB_STEP_SUMMARY`:** the review and publish jobs append a markdown table plus a **truncated**
**`agentctl logs --run …`** excerpt so reviewers see traces in the job summary without opening raw
logs. **`agentctl run -o json`** captures **`runId`** for that step.

---

## Optional: publish job (`workflow_dispatch`)

The same template file includes a second job that runs **`agentctl run … --approve …`** after a
**manual** **`workflow_dispatch`**, with **`owner` / `repo` / `number`** inputs. Use this only from
protected branches or environments after you are comfortable with real PR comments.

---

## In-repo reference layout

When developing **inside** the **agentic-control-plane** monorepo, you can point
**`AGENTIC_PROJECT`** at **`examples/pr-review-github-actions`** so the checked-in workflow matches
the copy-paste template without moving files.

See also **`examples/pr-review-github-actions/README.md`** for a short checklist.

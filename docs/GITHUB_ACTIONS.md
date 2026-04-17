# GitHub Actions integration

This document is **Phase D**: how to run **`agentctl`** from GitHub Actions against a declarative
project (for example the live PR review workflow under **`examples/pr-review-github/`**).

For the YAML resources and workflow semantics, see **`DESIGN_DOC.md`** and **`examples/pr-review-github/README.md`**.

---

## What you copy into your repository

1. **Project directory** — copy `examples/pr-review-github/` (or your own project) into your repo,
   e.g. **`agent-plane/`**, committed next to your application code.
2. **Workflow file** — copy
   **`examples/pr-review-github-actions/.github/workflows/agentctl-pr-review.yml`** into **your**
   **`.github/workflows/`** (same relative path under the repo root).

Then edit the workflow **`AGENTIC_PROJECT`** env (default in the template matches this monorepo;
in your repo set it to **`agent-plane`** or your path).

---

## Authentication and permissions

- Actions sets **`GITHUB_TOKEN`** automatically. For **`pull_request`** workflows, grant only what
  you need:
  - **`contents: read`** — checkout.
  - **`pull-requests: read`** — enough for `pull_request.get` / `diff` on **same-repo** PRs.
  - **`pull-requests: write`** — required only if a job runs **`post_comment`** for real (e.g. a
    separate publish job with **`--approve`**).

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

## Optional: publish job (`workflow_dispatch`)

The same template file includes a second job that runs **`agentctl run … --approve …`** after a
**manual** **`workflow_dispatch`**, with **`owner` / `repo` / `number`** inputs. Use this only from
protected branches or environments after you are comfortable with real PR comments.

---

## In-repo reference layout

When developing **inside** the **agentic-control-plane** monorepo, you can point
**`AGENTIC_PROJECT`** at **`examples/pr-review-github`** so the checked-in workflow matches the
copy-paste template without moving files.

See also **`examples/pr-review-github-actions/README.md`** for a short checklist.

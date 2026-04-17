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
2. **Workflow files** — copy **`.github/workflows/agentctl-pr-review.yml`** (runs on **`pull_request`**)
   from this repository’s **root** into **your** **`.github/workflows/`** (GitHub Actions only loads
   workflows from the repo root, not from under **`examples/`**). Optionally copy
   **`.github/workflows/agentctl-pr-review-publish.yml`** if you want the manual **`workflow_dispatch`**
   job that posts an approved PR comment (kept separate so PR runs do not show a skipped publish job).

Then edit the workflow **`AGENTIC_PROJECT`** env (default in the template matches this monorepo;
in your repo set it to **`agent-plane`** or your path).

### First-time introduction on a pull request

GitHub only schedules **`on: pull_request`** workflows from the **default branch** (e.g. **`main`**)
definition in many cases—especially for a **new** workflow file that does not yet exist on
**`main`**. If your PR adds **`.github/workflows/agentctl-pr-review.yml`** (or the publish workflow)
for the first time, the
**Agentic PR review** check may **not appear on that PR** until either:

1. The workflow file is merged to **`main`** (then **future** PRs get the check), or  
2. You split the change: merge a minimal version of the workflow to **`main`** first, then iterate on a follow-up PR.

Avoid **`concurrency`** / job expressions that read **`github.event.pull_request.*`** without guarding
on **`github.event_name == 'pull_request'`** — GitHub still **validates** workflow YAML on **push**
to **`.github/workflows/`**, and missing `pull_request` context can surface as a failed “workflow
file” run.

Configure repository secret **`OPENAI_API_KEY`** — the example project’s reviewer agent calls OpenAI
(**`gpt-4o-mini`**) during **`agentctl run`**.

---

## Authentication and permissions

- Actions sets **`GITHUB_TOKEN`** automatically. For **`pull_request`** workflows, grant only what
  you need:
  - **`contents: read`** — checkout.
  - **`pull-requests: write`** — required for the bundled **`review`** job: it runs
    **`agentctl run … --approve tool.github.pull_request.post_comment`**, which posts a real issue
    comment on the PR after the model review.
  - The optional **`post-pointer`** job (**`gh pr comment`**) also needs **`pull-requests: write`**
    when **`AGENTIC_GH_PR_COMMENT: "true"`** (short pointer to the Actions run).

**Fork PRs:** the default `GITHUB_TOKEN` for workflows triggered from forks is **restricted**; many
write APIs are unavailable. Treat PR-from-fork flows as **read-only review** unless you use a
separate token (PAT/GitHub App) with explicit policy. See GitHub’s documentation on
[permissions for the `GITHUB_TOKEN`](https://docs.github.com/en/actions/security-guides/automatic-token-authentication#permissions-for-the-github_token).

**Fork PRs and secrets:** `pull_request` workflows triggered from a **fork** do not receive your
repository **Actions secrets** (including **`OPENAI_API_KEY`**), except what GitHub documents for
that event class. The bundled template therefore **skips** the **`review`** job when
**`github.event.pull_request.head.repo.full_name != github.repository`**, so outside contributors
do not get a failing check from an empty API key. To run automated review on fork PRs you would need
a different trigger (for example **`pull_request_target`**, with its own security trade-offs) or a
path that supplies credentials safely.

---

## Exit code **5** (policy denial) in CI

`agentctl run` returns **exit code 5** when policy blocks a gated tool (for example
**`tool.github.pull_request.post_comment`** without **`--approve`**).

The default **`review`** job passes **`--approve tool.github.pull_request.post_comment`**, so a
successful run should end with exit **`0`** and a real PR comment. The workflow still treats **`5`**
as success so a forked copy that removes **`--approve`** can keep a **“review only, no comment”**
job without failing CI.

Hard failures use **1**,
**2**, **3**, **4** (see **section 11.2** in **`DESIGN_DOC.md`**).

---

## Installing `agentctl` in Actions

The template supports two modes via **`AGENTCTL_INSTALL`**:

| Value | When to use |
|-------|-------------|
| **`go-build`** (default in this monorepo) | The workflow checks out this repository and runs **`go build ./cmd/agentctl`**. Use this while developing here so CI always matches the native tools on the branch (no waiting on a release asset). |
| **`release`** | Set **`AGENTCTL_INSTALL`** to **`release`** (any value other than **`go-build`**) in a **downstream** repo that only copies the YAML project, not the Go source. Then set **`AGENTCTL_VERSION`** (e.g. **`v0.1.9`**) to a tag whose asset **`agentctl-<tag>-linux-amd64.tar.gz`** exists on **Releases**. |

Native GitHub REST tools (**`pull_request.get`**, **`pull_request.diff`**, etc.) require an
**`agentctl`** binary built from a release that includes them; if **`release`** mode fails with
unknown tool **`uses`**, bump **`AGENTCTL_VERSION`** after a newer release is published.

---

## Phase E polish (template defaults)

The workflow template sets these **workflow-level** env vars (tune after copying):

| Variable | Default | Purpose |
|----------|---------|---------|
| **`AGENTCTL_INSTALL`** | `go-build` in-repo | **`go-build`** compiles **`./cmd/agentctl`** after checkout. Downstream copies should use **`release`** and **`AGENTCTL_VERSION`**. |
| **`AGENTIC_CACHE_STATE`** | `false` | When `true`, restores/saves the SQLite state file between runs (update **`hashFiles()`** globs if **`AGENTIC_PROJECT`** is not **`examples/pr-review-github-actions`**). |
| **`AGENTIC_GH_PR_COMMENT`** | `false` | When `true`, the **`review`** job exports this flag so the follow-up **`post-pointer`** job can run (**`gh pr comment`** pointer to the Actions run). Job-level **`if:`** cannot read workflow **`env`**, so the template uses a step output instead. |

**`GITHUB_STEP_SUMMARY`:** the review job (and the optional publish workflow) append a markdown table
plus a **truncated** **`agentctl logs --run …`** excerpt so reviewers see traces in the job summary
without opening raw logs. **`agentctl run -o json`** captures **`runId`** for that step.

---

## Optional: publish workflow (`workflow_dispatch`)

**`.github/workflows/agentctl-pr-review-publish.yml`** runs **`agentctl run … --approve …`** after a
**manual** **`workflow_dispatch`**, with **`owner` / `repo` / `number`** inputs. It is **not** part of
the PR workflow file so **`pull_request`** checks do not list a permanently skipped publish job. Use
it only from protected branches or environments after you are comfortable with real PR comments.

---

## In-repo reference layout

When developing **inside** the **agentic-control-plane** monorepo, you can point
**`AGENTIC_PROJECT`** at **`examples/pr-review-github-actions`** so the checked-in workflow matches
the copy-paste template without moving files.

See also **`examples/pr-review-github-actions/README.md`** for a short checklist.

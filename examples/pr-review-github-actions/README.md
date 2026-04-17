# GitHub Actions template (Phase D)

This directory holds a **ready-to-copy** GitHub Actions workflow for running **`agentctl`** on pull
requests (validate → plan → apply → `workflow/pr-review-github`), plus an optional **manual publish**
job.

## Files

| Path | Purpose |
|------|---------|
| [`.github/workflows/agentctl-pr-review.yml`](.github/workflows/agentctl-pr-review.yml) | Copy into **your** repo’s `.github/workflows/` |

## Checklist

1. Copy **`examples/pr-review-github/`** into your repository (e.g. **`agent-plane/`**), or maintain your own project with the same workflow name **`pr-review-github`**.
2. Copy the workflow YAML into **`.github/workflows/`**.
3. Set **`AGENTIC_PROJECT`** in the workflow to your project directory (default in the template is **`examples/pr-review-github`** for use **inside this monorepo**).
4. Pin **`AGENTCTL_VERSION`** to a [release](https://github.com/LAA-Software-Engineering/agentic-control-plane/releases) tag.
5. Adjust **`permissions`** to the minimum your jobs need (see [`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md)).

## Related docs

- **[`docs/GITHUB_ACTIONS.md`](../../docs/GITHUB_ACTIONS.md)** — exit code **5**, tokens, fork PR caveats.
- **[`examples/pr-review-github/README.md`](../pr-review-github/README.md)** — what the workflow runs.

# Contributing

Thank you for helping improve Agentic Control Plane. This document describes how to work on the repo locally and what we expect in pull requests.

All participants are expected to follow the **[Code of Conduct](CODE_OF_CONDUCT.md)**.

## Prerequisites

- [Go 1.22+](https://go.dev/dl/) (see `go.mod`)
- GNU **Make** (optional; the [`Makefile`](Makefile) wraps common Go commands)

## Getting started

```bash
git clone https://github.com/LAA-Software-Engineering/agentic-control-plane.git
cd agentic-control-plane
go mod download
make build    # or: go build -o bin/agentctl ./cmd/agentctl
```

Run **`make`** or **`make help`** for a full list of targets.

## Before you open a pull request

User-visible behavior changes should include an entry under **[Unreleased]** in [`CHANGELOG.md`](CHANGELOG.md) (especially breaking changes and migrations).

1. **Format** — `make fmt` or ensure `gofmt -l .` prints nothing (same check as CI).
2. **Static analysis** — `make vet` (or `go vet ./...`).
3. **Tests** — `make test` (`go test ./... -race`).

A convenient local gate (format check, vet, tests; no build):

```bash
make ci
```

CI in [`.github/workflows/ci.yml`](.github/workflows/ci.yml) also **builds** `agentctl`, runs tests with **`-count=1 -shuffle=on -timeout=10m`**, and writes a **coverage** profile. To mirror that test invocation closely:

```bash
go test -race -count=1 -shuffle=on -timeout=10m ./...
go build -o bin/agentctl ./cmd/agentctl
```

### CLI golden tests

If you intentionally change table (or other) CLI output captured by golden files:

```bash
GO_UPDATE_GOLDEN=1 go test ./internal/cli/... -run TestGolden_
```

Commit the updated files under `internal/cli/testdata/` with your change.

## Project layout

High-level map (details in [`README.md`](README.md) and [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md)):

| Area | Path |
|------|------|
| CLI entrypoint | `cmd/agentctl` |
| Commands, flags, golden tests | `internal/cli` |
| YAML spec types and validation | `internal/spec`, `internal/project` |
| Plan / apply / engine / policy | `internal/plan`, `internal/apply`, `internal/engine`, `internal/policy` |
| SQLite state | `internal/state/sqlite` |
| End-to-end CLI tests | `test/integration` |

## Code style

- Match existing patterns in nearby files (naming, error handling, test style).
- Prefer small, focused changes; avoid unrelated refactors in the same PR.
- Keep user-visible strings and behaviour aligned with [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md) when touching the CLI or YAML semantics.

## Pull requests

- Describe **what** changed and **why** (context for reviewers).
- Link related **issues** when applicable.
- Ensure **CI would pass** (formatting, vet, tests, build). Note: pushes that **only** change `Makefile` or `**/*.md` do not trigger CI via `paths-ignore`; for doc-only edits, run `make ci` locally if you also changed Go code elsewhere in the same branch.

### Agentic PR review (fork and first-time contributors)

This repository runs an automated **Agentic PR review** workflow
([`.github/workflows/agentctl-pr-review.yml`](.github/workflows/agentctl-pr-review.yml)) on pull
requests. You may **not** see that check on your PR in these common cases:

- **Fork-originated PRs** — the `review` job runs only when the PR head branch lives in the
  **canonical** repository (`github.event.pull_request.head.repo.full_name == github.repository`).
  Fork PRs intentionally skip the job so CI does not fail from missing repository secrets (for
  example **`OPENAI_API_KEY`**), which GitHub does not expose to workflows triggered from forks.
- **First PR that adds the workflow** — GitHub often schedules `pull_request` workflows from the
  default branch definition; a brand-new workflow file may not run on the PR that introduces it
  until it is merged to **`main`** (later PRs then get the check).

That is expected. Maintainers can still validate your change: push the same commits to a branch on
the canonical repo (for example after opening the fork PR) or re-run review from an in-repo branch
when appropriate.

For permissions, secrets, exit code **5**, and other Actions details, see
**[`docs/GITHUB_ACTIONS.md`](docs/GITHUB_ACTIONS.md)** (sections on fork PRs and first-time workflow
introduction).

## License

By contributing, you agree that your contributions will be licensed under the same terms as the project: **[MIT](LICENSE)**.

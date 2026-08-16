# Agentic Control Plane — agent instructions

Declarative YAML control plane for agents, tools, workflows, and policies. Primary interface is the Go CLI `agentctl` (Terraform-style validate / plan / apply / run / logs).

Canonical product detail: [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md). Human contributor guide: [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Layout

| Area | Path |
|------|------|
| CLI entrypoint | `cmd/agentctl` |
| Commands, flags, golden tests | `internal/cli` |
| YAML spec types and validation | `internal/spec`, `internal/project` |
| Plan / apply / engine / policy | `internal/plan`, `internal/apply`, `internal/engine`, `internal/policy` |
| SQLite state | `internal/state/sqlite` |
| End-to-end CLI tests | `test/integration` |
| Examples | `examples/` |

## Commands

- `make fmt` / `make verify-fmt` — format / CI format gate
- `make vet` — `go vet ./...`
- `make test` — `go test ./... -race`
- `make ci` — verify-fmt + vet + test (local gate before PR)
- `make build` — `bin/agentctl`

Intentional CLI golden updates:

```bash
GO_UPDATE_GOLDEN=1 go test ./internal/cli/... -run TestGolden_
```

## Conventions

- Match existing patterns in nearby files; prefer small, focused changes.
- Keep CLI and YAML semantics aligned with `docs/DESIGN_DOC.md`.
- User-visible or breaking changes: entry under **Unreleased** in `CHANGELOG.md`.
- Do not commit secrets, credentials, or local `.agentic/state.db` noise.

## Skills

Project workflows live under `.cursor/skills/`:

- `/feature` — implement a capability or enhancement
- `/bugfix` — diagnose and fix a defect
- `/code-review` — defect-first review of a PR or diff

## Pull requests (required for agents)

When creating a pull request (including via `gh pr create`):

1. Read [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md).
2. Use that **exact** structure for the PR body (Summary, Test plan, Checklist) via a HEREDOC.
3. Fill every section with concrete content for this change. Do not invent a different format.
4. Mark checklist items that do not apply as done only if truly N/A, and note N/A briefly in Summary when needed.

Human PRs opened in the GitHub UI get this template automatically; agents must copy it into `--body` because CLI creation bypasses the UI prefill.

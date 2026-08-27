# Terfyn — agent instructions

Declarative YAML control plane for agents, tools, workflows, and policies. Primary interface is the Go CLI `terfyn` (Terraform-style validate / plan / apply / run / logs).

Canonical product detail: [`docs/DESIGN_DOC.md`](docs/DESIGN_DOC.md). Human contributor guide: [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Layout

| Area | Path |
|------|------|
| CLI entrypoint | `cmd/terfyn` |
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
- `make build` — `bin/terfyn`

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
- `/code-review` — adversarial senior review of a PR or diff; significant findings must be posted as inline code comments

## Code review standard

Code reviews must determine whether a change deserves to merge, not whether it deserves encouragement. Use the `/code-review` skill for PRs and diffs, and review as the maintainer who will own the repository for the next decade.

- Technical analysis comes before presentation. Do not invent findings for tone.
- Identify the claimed contract, then trace each important abstraction end-to-end through validation, storage, runtime behavior, error handling, and tests.
- Prioritize memory safety, security, incorrect behavior, semantic/runtime mismatches, ownership/lifetime errors, broken API contracts, invariant violations, specification divergence, and error recovery corruption.
- Treat comments and documentation as executable claims: "thread-safe", "zero-copy", "backwards compatible", "safe", "generic", "fully typed", "constant time", and similar statements require evidence.
- Findings must be posted as inline code comments on the PR diff whenever a diff line can anchor the issue. The top-level review should contain only the verdict and concise summary unless an issue cannot be attached inline.
- Do not praise effort. Acknowledge good engineering only when it conveys useful technical contrast.
- If no meaningful defect can be demonstrated, approve. Never manufacture a blocker.
- **Authority TCB (raised standard).** For changes touching `internal/{effects,policy,deploy,tools,state,runtime,engine,plan,lang/lower}` or identity/manifest/schema fields in `internal/spec`, review against the soundness invariants in [`docs/SOUNDNESS.md`](docs/SOUNDNESS.md), not just local correctness. First question: *which invariants (S1–S8) can this change affect, and where are the regression tests?* A policy path that consults live disk during a pinned resume makes the central product claim false while everything looks green — that is not the same class of risk as a renderer bug. Scope decisions for new subsystems follow [ADR 004](docs/adr/004-scope-and-non-goals.md)'s necessity test.

## Pull requests (required for agents)

When creating a pull request (including via `gh pr create`):

1. Read [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md).
2. Use that **exact** structure for the PR body (Summary, Test plan, Checklist) via a HEREDOC.
3. Fill every section with concrete content for this change. Do not invent a different format.
4. Mark checklist items that do not apply as done only if truly N/A, and note N/A briefly in Summary when needed.

Human PRs opened in the GitHub UI get this template automatically; agents must copy it into `--body` because CLI creation bypasses the UI prefill.

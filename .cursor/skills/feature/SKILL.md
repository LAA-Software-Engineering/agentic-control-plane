---
name: feature
description: Implement a new capability or enhancement in this repo. Use when the user asks to add a feature, implement a capability, ship an enhancement, or build new terfyn/YAML behavior.
---

# Feature

## Workflow

1. **Clarify scope** — Read `docs/DESIGN_DOC.md` and nearby code before coding. Prefer the smallest change that meets the ask.
2. **Implement** — Match patterns in the same package (`cmd/terfyn`, `internal/*`). Avoid unrelated refactors.
3. **Tests** — Add or update unit/integration coverage. For intentional CLI output changes:
   `GO_UPDATE_GOLDEN=1 go test ./internal/cli/... -run TestGolden_`
4. **Verify** — Run `make ci` (verify-fmt, vet, test). Fix failures before claiming done.
5. **Changelog** — If user-visible or breaking, add an entry under **Unreleased** in `CHANGELOG.md`.
6. **Pull request** — When opening a PR, read `.github/PULL_REQUEST_TEMPLATE.md` and use that exact structure for the body (HEREDOC with `gh pr create`). Fill Summary, Test plan, and Checklist. Do not invent a different format.

## Layout reminders

| Area | Path |
|------|------|
| CLI entry | `cmd/terfyn` |
| Commands / goldens | `internal/cli` |
| Spec / project load | `internal/spec`, `internal/project` |
| Plan / apply / engine / policy | `internal/plan`, `internal/apply`, `internal/engine`, `internal/policy` |
| E2E | `test/integration` |

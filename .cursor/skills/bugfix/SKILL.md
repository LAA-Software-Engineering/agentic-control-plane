---
name: bugfix
description: Diagnose and fix bugs or regressions in this repo. Use when the user reports a bug, failure, incorrect behavior, flaky test, or asks to fix a defect.
---

# Bugfix

## Workflow

1. **Reproduce** — Capture the failing command, test, or CLI path. Prefer a failing test over manual-only repro.
2. **Root cause** — Read the failing code path; identify the real cause before patching symptoms.
3. **Minimal fix** — Change only what is required. Match nearby error-handling and style.
4. **Regression test** — Add or extend a test that fails without the fix. Update goldens only when intentional:
   `GO_UPDATE_GOLDEN=1 go test ./internal/cli/... -run TestGolden_`
5. **Verify** — Run `make ci`. Confirm the original failure is gone.
6. **Changelog** — If user-visible, note under **Unreleased** in `CHANGELOG.md`.
7. **Pull request** — When opening a PR, read `.github/PULL_REQUEST_TEMPLATE.md` and use that exact structure for the body (HEREDOC with `gh pr create`). Fill Summary, Test plan, and Checklist. Do not invent a different format.

## Tips

- Policy/safety gates and exit codes matter — check `docs/DESIGN_DOC.md` and existing CLI exit handling before changing behavior.
- Prefer fixing at the package boundary that owns the bug (`internal/cli`, `internal/engine`, `internal/policy`, etc.).

---
name: code-review
description: Review pull requests or diffs for defects, security, and project conventions. Use when reviewing a PR, examining a diff, or when the user asks for a code review.
---

# Code review

## Approach

Defect-first: prioritize correctness and safety over style nits. Be specific (file + reason); skip praise padding.

## Checklist

1. **Correctness** — Logic, edge cases, error paths, exit codes, concurrency (race-safe assumptions).
2. **Policy / safety** — Tool permissions, HITL, fail-closed behavior, secrets/redaction if touched.
3. **API / YAML semantics** — Align with `docs/DESIGN_DOC.md` when CLI or resource behavior changes.
4. **Tests** — Adequate coverage; goldens updated intentionally; `make ci` would pass.
5. **Changelog** — User-visible or breaking changes under **Unreleased** in `CHANGELOG.md`.
6. **PR description** — Body follows `.github/PULL_REQUEST_TEMPLATE.md` (Summary, Test plan, Checklist). Flag missing sections.
7. **Scope** — No unrelated refactors or drive-by edits.

## Output format

Group findings by severity:

- **Must fix** — Blocks merge (bugs, security, broken tests, wrong semantics).
- **Should fix** — Clear improvements that should land before or soon after merge.
- **Nit** — Optional style/clarity.

For each finding: what is wrong, where, and why it matters. If no issues, say so briefly and note any residual risk.

# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Built-in policy presets** (issue #104): `strict`, `permissive`, and `shell_safe`. Select via `Project.spec.defaults.policy`, by referencing a preset name on agents/workflows, or with `Policy.spec.preset` (local rules layer on top). Presets expand during [NormalizeProjectGraph]; `strict`/`permissive` materialize approval flags, while `shell_safe` sets `ResolvedPreset` and relies on runtime token classification plus tool safety metadata for plan risk.
- **`shell_safe` token classification** for native `command.run` / `run` / `exec` / `shell` operations: read-only first tokens (`ls`, `cat`, …) run unattended when the command contains no shell metacharacters (`;|&$`, newlines, `` ` ``, `$(…)`); risky tokens, unknown tokens, and side-effecting non-shell tools require `--approve`. **Heuristic only — not a sandbox.**
- **`spec.safety` on Tool resources** (issue #103): optional `trusted`, `sideEffects`, and `requiresApproval` fields. [NormalizeProjectGraph] materializes fail-closed defaults on load.
- **Policy safety fallback**: when no `approvals.requiredFor` entry matches the exact `uses` string, [policy.Derive] consults resolved safety metadata. Unattended mutating tools require `--approve` (exit code **5**, `approval_required`).
- **Plan risk hints** for tools that will require approval at run, including decision source (`explicit_policy_rule`, `safety_metadata`, `fail_closed_default`).

### Changed

- **Breaking — tool calls without explicit policy are no longer unrestricted.** Previously, `CheckToolCall` with a nil [spec.PolicySpec] allowed all tools. Now fail-closed safety always applies from the project graph (even when the workflow omits `spec.policy` or the Policy resource is missing).
- Tools with **no** `spec.safety` block behave as **untrusted with side effects** after normalization → require `--approve` unless an explicit `approvals.requiredFor` rule matches.

### Migration

1. For **read-only** native or mock tools (echo, fetch, identity), add:
   ```yaml
   spec:
     safety:
       sideEffects: false
   ```
2. For tools where you accept **tool-wide** unattended use but still gate specific operations, set `trusted: true` and list write operations under `Policy.spec.approvals.requiredFor` (exact `uses` strings).
3. Do **not** set `trusted: true` unless you intend every operation on that tool to run without safety-derived approval; per-action gating remains `requiredFor` only (exact match at runtime).

### Not yet wired

- MCP discovery does **not** yet apply [spec.SafetyFromMCPMeta] / [spec.MergeToolSafety]; author-set `spec.safety` in YAML is the source of truth until MCP merge lands (tracked separately from #103).

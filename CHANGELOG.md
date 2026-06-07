# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Tamper-evident trace audit chain** (issue #116): per-run hash-linked `trace_events` (`prev_hash`, `hash`; migration `007`). Each hash covers canonical redacted fields plus the previous event hash. `agentctl audit verify [--run <id>]` re-derives the chain and exits non-zero on breaks; pre-migration rows without hashes are reported as **unchained** and do not fail verification. JSON trace output includes `prevHash` / `hash`. See [`docs/AUDIT_CHAIN.md`](docs/AUDIT_CHAIN.md).
- **Closed trace event taxonomy** (issue #115): versioned `EventType` and `ActorType` enums in `internal/trace` (`TaxonomyVersion` 1). Engine, HITL, and policy emissions use typed events (`run_started`, `tool_execution`, `hitl_request_created`, …). Reserved types `memory_read` and `memory_write` are defined but not yet emitted. SQLite migration `006` adds `actor_type` and migrates legacy dot-notation types. `agentctl logs --event <type>` filters by closed vocabulary; unknown values error with the typed list. Inspector timeline and JSON payloads include `timelineIcon`, `timelineGroup`, and OTel `spanName` derived from the same enum.
- **Config resolution hardening** (issue #112): user-local overlays (`~/.config/agentctl/config.yaml`, `.agentic/local.yaml`), strict unknown-key rejection in all YAML layers, and an immutable resolved-config snapshot (`.agentic/resolved-config.json`) with digest checks across `validate`/`plan`/`apply` → `run` (exit **3** on drift). `plan` JSON/YAML includes `resolvedConfigDigest`.
- **Run attribution** (issue #111): `tenant_id`, `thread_id`, `actor_id`, `parent_run_id`, `request_id`, `idempotency_key`, and `source` on `runs`; trace events carry matching tenant/thread/actor for filterable logs and inspector queries. `agentctl run` accepts `--tenant-id`, `--thread-id`, `--actor-id` (local defaults `tenant-1` / `thread-1` / `user-1`); `agentctl logs` and `GET /api/runs` filter by the same dimensions. `--resume` reuses persisted `run_id` and `thread_id`. OTel spans emit `gen_ai.tenant.id`, `gen_ai.thread.id`, `gen_ai.actor.id`, and `gen_ai.request.id`. See [`docs/ATTRIBUTION.md`](docs/ATTRIBUTION.md).
- **Trace payload redaction** (issue #110): trace events are sanitized, key-redacted, and size-capped before SQLite storage. Defaults mask common secret key names; override via `Project.spec.traces.redactKeys`, `maxPayloadBytes`, and `spec.traces.redaction` (`maxDepth`, `maxBytes` for binary previews, `maxStringChars`). HITL edit `argsDiff` is redacted before persistence. Local runs use [trace.NewRecorderForGraph] from project spec.
- **Optional OpenTelemetry trace export** (issue #108): `Project.spec.telemetry` (`enabled`, `serviceName`, `endpoint` with `env:` tokens, `consoleExport`) emits WayFind-aligned `gen_ai.*` spans (`agent.run`, `model.chat`, `tool.exec`, `approval`) alongside SQLite traces. Disabled by default; init failures log a warning and never fail runs. See [`docs/OTEL.md`](docs/OTEL.md) for a Jaeger quick start.
- **`agentctl inspect --web`** — read-only local inspector (default `http://127.0.0.1:8787`) over SQLite state: runs, trace timeline, run steps, applied deployment resources, and checkpoints ([#109](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/109)).
- **Run checkpointing and resume** (issue #105): SQLite `run_checkpoints` table stores per-run execution snapshots after each completed step. `agentctl run --resume <run-id>` rehydrates interpolation context and continues from the next step without replaying earlier steps. Interrupted runs exit cleanly (status `interrupted`, exit code 0) and cascade with trace retention pruning. Checkpoints are written before step rows are marked succeeded to avoid replay on crash; runs pin `workflow_spec_hash` and `environment_name` for safe resume.
- **Built-in policy presets** (issue #104): `strict`, `permissive`, and `shell_safe`. Select via `Project.spec.defaults.policy`, by referencing a preset name on agents/workflows, or with `Policy.spec.preset` (local rules layer on top). Presets expand during [NormalizeProjectGraph]; `strict`/`permissive` materialize approval flags, while `shell_safe` sets `ResolvedPreset` and relies on runtime token classification plus tool safety metadata for plan risk.
- **`shell_safe` token classification** for native `command.run` / `run` / `exec` / `shell` operations: read-only first tokens (`ls`, `cat`, …) run unattended when the command contains no shell metacharacters (`;|&$`, newlines, `` ` ``, `$(…)`); risky tokens, unknown tokens, and side-effecting non-shell tools require `--approve`. **Heuristic only — not a sandbox.**
- **`spec.safety` on Tool resources** (issue #103): optional `trusted`, `sideEffects`, and `requiresApproval` fields. [NormalizeProjectGraph] materializes fail-closed defaults on load.
- **Policy safety fallback**: when no `approvals.requiredFor` entry matches the exact `uses` string, [policy.Derive] consults resolved safety metadata. Unattended mutating tools require `--approve` (exit code **5**, `approval_required`).
- **Plan risk hints** for tools that will require approval at run, including decision source (`explicit_policy_rule`, `safety_metadata`, `fail_closed_default`).

### Changed

- **Trace event `type` values** (issue #115): persisted and displayed event types are now snake_case (`run_started`, `tool_execution`, …) instead of dot notation (`run.started`, `tool.called`). Migration `006` and read-time normalization upgrade existing SQLite rows; JSON output uses canonical names. Scripts grepping dot-notation types in `agentctl logs` or inspector output should switch to snake_case or use `-o json` with the `type` field. The trace events table adds an `ACTOR TYPE` column (`user` / `agent` / `system`); the run list `ACTOR` column remains the attribution `actor_id` (issue #111).
- **`agentctl logs` table output** (issue #111): the default (non-JSON) run list adds `TENANT`, `THREAD`, and `ACTOR` columns. Scripts that parse fixed column positions should switch to `-o json` or match by header names.
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

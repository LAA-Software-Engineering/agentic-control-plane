# ADR 001: Control plane vs runtime boundary

## Status

Accepted (2026-06-06)

## Context

Agentic Control Plane (ACP) separates **declarative operations** (declare, validate, plan, apply, inspect) from **workflow execution**. Today only `runtime: local` exists, but the roadmap includes additional execution targets (remote executor, container runtime, hosted service).

Without an explicit boundary, execution code tends to reload project YAML/TOML, duplicate config resolution, and entangle policy/HITL/checkpoint wiring with CLI concerns.

## Decision

Introduce a **runtime adapter interface** in `internal/runtime`:

| Layer | Responsibility |
|-------|----------------|
| **Control plane** (`internal/cli`, `internal/config`, `internal/spec`, `internal/plan`, `internal/apply`) | Load and merge config, normalize, validate, fingerprint into an immutable [`ResolvedConfig`](internal/config/resolved.go), select a runtime by name, wire dependencies (SQLite store, attribution, HITL flags). |
| **Runtime adapter** (`internal/runtime`, `internal/runtime/local`, future adapters) | Execute workflows from the resolved snapshot only: `Invoke`, `Resume`, `Health`. Emit trace events, honor engine policy/HITL/checkpoint contracts. **Must not** call `project.LoadProject` or re-read user/project YAML. |

### Registry

- `spec.runtime` resolves through `internal/runtime/catalog` at validate time and `internal/runtime.Lookup` at run time.
- Unknown runtime names fail `validate` with a clear message listing valid names (same pattern as policy presets, issue #104).
- MVP registers only `local` via `internal/runtime/local/register.go`.

### Contract surface

```text
Invoke(ResolvedConfig, InvokeOptions) -> RunResult
Resume(ResolvedConfig, ResumeOptions) -> RunResult
Health() -> HealthStatus { ok | degraded | error }
```

Streaming events (`Stream`) are reserved for a follow-up; trace persistence remains the MVP observability path.

### Package layout

```text
internal/runtime/
  catalog/     # validate-time runtime names (no heavy imports)
  registry.go  # name -> factory
  runtime.go   # Runtime interface + options
  local/       # disk + SQLite MVP adapter
```

## Consequences

- **Positive:** Adding a second runtime is a new adapter package plus `runtime.Register`; control-plane wiring stays stable.
- **Positive:** Plan→run contract (#112) stays in the control plane; runtimes consume the frozen graph.
- **Negative:** Resume with a pinned environment requires the CLI to resolve config for that environment before calling `Resume` (`local.ResolvedConfigForRun`).
- **Testing:** Local adapter tests corrupt on-disk project YAML after resolve to prove execution uses the snapshot, not disk.

## References

- Issue #114 — formalize control-plane vs runtime-engine boundary
- Issue #112 — immutable resolved config snapshot
- Issue #105 — checkpoint resume
- Issue #106 — HITL approvals
- WayFind sibling design — runtime + runner + adapters split

# ADR 006: External agent runtimes and `RuntimeTarget`

## Status

Accepted (2026-09-02) — epic [#335](https://github.com/Terfyn/terfyn/issues/335)

## Context

[ADR 004](004-scope-and-non-goals.md) §3 puts "adapters to runtime targets" in scope and §5 makes
the *honesty boundary* — reviewed authority equals authority at execution — a non-goal to ever
weaken. The [`DESIGN_DOC`](../DESIGN_DOC.md) listed `RuntimeTarget` as a "later" concept: a named
selector for *how* a reviewed program executes, distinct from *what* it is allowed to do.

"Later" arrived. There is real demand to run a reviewed `.agent` program on an existing agent CLI —
`claude -p` (Claude Code) first — rather than only on Terfyn's built-in engine. Doing so naively
would dissolve the central claim: point an agent harness at a task and it brings its own tools
(shell, file writes, network) and its own budget accounting, none of which Terfyn reviewed. The
question this ADR settles is **how an external runtime can be a real, first-class target without any
authority escaping the reviewed program.**

The alternative — refusing external runtimes — was rejected: it cedes the actual way people run
agents today and contradicts ADR 004 §3. The other alternative — a thin "shell out and trust it"
adapter — was rejected because it makes `plan` a lie the moment the harness has a built-in tool.

## Decision

**Promote `RuntimeTarget` from "later" to a real (minimal) selector, and make an external runtime a
boundary adapter that keeps Terfyn the enforcer of record.**

### 1. RuntimeTarget is a selector, not an authority

A runtime target names *how* a workflow executes. It is resolved from `spec.runtime` (workflow),
else `defaults.runtime` (project), else the built-in `local` engine, and overridable per run with
`--runtime <name>`. It carries **no** authority of its own: the effect bound and the capability
manifest are computed from the graph alone, so they are byte-identical whichever target runs the
program. `terfyn plan` surfaces the resolved target and reports a change to it as
`runtime_target_change` — an execution-substrate change, explicitly *not* an authority widening.

### 2. The adapter drives the CLI; Terfyn keeps authority around it

An external target (`internal/runtime/claudecode`) spawns the CLI non-interactively and parses its
structured output into a `Session`. It does not route tools or write trace itself. Wrapped around it:

- **Grant → per-run MCP server.** An agent's grants compile, against the pinned capability manifest
  (ADR 002 / [#204](https://github.com/Terfyn/terfyn/issues/204)), into exactly the MCP operations it
  may call. Every `tools/call` takes the same inner path as the internal loop: `CheckToolCall` →
  policy → HITL → `Tools.Call`.
- **Budget / iteration / timeout** map onto the harness knobs, but Terfyn's `CheckRun` stays
  authoritative and a breach fails closed with `limit_hit`.
- **Trace** folds external turns and tool calls into the hash-linked audit chain, so `terfyn logs`
  and `terfyn audit verify` cover an external run identically to a local one.

### 3. A grant is never a built-in (the soundness core)

A capability grant compiles to a Terfyn-owned MCP operation, **never** to a built-in tool of the
external CLI. The external agent is spawned with no built-in tools; the adapter fails closed before
spawning if a flag would expose one or bypass the permission boundary. Arbitrary shell is not
forbidden — it is made loud: an explicit `grants { tool.shell.exec }` that `plan` shows. This is
permanent obligation **S9** in [`SOUNDNESS.md`](../SOUNDNESS.md), which also carries the
live-verification obligation (an integration check against the pinned CLI must confirm built-ins are
actually denied before a live run).

### 4. Out of scope for this ADR

The live wiring of a `claude -p` run through the adapter + per-run server is the final integration
step of the epic; until it lands, `terfyn run --runtime claude-code` fails closed rather than
silently degrading. Interactive HITL round-trips for an out-of-process agent, and any second
external adapter (Codex, Gemini CLI), are follow-ups that satisfy the same boundary. The explicit bar
such an adapter must meet is written up in
[`RUNTIME_TARGET_CONTRACT.md`](../RUNTIME_TARGET_CONTRACT.md).

## Consequences

- `RuntimeTarget` is real: a reviewed program can name its execution substrate, and `plan` makes the
  choice and any change to it visible.
- The TCB grows by two packages — `internal/runtime/claudecode` and `internal/runtime/mcpserver`
  (see [`SOUNDNESS.md`](../SOUNDNESS.md)) — the exhibits for S9.
- The honesty boundary (ADR 004 §5) holds across runtimes by construction: authority is a property
  of the graph, and the adapter cannot widen it. `examples/external-runtime-reviewer` is the
  end-to-end demonstration; [`EXTERNAL_RUNTIME.md`](../EXTERNAL_RUNTIME.md) is the boundary reference.

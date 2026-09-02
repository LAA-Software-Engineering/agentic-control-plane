# The RuntimeTarget contract

A **runtime target** names *how* a reviewed `.agent` program executes — the built-in `local` engine,
or an **external** target that drives a third-party agent CLI (`claude -p` is the first) as the
execution substrate. A target carries **no authority of its own**: the effect bound and capability
manifest are computed from the graph alone, so they are byte-identical whichever target runs the
program ([ADR 006](adr/006-external-agent-runtimes.md)).

This document is the **contract a new external runtime must satisfy.** It exists because the contract
was, until now, implicit — encoded only in `internal/runtime/claudecode` and shaped around one CLI.
A second adapter (Codex, then Gemini CLI — epic [#409](https://github.com/Terfyn/terfyn/issues/409))
needs an explicit bar. The governing principle is unchanged: **the runtime is replaceable; the
authority is not.**

## The one invariant a target may never break

**A capability grant compiles to a Terfyn-owned MCP operation, never to a built-in tool of the
external CLI** (invariant **S9**, [`SOUNDNESS.md`](SOUNDNESS.md)). The external agent sees *exactly*
the operations the grant compiled into the per-run MCP server, and nothing else — no Bash, no Edit,
no WebFetch. Mapping a scoped grant onto a built-in (`tool.workspace.run_tests → Bash`) is the
canonical violation: `terfyn plan` would advertise `effect bound: workspace.test` while the real
authority is Bash's unbounded filesystem/network/process/git. Broad capability is allowed only
*loudly* — an explicit `grants { tool.shell.exec }` that compiles to a Terfyn-owned MCP op, so `plan`
shows the true surface. Everything below serves this invariant.

## What Terfyn already provides (generic, CLI-agnostic)

An adapter does **not** re-implement any of this. It is shared by every external runtime and sits
*around* the driver:

- **Grant → per-run MCP server** — `mcpserver.Compile(graph, agentName)` yields a closed-world
  `tools/list`; `NewPolicyDispatcher(eval, exec, run).WithTrace(rec, runID)` routes every
  `tools/call` through the **same** inner path as the internal engine: policy `CheckToolCall` → HITL
  → `Tools.Call`.
- **Authenticated loopback transport** — `Server.ListenLocal()` binds the server to `127.0.0.1`,
  mints a per-run 256-bit bearer token, and returns a `Transport{URL, Headers}`;
  `WriteMCPConfig(dir, name, transport)` writes the CLI-facing config file.
- **Composition, budget, trace** — `ClaudeCodeRuntime.RunExternalAgent` shows the standard assembly:
  compile → serve → write config → `RunSession` under a timeout → `EmitSessionTurns` into the
  hash-linked audit chain → `EnforceBudget` (fail closed with `limit_hit`). A new adapter reuses this
  composition; only `RunSession` differs.

## What each adapter must supply

The adapter implements `AgentRuntime.RunSession(ctx, RunSpec) (Session, error)` — spawn the CLI,
constrain it, parse its output. Six requirements:

### R1 — Non-interactive single-shot invocation
The CLI must run headless: prompt + system prompt in, one run out, no TTY, no interactive approvals.
`RunSpec` carries `Prompt`, `SystemPrompt`, `MCPConfig`, and the mapped limits; the adapter turns
these into an argv. *Claude:* `-p <prompt> --output-format stream-json --verbose --system-prompt …`.

### R2 — Per-run MCP server injection + closed world
The adapter must point the CLI at the per-run `Transport` (URL + `Authorization` header from
`WriteMCPConfig`) **and** put it in a closed/strict mode so those are the *only* tools it can call —
a CLI that also loads ambient/user MCP servers or default tools breaks the closed world. *Claude:*
`--mcp-config <path> --strict-mcp-config`. The adapter must also **fail closed** if a caller-supplied
extra flag would register a second server or widen scope out of band (`--mcp-config`, `--add-dir`):
see `checkExtraArgsNoAuthoritySurface`.

### R3 — Built-in-tool lockdown + live S9 verification  *(the load-bearing one)*
The CLI's own built-in tools must be **fully disabled**, so the grant-compiled MCP server is the only
authority surface. The adapter must (a) emit the flags that disable built-ins, (b) fail closed before
spawning if any assembled arg would re-expose one or drop the permission boundary
(`checkNoBuiltinToolExposure` rejects `--dangerously-skip-permissions`, `--permission-mode
bypassPermissions`, and any allow-position built-in), and **(c) prove it against the pinned CLI.**
Requirement (c) cannot be met by a unit test with a fake process — it must be an integration check
that the *real* pinned binary denies a built-in for the emitted flags. Exhibit:
`TestS9Live_builtinsAreDeniedByPinnedCLI` (build tag `s9live`); see
[`EXTERNAL_RUNTIME.md`](EXTERNAL_RUNTIME.md) → *Running the S9 live check*. **Until this passes
against a pinned version, that runtime must not serve a live run.**

> The Claude adapter's `denyBuiltinToolsArgs()` returns `--tools ""` — flagged in-code as an
> *unverified guess* (the documented knobs are `--allowedTools` / `--disallowedTools`). This is
> exactly the kind of per-CLI assumption R3 exists to pin. Every adapter starts by confirming the
> real disable mechanism for its CLI, not by copying Claude's.

### R4 — Structured output → `Session`, or graceful degradation
The adapter parses the CLI's output into a `Session`: `AdvertisedTools` (the callable set the CLI
reported at init — the S9 init-layer control), `Turns`/`ToolUses`, `FinalText`, `NumTurns`,
`CostUSD`, and a normalized `StopReason` (`success` / `max_turns` / `error`). If a CLI does not emit
structured turns, the adapter must degrade safely — a run with no parseable result is an error with
no `Session`, never a silent success. *Claude:* `--output-format stream-json`, parsed by
`parseStreamJSON`. **This is a Claude-ism to lift:** `Session` currently assumes stream-json.

### R5 — Budget / turn / timeout signals
`Limits.ApplyTo(&RunSpec)` maps `constraints.maxIterations → MaxTurns`, `execution.maxTotalCostUsd →
MaxBudgetUSD`, and `timeoutSeconds` → a context deadline (`Limits.WithTimeout`). The adapter forwards
these to the CLI's knobs **as a belt only** — Terfyn's `EnforceBudget` / `CheckRun` remains the
enforcer of record and a breach fails closed regardless of what the harness reports. A CLI that lacks
a turn or budget knob is acceptable; Terfyn still bounds the run.

### R6 — Authenticated loopback
The CLI must send the `Authorization: Bearer …` header from the MCP config with each request. A CLI
that cannot attach per-server headers cannot reach the endpoint (which refuses unauthenticated calls
with constant-time comparison), so this is a hard prerequisite, not a nicety.

## Generic vs. per-adapter — the seams to lift

| Concern | Generic (do not touch) | Per-adapter |
|---|---|---|
| Grant compilation, closed-world `tools/list` | `mcpserver.Compile` | — |
| Policy / HITL / trace on each call | `PolicyDispatcher`, `CheckToolCall` | — |
| Transport + auth | `ListenLocal`, `WriteMCPConfig` | attach header to CLI config (R6) |
| Budget / limits enforcement | `EnforceBudget`, `Limits` | forward to CLI knobs (R5) |
| Invocation flags | — | argv construction (R1, R2) |
| Built-in lockdown | fail-closed guards (`checkNoBuiltinToolExposure`) | the disable flags + live proof (R3) |
| Output → `Session` | the `Session` shape | parser (R4) — lift `stream-json` assumption |

The `RunSpec` field comments (`MaxTurns → --max-turns`, `MCPConfig → --mcp-config`) name Claude flags;
those mappings are an *implementation detail behind the adapter*, not part of the interface. Lifting
the `stream-json` assumption in `Session` and the flag names out of shared comments is the interface
refactor that the second adapter (#409) will force.

## Checklist for a new adapter

1. **Verify the CLI, pinned.** Confirm — against a specific version — the non-interactive invocation,
   MCP-config injection + strict mode, the built-in-disable mechanism, per-server auth headers, and
   the output format. Do not assume; CLIs change.
2. Implement `AgentRuntime.RunSession`: build the argv (R1, R2, R5), run the guards
   (`checkNoBuiltinToolExposure`, `checkExtraArgsNoAuthoritySurface`), spawn, parse → `Session` (R4).
3. Reuse `RunExternalAgent`-style composition for the server/trace/budget wrapper.
4. Register the runtime name in `internal/runtime`.
5. **Write the gated live S9 test** (R3) for the pinned CLI. This is the exhibit that the boundary
   holds; without it the runtime stays fail-closed and undelivered.

## See also

- [ADR 006](adr/006-external-agent-runtimes.md) — why `RuntimeTarget` is a real selector.
- [`EXTERNAL_RUNTIME.md`](EXTERNAL_RUNTIME.md) — the boundary reference and the S9 live-check runbook.
- [`SOUNDNESS.md`](SOUNDNESS.md) — invariant **S9** and its live-verification obligation.

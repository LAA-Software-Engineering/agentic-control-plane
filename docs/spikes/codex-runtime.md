# Spike: Codex CLI as a Terfyn external runtime

**Date:** 2026-09-02 · **Epic:** [#409](https://github.com/Terfyn/terfyn/issues/409) · **Contract:**
[`RUNTIME_TARGET_CONTRACT.md`](../RUNTIME_TARGET_CONTRACT.md)

**Verdict: not currently viable — blocked on R3.** Codex has no supported way to disable its built-in
shell/`apply_patch` tools for an "MCP-only" run, so the grant-compiled MCP server cannot be made the
*only* authority surface (invariant S9). Re-evaluate when openai/codex
[#6049](https://github.com/openai/codex/issues/6049) ships.

> Documentation-only research against Codex's Sept 2026 `codex exec` + `config.toml` model; no binary
> was run. Any future adapter must re-confirm against a pinned version (see the contract checklist).

## Contract scorecard

| Req | Status | Notes |
|---|---|---|
| R1 non-interactive | ✅ | `codex exec [prompt]` runs headless, no TUI. |
| R2 MCP injection + closed world | ⚠️ partial | HTTP MCP via `[mcp_servers.x] url=…` works, but MCP tools are *added alongside* built-ins, not instead of them. Config is TOML in `~/.codex` / `$CODEX_HOME`, not a `--mcp-config` flag (a seam, not a blocker). |
| **R3 built-in lockdown + live S9** | ❌ **blocker** | **No supported flag to run MCP-only** (upstream feature request #6049). Sandbox modes bound what built-ins *do*, not whether they *exist*: under `--sandbox read-only` the agent still reads the filesystem and runs read commands — authority `plan` never advertised and that never passes `CheckToolCall`/HITL/trace. Codex docs frame MCP and the command sandbox as *separate trust boundaries* (issue #4152), architecturally at odds with "the MCP server is the boundary." |
| R4 structured output → `Session` | ✅ (better than Claude) | `--json` emits JSONL events (`thread.started`, `turn.*`, `item.*` incl. MCP tool calls); `--output-schema` constrains final output. |
| R5 budget / turn / timeout | ⚠️ partial | `tool_timeout_sec` / `startup_timeout_sec`; turn/cost caps less explicit — acceptable since Terfyn's `EnforceBudget` stays enforcer of record. |
| R6 authenticated loopback | ✅ | `bearer_token` / `bearer_token_env_var` → `Authorization: Bearer …`, plus `http_headers`; maps onto `ListenLocal`'s token. |

## Why R3 is fatal (not a workaround away)

Terfyn's claim is that the grant-compiled MCP server is the **only** authority surface (S9). Codex is a
shell-first agent that **always** carries a built-in shell + `apply_patch` with no mode to remove them.
No sandbox setting closes this:

- `--sandbox read-only` still grants ungranted filesystem-read authority via the built-in shell, and
  those calls bypass `CheckToolCall` / HITL / the audit chain entirely.
- The "declare shell loudly" escape hatch (`grants { tool.shell.exec }`) doesn't rescue it: Codex's
  built-in shell wouldn't route through Terfyn's MCP server/policy/trace — it executes directly in the
  sandbox, still off-boundary.

So a reviewer trusting `terfyn plan` would be misled — exactly the capability escape S9 forbids.

## Unblock condition

openai/codex [#6049](https://github.com/openai/codex/issues/6049) — a flag/config to disable built-in
tools for a true MCP-only mode. If it ships, Codex becomes a strong candidate: R1/R4/R6 are already
met and R4 is nicer than Claude's stream-json. Track it; do not build the adapter before then.

## Sources

- [Codex non-interactive mode](https://developers.openai.com/codex/noninteractive.md)
- [openai/codex #4152 — read-only ignored for MCP edits](https://github.com/openai/codex/issues/4152)
- [Codex agent approvals & security](https://developers.openai.com/codex/agent-approvals-security)
- [Codex MCP servers: transports & auth (bearer_token, http_headers)](https://github.com/netdata/netdata/blob/master/docs/netdata-ai/mcp/mcp-clients/codex-cli.md)
